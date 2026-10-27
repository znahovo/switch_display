package linkapp

import (
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/znahovo/switch-display/internal/domain/link"
)

// ---- 测试替身 ----

type fakeTransport struct {
	mu       sync.Mutex
	self     link.Self
	peers    map[string]link.Peer
	sent     []link.Message
	listened int
	closed   bool
	onMsg    func(m link.Message, from netip.AddrPort)
	onPeer   func(p link.Peer)
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{peers: make(map[string]link.Peer)}
}

func (f *fakeTransport) SetSelf(s link.Self) {
	f.mu.Lock()
	f.self = s
	f.mu.Unlock()
}

func (f *fakeTransport) Self() link.Self {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.self
}

func (f *fakeTransport) Peers() []link.Peer {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]link.Peer, 0, len(f.peers))
	for _, p := range f.peers {
		out = append(out, p)
	}
	return out
}

func (f *fakeTransport) Peer(id string) (link.Peer, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.peers[id]
	return p, ok
}

func (f *fakeTransport) addPeer(p link.Peer) {
	f.mu.Lock()
	f.peers[p.ID] = p
	f.mu.Unlock()
}

func (f *fakeTransport) Send(m link.Message, _ netip.AddrPort) error {
	f.mu.Lock()
	f.sent = append(f.sent, m)
	f.mu.Unlock()
	return nil
}

func (f *fakeTransport) Broadcast(m link.Message) error {
	f.mu.Lock()
	f.sent = append(f.sent, m)
	f.mu.Unlock()
	return nil
}

func (f *fakeTransport) Listen(port int) error {
	f.mu.Lock()
	f.listened = port
	f.mu.Unlock()
	return nil
}

func (f *fakeTransport) Run(ctx context.Context) { <-ctx.Done() }

func (f *fakeTransport) Close() error { f.closed = true; return nil }

func (f *fakeTransport) SetHandlers(onMessage func(m link.Message, from netip.AddrPort), onPeer func(p link.Peer)) {
	f.onMsg = onMessage
	f.onPeer = onPeer
}

func (f *fakeTransport) sentMessages() []link.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]link.Message, len(f.sent))
	copy(out, f.sent)
	return out
}

type fakeCore struct {
	mu          sync.Mutex
	fingerprint string
	regenerated bool
	locateErr   error
	dir         string
	status      link.RuntimeStatus
	startedSrv  []link.ServerSpec
	startedCli  []link.ClientSpec
	stopped     int
	runErr      error
}

func newFakeCore(fp string) *fakeCore {
	return &fakeCore{
		fingerprint: fp,
		dir:         "/tmp/deskflow",
		status:      link.RuntimeStatus{State: link.StateStopped, Role: link.RoleIdle},
	}
}

func (f *fakeCore) Locate() (string, error) {
	if f.locateErr != nil {
		return "", f.locateErr
	}
	return "/usr/local/bin/deskflow-core", nil
}

func (f *fakeCore) ResetFinder() {}

func (f *fakeCore) Dir() string { return f.dir }

func (f *fakeCore) EnsureIdentity() (string, bool, error) {
	return f.fingerprint, f.regenerated, nil
}

func (f *fakeCore) StartServer(_ context.Context, spec link.ServerSpec) error {
	if f.runErr != nil {
		return f.runErr
	}
	f.mu.Lock()
	f.startedSrv = append(f.startedSrv, spec)
	f.status = link.RuntimeStatus{State: link.StateRunning, Role: link.RoleServer, PID: 1234}
	f.mu.Unlock()
	return nil
}

func (f *fakeCore) StartClient(_ context.Context, spec link.ClientSpec) error {
	if f.runErr != nil {
		return f.runErr
	}
	f.mu.Lock()
	f.startedCli = append(f.startedCli, spec)
	f.status = link.RuntimeStatus{State: link.StateRunning, Role: link.RoleClient, PID: 2345}
	f.mu.Unlock()
	return nil
}

func (f *fakeCore) Stop(context.Context) error {
	f.mu.Lock()
	f.stopped++
	f.status = link.RuntimeStatus{State: link.StateStopped, Role: link.RoleIdle}
	f.mu.Unlock()
	return nil
}

