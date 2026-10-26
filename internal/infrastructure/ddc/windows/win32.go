//go:build windows

package windows

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/znahovo/switch-display/internal/domain/display"
)

// 本文件是 Win32/Dxva2 的薄封装：只负责把 DLL 调用翻译成 Go 值，
// 不含业务判断。分出来是为了让 port.go 读起来像「在做什么」而不是「在调什么」。
//
// 为什么不用 cgo：architecture.md R6 规定 import "C" 只能出现在
// infrastructure/ddc/darwin/（那是 IOKit 私有 API 唯一可行的路子）。
// 而在 Windows 上 user32/dxva2 都是稳定的系统 DLL，可以用 x/sys/windows
// 的 LazyDLL 直接按名字解析，二进制不需要 MSVC 工具链。

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modDxva2  = windows.NewLazySystemDLL("dxva2.dll")

	procEnumDisplayMonitors = modUser32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = modUser32.NewProc("GetMonitorInfoW")
	procEnumDisplayDevicesW = modUser32.NewProc("EnumDisplayDevicesW")

	procGetNumberOfPhysicalMonitors = modDxva2.NewProc("GetNumberOfPhysicalMonitorsFromHMONITOR")
	procGetPhysicalMonitors         = modDxva2.NewProc("GetPhysicalMonitorsFromHMONITOR")
	procDestroyPhysicalMonitors     = modDxva2.NewProc("DestroyPhysicalMonitors")
	procGetVCPFeature               = modDxva2.NewProc("GetVCPFeatureAndVCPFeatureReply")
	procSetVCPFeature               = modDxva2.NewProc("SetVCPFeature")
)

// eddGetDeviceInterfaceName 让 EnumDisplayDevices 返回设备接口路径
// （\\?\DISPLAY#XMI27B1#5&187333d6&0&UID20738#{e6f07b5f-...}）而不是
// 只有 MONITOR\XMI27B1\{4d36e96e-...}\0002 这种 Class 路径。
//
// 用它的原因：设备接口路径是显示器在当前机器上的稳定标识，跨重启、
// 换显卡插口都不变，正好符合 display.DisplayID 的语义。
const eddGetDeviceInterfaceName = 0x00000001

type rect struct {
	left, top, right, bottom int32
}

// monitorInfoEx 对应 MONITORINFOEXW。cbSize 必须等于 104（含 szDevice），
// 传 MONITORINFO 的大小会让 szDevice 拿不到值。
type monitorInfoEx struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
	szDevice  [32]uint16
}

// displayDevice 对应 DISPLAY_DEVICEW。
type displayDevice struct {
	cb           uint32
	deviceName   [32]uint16
	deviceString [128]uint16
	stateFlags   uint32
	deviceID     [128]uint16
	deviceKey    [128]uint16
}

// physicalMonitor 对应 PHYSICAL_MONITOR：一个句柄加一段人类可读的描述。
// 64 位下 HANDLE 是 8 字节，结构体总长 264，与 C 端一致（见 port_test.go 的布局断言）。
type physicalMonitor struct {
	handle      uintptr
	description [128]uint16
}

// monitorRef 是一台「接到桌面上的显示器」的定位信息。
//
// 两套名字各有用途，不能只留一个：
//   - gdi：\\.\DISPLAY1，枚举与定位 HMONITOR 用；
//   - id：设备接口路径，对外就是 display.DisplayID。
type monitorRef struct {
	gdi  string
	id   display.DisplayID
	hMon uintptr
}

// foundMonitor 是回调里收集到的原始信息（还没查设备接口路径）。
type foundMonitor struct {
	hMon uintptr
	gdi  string
}

