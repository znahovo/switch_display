package linkapp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/application/dto"
	"github.com/znahovo/switch-display/internal/domain/link"
)

// 用例层错误。表现层把它们映射成稳定错误码。
var (
	// ErrNoCore 表示找不到 deskflow-core。
	ErrNoCore = errors.New("deskflow core not found")
	// ErrPeerNotFound 表示要配对的对端不在发现列表里。
	ErrPeerNotFound = errors.New("peer not found")
	// ErrBusy 表示已有共享在进行，需先停止。
	ErrBusy = errors.New("sharing already active")
	// ErrNotPaired 表示对该设备没有配对记录。
	ErrNotPaired = errors.New("peer not paired")
	// ErrInvalidName 表示设备名不合法。
	ErrInvalidName = errors.New("invalid device name")
	// ErrNoIdentity 表示本机还没有可用的证书指纹（证书生成失败）。
	ErrNoIdentity = errors.New("device certificate unavailable")
)

// Deps 是用例依赖。
type Deps struct {
	// Repo 是 link.json 仓储。
	Repo link.Repository
	// Transport 是发现与信令端口。
	Transport Transport
	// Core 是 Deskflow core 驱动端口。
	Core Core
	// Events 是事件发布端口。
	Events application.EventPublisher
	// Log 是日志端口。
	Log application.Logger
	// Clock 用于超时与时间戳（可为 nil，默认 time.Now）。
	Clock func() time.Time
	// Port 是发现服务监听的 UDP 端口（0 表示 link.DiscoveryPort）。
	// 之所以可配：同机跑测试/两个实例时能换一个端口，互不干扰。
	Port int
}

// Service 编排「发现 → 配对 → 启停 core」。
//
// 并发模型：所有可变状态由 mu 保护；网络回调与前端调用可能来自不同
// goroutine，因此每个导出方法都要加锁，且持锁期间不做 IO（先算好再放锁）。
type Service struct {
	repo   link.Repository
	trans  Transport
	core   Core
	events application.EventPublisher
	log    application.Logger
	now    func() time.Time
	port   int // 发现服务端口，0 表示 link.DiscoveryPort

	mu      sync.RWMutex
	snap    link.Snapshot
	lastErr string
}

// New 构造用例。
func New(d Deps) *Service {
	now := d.Clock
	if now == nil {
		now = time.Now
	}
	s := &Service{
		repo:   d.Repo,
		trans:  d.Transport,
		core:   d.Core,
		events: d.Events,
		log:    d.Log,
		now:    now,
		port:   d.Port,
		snap:   link.EmptySnapshot(),
	}
	if s.port <= 0 {
		s.port = link.DiscoveryPort
	}

	// 回调里不能拿写锁：它们可能从收包 goroutine 进来，而 Start 正持锁。
	s.trans.SetHandlers(s.onMessage, s.onPeerChange)
	return s
}

// Start 加载身份、生成证书、开始发现，并在 ctx 结束或 Stop 时收尾。
//
// 返回后不阻塞：发现循环在后台 goroutine 里跑。
func (s *Service) Start(ctx context.Context) error {
	snap, err := s.repo.Load(ctx)
	if err != nil {
		return fmt.Errorf("load link config: %w", err)
	}

	// 首次运行：生成设备 ID（持久化，之后不再变）。
	if snap.Settings.DeviceID == "" {
		id, err := link.NewDeviceID()
		if err != nil {
			return fmt.Errorf("generate device id: %w", err)
		}
		snap.Settings.DeviceID = id
	}
	if snap.Settings.DeviceName == "" {
		snap.Settings.DeviceName = defaultDeviceName()
	}

	// 证书指纹：core 必须有一份可用证书才能开 TLS。
	fp, regenerated, certErr := s.core.EnsureIdentity()
	if certErr != nil {
		// 证书失败不该阻断发现：用户还能看到对方，只是不能配对。
		s.log.Error("ensure deskflow certificate", certErr)
		snap.Trusted = nil
	} else {
		if regenerated {
			// 新证书意味着旧指纹全部失效，已配对记录必须清空，
			// 否则对端会因指纹不匹配而拒绝连接。
			s.log.Info("deskflow certificate regenerated, clearing trusted peers")
			snap.Trusted = nil
		}
	}

	if err := s.repo.Save(ctx, snap); err != nil {
		return fmt.Errorf("save link config: %w", err)
	}

	self := link.Self{
		ID:          snap.Settings.DeviceID,
		Name:        snap.Settings.DeviceName,
		Role:        link.RoleIdle,
		Fingerprint: fp,
		Port:        link.CorePort,
	}
	s.trans.SetSelf(self)

	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()

	if err := s.trans.Listen(s.port); err != nil {
		return fmt.Errorf("start discovery: %w", err)
	}

	go s.trans.Run(ctx)
	s.log.Info("link service started", "device", self.Name, "id", self.ID, "port", link.DiscoveryPort)
	return nil
}

