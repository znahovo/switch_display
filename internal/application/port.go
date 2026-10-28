// Package application 是用例层：编排领域对象与基础设施端口。
//
// 它不含业务规则判定（那在 domain），也不含协议细节（那在 infrastructure）；
// 它的职责是「一次操作 = 一个事务边界」：读配置 → 调领域 → 存配置 → 通知。
package application

import (
	"time"

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
	// EventShowView 请求前端显示某个视图，payload 为视图名（"switcher"/"sources"/"share"/"settings"）。
	EventShowView = "ui:show-view"
	// EventLinkChanged 在共享状态、对端列表或 core 状态变化时发出，
	// payload 为 nil（前端收到后重新拉 LinkService.State）。
	EventLinkChanged = "link:changed"
	// EventLogAppended 在有新日志时发出，payload 为 nil。
	//
	// 事件是合批的（见 infrastructure/logging.NotifyInterval），
	// 前端收到后按游标增量拉，而不是整表重取。
	EventLogAppended = "log:appended"
)

// LogEntry 是一条已落地的日志。
//
// 放在 application 而不是 infrastructure：用例层要按游标把它读出来给前端，
// 而用例层不许 import 基础设施包（R2），只能反过来。
type LogEntry struct {
	// Seq 是单调递增序号，前端按它做增量拉取。
	Seq uint64
	// Time 是写入时间。
	Time time.Time
	// Level 是 debug/info/warn/error/raw。
	Level string
	// Source 标识来源（app/deskflow/wails…）。
	Source string
	// Message 是日志正文。
	Message string
	// Fields 是 kv 的渲染结果，形如 `pid=123 mode=server`。
	Fields string
}

// LogBuffer 是日志环形缓冲端口。
type LogBuffer interface {
	// Since 返回序号大于 after 的条目（最多 limit 条）。
	Since(after uint64, limit int) []LogEntry
	// Latest 返回当前最大序号。
	Latest() uint64
	// Oldest 返回缓冲里还剩的最小序号（空时返回 0）。
	Oldest() uint64
	// Dropped 返回因容量被丢弃的条数。
	Dropped() int
	// Capacity 返回缓冲容量。
	Capacity() int
	// FilePath 返回落盘路径（空表示不落盘）。
	FilePath() string
	// FileError 返回落盘错误说明（空表示正常）。
	FileError() string
	// Clear 清空内存缓冲。
	Clear()
}

// LogSink 是外部进程原始输出的落地端口。
//
// 定义在这里而不是 logging 包：deskflow 只知道「有一行原始输出要交代出去」，
// 不该知道它最终进了环形缓冲还是文件（R8）。
type LogSink interface {
	// Raw 记录一行原始输出。source 用于区分来源（如 "deskflow"）。
	Raw(source, line string)
}
