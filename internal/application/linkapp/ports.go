// Package linkapp 是「局域网共享鼠标键盘」用例层：发现、配对、选主机、启停 core。
//
// 它不含协议报文细节（在 infrastructure/link/discovery）、不含 Deskflow 配置
// 语法（在 infrastructure/link/deskflow），只负责编排与状态判断。
package linkapp

import (
	"context"
	"net/netip"

	"github.com/znahovo/switch-display/internal/domain/link"
)

// 配对拒绝原因码。这些字符串会穿过网络到达对端，所以必须是稳定标识
// 而不是自然语言——对端的界面语言未必和我们一样。
const (
	// ReasonFingerprintInvalid 表示请求方没有携带合法的证书指纹。
	ReasonFingerprintInvalid = "fingerprint_invalid"
	// ReasonBusy 表示本机已经在共享中，不能同时再接受一次配对。
	ReasonBusy = "busy"
	// ReasonUnknown 表示对端没给原因（旧版本或异常报文）。
	ReasonUnknown = "unknown"
)

// Transport 是发现与信令端口（实现见 infrastructure/link/discovery）。
//
// 接口定义在使用方（R8）：discovery 包不 import 本包。
type Transport interface {
	// SetSelf 更新本机广播内容。
	SetSelf(s link.Self)
	// Self 返回当前广播身份。
	Self() link.Self
	// Peers 返回当前对端列表。
	Peers() []link.Peer
	// Peer 按 ID 查找对端。
	Peer(id string) (link.Peer, bool)
	// Send 单播一条报文。
	Send(m link.Message, to netip.AddrPort) error
	// Broadcast 广播一条报文。
	Broadcast(m link.Message) error
	// Listen 绑定端口；负数表示由内核分配（测试用）。
	Listen(port int) error
	// Run 开始收包与周期广播，阻塞到 ctx 结束。
	Run(ctx context.Context)
	// Close 关闭 socket。
	Close() error
	// SetHandlers 注入回调，必须在 Run 之前调用。
	SetHandlers(onMessage func(m link.Message, from netip.AddrPort), onPeer func(p link.Peer))
}

// Core 是 Deskflow core 的驱动端口（实现见 infrastructure/link/deskflow）。
type Core interface {
	// Locate 返回可执行文件路径。
	Locate() (string, error)
	// ResetFinder 清空可执行文件查找缓存。
	ResetFinder()
	// Dir 返回托管配置目录。
	Dir() string
	// EnsureIdentity 确保证书存在并返回指纹；regenerated 表示新生成。
	EnsureIdentity() (fingerprint string, regenerated bool, err error)
	// StartServer 以主机身份启动 core。
	StartServer(ctx context.Context, spec link.ServerSpec) error
	// StartClient 以客机身份启动 core。
	StartClient(ctx context.Context, spec link.ClientSpec) error
	// Stop 停止 core。
	Stop(ctx context.Context) error
	// Status 返回 core 状态。
	Status() link.RuntimeStatus
	// LogTail 返回最近的核心日志。
	LogTail() []string
}
