//go:build deskflow_real

// 这个测试用真正的 deskflow-core 跑一遍完整配对，验证 TLS 指纹信任库
// 在真进程上生效（单测里的假 core 只能验证「我们传参/写文件对不对」，
// 验证不了 Deskflow 认不认）。
//
// 跑法：
//
//	DESKFLOW_CORE=/Applications/Deskflow.app/Contents/MacOS/deskflow-core \
//	  go test -tags deskflow_real ./internal/application/linkapp/ -run TestRealCorePair -v
//
// 前置条件：显示器必须处于唤醒状态。Deskflow 的 OSXComputer 用
// CGGetActiveDisplayList 判断显示器数量，休眠时返回 0 就会抛
// DisplayInvalidException 直接退出（OSXComputer.mm:119），跟本集成无关。
package linkapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/znahovo/switch-display/internal/domain/link"
	"github.com/znahovo/switch-display/internal/infrastructure/link/deskflow"
	"github.com/znahovo/switch-display/internal/infrastructure/link/discovery"
	"github.com/znahovo/switch-display/internal/infrastructure/persistence"
)

// realCorePort 故意避开应用默认的 24801，防止与本机正在运行的实例撞车。
const realCorePort = 24983

type nopEv struct{}

func (nopEv) Emit(string, any) {}

func newRealService(t *testing.T, name string, corePath string) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := persistence.NewLinkStore(filepath.Join(dir, "link.json"))
	if err != nil {
		t.Fatal(err)
	}
	eng := deskflow.NewEngine(deskflow.EngineDeps{
		Dir:        filepath.Join(dir, "deskflow"),
		BinaryPath: corePath,
		Log:        nopLog{},
	})
	svc := New(Deps{
		Repo:      repo,
		Transport: discovery.New(discovery.Deps{Log: nopLog{}}),
		Core:      eng,
		Events:    nopEv{},
		Log:       nopLog{},
		Port:      realCorePort,
	})
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	if err := svc.SetDeviceName(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	return svc, dir
}

func TestRealCorePair(t *testing.T) {
	corePath := os.Getenv("DESKFLOW_CORE")
	if corePath == "" {
		t.Skip("set DESKFLOW_CORE to a real deskflow-core binary")
	}
	if _, err := os.Stat(corePath); err != nil {
		t.Skipf("DESKFLOW_CORE not usable: %v", err)
	}

	a, dirA := newRealService(t, "Alice", corePath)
	b, dirB := newRealService(t, "Bob", corePath)
	defer func() {
		for _, s := range []*Service{a, b} {
			_ = s.Stop(context.Background())
			s.Shutdown(context.Background())
		}
	}()

	// 1) 两边都应该能通过 UDP 广播发现对方。
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && (len(a.State().Peers) == 0 || len(b.State().Peers) == 0) {
		time.Sleep(200 * time.Millisecond)
	}
	peers := a.State().Peers
	if len(peers) == 0 {
		t.Fatalf("no peers discovered: %+v", a.State())
	}

	// 2) A 点「加入」，A 应该是 client、B 应该是 server。
	if _, err := a.JoinHost(context.Background(), peers[0].ID); err != nil {
		t.Fatalf("join: %v", err)
	}

	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		sa, sb := a.State(), b.State()
		if sa.Core.State == link.StateRunning && sb.Core.State == link.StateRunning {
			break
		}
		if sa.Core.State == link.StateFailed || sb.Core.State == link.StateFailed {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	sa, sb := a.State(), b.State()
	t.Logf("A: role=%s core=%s msg=%q peers=%+v", sa.Self.Role, sa.Core.State, sa.Core.Message, sa.Peers)
	t.Logf("A trusted=%+v", sa.Trusted)
	t.Logf("B: role=%s core=%s msg=%q trusted=%+v", sb.Self.Role, sb.Core.State, sb.Core.Message, sb.Trusted)

	if sa.Self.Role != string(link.RoleClient) {
		t.Errorf("A role = %q, want client", sa.Self.Role)
	}
	if sb.Self.Role != string(link.RoleServer) {
		t.Errorf("B role = %q, want server", sb.Self.Role)
	}
	if sa.LastHostID == "" {
		t.Error("A lastHostId should record the host it joined")
	}
	if !hasFile(dirA, "deskflow", "tls", "trusted-servers") {
		t.Error("A should have written tls/trusted-servers")
	}
	if !hasFile(dirB, "deskflow", "tls", "trusted-clients") {
		t.Error("B should have written tls/trusted-clients")
	}

	// 3) 停止后角色回到 idle，core 也要真的退出。
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("stop A: %v", err)
	}
	if got := a.State(); got.Self.Role != string(link.RoleIdle) {
		t.Errorf("after stop, A role = %q, want idle", got.Self.Role)
	}
}

func hasFile(dir string, parts ...string) bool {
	_, err := os.Stat(filepath.Join(append([]string{dir}, parts...)...))
	return err == nil
}