func (f *fakeCore) Status() link.RuntimeStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeCore) LogTail() []string { return nil }

type fakeRepo struct {
	mu   sync.Mutex
	snap link.Snapshot
	err  error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{snap: link.EmptySnapshot()}
}

func (f *fakeRepo) Load(context.Context) (link.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, f.err
}

func (f *fakeRepo) Save(_ context.Context, s link.Snapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.snap = s
	return nil
}

func (f *fakeRepo) Path() string { return "/tmp/link.json" }

func (f *fakeRepo) current() link.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}

type fakeEvents struct {
	mu     sync.Mutex
	names  []string
	events []string
}

func (f *fakeEvents) Emit(name string, _ any) {
	f.mu.Lock()
	f.names = append(f.names, name)
	f.mu.Unlock()
}

func (f *fakeEvents) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, got := range f.names {
		if got == name {
			n++
		}
	}
	return n
}

type nopLog struct{}

func (nopLog) Debug(string, ...any)        {}
func (nopLog) Info(string, ...any)         {}
func (nopLog) Error(string, error, ...any) {}

// ---- 测试 ----

const testFP = "v2:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func newService(t *testing.T) (*Service, *fakeTransport, *fakeCore, *fakeRepo, *fakeEvents) {
	t.Helper()
	tr := newFakeTransport()
	core := newFakeCore(testFP)
	repo := newFakeRepo()
	ev := &fakeEvents{}
	svc := New(Deps{Repo: repo, Transport: tr, Core: core, Events: ev, Log: nopLog{}})
	return svc, tr, core, repo, ev
}

func mustPeer(t *testing.T, name string) link.Peer {
	t.Helper()
	id, err := link.NewDeviceID()
	if err != nil {
		t.Fatalf("NewDeviceID() error = %v", err)
	}
	return link.Peer{
		ID:          id,
		Name:        name,
		Addr:        netip.MustParseAddrPort("192.168.1.30:24801"),
		Fingerprint: testFP,
		Role:        link.RoleIdle,
		LastSeen:    time.Now(),
	}
}

func TestServiceStartGeneratesIdentity(t *testing.T) {
	svc, tr, _, repo, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	snap := repo.current()
	if !link.ValidDeviceID(snap.Settings.DeviceID) {
		t.Fatalf("DeviceID = %q, want valid", snap.Settings.DeviceID)
	}
	if snap.Settings.DeviceName == "" {
		t.Fatal("DeviceName is empty")
	}
	if tr.listened != link.DiscoveryPort {
		t.Fatalf("listened port = %d, want %d", tr.listened, link.DiscoveryPort)
	}
	self := tr.Self()
	if self.Fingerprint != testFP {
		t.Fatalf("self.Fingerprint = %q, want %q", self.Fingerprint, testFP)
	}
	if self.Role != link.RoleIdle {
		t.Fatalf("self.Role = %q, want idle", self.Role)
	}
}

func TestServiceStartClearsTrustedOnRegen(t *testing.T) {
	svc, _, core, repo, _ := newService(t)
	// 预置一条已配对记录。
	peerID, _ := link.NewDeviceID()
	devID, _ := link.NewDeviceID()
	repo.snap = link.Snapshot{
		Version:  link.CurrentVersion,
		Settings: link.Settings{DeviceID: devID, DeviceName: "me"},
		Trusted: []link.TrustedPeer{{
			ID: peerID, Name: "peer", Fingerprint: testFP, PairedAt: time.Now(),
		}},
	}
	core.regenerated = true

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if got := repo.current().Trusted; len(got) != 0 {
		t.Fatalf("Trusted = %v, want empty after cert regeneration", got)
	}
}

func TestServiceStartKeepsTrustedOnReuse(t *testing.T) {
	svc, _, core, repo, _ := newService(t)
	peerID, _ := link.NewDeviceID()
	devID, _ := link.NewDeviceID()
	repo.snap = link.Snapshot{
		Version:  link.CurrentVersion,
		Settings: link.Settings{DeviceID: devID, DeviceName: "me"},
		Trusted: []link.TrustedPeer{{
			ID: peerID, Name: "peer", Fingerprint: testFP, PairedAt: time.Now(),
		}},
	}
	core.regenerated = false

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if got := repo.current().Trusted; len(got) != 1 {
		t.Fatalf("Trusted = %v, want 1 entry", got)
	}
}

