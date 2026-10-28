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
	"os"
	"strings"
	"sync"

	wails "github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/znahovo/switch-display/internal/application/configapp"
	"github.com/znahovo/switch-display/internal/application/displayapp"
	"github.com/znahovo/switch-display/internal/application/linkapp"
	"github.com/znahovo/switch-display/internal/application/logapp"
	"github.com/znahovo/switch-display/internal/application/switchapp"
	"github.com/znahovo/switch-display/internal/domain/switching"
	"github.com/znahovo/switch-display/internal/infrastructure/ddc"
	"github.com/znahovo/switch-display/internal/infrastructure/link/deskflow"
	"github.com/znahovo/switch-display/internal/infrastructure/link/discovery"
	"github.com/znahovo/switch-display/internal/infrastructure/logging"
	"github.com/znahovo/switch-display/internal/infrastructure/persistence"
	"github.com/znahovo/switch-display/internal/infrastructure/platform"
	"github.com/znahovo/switch-display/internal/presentation/tray"
	"github.com/znahovo/switch-display/internal/presentation/wailsservice"
)

// App 持有装配好的所有组件，供 main.go 与托盘使用。
type App struct {
	DisplayService *wailsservice.DisplayService
	LinkService    *wailsservice.LinkService
	LogService     *wailsservice.LogService
	Displays       *displayapp.Service
	Config         *configapp.Service
	Switcher       *switchapp.Service
	Link           *linkapp.Service
	Tray           *tray.Tray

	// wailsApp 只用于「关窗口即退出」：两个平台的
	// ApplicationShouldTerminateAfterLastWindowClosed / DisableQuitOnLastWindowClosed
	// 都是 false/true（否则收进托盘就会变成退出），所以关掉最后一个窗口不会
	// 自动结束进程，必须显式 Quit。
	wailsApp *wails.App

	hotkeys *platform.Hotkeys
	events  *platform.Events
	watcher *displayapp.Watcher
	log     *platform.Logger
	logs    *logging.Bus
}

// Build 装配整个应用。
//
// 顺序是有意义的：先建基础设施（DDC、存储、时钟、日志），再建用例，
// 最后建表现层——因为表现层依赖用例，用例依赖基础设施。
func Build(wailsApp *wails.App) (*App, error) {
	clock := platform.NewClock()
	events := platform.NewEvents(wailsApp)
	hotkeys := platform.NewHotkeys(wailsApp)

	// ⓪ 日志总线：界面上的「日志」页是用户唯一能看到的地方
	// （双击打开的 .app/.exe 没有终端），而共享键鼠的问题往往只体现在
	// deskflow-core 的输出里。总线已在 wails.New 之前由 main.go 建好，
	// 这里只是把事件发布者接上（那时它还不存在）。
	logs := SystemBus()
	logs.SetEvents(events)
	log := platform.NewLogger(SystemLogger())

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

	// ③½ 局域网共享：Deskflow core 驱动 + UDP 发现。
	//
	// 存储单独一份 link.json：它是运维态（设备 ID、配对记录），
	// 丢了最多重新配对一次，不该让它参与 config.json 的版本迁移。
	linkRepo, err := persistence.NewLinkStore("")
	if err != nil {
		return nil, fmt.Errorf("init link store: %w", err)
	}
	deskflowDir, err := persistence.DefaultDeskflowDir()
	if err != nil {
		return nil, fmt.Errorf("init deskflow dir: %w", err)
	}
	coreEngine := deskflow.NewEngine(deskflow.EngineDeps{
		Dir:  deskflowDir,
		Log:  log,
		Sink: logs,
	})
	transport := discovery.New(discovery.Deps{Log: log})
	linkSvc := linkapp.New(linkapp.Deps{
		Repo:      linkRepo,
		Transport: transport,
		Core:      coreEngine,
		Events:    events,
		Log:       log,
	})

	// ③¾ 日志用例：只读缓冲，向界面提供游标查询。
	logsSvc := logapp.New(logapp.Deps{Buffer: logs})

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
		Link:     linkSvc,
		Tray:     trayView,
		wailsApp: wailsApp,
		hotkeys:  hotkeys,
		events:   events,
		log:      log,
		logs:     logs,
	}
	svc := wailsservice.New(wailsservice.Deps{
		Displays:  displays,
		Config:    config,
		Switcher:  switcher,
		OnStartup: app.Start,
	})
	app.DisplayService = svc
	app.LinkService = wailsservice.NewLink(wailsservice.LinkDeps{
		Link:      linkSvc,
		OnStartup: app.startLink,
	})
	app.LogService = wailsservice.NewLogService(wailsservice.LogDeps{Logs: logsSvc})

	hotkeys.OnFire(func(sourceID string) {
		go func() {
			if _, err := switcher.SwitchByID(context.Background(), sourceID); err != nil {
				log.Error("hotkey switch failed", err, "source", sourceID)
			}
		}()
	})

	return app, nil
}

// 全进程共用的日志总线。
//
// 为什么是包级单例而不是每次 Build 新建一份：Wails 在 wails.New() 时就把
// 自己的 logger 固定下来了，之后改 slog.SetDefault 对它无效。要让
// WebView2 / 资源服务器这些系统消息也进日志页，必须把这个 logger 在
// wails.New 之前交给 Wails——而那时 Build 还没跑。单例是唯一能让
// main.go 和 Build 共用同一条总线、又不把总线细节泄漏给 main.go 的方式。
var (
	systemOnce   sync.Once
	systemLogs   *logging.Bus
	systemLogger *slog.Logger
	wailsLogger  *slog.Logger
)

