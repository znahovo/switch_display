package bootstrap

import (
	"context"
	"testing"
	"time"

	wails "github.com/wailsapp/wails/v3/pkg/application"

	"github.com/znahovo/switch-display/internal/infrastructure/logging"
)

func TestBuildWiresAllComponents(t *testing.T) {
	// 装配根是唯一知道所有层的地方，漏接一个组件会让运行期空指针，
	// 所以这里只做一件事：确认每个成分都非 nil。
	app := wails.New(wails.Options{Name: "Switch Display Test"})
	got, err := Build(app)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got.DisplayService == nil {
		t.Error("DisplayService not wired")
	}
	if got.LinkService == nil {
		t.Error("LinkService not wired")
	}
	if got.LogService == nil {
		t.Error("LogService not wired")
	}
	if got.Displays == nil || got.Config == nil || got.Switcher == nil {
		t.Error("use case services not wired")
	}
	if got.Tray == nil {
		t.Error("tray not wired")
	}
	if got.hotkeys == nil || got.events == nil || got.log == nil || got.logs == nil {
		t.Error("infrastructure adapters not wired")
	}
}

func TestAttachWindowNilIsSafe(t *testing.T) {
	// main.go 在某些平台可能拿不到窗口；不能因此 panic。
	app := wails.New(wails.Options{Name: "Switch Display Test"})
	got, err := Build(app)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got.AttachWindow(nil)
}

func TestLogServiceReadsBus(t *testing.T) {
	// 日志页是用户唯一能看到子进程输出的地方，接线错了等于没有日志。
	app := wails.New(wails.Options{Name: "Switch Display Test"})
	got, err := Build(app)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// 总线是进程级单例（Wails 在 wails.New 时就要拿到 logger），
	// 所以这里用游标取增量，而不是假设缓冲是空的。
	before := got.logs.Latest()
	got.logs.Append(loggingEntry("boot message"))

	batch := got.LogService.Query(before, 10)
	if len(batch.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(batch.Entries))
	}
	if batch.Entries[0].Message != "boot message" {
		t.Errorf("message = %q", batch.Entries[0].Message)
	}
	// 游标回传后应当拿不到重复内容。
	if again := got.LogService.Query(batch.Latest, 10); len(again.Entries) != 0 {
		t.Errorf("cursor did not advance: %d entries", len(again.Entries))
	}
}

func TestStartFailsFastOnCancelledContext(t *testing.T) {
	// 退出中的启动不该继续读配置/枚举显示器。
	app := wails.New(wails.Options{Name: "Switch Display Test"})
	got, err := Build(app)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := got.Start(ctx); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// loggingEntry 造一条日志（省得在测试里 import 两个包）。
func loggingEntry(msg string) logging.Entry {
	return logging.Entry{Time: time.Now(), Level: logging.LevelInfo, Source: "app", Message: msg}
}
