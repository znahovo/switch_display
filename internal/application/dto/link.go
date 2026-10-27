package dto

// LinkSelfDTO 是本机在共享网络里的身份。
type LinkSelfDTO struct {
	// ID 是持久化设备 ID（32 位小写 hex）。
	ID string `json:"id"`
	// Name 是用户可见设备名。
	Name string `json:"name"`
	// Role 是 idle/server/client。
	Role string `json:"role"`
	// Fingerprint 是本机 Deskflow 证书指纹；空串表示证书尚未生成。
	Fingerprint string `json:"fingerprint"`
}

// PeerDTO 是发现到的一台对端设备。
type PeerDTO struct {
	// ID 是对端设备 ID。
	ID string `json:"id"`
	// Name 是对端设备名。
	Name string `json:"name"`
	// Address 是对端 IP（用于展示与排障）。
	Address string `json:"address"`
	// Role 是对端自报的角色。
	Role string `json:"role"`
	// Paired 表示这台设备是否已在信任列表里。
	Paired bool `json:"paired"`
	// Reachable 表示最近一次广播是否还在 TTL 内（离线对端会短暂保留）。
	Reachable bool `json:"reachable"`
}

// TrustedPeerDTO 是已配对设备。
type TrustedPeerDTO struct {
	// ID 是设备 ID。
	ID string `json:"id"`
	// Name 是配对时的设备名。
	Name string `json:"name"`
	// PairedAt 是配对时间（RFC3339）；零值时为 null。
	PairedAt string `json:"pairedAt,omitempty"`
}

// CoreStatusDTO 是 Deskflow core 的运行状态。
type CoreStatusDTO struct {
	// State 是 stopped/starting/running/failed。
	State string `json:"state"`
	// Role 是当前运行的 deskflow 角色（server/client）；未运行时为空。
	Role string `json:"role"`
	// PID 是子进程 PID。
	PID int `json:"pid"`
	// Message 是最近一条有意义的日志/错误。
	Message string `json:"message"`
	// LogTail 是最近若干行 core 输出（便于用户自查）。
	LogTail []string `json:"logTail"`
}

// LinkStateDTO 是「共享」页需要的全部状态。
type LinkStateDTO struct {
	// Self 是本机身份。
	Self LinkSelfDTO `json:"self"`
	// Peers 是当前局域网内发现到的对端（不含自己）。
	Peers []PeerDTO `json:"peers"`
	// Trusted 是已配对设备。
	Trusted []TrustedPeerDTO `json:"trusted"`
	// Core 是 core 运行状态。
	Core CoreStatusDTO `json:"core"`
	// DeskflowAvailable 表示是否找到 deskflow-core 可执行文件。
	DeskflowAvailable bool `json:"deskflowAvailable"`
	// DeskflowBinary 是找到的可执行文件路径（未找到时为空）。
	DeskflowBinary string `json:"deskflowBinary"`
	// DeskflowDir 是我们托管 Deskflow 配置的目录。
	DeskflowDir string `json:"deskflowDir"`
	// ConfigPath 是 link.json 的路径。
	ConfigPath string `json:"configPath"`
	// DiscoverPort 是发现服务使用的 UDP 端口。
	DiscoverPort int `json:"discoverPort"`
	// LastHostID 是上次成功连上的主机设备 ID；空串表示还没有。
	// 前端只用来在列表里给那台设备加一个「上次的主机」角标。
	LastHostID string `json:"lastHostId"`
	// Error 是最近一次操作的错误文案；无错误时为空。
	Error string `json:"error"`
}

// PairResultDTO 是一次配对尝试的结果。
type PairResultDTO struct {
	// OK 表示配对成功（双方 core 已按 host/client 拉起）。
	OK bool `json:"ok"`
	// Message 是给用户看的说明（失败原因或成功提示）。
	Message string `json:"message"`
}