// Shutdown 停止共享与发现（应用退出时调用）。
func (s *Service) Shutdown(ctx context.Context) {
	_ = s.Stop(ctx)
	_ = s.trans.Close()
}

// State 返回共享页所需的完整状态。
func (s *Service) State() dto.LinkStateDTO {
	s.mu.RLock()
	snap := s.snap
	lastErr := s.lastErr
	s.mu.RUnlock()

	selfInfo := s.trans.Self()
	status := s.core.Status()
	dir := s.core.Dir()
	binary, binErr := s.core.Locate()

	out := dto.LinkStateDTO{
		Self: dto.LinkSelfDTO{
			ID:          snap.Settings.DeviceID,
			Name:        snap.Settings.DeviceName,
			Role:        string(selfInfo.Role),
			Fingerprint: selfInfo.Fingerprint,
		},
		DeskflowAvailable: binErr == nil,
		DeskflowBinary:    binary,
		DeskflowDir:       dir,
		ConfigPath:        s.repo.Path(),
		DiscoverPort:      s.port,
		LastHostID:        snap.Settings.LastHostID,
		Error:             lastErr,
		Core: dto.CoreStatusDTO{
			State:   status.State,
			Role:    string(status.Role),
			PID:     status.PID,
			Message: status.Message,
			LogTail: s.core.LogTail(),
		},
		Peers:   make([]dto.PeerDTO, 0, 8),
		Trusted: make([]dto.TrustedPeerDTO, 0, len(snap.Trusted)),
	}

	now := s.now()
	for _, p := range s.trans.Peers() {
		_, paired := snap.FindTrusted(p.ID)
		out.Peers = append(out.Peers, dto.PeerDTO{
			ID:        p.ID,
			Name:      p.Name,
			Address:   p.Addr.Addr().String(),
			Role:      string(p.Role),
			Paired:    paired,
			Reachable: !p.Stale(now, 10*time.Second),
		})
	}
	sort.Slice(out.Peers, func(i, j int) bool { return out.Peers[i].Name < out.Peers[j].Name })

	for _, t := range snap.Trusted {
		item := dto.TrustedPeerDTO{ID: t.ID, Name: t.Name}
		if !t.PairedAt.IsZero() {
			item.PairedAt = t.PairedAt.UTC().Format(time.RFC3339)
		}
		out.Trusted = append(out.Trusted, item)
	}
	sort.Slice(out.Trusted, func(i, j int) bool { return out.Trusted[i].Name < out.Trusted[j].Name })

	return out
}

// SetDeviceName 修改本机展示名并广播。
func (s *Service) SetDeviceName(ctx context.Context, name string) error {
	clean := link.CleanDeviceName(name)
	if clean == "" {
		return ErrInvalidName
	}

	s.mu.Lock()
	snap := s.snap
	snap.Settings.DeviceName = clean
	s.snap = snap
	s.lastErr = ""
	self := s.trans.Self()
	self.Name = clean
	s.trans.SetSelf(self)
	s.mu.Unlock()

	if err := s.repo.Save(ctx, snap); err != nil {
		return fmt.Errorf("save link config: %w", err)
	}
	s.emitChanged()
	return nil
}

// HostWith 让本机成为主机，指定对端作为客机。
//
// 这是 A1/A2 的核心动作：用户在本机点「用这台作为主机」，本机变 server，
// 对端自动变 client。
func (s *Service) HostWith(ctx context.Context, peerID string) (dto.PairResultDTO, error) {
	return s.requestPair(peerID, link.RoleServer)
}

// JoinHost 让本机成为客机，指定对端作为主机。
//
// 与 HostWith 对称，只是本机想担任的角色不同。
func (s *Service) JoinHost(ctx context.Context, peerID string) (dto.PairResultDTO, error) {
	return s.requestPair(peerID, link.RoleClient)
}