// SystemLogger 返回本应用业务代码用的 slog.Logger（「终端 + 界面」双写）。
//
// 它同时被装成 slog 的默认 logger，于是所有 slog.Default() 调用点
// （含第三方库）都会进日志页。
func SystemLogger() *slog.Logger {
	initSystemLogs()
	return systemLogger
}

// WailsLogger 返回交给 Wails 框架自己的 logger。
//
// 它和 SystemLogger 分开，是因为框架的日志绝大多数与用户无关：每个静态
// 资源请求一条 "Handling request"、每个前端绑定调用一条 DEBUG，启动时
// 还会刷二十来条 "Registering bound method"。这些混进来会把日志页的
// 有效信息埋掉（用户的原话是「很多无用的日志 还有很多斜线那种」）。
// 但框架的少数几行恰恰关键（Build Info / Platform Info / 任何 WARN 以上），
// 所以不是简单关掉，而是按消息筛。
func WailsLogger() *slog.Logger {
	initSystemLogs()
	return wailsLogger
}

// SystemBus 返回与 SystemLogger 共用的日志总线。
func SystemBus() *logging.Bus {
	initSystemLogs()
	return systemLogs
}

// initSystemLogs 构造总线，幂等。
func initSystemLogs() {
	systemOnce.Do(func() {
		path, pathErr := persistence.DefaultLogPath()
		if pathErr != nil {
			// 落盘路径取不到时退化成「只在内存里」：界面里的日志仍然可用，
			// 不能让一个打不开的目录把应用拖死。
			path = ""
			fmt.Fprintf(os.Stderr, "log file unavailable: %v\n", pathErr)
		}
		systemLogs = logging.New(logging.Deps{FilePath: path})
		if msg := systemLogs.FileError(); msg != "" {
			fmt.Fprintf(os.Stderr, "log file unavailable: %s\n", msg)
		}

		term := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})

		// 业务日志：全部进总线。本项目自己的 Debug 只有六处（轮询失败、
		// 丢弃坏包、Windows 托盘越界），数量少且都与排障相关。
		systemLogger = slog.New(logging.Tee(term, systemLogs.Handler(sourceApp, slog.LevelDebug)))
		slog.SetDefault(systemLogger)

		// 框架日志：终端照旧全集（开发时仍要看得到），界面只留有用的。
		wailsLogger = slog.New(logging.Tee(term,
			logging.Filtered(systemLogs.Handler(sourceWails, slog.LevelInfo), keepWailsLog),
		))
	})
}

// 日志来源标签。前端按这两个值区分「自己的代码」和「框架」。
const (
	sourceApp   = "app"
	sourceWails = "wails"
)

// wailsNoise 是 Wails 里「每次调用都打一条、但排障时毫无价值」的消息前缀。
//
// 来源是 wails v3.0.0-beta.34：assetserver 的 "Handling request"、"Asset Request:"，
// application 的 "handleWebViewRequest"、"Registering bound method"、
// "Starting up service"、"Binding call complete"。它们的级别写死在
// Wails 源码里（Info/Debug），调用方改不了，只能在 handler 这一层拦。
var wailsNoise = []string{
	"Handling request",
	"Asset Request",
	"handleWebViewRequest",
	"Registering bound method",
	"Starting up service",
	"Binding call complete",
}

// keepWailsLog 决定一条框架日志是否进界面。
//
// 规则很简单：WARN 及以上全留（真正的问题），INFO 只留白名单里的
// （Build Info / Platform Info 是用户报 bug 时最有用的两行）。
func keepWailsLog(level slog.Level, msg string) bool {
	if level >= slog.LevelWarn {
		return true
	}
	for _, noisy := range wailsNoise {
		if strings.Contains(msg, noisy) {
			return false
		}
	}
	return level >= slog.LevelInfo
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

// startLink 启动共享：加载身份、生成证书、开始 UDP 发现。
//
// 失败不阻断整个应用启动——用户可能只是没装 Deskflow，或本机端口被占，
// 显示器切换功能不该因此不可用。
func (a *App) startLink(ctx context.Context) error {
	if err := a.Link.Start(ctx); err != nil {
		a.log.Error("link service unavailable", err)
	}
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
		// 用户关掉了「关闭时隐藏到托盘」时，红叉就是真退出。
		//
		// 这里必须显式 Quit：两个平台都把「关掉最后一个窗口」的自动退出关掉了
		// （macOS 的 ApplicationShouldTerminateAfterLastWindowClosed、
		// Windows 的 DisableQuitOnLastWindowClosed），因为不关的话「收进托盘」
		// 会变成「退出程序」。反过来，用户想要真退出时就得自己调 Quit，
		// 否则窗口没了、进程还在后台隐身。
		if !a.Config.Snapshot().Settings.HideToTrayOnClose {
			if a.wailsApp != nil {
				a.wailsApp.Quit()
			}
			return
		}
		// 默认语义是「收起来」：关闭窗口后进程仍活着（托盘还在），
		// 不拦截的话用户就再也找不到入口了。真正退出走托盘菜单。
		e.Cancel()
		a.Tray.HideWindow()
	})
}