var (
	// enumMu 保护下面的 sink 与 EnumDisplayMonitors 调用的原子性：
	// 回调是包级的，两次枚举不能同时往里写。
	enumMu sync.Mutex
	// enumSink 是当前这次枚举的收集地，由 enumMonitors 在持锁期间设置。
	enumSink *[]foundMonitor
	// enumCallback 只创建一次。
	//
	// 这是必须的：syscall.NewCallback 会从一张固定大小的表（2000 项）里
	// 永久占一个槽，没有释放接口。displayapp.Watcher 每 5 秒枚举一次，
	// 每次新建回调的话大约 3 小时后就会 panic "too many callback functions"。
	enumCallback = syscall.NewCallback(func(hMon, _ uintptr, _ uintptr, _ uintptr) uintptr {
		var mi monitorInfoEx
		mi.cbSize = uint32(unsafe.Sizeof(mi))
		if r, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); r == 0 {
			return 1 // 读不到信息就跳过这一台，但继续枚举
		}
		if enumSink != nil {
			*enumSink = append(*enumSink, foundMonitor{
				hMon: hMon,
				gdi:  windows.UTF16ToString(mi.szDevice[:]),
			})
		}
		return 1 // 返回 1 表示继续枚举
	})
)

// enumMonitors 枚举当前接到桌面上的显示器。
//
// 每次调用都重新枚举、不缓存句柄：显示器热插拔或分辨率切换后 HMONITOR 会失效，
// 缓存它只会引入「切一次好一次坏」（darwin 实现踩过同样的坑）。
func enumMonitors(ctx context.Context) ([]monitorRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	enumMu.Lock()
	collected := make([]foundMonitor, 0, 4)
	enumSink = &collected
	r, _, callErr := procEnumDisplayMonitors.Call(0, 0, enumCallback, 0)
	enumSink = nil
	enumMu.Unlock()

	if r == 0 {
		return nil, fmt.Errorf("EnumDisplayMonitors: %w", callErr)
	}

	out := make([]monitorRef, 0, len(collected))
	for _, f := range collected {
		out = append(out, monitorRef{gdi: f.gdi, id: interfaceID(f.gdi), hMon: f.hMon})
	}
	return out, nil
}

// interfaceID 把 GDI 设备名（\\.\DISPLAY1）翻译成显示器设备接口路径。
//
// 拿不到接口路径时退回 GDI 名：那是会话内的临时名字（重启/重排会变），
// 但比「枚举失败」好——至少本次运行内能读写。
func interfaceID(gdi string) display.DisplayID {
	name, err := windows.UTF16PtrFromString(gdi)
	if err != nil {
		return display.DisplayID(gdi)
	}
	var dd displayDevice
	dd.cb = uint32(unsafe.Sizeof(dd))
	r, _, _ := procEnumDisplayDevicesW.Call(uintptr(unsafe.Pointer(name)), 0,
		uintptr(unsafe.Pointer(&dd)), eddGetDeviceInterfaceName)
	if r == 0 {
		return display.DisplayID(gdi)
	}
	if id := windows.UTF16ToString(dd.deviceID[:]); id != "" {
		return display.DisplayID(id)
	}
	return display.DisplayID(gdi)
}

