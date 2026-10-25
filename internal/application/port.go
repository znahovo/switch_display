// Package application 是用例层：编排领域对象与基础设施端口。
//
// 它不含业务规则判定（那在 domain），也不含协议细节（那在 infrastructure）；
// 它的职责是「一次操作 = 一个事务边界」：读配置 → 调领域 → 存配置 → 通知。
package application

import (
	"github.com/znahovo/switch-display/internal/domain/switching"
)

// EventPublisher 是向外（前端、托盘）广播事件的端口。
type EventPublisher interface {
	// Emit 广播一个命名事件。实现方不得阻塞：事件消费慢不能拖住用例。
	Emit(name string, payload any)
}

// Binding 是一条快捷键绑定。
type Binding struct {
	Accelerator string // 形如 "Cmd+Option+1"
	// SourceID 是这条快捷键要切换到的信号源。
	SourceID string
}

// HotkeyRegistrar 是全局快捷键端口。
type HotkeyRegistrar interface {
	// RegisterAll 重新注册所有绑定。返回的错误按下标对应 bindings，
	// nil 表示该条成功——部分失败是常态（快捷键被别的 App 占用）。
	RegisterAll(bindings []Binding) []error
	// UnregisterAll 清空所有已注册快捷键。
	UnregisterAll()
}

// Clock 复用领域层的抽象，避免出现两个语义相同但类型不同的时钟接口。
type Clock = switching.Clock

// Logger 是日志端口。
type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Error(msg string, err error, kv ...any)
}

// 事件名常量。前端与托盘都按这些名字订阅，改名属于破坏性变更。
const (
	// EventSwitchStarted 在开始切换时发出，payload 为 SwitchResultDTO。
	EventSwitchStarted = "switch:started"
	// EventSwitchFinished 在切换结束时发出，payload 为 SwitchResultDTO。
	EventSwitchFinished = "switch:finished"
	// EventStateChanged 在配置或设备状态变化时发出，payload 为 StateDTO。
	EventStateChanged = "state:changed"
	// EventOpenSettings 由托盘「设置…」发出，前端收到后跳到设置页。
	EventOpenSettings = "dialogs:open-settings"
	// EventShowView 请求前端显示某个视图，payload 为视图名（"switcher"/"sources"/"settings"）。
	EventShowView = "ui:show-view"
)
