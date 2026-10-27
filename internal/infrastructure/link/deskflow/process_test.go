//go:build !windows

package deskflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/znahovo/switch-display/internal/domain/link"
)

// fakeCore 写一个假的可执行脚本，行为由 body 决定。
// 用它来验证进程生命周期，避免测试依赖真的 Deskflow 安装。
func fakeCore(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "deskflow-core")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", msg)
}

func TestRuntimeStartStop(t *testing.T) {
	bin := fakeCore(t, `echo "[2026-01-01T00:00:00] INFO: started client"; sleep 30`)
	rt := NewRuntime(bin, nopTestLogger{})

	ctx := context.Background()
	if err := rt.Start(ctx, ModeClient, "/tmp/x.conf", 0); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, 3*time.Second, func() bool {
		s := rt.Status()
		return s.PID != 0
	}, "pid")

	// 日志应被采集到。
	waitFor(t, 3*time.Second, func() bool {
		return len(rt.LogTail()) > 0
	}, "log tail")

	joined := strings.Join(rt.LogTail(), "\n")
	if !strings.Contains(joined, "started client") {
		t.Fatalf("log tail = %q, want 'started client'", joined)
	}

	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if s := rt.Status(); s.State != StateStopped {
		t.Fatalf("State = %q, want %q", s.State, StateStopped)
	}
	if rt.Status().PID != 0 {
		t.Fatalf("PID = %d, want 0", rt.Status().PID)
	}
}

func TestRuntimeStopIsIdempotent(t *testing.T) {
	bin := fakeCore(t, `sleep 30`)
	rt := NewRuntime(bin, nopTestLogger{})
	if err := rt.Start(context.Background(), ModeClient, "/tmp/x.conf", 0); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

func TestRuntimeBusy(t *testing.T) {
	bin := fakeCore(t, `sleep 30`)
	rt := NewRuntime(bin, nopTestLogger{})
	if err := rt.Start(context.Background(), ModeClient, "/tmp/x.conf", 0); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Stop(context.Background())

	if err := rt.Start(context.Background(), ModeServer, "/tmp/y.conf", 0); err != ErrBusy {
		t.Fatalf("second Start() error = %v, want ErrBusy", err)
	}
}

func TestRuntimeReportsExitCode(t *testing.T) {
	// 退出码 4 = 配置错误（Deskflow 的 s_exitConfig）。
	bin := fakeCore(t, `echo "[2026-01-01T00:00:00] CRIT: failed to load config" >&2; exit 4`)
	rt := NewRuntime(bin, nopTestLogger{})

	if err := rt.Start(context.Background(), ModeServer, "/tmp/x.conf", 0); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		return rt.Status().State == StateFailed
	}, "failed state")

	msg := rt.Status().Message
	if !strings.Contains(msg, "配置") && !strings.Contains(msg, "failed to load config") {
		t.Fatalf("Message = %q, want a config error hint", msg)
	}
}

func TestRuntimeStartMissingBinary(t *testing.T) {
	rt := NewRuntime("/nonexistent/deskflow-core", nopTestLogger{})
	err := rt.Start(context.Background(), ModeClient, "/tmp/x.conf", 0)
	if err == nil {
		t.Fatal("Start() = nil error, want error")
	}
	if rt.Status().State != StateFailed {
		t.Fatalf("State = %q, want %q", rt.Status().State, StateFailed)
	}
}

func TestRuntimeRejectsBadMode(t *testing.T) {
	rt := NewRuntime("/bin/true", nopTestLogger{})
	if err := rt.Start(context.Background(), Mode("host"), "/tmp/x.conf", 0); err == nil {
		t.Fatal("Start(bad mode) = nil error, want error")
	}
}