// deviceInstance 从设备接口路径推出注册表实例路径。
//
//	\\?\DISPLAY#XMI27B1#5&187333d6&0&UID20738#{e6f07b5f-...}
//	→ DISPLAY\XMI27B1\5&187333d6&0&UID20738
//
// EDID 就挂在 HKLM\SYSTEM\CurrentControlSet\Enum\<实例>\Device Parameters 下。
// gdi 名或其它形状的 ID 返回 false。
func deviceInstance(id string) (string, bool) {
	const prefix = `\\?\`
	if !strings.HasPrefix(id, prefix) {
		return "", false
	}
	rest := id[len(prefix):]
	if i := strings.Index(rest, "#{"); i >= 0 {
		rest = rest[:i]
	}
	// 至少要有 class 名和实例名两段，否则不是设备接口路径。
	parts := strings.Split(rest, "#")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return strings.Join(parts, `\`), true
}

// physicalMonitorCount 问系统这台显示器有几个物理监视器（DDC 通道）。
//
// 只读计数、不打开句柄，开销极小；虚拟显示器与笔记本内屏都会返回 0。
func physicalMonitorCount(hMon uintptr, out *uint32) bool {
	r, _, _ := procGetNumberOfPhysicalMonitors.Call(hMon, uintptr(unsafe.Pointer(out)))
	return r != 0
}

// openPhysical 打开显示器对应的物理监视器，返回可用的 DDC 句柄。
//
// 注意句柄 0 是合法值（本机 DISPLAY1 就是 0），所以调用方不能拿「句柄==0」
// 当失败判据，必须看 error。release 负责 DestroyPhysicalMonitors。
func openPhysical(hMon uintptr) (uintptr, func(), error) {
	var n uint32
	if r, _, err := procGetNumberOfPhysicalMonitors.Call(hMon, uintptr(unsafe.Pointer(&n))); r == 0 {
		return 0, nil, fmt.Errorf("GetNumberOfPhysicalMonitorsFromHMONITOR: %w", err)
	} else if n == 0 {
		// 虚拟显示器/未接线的口会走到这里：它能出现在桌面上，但没有 DDC 通道。
		return 0, nil, fmt.Errorf("%w: display exposes no physical monitor", display.ErrUnsupported)
	}

	monitors := make([]physicalMonitor, n)
	if r, _, err := procGetPhysicalMonitors.Call(hMon, uintptr(n), uintptr(unsafe.Pointer(&monitors[0]))); r == 0 {
		return 0, nil, fmt.Errorf("GetPhysicalMonitorsFromHMONITOR: %w", err)
	}
	// 只保留第一个物理监视器：一台显示器一个 DDC 通道，多出来的（比如
	// 走 DisplayPort MST 的分支）对「切输入源」没有意义。
	for i := 1; i < len(monitors); i++ {
		procDestroyPhysicalMonitors.Call(1, uintptr(unsafe.Pointer(&monitors[i])))
	}
	first := monitors[0]
	release := func() {
		procDestroyPhysicalMonitors.Call(1, uintptr(unsafe.Pointer(&first)))
	}
	return first.handle, release, nil
}

// getVCP 读一个 VCP 代码的当前值与最大值。
func getVCP(handle uintptr, code byte) (current, maximum uint32, err error) {
	var cur, max uint32
	r, _, callErr := procGetVCPFeature.Call(handle, uintptr(code),
		// 第三个参数是 MC_VCP_CODE_TYPE*，传 0 表示不关心是「设置型」还是「瞬时型」。
		0,
		uintptr(unsafe.Pointer(&cur)), uintptr(unsafe.Pointer(&max)))
	if r == 0 {
		return 0, 0, fmt.Errorf("GetVCPFeatureAndVCPFeatureReply(0x%02X): %w", code, callErr)
	}
	return cur, max, nil
}

// setVCP 写一个 VCP 代码。
func setVCP(handle uintptr, code byte, value uint16) error {
	if r, _, callErr := procSetVCPFeature.Call(handle, uintptr(code), uintptr(value)); r == 0 {
		return fmt.Errorf("SetVCPFeature(0x%02X=%d): %w", code, value, callErr)
	}
	return nil
}

// readEDID 从注册表读显示器 EDID。
//
// 为什么走注册表而不是 DDC：EDID 由系统在 PnP 枚举时采好放在
// Enum\<实例>\Device Parameters 下，读它不碰 I2C，不会和 DDC 抢总线，
// 也不会在显示器休眠时超时。Driver 没采到（比如 Default_Monitor）时返回 false。
func readEDID(id display.DisplayID) ([]byte, bool) {
	instance, ok := deviceInstance(string(id))
	if !ok {
		return nil, false
	}
	path := `SYSTEM\CurrentControlSet\Enum\` + instance + `\Device Parameters`
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return nil, false
	}
	defer key.Close()

	data, _, err := key.GetBinaryValue("EDID")
	if err != nil || len(data) < edidMinLength {
		return nil, false
	}
	return data, true
}