func TestServiceSetDeviceName(t *testing.T) {
	svc, tr, _, _, ev := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := svc.SetDeviceName(context.Background(), "  My   Mac  "); err != nil {
		t.Fatalf("SetDeviceName() error = %v", err)
	}
	if got := tr.Self().Name; got != "My Mac" {
		t.Fatalf("self.Name = %q, want %q", got, "My Mac")
	}
	if ev.count("link:changed") == 0 {
		t.Fatal("expected link:changed event")
	}
}

func TestServiceSetDeviceNameRejectsEmpty(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := svc.SetDeviceName(context.Background(), "   "); err != ErrInvalidName {
		t.Fatalf("SetDeviceName() error = %v, want ErrInvalidName", err)
	}
}

func TestServiceHostWithSendsRequest(t *testing.T) {
	svc, tr, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "Office PC")
	tr.addPeer(peer)

	res, err := svc.HostWith(context.Background(), peer.ID)
	if err != nil {
		t.Fatalf("HostWith() error = %v", err)
	}
	if !res.OK {
		t.Fatalf("HostWith() = %+v, want OK", res)
	}

	sent := tr.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sent))
	}
	if sent[0].Type != link.TypePairRequest {
		t.Fatalf("type = %q, want %q", sent[0].Type, link.TypePairRequest)
	}
	if sent[0].Role != link.RoleServer {
		t.Fatalf("role = %q, want server", sent[0].Role)
	}
	if sent[0].To != peer.ID {
		t.Fatalf("to = %q, want %q", sent[0].To, peer.ID)
	}

	// 此时还不应启动 core（等对方 accept）。
	if len(core.startedSrv) != 0 || len(core.startedCli) != 0 {
		t.Fatal("core started before accept")
	}
}

func TestServiceJoinHostSendsRequest(t *testing.T) {
	svc, tr, _, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "Host Mac")
	tr.addPeer(peer)

	if _, err := svc.JoinHost(context.Background(), peer.ID); err != nil {
		t.Fatalf("JoinHost() error = %v", err)
	}
	sent := tr.sentMessages()
	if len(sent) != 1 || sent[0].Role != link.RoleClient {
		t.Fatalf("sent = %+v, want one pair_request with role client", sent)
	}
}

func TestServiceHostWithUnknownPeer(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := svc.HostWith(context.Background(), "ffffffffffffffffffffffffffffffff"); err != ErrPeerNotFound {
		t.Fatalf("HostWith(unknown) error = %v, want ErrPeerNotFound", err)
	}
}

func TestServiceHostWithBusy(t *testing.T) {
	svc, tr, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "PC")
	tr.addPeer(peer)
	// 模拟已在运行。
	core.status = link.RuntimeStatus{State: link.StateRunning, Role: link.RoleServer}

	if _, err := svc.HostWith(context.Background(), peer.ID); err != ErrBusy {
		t.Fatalf("HostWith() error = %v, want ErrBusy", err)
	}
}

func TestServiceHostWithNoCore(t *testing.T) {
	svc, tr, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "PC")
	tr.addPeer(peer)
	core.locateErr = errFakeNoCore

	if _, err := svc.HostWith(context.Background(), peer.ID); err != ErrNoCore {
		t.Fatalf("HostWith() error = %v, want ErrNoCore", err)
	}
}

