package bootstrap

import (
	"context"
	"testing"

	wails "github.com/wailsapp/wails/v3/pkg/application"
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
	if got.Displays == nil || got.Config == nil || got.Switcher == nil {
		t.Error("use case services not wired")
	}
	if got.Tray == nil {
		t.Error("tray not wired")
	}
	if got.hotkeys == nil || got.events == nil || got.log == nil {
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
