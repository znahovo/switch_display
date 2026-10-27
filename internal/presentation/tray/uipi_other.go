//go:build !windows

package tray

import "github.com/znahovo/switch-display/internal/application"

// setActive 记录当前托盘实例。只有 Windows 需要它：那边的
// WndProcInterceptor 是包级函数，拿不到 Tray 指针。
func setActive(*Tray) {}

// allowTrayCallbackFromShell 在非 Windows 平台是空操作：没有 UIPI，也没有
// Shell_NotifyIcon 那套回调机制。
func allowTrayCallbackFromShell(application.Logger) {}

// InterceptWindowMessage 在非 Windows 平台不拦截任何消息。main.go 里可以
// 无条件引用这个符号，不必按平台分叉装配代码。
func InterceptWindowMessage(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool) {
	return 0, false
}