func TestServiceHandlePairRequestAsServer(t *testing.T) {
	svc, tr, core, repo, ev := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "Client PC")
	tr.addPeer(peer)

	// 对方想当 client → 本机当 server。
	svc.onMessage(link.Message{
		V:           link.MessageVersion,
		Type:        link.TypePairRequest,
		ID:          peer.ID,
		Name:        peer.Name,
		Role:        link.RoleClient,
		Fingerprint: testFP,
		Port:        link.CorePort,
	}, peer.Addr)

	// 应该回 accept。
	sent := tr.sentMessages()
	if len(sent) != 1 || sent[0].Type != link.TypePairAccept {
		t.Fatalf("sent = %+v, want one pair_accept", sent)
	}
	if sent[0].Role != link.RoleServer {
		t.Fatalf("accept role = %q, want server", sent[0].Role)
	}

	// 应该启动 server core，并把对方指纹写进信任列表。
	if len(core.startedSrv) != 1 {
		t.Fatalf("startedSrv = %d, want 1", len(core.startedSrv))
	}
	spec := core.startedSrv[0]
	if len(spec.TrustedClients) != 1 || spec.TrustedClients[0] != testFP {
		t.Fatalf("TrustedClients = %v", spec.TrustedClients)
	}
	if len(spec.Neighbors) != 1 {
		t.Fatalf("Neighbors = %v, want one entry", spec.Neighbors)
	}

	snap := repo.current()
	if len(snap.Trusted) != 1 || snap.Trusted[0].ID != peer.ID {
		t.Fatalf("Trusted = %+v, want the peer", snap.Trusted)
	}
	if ev.count("link:changed") == 0 {
		t.Fatal("expected link:changed event")
	}
	if got := tr.Self().Role; got != link.RoleServer {
		t.Fatalf("self.Role = %q, want server", got)
	}
}

func TestServiceHandlePairRequestAsClient(t *testing.T) {
	svc, tr, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "Host PC")
	tr.addPeer(peer)

	// 对方想当 server → 本机当 client。
	svc.onMessage(link.Message{
		V:           link.MessageVersion,
		Type:        link.TypePairRequest,
		ID:          peer.ID,
		Name:        peer.Name,
		Role:        link.RoleServer,
		Fingerprint: testFP,
		Port:        link.CorePort,
	}, peer.Addr)

	if len(core.startedCli) != 1 {
		t.Fatalf("startedCli = %d, want 1", len(core.startedCli))
	}
	spec := core.startedCli[0]
	if spec.ServerHost != "192.168.1.30" {
		// 必须用来源地址（内核填的），不能用报文里的自报地址。
		t.Fatalf("ServerHost = %q, want the packet source IP", spec.ServerHost)
	}
	if len(spec.TrustedServers) != 1 || spec.TrustedServers[0] != testFP {
		t.Fatalf("TrustedServers = %v", spec.TrustedServers)
	}
}

func TestServiceRejectsInvalidFingerprint(t *testing.T) {
	svc, tr, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "Bad PC")
	tr.addPeer(peer)

	svc.onMessage(link.Message{
		V:           link.MessageVersion,
		Type:        link.TypePairRequest,
		ID:          peer.ID,
		Name:        peer.Name,
		Role:        link.RoleClient,
		Fingerprint: "v2:sha256:short",
	}, peer.Addr)

	sent := tr.sentMessages()
	if len(sent) != 1 || sent[0].Type != link.TypePairReject {
		t.Fatalf("sent = %+v, want one pair_reject", sent)
	}
	if len(core.startedSrv) != 0 || len(core.startedCli) != 0 {
		t.Fatal("core must not start for invalid fingerprint")
	}
}

func TestServiceRejectsWhenBusy(t *testing.T) {
	svc, tr, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	core.status = link.RuntimeStatus{State: link.StateRunning, Role: link.RoleServer}
	peer := mustPeer(t, "PC")
	tr.addPeer(peer)

	svc.onMessage(link.Message{
		V:           link.MessageVersion,
		Type:        link.TypePairRequest,
		ID:          peer.ID,
		Name:        peer.Name,
		Role:        link.RoleClient,
		Fingerprint: testFP,
	}, peer.Addr)

	sent := tr.sentMessages()
	if len(sent) != 1 || sent[0].Type != link.TypePairReject {
		t.Fatalf("sent = %+v, want pair_reject when busy", sent)
	}
}

func TestServiceHandlePairAccept(t *testing.T) {
	svc, tr, core, repo, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "Peer")
	tr.addPeer(peer)

	// 对方接受了，并说自己是 client → 本机是 server。
	svc.onMessage(link.Message{
		V:           link.MessageVersion,
		Type:        link.TypePairAccept,
		ID:          peer.ID,
		Name:        peer.Name,
		Role:        link.RoleClient,
		Fingerprint: testFP,
		Port:        link.CorePort,
	}, peer.Addr)

	if len(core.startedSrv) != 1 {
		t.Fatalf("startedSrv = %d, want 1", len(core.startedSrv))
	}
	if got := repo.current().Trusted; len(got) != 1 || got[0].ID != peer.ID {
		t.Fatalf("Trusted = %+v", got)
	}
}

