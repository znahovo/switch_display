package bootstrap

import (
	"context"
	"log/slog"
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

func TestKeepWailsLog(t *testing.T) {
	// 这条规则决定了用户在日志页能看见多少框架噪音，值得单独钉住。
	cases := []struct {
		name  string
		level slog.Level
		msg   string
		want  bool
	}{
		{"asset request dropped", slog.LevelInfo, "Handling request", false},
		{"asset request debug dropped", slog.LevelDebug, "Asset Request:", false},
		{"webview request dropped", slog.LevelDebug, "handleWebViewRequest: Processing request", false},
		{"bound method dropped", slog.LevelDebug, "Registering bound method:", false},
		{"service startup dropped", slog.LevelDebug, "Starting up service:", false},
		{"binding call dropped", slog.LevelDebug, "Binding call complete:", false},
		{"build info kept", slog.LevelInfo, "Build Info:", true},
		{"platform info kept", slog.LevelInfo, "Platform Info:", true},
		{"warn always kept", slog.LevelWarn, "Handling request", true},
		{"error always kept", slog.LevelError, "Asset Request:", true},
		{"debug non-noise dropped", slog.LevelDebug, "something else", false},
	}
	for _, c := range cases {
		if got := keepWailsLog(c.level, c.msg); got != c.want {
			t.Errorf("%s: keepWailsLog(%v, %q) = %v, want %v", c.name, c.level, c.msg, got, c.want)
		}
	}
}

func TestWailsLoggerIsSeparateFromAppLogger(t *testing.T) {
	// 两者必须是不同的 logger：交给 Wails 的那份带噪音过滤，
	// 业务这份不能（否则我们自己的 Debug 也会被同类规则误伤）。
	if WailsLogger() == SystemLogger() {
		t.Fatal("WailsLogger and SystemLogger must be distinct")
	}
	app := wails.New(wails.Options{Name: "Switch Display Test"})
	got, err := Build(app)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	before := got.logs.Latest()

	// 框架噪音不该进总线，框架的关键信息该进。
	WailsLogger().Info("Handling request", "url", "/assets/index.js")
	WailsLogger().Info("Build Info:")
	batch := got.LogService.Query(before, 10)
	var msgs []string
	for _, e := range batch.Entries {
		if e.Source == sourceWails {
			msgs = append(msgs, e.Message)
		}
	}
	if len(msgs) != 1 || msgs[0] != "Build Info:" {
		t.Fatalf("wails entries = %v, want [Build Info:]", msgs)
	}
}