// requestPair 向对端发出配对邀请，want 是本机想担任的角色。
//
// HostWith / JoinHost 只差这一个参数，合并成一处：
// 两边的前置校验（对端在线、证书就绪、core 空闲）必须永远一致，
// 各写一份迟早会漂移成「一个按钮能点、另一个不能」。
func (s *Service) requestPair(peerID string, want link.Role) (dto.PairResultDTO, error) {
	peer, ok := s.trans.Peer(peerID)
	if !ok {
		return dto.PairResultDTO{}, ErrPeerNotFound
	}
	if _, err := s.core.Locate(); err != nil {
		return dto.PairResultDTO{}, ErrNoCore
	}
	self := s.trans.Self()
	if self.Fingerprint == "" {
		return dto.PairResultDTO{}, ErrNoIdentity
	}
	if s.core.Status().Running() {
		return dto.PairResultDTO{}, ErrBusy
	}

	msg := link.Message{
		V:           link.MessageVersion,
		Type:        link.TypePairRequest,
		ID:          self.ID,
		Name:        self.Name,
		Role:        want,
		Fingerprint: self.Fingerprint,
		Port:        link.CorePort,
		To:          peer.ID,
	}
	// 用广播而不是单播：信令包很小、只在用户点击时发一次，而单播依赖
	// 「我看到的对端地址能直达它」——多网卡/VPN/同机两实例时这个地址
	// 常常不通（SO_REUSEPORT 会把单播只投给其中一个 socket）。广播在
	// 同一个广播域内一定到达，报文里的 To 字段保证只有目标会响应。
	if err := s.trans.Broadcast(msg); err != nil {
		s.setError(err)
		return dto.PairResultDTO{}, fmt.Errorf("send pair request: %w", err)
	}
	s.log.Info("pair request sent", "peer", peer.Name, "want", string(want))

	// Message 只给稳定的英文/中性标识，具体文案由前端 i18n 渲染
	// （与 switchapp 的 messageFor 同一约定）。
	notice := "pair_invite_sent"
	if want == link.RoleClient {
		notice = "pair_join_sent"
	}
	return dto.PairResultDTO{OK: true, Message: notice}, nil
}

// Stop 停止共享：广播 bye、停 core、角色回 idle。
func (s *Service) Stop(ctx context.Context) error {
	self := s.trans.Self()

	if err := s.core.Stop(ctx); err != nil {
		s.log.Error("stop deskflow core", err)
	}

	self.Role = link.RoleIdle
	s.trans.SetSelf(self)

	// 通知对端「我不再共享了」，让它们立刻把状态刷成 idle，
	// 而不是等 TTL 过期。
	_ = s.trans.Broadcast(link.Message{
		V:    link.MessageVersion,
		Type: link.TypeBye,
		ID:   self.ID,
	})

	s.log.Info("link sharing stopped")
	s.emitChanged()
	return nil
}

// Unpair 删除一条配对记录；若当前正与该设备共享则先停止。
func (s *Service) Unpair(ctx context.Context, peerID string) error {
	s.mu.Lock()
	snap := s.snap
	if _, ok := snap.FindTrusted(peerID); !ok {
		s.mu.Unlock()
		return ErrNotPaired
	}
	kept := make([]link.TrustedPeer, 0, len(snap.Trusted))
	for _, t := range snap.Trusted {
		if t.ID != peerID {
			kept = append(kept, t)
		}
	}
	snap.Trusted = kept
	if snap.Settings.LastHostID == peerID {
		snap.Settings.LastHostID = ""
	}
	s.snap = snap
	s.mu.Unlock()

	if err := s.core.Stop(ctx); err != nil {
		s.log.Error("stop deskflow core", err)
	}
	if err := s.repo.Save(ctx, snap); err != nil {
		return fmt.Errorf("save link config: %w", err)
	}
	s.emitChanged()
	return nil
}

// RefreshDeskflow 清空可执行文件查找缓存（用户刚安装完 Deskflow）。
func (s *Service) RefreshDeskflow() bool {
	s.core.ResetFinder()
	_, err := s.core.Locate()
	s.emitChanged()
	return err == nil
}

