// Package discovery 实现局域网自发现：UDP 广播 + 单播信令 + 对端表。
//
// 为什么自研而不是用 mDNS：Deskflow 本身没有任何发现机制（见
// docs/deskflow-integration.md §2.1），而引入 zeroconf 依赖只为两台机器
// 互相看见一次并不划算——一个 UDP 广播端口就够了，且行为完全可控。
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/znahovo/switch-display/internal/domain/link"
)

// 默认参数。
const (
	// DefaultPort 是发现服务的 UDP 端口。与 Deskflow 的 24800/TCP 错开，
	// 避免与 core 自己的监听混淆。
	DefaultPort = 24801
	// DefaultAnnounceInterval 是广播间隔。2 秒是「列表感觉是实时的」
	// 与「不刷爆局域网」之间的折中。
	DefaultAnnounceInterval = 2 * time.Second
	// DefaultPeerTTL 是对端过期时间。取 3 个广播周期，丢一两个包不会被误删。
	DefaultPeerTTL = 7 * time.Second
	// maxPacketSize 是单包上限（防止恶意大包）。
	maxPacketSize = 4 * 1024
)

// ErrClosed 表示 Transport 已关闭。
var ErrClosed = errors.New("discovery closed")

// Logger 是发现层需要的日志端口。
type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Error(msg string, err error, kv ...any)
}

// Transport 是 UDP 发现服务。
//
// 同一台机器上可能同时跑多个实例（开发调试 + 正式版），所以监听时
// 打开 SO_REUSEADDR/SO_REUSEPORT，让多个 socket 都能收到广播。
type Transport struct {
	log Logger
	// port 是发现服务的 UDP 端口（广播与单播都用它）。
	port int

	mu     sync.RWMutex
	self   link.Self
	peers  map[string]link.Peer
	conn   *net.UDPConn
	closed bool
	cancel context.CancelFunc

	// onMessage 处理收到的信令（pair_request / pair_accept / ...）。
	// 由用例层注入，避免 discovery 知道配对语义。
	onMessage func(m link.Message, from netip.AddrPort)

	// onPeer 在对端表发生变化（新增/更新/过期移除）时被调用。
	onPeer func(p link.Peer)
}

// Deps 是 Transport 的构造参数。
type Deps struct {
	// Port 是监听端口；0 表示用 DefaultPort。
	Port int
	// Self 是本机广播身份。
	Self link.Self
	// Log 是日志端口。
	Log Logger
}

// New 构造 Transport（不监听）。Port 为 0 表示监听时用 DefaultPort。
func New(d Deps) *Transport {
	return &Transport{
		log:   d.Log,
		port:  d.Port,
		self:  d.Self,
		peers: make(map[string]link.Peer),
	}
}

// SetHandlers 注入回调。必须在 Run 之前调用。
func (t *Transport) SetHandlers(onMessage func(m link.Message, from netip.AddrPort), onPeer func(p link.Peer)) {
	t.mu.Lock()
	t.onMessage = onMessage
	t.onPeer = onPeer
	t.mu.Unlock()
}

// SetSelf 更新本机广播内容（角色变化时调用）。
func (t *Transport) SetSelf(s link.Self) {
	t.mu.Lock()
	t.self = s
	t.mu.Unlock()
}

// Self 返回当前广播身份。
func (t *Transport) Self() link.Self {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.self
}

// Peers 返回当前对端列表（已按最后活跃时间过滤）。
func (t *Transport) Peers() []link.Peer {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]link.Peer, 0, len(t.peers))
	for _, p := range t.peers {
		out = append(out, p)
	}
	return out
}

// Peer 按 ID 查找对端。
func (t *Transport) Peer(id string) (link.Peer, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.peers[id]
	return p, ok
}

// Listen 绑定 UDP 端口。port 为 0 时用 DefaultPort，负数表示由内核分配
// 临时端口（仅用于同机多实例的测试）。
func (t *Transport) Listen(port int) error {
	ephemeral := port < 0
	if port == 0 {
		port = DefaultPort
	}
	if ephemeral {
		port = 0
	}
	conn, err := listenUDP(port)
	if err != nil && port == DefaultPort {
		conn, err = listenUDP(0)
	}
	if err != nil {
		return fmt.Errorf("listen udp %d: %w", port, err)
	}

	actual := conn.LocalAddr().(*net.UDPAddr).Port
	t.mu.Lock()
	t.port = actual
	t.conn = conn
	t.mu.Unlock()
	t.log.Info("discovery listening", "port", actual)
	return nil
}

// Run 开始收包与周期广播，直到 ctx 结束或 Close 被调用。
//
// 广播用 255.255.255.255（受限广播）+ 各网卡定向广播：前者在多数家用
// 路由上够用，后者覆盖部分禁止受限广播的企业网络。
func (t *Transport) Run(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()
	defer cancel()

	go t.recvLoop(runCtx)
	go t.announceLoop(runCtx)
	go t.expireLoop(runCtx)

	<-runCtx.Done()
	_ = t.Close()
}

// Close 关闭 socket（可重复调用）。
func (t *Transport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	conn := t.conn
	t.conn = nil
	cancel := t.cancel
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// announceLoop 周期广播本机状态。
func (t *Transport) announceLoop(ctx context.Context) {
	// 起手立刻发一轮，让对端尽快发现（而不是等满一个周期）。
	t.broadcast()
	ticker := time.NewTicker(DefaultAnnounceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.broadcast()
		}
	}
}