func TestLogEventFor(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantMsg   string
		wantReady bool
		wantFail  bool
		wantOK    bool
	}{
		{
			name:      "client tls handshake ok",
			line:      "[2026-01-01T00:00:00] INFO: connected to secure socket",
			wantReady: true,
			wantOK:    true,
		},
		{
			name:      "server accepted client",
			line:      "[2026-01-01T00:00:00] INFO: accepted client connection",
			wantReady: true,
			wantOK:    true,
		},
		{
			name:    "client started",
			line:    "[2026-01-01T00:00:00] INFO: started client",
			wantMsg: MsgClientStarted,
			wantOK:  true,
		},
		{
			// 我们的 TCP 就绪探测会触发这一行，不能当成故障。
			name:   "probe noise is ignored",
			line:   "[2026-01-01T00:00:00] ERROR: failed to accept secure socket",
			wantOK: false,
		},
		{
			name:     "real error fails",
			line:     "[2026-01-01T00:00:00] ERROR: cannot open configuration",
			wantMsg:  "cannot open configuration",
			wantFail: true,
			wantOK:   true,
		},
		{
			// client 自动重试，这行不能判死，否则「先起 client」就永远起不来。
			name:    "connect retry warning is not fatal",
			line:    "[2026-01-01T00:00:00] WARN: failed to connect to server: Connection refused",
			wantMsg: "failed to connect to server: Connection refused",
			wantOK:  true,
		},
		{
			name:   "debug is filtered by level=info anyway",
			line:   "[2026-01-01T00:00:00] DEBUG: connected to server",
			wantOK: false,
		},
		{
			name:   "unrelated info dropped",
			line:   "[2026-01-01T00:00:00] INFO: reloaded configuration",
			wantOK: false,
		},
		{
			name:   "no timestamp",
			line:   "no timestamp here",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := logEventFor(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ev.Message != tt.wantMsg {
				t.Fatalf("Message = %q, want %q", ev.Message, tt.wantMsg)
			}
			if ev.Ready != tt.wantReady {
				t.Fatalf("Ready = %v, want %v", ev.Ready, tt.wantReady)
			}
			if ev.Failed != tt.wantFail {
				t.Fatalf("Failed = %v, want %v", ev.Failed, tt.wantFail)
			}
		})
	}
}

// TestRuntimeRecoversAfterInitialFailure 保证 client 首次连接失败被
// 判死后，重试成功还能翻回 running（否则「先起 client、后起 server」
// 这个常见顺序就永远起不来）。
func TestRuntimeRecoversAfterInitialFailure(t *testing.T) {
	rt := NewRuntime("/bin/true", nopTestLogger{})
	rt.mu.Lock()
	rt.status = Status{State: StateStarting, Role: link.RoleClient}
	rt.mu.Unlock()

	rt.appendLog("[2026-01-01T00:00:00] ERROR: cannot open configuration")
	if got := rt.Status(); got.State != StateFailed {
		t.Fatalf("State = %q, want %q", got.State, StateFailed)
	}

	rt.appendLog("[2026-01-01T00:00:00] INFO: connected to secure socket")
	if got := rt.Status(); got.State != StateRunning {
		t.Fatalf("State = %q, want %q after reconnect", got.State, StateRunning)
	}
	if got := rt.Status().Message; got != "" {
		t.Fatalf("Message = %q, want empty after ready", got)
	}
}

// TestRuntimeIgnoresProbeErrorNoise 保证就绪探测造成的 ERROR 不会把
// 已经跑起来的 server 判成失败。
func TestRuntimeIgnoresProbeErrorNoise(t *testing.T) {
	rt := NewRuntime("/bin/true", nopTestLogger{})
	rt.mu.Lock()
	rt.status = Status{State: StateRunning, Role: link.RoleServer}
	rt.mu.Unlock()

	rt.appendLog("[2026-01-01T00:00:00] ERROR: failed to accept secure socket")

	if got := rt.Status(); got.State != StateRunning {
		t.Fatalf("State = %q, want %q", got.State, StateRunning)
	}
}

func TestLocateExplicitMissing(t *testing.T) {
	_, err := Locate("/definitely/not/here/deskflow-core")
	if err == nil {
		t.Fatal("Locate(missing) = nil error, want error")
	}
	if !strings.Contains(err.Error(), "deskflow-core not found") {
		t.Fatalf("err = %v, want ErrNoCore", err)
	}
}

func TestLocateExplicitValid(t *testing.T) {
	bin := fakeCore(t, `exit 0`)
	got, err := Locate(bin)
	if err != nil {
		t.Fatalf("Locate() error = %v", err)
	}
	if got != bin {
		t.Fatalf("Locate() = %q, want %q", got, bin)
	}
}

func TestLocateNotExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deskflow-core")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Locate(path); err == nil {
		t.Fatal("Locate(non-executable) = nil error, want error")
	}
}

func TestFinderCaches(t *testing.T) {
	bin := fakeCore(t, `exit 0`)
	f := NewFinder(bin)
	first, err := f.Find()
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	second, err := f.Find()
	if err != nil {
		t.Fatalf("Find() again error = %v", err)
	}
	if first != second {
		t.Fatalf("Find() not cached: %q vs %q", first, second)
	}
}

type nopTestLogger struct{}

func (nopTestLogger) Debug(string, ...any)        {}
func (nopTestLogger) Info(string, ...any)         {}
func (nopTestLogger) Error(string, error, ...any) {}
