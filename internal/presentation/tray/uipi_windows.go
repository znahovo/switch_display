//go:build windows

package tray

import (
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/znahovo/switch-display/internal/application"
)

// 这一组常量的来历见文件末尾的注释。
const (
	// trayCallbackMessage 是托盘窗口接收 Shell_NotifyIcon 回调的消息号。
	// 必须和 Wails 侧一致：`systemtray_windows.go` 写死了 `wmUserSystray =
	// w32.WM_USER + 1`，它未导出，只能照抄。Wails 升级后若对不上，表现是
	// 托盘点击又没反应。
	trayCallbackMessage = 0x0400 + 1 // WM_USER + 1

	// wmContextMenu 是右键通知。
	wmContextMenu = 0x007B

	// msgFltAdd / msgFltAllow 来自 winuser.h，两个值恰好相同。
	msgFltAdd = 1

	// hwndMessage 是 HWND_MESSAGE，即 (HWND)-3。消息窗口的父窗口恒为它，
	// 用它可以把托盘窗口和真正的可见窗口区分开。
	hwndMessage = ^uintptr(0) - 2
)

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")

	procChangeWindowMessageFilter   = modUser32.NewProc("ChangeWindowMessageFilter")
	procChangeWindowMessageFilterEx = modUser32.NewProc("ChangeWindowMessageFilterEx")
	procFindWindowExW               = modUser32.NewProc("FindWindowExW")
	procGetWindowThreadProcessID    = modUser32.NewProc("GetWindowThreadProcessId")
	procGetParent                   = modUser32.NewProc("GetParent")
)

// active 是当前托盘实例，供 WndProcInterceptor（由 Wails 主消息循环调用，
// 拿不到 Tray 指针）找到它。
var active atomic.Pointer[Tray]

// setActive 记录当前托盘实例。
func setActive(t *Tray) { active.Store(t) }

// changeFilterStruct 对应 CHANGEFILTERSTRUCT。
type changeFilterStruct struct {
	cbSize    uint32
	extStatus uint32
}

// WndProcInterceptor 挂在 wails.WindowsOptions.WndProcInterceptor 上，
// 用来补上 Wails 在 Windows 上漏掉的右键通知。
//
// 为什么需要它：托盘图标用 NOTIFYICON_VERSION_4 注册时，资源管理器除了把
// WM_CONTEXTMENU 塞进 uCallbackMessage 的 lParam，还会**把它当成一条普通的
// 窗口消息**直接投给托盘窗口。Wails 的托盘 wndProc 只在 `wmUserSystray`
// 分支里处理 WM_CONTEXTMENU，漏了这条直接投递的；未命中的消息落到
// DefWindowProc，于是右键在视觉上「完全没反应」。左键不受影响，因为它只走
// uCallbackMessage 那一条路，这正是「左键好、右键死」的原因。
//
// 这里只对**消息窗口**（父窗口为 HWND_MESSAGE 的托盘窗口）生效，避免把
// 应用界面里的右键菜单也截走。
func WndProcInterceptor(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool) {
	if msg != wmContextMenu {
		return 0, false
	}
	if !isMessageWindow(hwnd) {
		return 0, false
	}
	t := active.Load()
	if t == nil {
		return 0, false
	}
	// Wails 的 ShowAt 用 CAS 防重入：万一它自己的那条路也开了菜单，这里
	// 重复调用会被静默忽略，不会弹出两个。
	t.OpenMenu()
	return 0, true
}

// InterceptWindowMessage 是给 main.go 用的平台无关入口。
func InterceptWindowMessage(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool) {
	return WndProcInterceptor(hwnd, msg, wParam, lParam)
}

// isMessageWindow 判断 hwnd 是不是消息专用窗口。
func isMessageWindow(hwnd uintptr) bool {
	parent, _, _ := procGetParent.Call(hwnd)
	return parent == hwndMessage
}

// allowTrayCallbackFromShell 放行 Shell_NotifyIcon 的回调消息。
//
// 为什么需要：本程序用 requireAdministrator 运行（DDC/CI 需要），而任务栏
// 属于普通权限的 explorer.exe。UIPI 默认禁止「低完整性进程向高完整性进程
// 发窗口消息」，此时那条 WM_USER+1 在内核层就被丢掉 —— 图标看得见、点了
// 没反应，代码里连断点都进不去。
//
// 两级都做：进程级的 ChangeWindowMessageFilter 不针对窗口，早于托盘窗口
// 创建也能覆盖；窗口级的 ChangeWindowMessageFilterEx 权限最小，但要句柄，
// 所以轮询等托盘窗口出现。拿不到句柄不算致命，只记日志。
func allowTrayCallbackFromShell(log application.Logger) {
	if err := changeWindowMessageFilter(trayCallbackMessage, msgFltAdd); err != nil {
		log.Debug("process-wide message filter not applied", "error", err)
	}

	go func() {
		for _, delay := range []time.Duration{0, 200 * time.Millisecond, time.Second, 3 * time.Second} {
			time.Sleep(delay)
			if n := allowOnMessageWindows(trayCallbackMessage); n > 0 {
				log.Debug("tray callback allowed on message window", "windows", n)
				return
			}
		}
		log.Debug("tray message window not found; relying on process-wide filter")
	}()
}

// allowOnMessageWindows 对本进程的每个消息窗口放开 msg，返回命中数量。
//
// 不能直接用 FindWindow(class, nil)：托盘窗口和主窗口共用 Wails 的
// Windows.WndClass，按类名找会先撞上主窗口，而给主窗口放行是没用的 ——
// 回调消息发给的是托盘那个消息窗口。所以按「父窗口是 HWND_MESSAGE +
// 进程 ID 相同」筛。
func allowOnMessageWindows(msg uint32) int {
	pid := uint32(windows.GetCurrentProcessId())
	var count int
	var after uintptr
	for {
		hwnd, _, _ := procFindWindowExW.Call(hwndMessage, after, 0, 0)
		if hwnd == 0 {
			break
		}
		after = hwnd
		if windowPID(hwnd) != pid {
			continue
		}
		if err := changeWindowMessageFilterEx(hwnd, msg, msgFltAdd); err != nil {
			continue
		}
		count++
	}
	return count
}

// windowPID 返回窗口所属进程 ID。
func windowPID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// changeWindowMessageFilter 调进程级的 ChangeWindowMessageFilter。
func changeWindowMessageFilter(msg uint32, action uintptr) error {
	ret, _, err := procChangeWindowMessageFilter.Call(uintptr(msg), action)
	if ret == 0 {
		return err
	}
	return nil
}

// changeWindowMessageFilterEx 调窗口级的 ChangeWindowMessageFilterEx。
func changeWindowMessageFilterEx(hwnd uintptr, msg uint32, action uint32) error {
	cf := changeFilterStruct{cbSize: uint32(unsafe.Sizeof(changeFilterStruct{}))}
	ret, _, err := procChangeWindowMessageFilterEx.Call(
		hwnd, uintptr(msg), uintptr(action), uintptr(unsafe.Pointer(&cf)),
	)
	if ret == 0 {
		return err
	}
	return nil
}
