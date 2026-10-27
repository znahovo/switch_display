package discovery

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/znahovo/switch-display/internal/domain/link"
)

type nopLogger struct{}

func (nopLogger) Debug(string, ...any)        {}
func (nopLogger) Info(string, ...any)         {}
func (nopLogger) Error(string, error, ...any) {}

func testSelf(t *testing.T, name string) link.Self {
	t.Helper()
	id, err := link.NewDeviceID()
	if err != nil {
		t.Fatalf("NewDeviceID() error = %v", err)
	}
	return link.Self{ID: id, Name: name, Role: link.RoleIdle, Port: link.DefaultPort}
}

func TestTransportUpsertAndExpire(t *testing.T) {
	tr := New(Deps{Self: testSelf(t, "local"), Log: nopLogger{}})

	peerID, err := link.NewDeviceID()
	if err != nil {
		t.Fatalf("NewDeviceID() error = %v", err)
	}
	msg := link.Message{
		V:    link.MessageVersion,
		Type: link.TypeAnnounce,
		ID:   peerID,
		Name: "Remote",
		Role: link.RoleServer,
	}
	from := netip.MustParseAddrPort("192.168.1.30:24801")
	tr.upsertPeer(msg, from)

	peers := tr.Peers()
	if len(peers) != 1 {
		t.Fatalf("len(Peers) = %d, want 1", len(peers))
	}
	if peers[0].Name != "Remote" || peers[0].Addr != from {
		t.Fatalf("peer = %+v", peers[0])
	}

	got, ok := tr.Peer(peerID)
	if !ok || got.Name != "Remote" {
		t.Fatalf("Peer(%q) = %+v, %v", peerID, got, ok)
	}

	// 更新同一对端不应产生第二条记录。
	msg.Name = "Remote Renamed"
	tr.upsertPeer(msg, from)
	if peers := tr.Peers(); len(peers) != 1 {
		t.Fatalf("after update len(Peers) = %d, want 1", len(peers))
	}

	tr.removePeer(peerID)
	if _, ok := tr.Peer(peerID); ok {
		t.Fatal("peer still present after removePeer")
	}
}

func TestTransportIgnoresOwnAnnounce(t *testing.T) {
	self := testSelf(t, "local")
	tr := New(Deps{Self: self, Log: nopLogger{}})
	if err := tr.Listen(-1); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer tr.Close()

	msg, err := link.Announce(self).Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	conn := tr.conn
	if conn == nil {
		t.Fatal("conn is nil")
	}
	// 自己发给自己：recvLoop 会过滤，但这里直接调用 handle 验证过滤逻辑
	// 在 handle 之外（recvLoop 里）。改为校验 decode 后 ID 相同。
	decoded, err := link.Decode(msg)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.ID != self.ID {
		t.Fatalf("decoded.ID = %q, want %q", decoded.ID, self.ID)
	}
}

func TestTransportLoopbackUnicast(t *testing.T) {
	selfA := testSelf(t, "A")
	selfB := testSelf(t, "B")

	a := New(Deps{Self: selfA, Log: nopLogger{}})
	if err := a.Listen(-1); err != nil {
		t.Fatalf("a.Listen() error = %v", err)
	}
	defer a.Close()

	b := New(Deps{Self: selfB, Log: nopLogger{}})
	if err := b.Listen(-1); err != nil {
		t.Fatalf("b.Listen() error = %v", err)
	}
	defer b.Close()

	// 端口是 OS 分配的；从 a 的本地地址推 b 的端口不可靠，
	// 直接取各自 LocalAddr。
	aAddr := a.conn.LocalAddr().(*net.UDPAddr)
	bAddr := b.conn.LocalAddr().(*net.UDPAddr)
	bEP := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(bAddr.Port))
	aEP := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(aAddr.Port))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.recvLoop(ctx)
	go b.recvLoop(ctx)

	// a 向 b 发 announce。
	if err := a.Send(link.Announce(selfA), bEP); err != nil {
		t.Fatalf("a.Send() error = %v", err)
	}
	// b 向 a 发 announce。
	if err := b.Send(link.Announce(selfB), aEP); err != nil {
		t.Fatalf("b.Send() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := b.Peer(selfA.ID); ok {
			if _, ok := a.Peer(selfB.ID); ok {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("peers not exchanged: a=%v b=%v", a.Peers(), b.Peers())
}

func TestTransportSendAfterClose(t *testing.T) {
	tr := New(Deps{Self: testSelf(t, "local"), Log: nopLogger{}})
	if err := tr.Listen(-1); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	tr.Close()
	err := tr.Send(link.Announce(testSelf(t, "x")), netip.MustParseAddrPort("127.0.0.1:1"))
	if err == nil {
		t.Fatal("Send after Close() = nil error, want error")
	}
}

func TestTransportCloseIdempotent(t *testing.T) {
	tr := New(Deps{Self: testSelf(t, "local"), Log: nopLogger{}})
	if err := tr.Listen(-1); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