// onMessage 处理收到的信令报文。运行在收包 goroutine，不能阻塞太久。
func (s *Service) onMessage(m link.Message, from netip.AddrPort) {
	// 信令走广播（见 requestPair 的注释），所以每条都要先确认是不是给自己。
	// announce/bye 不需要 To（前者是给所有人看的，后者是「我走了」的通告）。
	if m.Type != link.TypeAnnounce && m.Type != link.TypeBye {
		if self := s.trans.Self(); m.To != "" && m.To != self.ID {
			return
		}
	}
	switch m.Type {
	case link.TypePairRequest:
		s.handlePairRequest(m, from)
	case link.TypePairAccept:
		s.handlePairAccept(m, from)
	case link.TypePairReject:
		s.log.Info("pair rejected", "peer", m.Name, "reason", m.Reason)
		// 只传「标识 + 对方给的原因码」，具体措辞由前端 i18n 拼。
		reason := m.Reason
		if reason == "" {
			reason = ReasonUnknown
		}
		s.setError(fmt.Errorf("pair_rejected:%s", reason))
	case link.TypeBye:
		// 对端停止共享：如果本机是它的客机，本机也要停（否则会对着
		// 一个已经不在的主机空转重连）。
		if status := s.core.Status(); status.Role == link.RoleClient {
			s.log.Info("host stopped sharing, stopping client", "peer", m.Name)
			_ = s.core.Stop(context.Background())
			self := s.trans.Self()
			self.Role = link.RoleIdle
			s.trans.SetSelf(self)
			s.emitChanged()
		}
	}
}

// handlePairRequest 处理配对邀请。
//
// 角色规则：报文里的 Role 是对方想担任的角色，本机担任另一个。
// 若本机已有 core 在跑，直接拒绝（不做抢占）。
func (s *Service) handlePairRequest(m link.Message, from netip.AddrPort) {
	// reason 用稳定标识而不是自然语言：这段文字会经 DTO 显示在对方
	// 界面上，而对方的界面语言未必和我一样（见 reject 的注释）。
	if m.Fingerprint == "" || !link.ValidFingerprint(m.Fingerprint) {
		s.reject(m, from, ReasonFingerprintInvalid)
		return
	}
	if s.core.Status().Running() {
		s.reject(m, from, ReasonBusy)
		return
	}

	myRole := oppositeRole(m.Role)

	// 先回 accept：对端要拿到本机指纹才能写信任库并启动 core。
	self := s.trans.Self()
	accept := link.Message{
		V:           link.MessageVersion,
		Type:        link.TypePairAccept,
		ID:          self.ID,
		Name:        self.Name,
		Role:        myRole,
		Fingerprint: self.Fingerprint,
		Port:        link.CorePort,
		To:          m.ID,
	}
	if err := s.trans.Broadcast(accept); err != nil {
		s.log.Error("send pair accept", err)
		return
	}

	// 用来源地址而不是报文里自报的地址：来源地址由内核填写，不会被伪造。
	s.adoptRole(myRole, m.ID, m.Name, m.Fingerprint, from.Addr().String())
}

// handlePairAccept 处理对方的接受：本机按之前请求的角色启动 core。
func (s *Service) handlePairAccept(m link.Message, from netip.AddrPort) {
	if m.Fingerprint == "" || !link.ValidFingerprint(m.Fingerprint) {
		s.setError(errors.New("对方返回的证书指纹无效"))
		return
	}
	if s.core.Status().Running() {
		return
	}
	s.adoptRole(oppositeRole(m.Role), m.ID, m.Name, m.Fingerprint, from.Addr().String())
}

// oppositeRole 返回对方担任 server/client 时本机该担任的角色。
func oppositeRole(peerRole link.Role) link.Role {
	if peerRole == link.RoleClient {
		return link.RoleServer
	}
	return link.RoleClient
}

// adoptRole 落定本机角色：记信任、起 core，失败则回滚成 idle。
//
// 两条握手路径（收到请求 / 收到接受）都要做同一串动作，
// 合并后「失败回滚」只需要维护一份。
func (s *Service) adoptRole(role link.Role, peerID, peerName, peerFP, host string) {
	if err := s.rememberTrusted(peerID, peerName, peerFP); err != nil {
		s.log.Error("remember trusted peer", err)
	}

	// 记住「上次连过的主机」：下次点一下就能直接回到同一台，
	// 不用在列表里再找一遍。只有当客机时才更新。
	if role == link.RoleClient {
		s.mu.Lock()
		snap := s.snap
		changed := snap.Settings.LastHostID != peerID
		snap.Settings.LastHostID = peerID
		s.snap = snap
		s.mu.Unlock()
		if changed {
			if err := s.repo.Save(context.Background(), snap); err != nil {
				s.log.Error("save last host", err)
			}
		}
	}

	self := s.trans.Self()
	self.Role = role
	s.trans.SetSelf(self)

	ctx := context.Background()
	var err error
	if role == link.RoleServer {
		err = s.startServer(ctx, peerID, peerFP)
	} else {
		err = s.startClient(ctx, host, peerFP)
	}
	if err != nil {
		s.log.Error("start deskflow core after pair", err)
		s.setError(err)
		self.Role = link.RoleIdle
		s.trans.SetSelf(self)
	}
	s.emitChanged()
}

