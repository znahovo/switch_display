package deskflow

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/znahovo/switch-display/internal/domain/link"
)

// Engine 把「共享关系的描述」翻译成 Deskflow 需要的三个文件，并驱动 core
// 进程。它是 linkapp.Core 端口的实现。
//
// 之所以把「写文件」与「跑进程」放在一起：两者必须原子地一致——配置写完
// 但进程没起，或进程在跑但配置指向旧角色，都会让 Deskflow 行为错乱。
type Engine struct {
	dir    string
	finder *Finder
	log    Logger

	runtime *Runtime
	port    int
}

// EngineDeps 是 Engine 的构造参数。
type EngineDeps struct {
	// Dir 是托管 Deskflow 文件的目录（见 persistence.DefaultDeskflowDir）。
	Dir string
	// BinaryPath 是用户显式指定的 core 路径；空表示自动查找。
	BinaryPath string
	// Port 是 TCP 端口；0 表示 link.CorePort。
	Port int
	// Log 是日志端口。
	Log Logger
}

// NewEngine 构造 Engine。此时还不校验可执行文件是否存在（首次打开共享页
// 才需要知道，且用户可能刚装完 Deskflow）。
func NewEngine(d EngineDeps) *Engine {
	if d.Port == 0 {
		d.Port = link.CorePort
	}
	return &Engine{
		dir:    d.Dir,
		finder: NewFinder(d.BinaryPath),
		log:    d.Log,
		port:   d.Port,
	}
}

// Locate 返回 core 可执行文件路径（带缓存）。
func (e *Engine) Locate() (string, error) { return e.finder.Find() }

// ResetFinder 让下次 Locate 重新扫描（用户刚装了 Deskflow）。
func (e *Engine) ResetFinder() { e.finder.Reset() }

// Dir 返回托管目录。
func (e *Engine) Dir() string { return e.dir }

// EnsureIdentity 确保证书存在，返回本机指纹。
//
// 返回值 regenerated=true 表示证书是新生成的（旧指纹失效，用例层需要
// 清空已配对列表）。
func (e *Engine) EnsureIdentity() (fingerprint string, regenerated bool, err error) {
	return EnsureCert(e.dir)
}

// StartServer 以主机身份启动 core。
func (e *Engine) StartServer(ctx context.Context, spec link.ServerSpec) error {
	binary, err := e.finder.Find()
	if err != nil {
		return err
	}
	if e.port != 0 {
		spec.Port = e.port
	}
	if spec.Port == 0 {
		spec.Port = link.CorePort
	}
	if spec.SelfName == "" {
		return errors.New("empty self name")
	}

	paths := Paths(e.dir)

	// 1) 布局：本机在左，客机依次排到右侧，双向声明。
	// 没有客机时写成空 links 段（Deskflow 允许，见 WriteServerLayout）。
	neighbors := cleanComputerNames(spec.Neighbors)
	self := CleanComputerName(spec.SelfName)
	links := make([]LayoutLink, 0, len(neighbors)*2)
	prev := self
	for _, n := range neighbors {
		if n == prev {
			continue
		}
		links = append(links,
			LayoutLink{From: prev, Side: "right", To: n},
			LayoutLink{From: n, Side: "left", To: prev},
		)
		prev = n
	}
	if err := WriteServerLayout(paths.ServerConfig, links); err != nil {
		return fmt.Errorf("write server layout: %w", err)
	}

	// 2) 信任库：只信任这次配对的客机。
	if err := WriteTrustedClients(paths.TLS.TrustedClients, spec.TrustedClients); err != nil {
		return fmt.Errorf("write trusted clients: %w", err)
	}

	// 3) core 配置：computer_* 段必须包含布局里出现的所有主机名。
	computers := append([]string{spec.SelfName}, neighbors...)
	if err := WriteCoreConfig(CoreConfig{
		Mode:             ModeServer,
		ComputerName:     spec.SelfName,
		Port:             spec.Port,
		SettingsPath:     paths.Settings,
		ServerConfigPath: paths.ServerConfig,
		CertPath:         paths.TLS.Cert,
		Computers:        computers,
	}); err != nil {
		return fmt.Errorf("write core config: %w", err)
	}

	e.ensureRuntime(binary)
	return e.runtime.Start(ctx, ModeServer, paths.Settings, spec.Port)
}

// StartClient 以客机身份启动 core。
func (e *Engine) StartClient(ctx context.Context, spec link.ClientSpec) error {
	binary, err := e.finder.Find()
	if err != nil {
		return err
	}
	if e.port != 0 {
		spec.Port = e.port
	}
	if spec.Port == 0 {
		spec.Port = link.CorePort
	}
	if spec.ServerHost == "" {
		return errors.New("empty server host")
	}

	paths := Paths(e.dir)

	if err := WriteTrustedServers(paths.TLS.TrustedServers, spec.TrustedServers); err != nil {
		return fmt.Errorf("write trusted servers: %w", err)
	}

	if err := WriteCoreConfig(CoreConfig{
		Mode:         ModeClient,
		ComputerName: spec.SelfName,
		Port:         spec.Port,
		// 主机可能换 IP，把端口一起带上更容易被 Deskflow 解析；
		// 若对端地址是裸 IP，Deskflow 会用 [core] port 作默认端口。
		RemoteHost:   spec.ServerHost,
		SettingsPath: paths.Settings,
		CertPath:     paths.TLS.Cert,
	}); err != nil {
		return fmt.Errorf("write core config: %w", err)
	}

	e.ensureRuntime(binary)
	return e.runtime.Start(ctx, ModeClient, paths.Settings, spec.Port)
}

// Stop 停止 core。
func (e *Engine) Stop(ctx context.Context) error {
	if e.runtime == nil {
		return nil
	}
	return e.runtime.Stop(ctx)
}

// Status 返回 core 状态。
func (e *Engine) Status() link.RuntimeStatus {
	if e.runtime == nil {
		return link.RuntimeStatus{State: link.StateStopped, Role: link.RoleIdle}
	}
	return e.runtime.Status()
}

// LogTail 返回最近的核心日志。
func (e *Engine) LogTail() []string {
	if e.runtime == nil {
		return nil
	}
	return e.runtime.LogTail()
}

// ensureRuntime 惰性创建 Runtime（首次启动时才需要）。
func (e *Engine) ensureRuntime(binary string) {
	if e.runtime != nil {
		return
	}
	e.runtime = NewRuntime(binary, e.log)
}

// HostFor 从发现的来源地址里取出可用于 client 连接的 IP。
//
// 用来源地址而不是对端自报的 IP：来源地址是内核填的，不会被伪造，
// 且在有多个网卡的机器上一定是对端实际可达的那个。
func HostFor(addr netip.AddrPort) string {
	if !addr.IsValid() {
		return ""
	}
	return addr.Addr().String()
}
