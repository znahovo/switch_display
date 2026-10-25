package platform

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Events 把 application.EventPublisher 端口转发到 Wails 事件管理器。
type Events struct {
	app *application.App
}

// NewEvents 构造事件发布器。
func NewEvents(app *application.App) *Events { return &Events{app: app} }

// Emit 实现 application.EventPublisher。
//
// payload 为 nil 时只发「有变化」的信号，不带数据：这样前端拿不到
// 半新半旧的状态，只能通过重新拉取 State 获得一致视图。
func (e *Events) Emit(name string, payload any) {
	if e.app == nil || e.app.Event == nil {
		return
	}
	if payload == nil {
		e.app.Event.Emit(name)
		return
	}
	e.app.Event.Emit(name, payload)
}
