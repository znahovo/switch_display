// Package bootstrap 是组合根（composition root）的助手。
//
// 整个应用里只有它（和 main.go）知道所有层的存在：它按顺序构造
// 基础设施 → 领域服务 → 用例 → 表现层，然后注入。任何一层都不许
// 反向 import 别的层来「自己找依赖」（architecture.md §2 R5）。
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	wails "github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/znahovo/switch-display/internal/application/configapp"
	"github.com/znahovo/switch-display/internal/application/displayapp"
	"github.com/znahovo/switch-display/internal/application/switchapp"
	"github.com/znahovo/switch-display/internal/domain/switching"
	"github.com/znahovo/switch-display/internal/infrastructure/ddc"
	"github.com/znahovo/switch-display/internal/infrastructure/persistence"
	"github.com/znahovo/switch-display/internal/infrastructure/platform"
	"github.com/znahovo/switch-display/internal/presentation/tray"
	"github.com/znahovo/switch-display/internal/presentation/wailsservice"
)

// App 持有装配好的所有组件，供 main.go 与托盘使用。
type App struct {
	DisplayService *wailsservice.DisplayService
	Displays       *displayapp.Service
	Config         *configapp.Service
	Switcher       *switchapp.Service
	Tray           *tray.Tray

	hotkeys *platform.Hotkeys
	events  *platform.Events
	watcher *displayapp.Watcher
	log     *platform.Logger
}

// Build 装配整个应用。
//
// 顺序是有意义的：先建基础设施（DDC、存储、时钟、日志），再建用例，
// 最后建表现层——因为表现层依赖用例，用例依赖基础设施。
func Build(wailsApp *wails.App) (*App, error) {
	log := platform.NewLogger(slog.Default())
	clock := platform.NewClock()
	events := platform.NewEvents(wailsApp)
	hotkeys := platform.NewHotkeys(wailsApp)

	// ① 基础设施：真正的 DDC 端口 + JSON 存储。
	port := ddc.New()
	repo, err := persistence.NewJSONStore("")
	if err != nil {
		return nil, fmt.Errorf("init config store: %w", err)
	}

	// ② 领域服务：切换引擎（纯逻辑，只依赖端口与时钟）。
	engine := switching.NewService(port, clock)

	// ③ 用例层：显示器枚举、配置读写、切换。
	displays := displayapp.New(displayapp.Deps{Port: port, Log: log})
	config := configapp.New(configapp.Deps{
		Repo:   repo,
		Export: repo,
		Events: events,
		Hotkey: hotkeys,
		Log:    log,
		Clock:  clock,
	})
	switcher := switchapp.New(switchapp.Deps{
		Engine: engine,
		Config: config,
		Events: events,
		Log:    log,
	})

	// ⑤ 菜单栏：托盘是配置的投影，把它放最后建，这样它拿到的依赖都已就绪。
	// 真正的 Start（建图标 + 首次渲染）在 App.Start 里做，那时配置已经读进来。
	trayView := tray.New(tray.Deps{
		App:      wailsApp,
		Config:   config,
		Switcher: switcher,
		Displays: displays,
		Events:   events,
		Log:      log,
	})

	// ④ 表现层：暴露给前端。启动钩子在 Wails 服务启动阶段回调。
	app := &App{
		Displays: displays,
		Config:   config,
		Switcher: switcher,
		Tray:     trayView,
		hotkeys:  hotkeys,
		events:   events,
		log:      log,
	}
	svc := wailsservice.New(wailsservice.Deps{
		Displays:  displays,
		Config:    config,
		Switcher:  switcher,
		OnStartup: app.Start,
	})
	app.DisplayService = svc

	hotkeys.OnFire(func(sourceID string) {
		go func() {
			if _, err := switcher.SwitchByID(context.Background(), sourceID); err != nil {
				log.Error("hotkey switch failed", err, "source", sourceID)
			}
		}()
	})

	return app, nil
}

// Start 在应用启动后执行一次性的初始化：读配置、枚举显示器、启动热插拔轮询。
//
// 放在这里而不是 Build 里，是因为 Build 时 Wails 还没 Run，
// 枚举显示器能成功但事件发不出去（前端还没加载）。
func (a *App) Start(ctx context.Context) error {
	if _, err := a.Config.Load(ctx); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if _, err := a.Displays.Refresh(ctx); err != nil {
		// 枚举失败不阻断启动：可能只是当前没有外接显示器。
		a.log.Error("initial display enumeration failed", err)
	}

	// 按磁盘里的配置注册一次快捷键（启动路径不落盘，见 configapp.RebindHotkeys）。
	if warnings := a.Config.RebindHotkeys(); len(warnings) > 0 {
		for _, w := range warnings {
			a.log.Info("hotkey not available", "warning", w)
		}
	}

	// 托盘在配置读进来之后再建：菜单第一次渲染就要显示真实的源列表。
	if err := a.Tray.Start(); err != nil {
		// 托盘建不起来不阻断启动：窗口本身还能用。
		a.log.Error("system tray unavailable", err)
	}

	a.watcher = displayapp.NewWatcher(a.Displays, a.events, a.log, platform.NewClock(), 0)
	go a.watcher.Run(ctx)
	return nil
}

// AttachWindow 把主窗口交给托盘，并接管「关闭窗口 = 隐藏到后台」。
//
// 放在这里而不是托盘自己的 Start 里，是因为窗口由 main.go（装配根）创建，
// 而 bootstrap 不该反过来 import main。
func (a *App) AttachWindow(w *wails.WebviewWindow) {
	if w == nil {
		return
	}
	a.Tray.SetWindow(w)
	w.RegisterHook(events.Common.WindowClosing, func(e *wails.WindowEvent) {
		// 用户关掉了「关闭时隐藏到菜单栏」时，红叉就是真退出——
		// 否则窗口没了、托盘还在，用户会以为程序没关掉。
		if !a.Config.Snapshot().Settings.HideToTrayOnClose {
			return
		}
		// 默认语义是「收起来」：macOS 上关闭窗口后进程仍活着，
		// 不拦截的话用户就再也找不到入口了。真正退出走托盘菜单。
		e.Cancel()
		a.Tray.HideWindow()
	})
}
