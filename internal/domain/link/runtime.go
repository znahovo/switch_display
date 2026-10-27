package link

import "time"

// 本机共享引擎（Deskflow core）的运行状态。
//
// 这些是领域值对象而不是基础设施细节：UI 要显示「正在启动 / 运行中 /
// 启动失败」，用例层要据此决定是否允许再次启动，都与具体进程实现无关。
const (
	// StateStopped 表示没有 core 在跑。
	StateStopped = "stopped"
	// StateStarting 表示已 exec，但还没确认可用。
	StateStarting = "starting"
	// StateRunning 表示 core 进程存活（server 已监听 / client 已连上）。
	StateRunning = "running"
	// StateFailed 表示 core 启动失败或异常退出。
	StateFailed = "failed"
)

// RuntimeStatus 是一次运行状态快照。
type RuntimeStatus struct {
	// State 是上面四个之一。
	State string
	// Role 是当前运行角色（server/client）；未运行时为 RoleIdle。
	Role Role
	// PID 是子进程 PID；未运行时为 0。
	PID int
	// Message 是最近一条有意义的日志或错误，用于 UI 提示。
	Message string
	// StartedAt 是本次启动时间。
	StartedAt time.Time
}

// Running 报告 core 是否处于运行中（含正在启动）。
func (s RuntimeStatus) Running() bool {
	return s.State == StateRunning || s.State == StateStarting
}

// Port 默认值。
const (
	// CorePort 是 Deskflow core 监听的 TCP 端口。
	CorePort = 24800
	// DiscoveryPort 是发现服务的 UDP 端口。
	DiscoveryPort = 24801
)

// ServerSpec 是「本机作为主机」启动 Deskflow core 所需的全部语义输入。
//
// 放在领域层而不是基础设施层：这些是「共享关系的描述」（谁是谁的邻居、
// 信任谁），与桌面平台、文件路径无关；基础设施只负责把它翻译成
// deskflow.conf / 布局文件 / 信任库。
type ServerSpec struct {
	// SelfName 是本机在 Deskflow 里的主机名（已归一化）。
	SelfName string
	// Neighbors 是按顺序加入布局的客机主机名（已归一化）。
	Neighbors []string
	// TrustedClients 是客机证书指纹（v2:sha256:...）。
	TrustedClients []string
	// Port 是 TCP 端口。
	Port int
}

// ClientSpec 是「本机作为客机」启动 Deskflow core 所需的全部语义输入。
type ClientSpec struct {
	// SelfName 是本机在 Deskflow 里的主机名（已归一化）。
	SelfName string
	// ServerHost 是主机地址（IP 或主机名）。
	ServerHost string
	// TrustedServers 是主机证书指纹（v2:sha256:...）。
	TrustedServers []string
	// Port 是 TCP 端口。
	Port int
}