func TestServiceStop(t *testing.T) {
	svc, tr, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	self := tr.Self()
	self.Role = link.RoleServer
	tr.SetSelf(self)

	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if core.stopped != 1 {
		t.Fatalf("core.stopped = %d, want 1", core.stopped)
	}
	if got := tr.Self().Role; got != link.RoleIdle {
		t.Fatalf("self.Role = %q, want idle", got)
	}
	// 应该广播 bye。
	sent := tr.sentMessages()
	found := false
	for _, m := range sent {
		if m.Type == link.TypeBye {
			found = true
		}
	}
	if !found {
		t.Fatalf("no bye broadcast: %+v", sent)
	}
}

func TestServiceStopWhenHostSendsBye(t *testing.T) {
	svc, tr, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	// 本机当前是客机。
	core.status = link.RuntimeStatus{State: link.StateRunning, Role: link.RoleClient}
	self := tr.Self()
	self.Role = link.RoleClient
	tr.SetSelf(self)

	peer := mustPeer(t, "Host")
	svc.onMessage(link.Message{
		V:    link.MessageVersion,
		Type: link.TypeBye,
		ID:   peer.ID,
	}, peer.Addr)

	if core.stopped != 1 {
		t.Fatalf("core.stopped = %d, want 1 (host left)", core.stopped)
	}
	if got := tr.Self().Role; got != link.RoleIdle {
		t.Fatalf("self.Role = %q, want idle", got)
	}
}

func TestServiceUnpair(t *testing.T) {
	svc, _, core, repo, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "Peer")
	if err := svc.rememberTrusted(peer.ID, peer.Name, testFP); err != nil {
		t.Fatalf("rememberTrusted() error = %v", err)
	}
	if len(repo.current().Trusted) != 1 {
		t.Fatalf("Trusted = %+v, want 1", repo.current().Trusted)
	}

	if err := svc.Unpair(context.Background(), peer.ID); err != nil {
		t.Fatalf("Unpair() error = %v", err)
	}
	if len(repo.current().Trusted) != 0 {
		t.Fatalf("Trusted = %+v, want empty", repo.current().Trusted)
	}
	if core.stopped != 1 {
		t.Fatalf("core.stopped = %d, want 1", core.stopped)
	}
	if err := svc.Unpair(context.Background(), peer.ID); err != ErrNotPaired {
		t.Fatalf("second Unpair() error = %v, want ErrNotPaired", err)
	}
}

func TestServiceState(t *testing.T) {
	svc, tr, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	peer := mustPeer(t, "Peer")
	tr.addPeer(peer)
	core.status = link.RuntimeStatus{State: link.StateRunning, Role: link.RoleServer, PID: 999, Message: "ok"}

	state := svc.State()
	if state.Self.ID == "" || state.Self.Name == "" {
		t.Fatalf("Self = %+v", state.Self)
	}
	if len(state.Peers) != 1 || state.Peers[0].Name != "Peer" {
		t.Fatalf("Peers = %+v", state.Peers)
	}
	if !state.Peers[0].Reachable {
		t.Fatal("peer should be reachable")
	}
	if state.Core.State != link.StateRunning || state.Core.PID != 999 {
		t.Fatalf("Core = %+v", state.Core)
	}
	if !state.DeskflowAvailable {
		t.Fatal("DeskflowAvailable = false, want true")
	}
	if state.DiscoverPort != link.DiscoveryPort {
		t.Fatalf("DiscoverPort = %d", state.DiscoverPort)
	}
}

func TestRefreshDeskflow(t *testing.T) {
	svc, _, core, _, _ := newService(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !svc.RefreshDeskflow() {
		t.Fatal("RefreshDeskflow() = false, want true when core is locatable")
	}
	core.locateErr = errFakeNoCore
	if svc.RefreshDeskflow() {
		t.Fatal("RefreshDeskflow() = true, want false when core is missing")
	}
}
