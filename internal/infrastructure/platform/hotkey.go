package platform

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	appcore "github.com/znahovo/switch-display/internal/application"
)

// Hotkeys 把 application.HotkeyRegistrar 端口转发到 Wails 全局快捷键管理器。
//
// Wails 的 Register 只接受「一个 accel + 一个回调」，而端口是批量注册，
// 所以这里记住 accelerator → callback，避免重复注册时泄漏旧闭包。
type Hotkeys struct {
	mu       sync.Mutex
	app      *application.App
	handlers map[string]func() // accelerator → 已注册的回调
	fire     func(sourceID string)
}

// NewHotkeys 构造快捷键注册器。
func NewHotkeys(app *application.App) *Hotkeys {
	return &Hotkeys{app: app, handlers: make(map[string]func())}
}

// RegisterAll 实现 appcore.HotkeyRegistrar。
//
// 返回的切片与 bindings 等长，nil 表示成功。部分失败是常态（组合键被别的
// App 占用），调用方据此生成 warning 而不是整体失败（ADR-007）。
func (h *Hotkeys) RegisterAll(bindings []appcore.Binding) []error {
	h.mu.Lock()
	defer h.mu.Unlock()

	errs := make([]error, len(bindings))
	if h.app == nil || h.app.GlobalShortcut == nil {
		for i, b := range bindings {
			errs[i] = errUnsupportedShortcut(b.Accelerator)
		}
		return errs
	}

	for i, b := range bindings {
		if b.Accelerator == "" {
			continue
		}
		sourceID := b.SourceID // 闭包捕获局部副本，否则拿到的都是最后一个
		fn := func() { h.onFire(sourceID) }
		if err := h.app.GlobalShortcut.Register(b.Accelerator, fn); err != nil {
			errs[i] = err
			continue
		}
		h.handlers[b.Accelerator] = fn
	}
	return errs
}

// UnregisterAll 实现 appcore.HotkeyRegistrar。
func (h *Hotkeys) UnregisterAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.app != nil && h.app.GlobalShortcut != nil {
		for accel := range h.handlers {
			_ = h.app.GlobalShortcut.Unregister(accel)
		}
	}
	h.handlers = make(map[string]func())
}

// OnFire 设置快捷键触发时的处理函数。
//
// 用「注册回调」而不是让 Hotkeys 直接依赖切换用例：快捷键是基础设施，
// 它不该知道按下去之后发生什么（R3：infrastructure 不 import 用例包）。
func (h *Hotkeys) OnFire(fn func(sourceID string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fire = fn
}

func (h *Hotkeys) onFire(sourceID string) {
	h.mu.Lock()
	fn := h.fire
	h.mu.Unlock()
	if fn != nil {
		fn(sourceID)
	}
}
