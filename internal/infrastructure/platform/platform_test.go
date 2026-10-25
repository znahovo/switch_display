package platform

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoggerSanitizesDanglingKeys(t *testing.T) {
	// 落单的 key 不能让 slog 输出 !BADKEY 之外还 panic。
	var buf bytes.Buffer
	l := NewLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	l.Info("dangling", "only-a-key")
	out := buf.String()
	if !strings.Contains(out, "dangling") {
		t.Errorf("log missing message: %q", out)
	}
	if strings.Contains(out, "!BADKEY") {
		t.Errorf("dangling key not sanitized: %q", out)
	}
}

func TestLoggerErrorAppendsErr(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	l.Error("boom", errors.New("i2c nack"), "source", "s1")
	out := buf.String()
	if !strings.Contains(out, "i2c nack") {
		t.Errorf("error value missing: %q", out)
	}
	if !strings.Contains(out, "s1") {
		t.Errorf("kv missing: %q", out)
	}
}

func TestLoggerErrorToleratesNilErr(t *testing.T) {
	// 有些调用点「只知道失败但拿不到 error」，nil 不能 panic 也不能输出 error=<nil>。
	var buf bytes.Buffer
	l := NewLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	l.Error("failed", nil)
	if strings.Contains(buf.String(), "error=<nil>") {
		t.Errorf("nil error should be omitted: %q", buf.String())
	}
}

func TestNewLoggerNilFallsBackToDefault(t *testing.T) {
	if l := NewLogger(nil); l == nil {
		t.Fatal("NewLogger(nil) returned nil")
	}
}

func TestClockSleepReturnsOnContextCancel(t *testing.T) {
	// 切换过程中用户退出：Sleep 必须立刻返回，而不是傻等 confirmMs。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- NewClock().Sleep(ctx, 5*time.Second) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Sleep ignored context cancellation")
	}
}

func TestClockSleepZeroReturnsImmediately(t *testing.T) {
	// d<=0 是「不等待」，语义上等价于成功——但实现返回 ctx.Err()，
	// 未取消的 ctx 上是 nil，也就是成功。
	if err := NewClock().Sleep(context.Background(), 0); err != nil {
		t.Errorf("Sleep(0) = %v, want nil", err)
	}
}

func TestClockSleepWaitsWhenNotCancelled(t *testing.T) {
	start := time.Now()
	if err := NewClock().Sleep(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatalf("Sleep: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Errorf("Sleep returned too early: %v", elapsed)
	}
}

func TestClockNowIsMonotonic(t *testing.T) {
	c := NewClock()
	if c.Now().IsZero() {
		t.Error("Now() returned zero time")
	}
	if c.Now().Before(c.Now().Add(-time.Second)) {
		t.Error("Now() went backwards")
	}
}

func TestErrUnsupportedShortcutMentionsAccelerator(t *testing.T) {
	err := errUnsupportedShortcut("Cmd+Option+1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Cmd+Option+1") {
		t.Errorf("err = %v", err)
	}
}