// startServer 写信任库与布局并启动 server core。
func (s *Service) startServer(ctx context.Context, clientID, clientFP string) error {
	s.mu.RLock()
	selfID := s.snap.Settings.DeviceID
	s.mu.RUnlock()

	spec := link.ServerSpec{
		SelfName:       link.ComputerNameFor(selfID),
		Neighbors:      []string{link.ComputerNameFor(clientID)},
		TrustedClients: []string{clientFP},
		Port:           link.CorePort,
	}
	if err := s.core.StartServer(ctx, spec); err != nil {
		return fmt.Errorf("start deskflow server: %w", err)
	}
	s.mu.Lock()
	s.lastErr = ""
	s.mu.Unlock()
	s.log.Info("deskflow server started", "client", link.ComputerNameFor(clientID))
	return nil
}

// startClient 写信任库并启动 client core。
func (s *Service) startClient(ctx context.Context, host, serverFP string) error {
	s.mu.RLock()
	selfID := s.snap.Settings.DeviceID
	s.mu.RUnlock()

	spec := link.ClientSpec{
		SelfName:       link.ComputerNameFor(selfID),
		ServerHost:     host,
		TrustedServers: []string{serverFP},
		Port:           link.CorePort,
	}
	if err := s.core.StartClient(ctx, spec); err != nil {
		return fmt.Errorf("start deskflow client: %w", err)
	}
	s.mu.Lock()
	s.lastErr = ""
	s.mu.Unlock()
	s.log.Info("deskflow client started", "host", host)
	return nil
}

// rememberTrusted 把一条配对记录写进 link.json（幂等）。
func (s *Service) rememberTrusted(id, name, fingerprint string) error {
	s.mu.Lock()
	snap := s.snap
	entry := link.TrustedPeer{
		ID:          id,
		Name:        link.CleanDeviceName(name),
		Fingerprint: fingerprint,
		PairedAt:    s.now(),
	}
	replaced := false
	for i := range snap.Trusted {
		if snap.Trusted[i].ID == id {
			snap.Trusted[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		snap.Trusted = append(snap.Trusted, entry)
	}
	s.snap = snap
	s.mu.Unlock()

	if err := s.repo.Save(context.Background(), snap); err != nil {
		return fmt.Errorf("save link config: %w", err)
	}
	return nil
}

// reject 回一条 pair_reject。
func (s *Service) reject(m link.Message, _ netip.AddrPort, reason string) {
	self := s.trans.Self()
	_ = s.trans.Broadcast(link.Message{
		V:      link.MessageVersion,
		Type:   link.TypePairReject,
		ID:     self.ID,
		To:     m.ID,
		Reason: reason,
	})
	s.log.Info("pair request rejected", "peer", m.Name, "reason", reason)
}

// onPeerChange 在对端表变化时刷新前端。
func (s *Service) onPeerChange(_ link.Peer) {
	s.emitChanged()
}

// setError 记录最近一次错误（会出现在 State().Error 里）。
func (s *Service) setError(err error) {
	s.mu.Lock()
	if err != nil {
		s.lastErr = err.Error()
	}
	s.mu.Unlock()
	s.emitChanged()
}

// emitChanged 通知前端重取状态。
func (s *Service) emitChanged() {
	if s.events == nil {
		return
	}
	s.events.Emit(application.EventLinkChanged, nil)
}

// defaultDeviceName 返回首次运行时的默认设备名。
//
// 用主机名而不是「Switch Display 设备」：用户在对方列表里看到的是
// 「nnn-macbook」这类一眼能认出的名字。
func defaultDeviceName() string {
	host, err := os.Hostname()
	if err != nil {
		return "Switch Display"
	}
	return link.CleanDeviceName(host)
}
