//go:build link_integration

// 端到端联调：两个真实的 Service 在同一台机器上互相发现 + 完成配对握手。
//
// 之所以单独打 tag：这里用的是真 UDP 广播与真子进程，端口固定、耗时可到
// 十几秒，不适合混进 `go test ./internal/...`。跑法见
// docs/deskflow-integration.md §8。
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

// e2ePort 是联调用的发现端口，避开应用默认的 24801，免得本机正开着应用时打架。
const e2ePort = 24981

type nopEvents struct{}

func (nopEvents) Emit(string, any) {}

// fakeCoreScript 是一个假装成 deskflow-core 的 shell 脚本：
// 只打印一行日志然后挂着不退，用来验证参数拼装、日志泵与进程回收。
const fakeCoreScript = "#!/bin/sh\necho \"[2026-10-27T00:00:00] INFO: started client\"\nexec sleep 60\n"

func newE2EService(t *testing.T, name, corePath string) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := persistence.NewLinkStore(filepath.Join(dir, "link.json"))
	if err != nil {
		t.Fatalf("NewLinkStore: %v", err)
	}
	engine := deskflow.NewEngine(deskflow.EngineDeps{
		Dir:        filepath.Join(dir, "deskflow"),
		BinaryPath: corePath,
		Log:        nopLog{},
	})
	svc := New(Deps{
		Repo:      repo,
		Transport: discovery.New(discovery.Deps{Log: nopLog{}}),
		Core:      engine,
		Events:    nopEvents{},
		Log:       nopLog{},
		Port:      e2ePort,
	})
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start(%s): %v", name, err)
	}
	if err := svc.SetDeviceName(context.Background(), name); err != nil {
		t.Fatalf("SetDeviceName(%s): %v", name, err)
	}
	return svc, dir
}

func TestPairingEndToEnd(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "deskflow-core")
	if err := os.WriteFile(corePath, []byte(fakeCoreScript), 0o755); err != nil {
		t.Fatal(err)
	}

	a, dirA := newE2EService(t, "Alice", corePath)
	b, dirB := newE2EService(t, "Bob", corePath)
	t.Cleanup(func() {
		for _, s := range []*Service{a, b} {
			_ = s.Stop(context.Background())
			s.Shutdown(context.Background())
		}
	})

	// 1) 互相发现。
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && (len(a.State().Peers) == 0 || len(b.State().Peers) == 0) {
		time.Sleep(200 * time.Millisecond)
	}
	peersA := a.State().Peers
	if len(peersA) == 0 {
		t.Fatalf("A 未发现任何对端；state=%+v", a.State())
	}
	if peersA[0].Name != "Bob" {
		t.Fatalf("A 发现的是 %q，want Bob", peersA[0].Name)
	}

	// 2) A 请求加入 B（B 当主机）。
	if _, err := a.JoinHost(context.Background(), peersA[0].ID); err != nil {
		t.Fatalf("JoinHost: %v", err)
	}

	// 3) 双方角色互换完成。
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ra, rb := a.State(), b.State()
		if ra.Self.Role == string(link.RoleClient) && rb.Self.Role == string(link.RoleServer) &&
			len(ra.Trusted) == 1 && len(rb.Trusted) == 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	ra, rb := a.State(), b.State()
	if ra.Self.Role != string(link.RoleClient) {
		t.Errorf("A 角色 = %q，want client", ra.Self.Role)
	}
	if rb.Self.Role != string(link.RoleServer) {
		t.Errorf("B 角色 = %q，want server", rb.Self.Role)
	}
	if len(ra.Trusted) != 1 || len(rb.Trusted) != 1 {
		t.Errorf("信任记录数 A=%d B=%d，want 1/1", len(ra.Trusted), len(rb.Trusted))
	}

	// 4) 关键文件落盘：client 侧只写 trusted-servers，server 侧只有 trusted-clients。
	assertFileExists(t, filepath.Join(dirA, "deskflow", "deskflow.conf"))
	assertFileExists(t, filepath.Join(dirA, "deskflow", "tls", "deskflow.pem"))
	assertFileExists(t, filepath.Join(dirA, "deskflow", "tls", "trusted-servers"))
	assertFileExists(t, filepath.Join(dirB, "deskflow", "deskflow-server.conf"))
	assertFileExists(t, filepath.Join(dirB, "deskflow", "tls", "trusted-clients"))

	// 5) A 停止 → B 回到 idle（主机侧不跟着停，它还可能在等其它客机）。
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := a.State().Self.Role; got != string(link.RoleIdle) {
		t.Errorf("停止后 A 角色 = %q，want idle", got)
	}
	if got := a.State().Core.State; got != link.StateStopped {
		t.Errorf("停止后 A core = %q，want stopped", got)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("缺少文件 %s: %v", path, err)
	}
}