// broadcast 发一条 announce 到广播地址。
func (t *Transport) broadcast() {
	self := t.Self()
	if self.ID == "" {
		return
	}
	msg := link.Announce(self)
	data, err := msg.Encode()
	if err != nil {
		t.log.Error("encode announce", err)
		return
	}

	t.mu.RLock()
	conn := t.conn
	t.mu.RUnlock()
	if conn == nil {
		return
	}

	// 受限广播：覆盖同一广播域内的所有主机。
	limited := netip.AddrPortFrom(netip.MustParseAddr("255.255.255.255"), uint16(t.port))
	_, _ = conn.WriteToUDPAddrPort(data, limited)

	// 每个网卡的定向广播：部分网络会丢弃 255.255.255.255。
	for _, addr := range directedBroadcasts(t.port) {
		_, _ = conn.WriteToUDPAddrPort(data, addr)
	}
}

// Send 单播一条报文给指定对端。
func (t *Transport) Send(m link.Message, to netip.AddrPort) error {
	data, err := m.Encode()
	if err != nil {
		return err
	}
	t.mu.RLock()
	conn := t.conn
	closed := t.closed
	t.mu.RUnlock()
	if closed || conn == nil {
		return ErrClosed
	}
	if _, err := conn.WriteToUDPAddrPort(data, to); err != nil {
		return fmt.Errorf("send %s to %s: %w", m.Type, to, err)
	}
	return nil
}

// Broadcast 主动广播一条报文（如 bye）。
func (t *Transport) Broadcast(m link.Message) error {
	data, err := m.Encode()
	if err != nil {
		return err
	}
	t.mu.RLock()
	conn := t.conn
	closed := t.closed
	t.mu.RUnlock()
	if closed || conn == nil {
		return ErrClosed
	}
	limited := netip.AddrPortFrom(netip.MustParseAddr("255.255.255.255"), uint16(t.port))
	_, err = conn.WriteToUDPAddrPort(data, limited)
	return err
}

// recvLoop 收包并分发。
func (t *Transport) recvLoop(ctx context.Context) {
	buf := make([]byte, maxPacketSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		t.mu.RLock()
		conn := t.conn
		t.mu.RUnlock()
		if conn == nil {
			return
		}

		// 收包设超时，才能周期性检查 ctx 是否结束。
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, from, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			// socket 被关闭时会返回 EOF / use of closed connection。
			return
		}

		msg, err := link.Decode(buf[:n])
		if err != nil {
			t.log.Debug("dropping malformed packet", "from", from.String(), "err", err)
			continue
		}
		// 自己的广播会绕回来（尤其是同机两个实例），过滤掉。
		if msg.ID == t.Self().ID {
			continue
		}
		t.handle(msg, from)
	}
}

// handle 分发一条合法报文。
func (t *Transport) handle(msg link.Message, from netip.AddrPort) {
	switch msg.Type {
	case link.TypeAnnounce:
		t.upsertPeer(msg, from)
	case link.TypeBye:
		t.removePeer(msg.ID)
	}

	// 所有报文都给用例层（配对邀请、接受、拒绝）。
	t.mu.RLock()
	cb := t.onMessage
	t.mu.RUnlock()
	if cb != nil {
		cb(msg, from)
	}
}

// upsertPeer 更新对端表。
func (t *Transport) upsertPeer(msg link.Message, from netip.AddrPort) {
	p := link.PeerFrom(msg, from)
	p.LastSeen = time.Now()

	t.mu.Lock()
	prev, existed := t.peers[p.ID]
	t.peers[p.ID] = p
	t.mu.Unlock()

	// 只在首次出现或关键字段变化时通知，避免每 2 秒刷一次 UI。
	if !existed || prev.Name != p.Name || prev.Role != p.Role || prev.Fingerprint != p.Fingerprint {
		t.log.Info("peer discovered", "id", p.ID, "name", p.Name, "addr", p.Addr.String(), "role", string(p.Role))
		t.mu.RLock()
		cb := t.onPeer
		t.mu.RUnlock()
		if cb != nil {
			cb(p)
		}
	}
}

// removePeer 移除对端。
func (t *Transport) removePeer(id string) {
	t.mu.Lock()
	p, ok := t.peers[id]
	delete(t.peers, id)
	t.mu.Unlock()
	if ok {
		t.log.Info("peer removed", "id", id, "name", p.Name)
		t.mu.RLock()
		cb := t.onPeer
		t.mu.RUnlock()
		if cb != nil {
			cb(link.Peer{ID: id, Name: p.Name})
		}
	}
}

// expireLoop 周期性清理过期对端。
func (t *Transport) expireLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			var gone []string
			t.mu.Lock()
			for id, p := range t.peers {
				if p.Stale(now, DefaultPeerTTL) {
					gone = append(gone, id)
				}
			}
			for _, id := range gone {
				delete(t.peers, id)
			}
			t.mu.Unlock()
			t.mu.RLock()
			cb := t.onPeer
			t.mu.RUnlock()
			for _, id := range gone {
				t.log.Info("peer expired", "id", id)
				if cb != nil {
					cb(link.Peer{ID: id})
				}
			}
		}
	}
}
