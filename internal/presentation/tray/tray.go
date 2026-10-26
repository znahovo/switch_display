// Package tray 是菜单栏图标的展示层：菜单结构、打勾状态、点击转发。
//
// 它不碰 DDC（R4：presentation 不许 import infrastructure），所有硬件动作
// 都转给用例层。菜单在每次配置变化或切换结束后**全量重建**（ADR-005）：
// 增量更新原生菜单在 macOS 上状态容易不同步，而这里的菜单项只有十几条，
// 重建成本可以忽略。
package tray

import (
	"context"
	_ "embed"
	"fmt"
	"sync"

	wails "github.com/wailsapp/wails/v3/pkg/application"

	"github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/application/displayapp"
	"github.com/znahovo/switch-display/internal/application/switchapp"
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

// iconPNG 是 44×44 的黑/透明图形。macOS 把它当 template image（按菜单栏
// 明暗自动反色）；Windows / Linux 直接当位图画，所以还要一份白色的
// iconWhitePNG 给深色任务栏用。选哪个由 icon_*.go 按平台决定。
//
//go:embed assets/icon.png
var iconPNG []byte

//go:embed assets/icon-white.png
var iconWhitePNG []byte

// ConfigReader 是托盘对配置用例的最小视图。
type ConfigReader interface {
	Snapshot() source.Snapshot
}

// Switcher 是托盘对切换用例的最小视图。
type Switcher interface {
	SwitchByID(ctx context.Context, id string) (switchapp.Result, error)
}

// Deps 构造依赖。
type Deps struct {
	App      *wails.App
	Config   ConfigReader
	Switcher Switcher
	Displays *displayapp.Service
	Events   application.EventPublisher
	Log      application.Logger
}

// Tray 持有系统托盘与最近一次构建出的菜单。
type Tray struct {
	app      *wails.App
	config   ConfigReader
	switcher Switcher
	displays *displayapp.Service
	events   application.EventPublisher
	log      application.Logger

	tray *wails.SystemTray

	mu      sync.Mutex
	window  *wails.WebviewWindow
	busy    bool   // 正在切换：菜单项临时禁用，避免连点
	current string // 当前源 ID，仅供菜单打勾（读回失败时保留上次值）
}

// New 构造托盘。
func New(d Deps) *Tray {
	return &Tray{
		app:      d.App,
		config:   d.Config,
		switcher: d.Switcher,
		displays: d.Displays,
		events:   d.Events,
		log:      d.Log,
	}
}

// SetWindow 注入主窗口。窗口在 main.go 里创建（它属于装配根），
// 而托盘在 Build 阶段就建好了，所以用注入而不是构造参数。
func (t *Tray) SetWindow(w *wails.WebviewWindow) {
	t.mu.Lock()
	t.window = w
	t.mu.Unlock()
}

// Start 创建系统托盘并渲染第一次菜单。
//
// 必须在 Wails Run 之前调用（Build 阶段）：未 Run 时 SystemTray 的
// 创建只是排队，Run 之后统一执行，因此这里不会阻塞启动。
func (t *Tray) Start() error {
	if t.app == nil || t.app.SystemTray == nil {
		return fmt.Errorf("system tray unavailable")
	}
	tray := t.app.SystemTray.New()
	applyIcon(tray)
	tray.SetTooltip("Switch Display")
	tray.OnClick(func() { t.ShowWindow() })
	t.tray = tray

	// 左键点击托盘图标直接弹菜单（macOS 用户的习惯），右键也走菜单。
	t.Refresh()

	// 配置变化（前端保存、导入、热插拔）后重建菜单：菜单是配置的投影，
	// 不重建就会显示过期的源列表。
	if t.app.Event != nil {
		t.app.Event.On(application.EventStateChanged, func(*wails.CustomEvent) {
			t.Refresh()
		})
	}
	return nil
}

// HideWindow 把主窗口收进后台（关窗口的语义）。
func (t *Tray) HideWindow() {
	t.mu.Lock()
	w := t.window
	t.mu.Unlock()
	if w == nil {
		return
	}
	w.Hide()
}

// ShowWindow 显示并聚焦主窗口。
func (t *Tray) ShowWindow() {
	t.mu.Lock()
	w := t.window
	t.mu.Unlock()
	if w == nil {
		return
	}
	// WebviewWindow.Show/Focus 内部自己会 dispatch 到主线程，这里不必再包一层。
	w.Show()
	w.Focus()
}

// Refresh 重画菜单：先用缓存值立刻画一版，再去后台读一次真正的当前输入源
// 并纠正打勾。
//
// 分成两步是因为 DDC 读回会阻塞几十毫秒，而这条路径由 UI 事件驱动
// （保存配置、切换结束、托盘点击），卡在主线程上会让菜单明显迟钝。
func (t *Tray) Refresh() {
	t.rebuild()
	go t.resyncCurrent()
}

// rebuild 只用内存状态画菜单，不碰硬件。
func (t *Tray) rebuild() {
	if t.tray == nil {
		return
	}
	t.tray.SetMenu(t.buildMenu())
}

// buildMenu 按当前内存状态构造菜单。
//
// 与 rebuild 分开是为了能单测菜单结构：这里只碰 *wails.Menu（纯数据结构），
// 不需要真实的 SystemTray，也不需要 Wails 运行时。
func (t *Tray) buildMenu() *wails.Menu {
	snap := t.config.Snapshot()
	text := labelsFor(snap.Settings.Language)

	t.mu.Lock()
	busy := t.busy
	current := t.current
	t.mu.Unlock()

	menu := wails.NewMenu()
	menu.Add(text.Open).OnClick(func(*wails.Context) { t.ShowWindow() })
	menu.AddSeparator()

	if len(snap.Sources) == 0 {
		item := menu.Add(text.NoSources)
		item.SetEnabled(false)
	} else {
		for _, src := range snap.Sources {
			src := src
			// AddRadio 的第二个参数是「是否选中」而不是「是否可用」——
			// 选中态由下面的 SetChecked 决定，这里先占位 false 再由它覆盖。
			item := menu.AddRadio(sourceLabel(src), false)
			item.SetChecked(string(src.ID) == current)
			item.SetEnabled(!busy)
			applyAccelerator(item, src.Shortcut)
			item.OnClick(func(*wails.Context) { t.switchTo(string(src.ID), src.Name) })
		}
	}

	// 「切回本机」只在用户标记了本机源之后出现（selfSourceId 为空则整项省略）。
	if selfID := snap.Settings.SelfSourceID; selfID != "" {
		if self, ok := snap.FindByID(source.ID(selfID)); ok {
			menu.AddSeparator()
			item := menu.Add(text.BackToSelf)
			item.SetEnabled(!busy)
			applyAccelerator(item, self.Shortcut)
			item.OnClick(func(*wails.Context) { t.switchTo(string(self.ID), self.Name) })
		}
	}

	menu.AddSeparator()
	menu.Add(text.Settings).OnClick(func(*wails.Context) {
		t.ShowWindow()
		t.events.Emit(application.EventOpenSettings, nil)
	})
	menu.Add(text.Rescan).OnClick(func(*wails.Context) {
		go t.rescan()
	})
	menu.AddSeparator()
	menu.Add(text.Quit).OnClick(func(*wails.Context) {
		if t.app != nil {
			t.app.Quit()
		}
	})

	return menu
}

// applyAccelerator 只在真的有快捷键时才设置。
//
// 传空字符串会让 Wails 打一条 invalid accelerator 的错误日志，而绝大多数源
// 本来就没绑快捷键——不能让它污染日志。
func applyAccelerator(item *wails.MenuItem, accel string) {
	if accel == "" {
		return
	}
	item.SetAccelerator(accel)
}

// resyncCurrent 用 DDC 读回纠正「当前源」。
//
// 读回优先的理由：用户可能用显示器自己的按键切了口，或另一台机器切走了，
// 此时「最近一次点击」已经说谎了。但输入源读回在不少显示器上是垃圾值
// （见 plan.md 风险），所以读不出来时保留上一次的乐观值，不把打勾清空。
func (t *Tray) resyncCurrent() {
	snap := t.config.Snapshot()
	if len(snap.Sources) == 0 || t.displays == nil {
		return
	}

	// 同一台显示器可能有多个源（不同端口），每台只读一次。
	probed := make(map[display.DisplayID]uint16, len(snap.Sources))
	var order []display.DisplayID
	for _, src := range snap.Sources {
		if _, ok := probed[src.DisplayID]; ok {
			continue
		}
		order = append(order, src.DisplayID)
		rb, err := t.displays.Probe(context.Background(), string(src.DisplayID), byte(display.VCPInputSource), false)
		if err != nil {
			continue
		}
		probed[src.DisplayID] = rb.Value
	}

	found := ""
	for _, id := range order {
		value, ok := probed[id]
		if !ok || value == 0 {
			continue
		}
		for _, src := range snap.Sources {
			if src.DisplayID == id && uint16(src.Input.Value) == value {
				found = string(src.ID)
				break
			}
		}
		if found != "" {
			break
		}
	}
	if found == "" {
		return
	}

	t.mu.Lock()
	changed := t.current != found
	t.current = found
	t.mu.Unlock()
	if changed {
		t.rebuild()
	}
}

// switchTo 在后台执行一次切换，结束后重建菜单让打勾跟上。
func (t *Tray) switchTo(sourceID, name string) {
	t.mu.Lock()
	if t.busy {
		t.mu.Unlock()
		return
	}
	t.busy = true
	t.mu.Unlock()

	t.rebuild()
	t.log.Info("tray switch requested", "source", sourceID, "name", name)

	go func() {
		defer func() {
			t.mu.Lock()
			t.busy = false
			t.mu.Unlock()
			t.Refresh()
		}()

		res, err := t.switcher.SwitchByID(context.Background(), sourceID)
		if err != nil {
			t.log.Error("tray switch failed", err, "source", sourceID)
			return
		}
		if res.Domain.Outcome == "failed" {
			t.log.Error("tray switch rejected by display", res.Domain.Err, "source", sourceID)
			return
		}
		// 切成功后立刻乐观地打勾：下面 Refresh 里的读回会纠正它。
		t.mu.Lock()
		t.current = sourceID
		t.mu.Unlock()
		t.log.Info("tray switch done", "source", sourceID, "outcome", res.Domain.Outcome)
	}()
}

// rescan 重新枚举显示器并刷新菜单。
func (t *Tray) rescan() {
	if t.displays != nil {
		if _, err := t.displays.Refresh(context.Background()); err != nil {
			t.log.Error("tray rescan failed", err)
		}
	}
	t.events.Emit(application.EventStateChanged, nil)
	t.Refresh()
}

// sourceLabel 是菜单里显示的一行文字：名字 + 端口短标签，
// 让「Windows」和「Windows（第二个口）」这种同名源也能区分。
func sourceLabel(src source.SignalSource) string {
	return fmt.Sprintf("%s (%s)", src.Name, portKeyLabel(src))
}

// portKeyLabel 给出端口短标签；识别不出预设时退回原始数值。
func portKeyLabel(src source.SignalSource) string {
	if key := source.MatchPreset(src.Input); key != "" {
		return presetLabel(key)
	}
	return fmt.Sprintf("%d", src.Input.Value)
}

// presetLabel 把预设 key 转成好看的大写短标签。
func presetLabel(key string) string {
	switch key {
	case "dp1", "alt-dp1":
		return "DP1"
	case "dp2", "alt-dp2":
		return "DP2"
	case "hdmi1", "alt-hdmi1":
		return "HDMI1"
	case "hdmi2", "alt-hdmi2":
		return "HDMI2"
	case "usbc", "alt-usbc":
		return "USB-C"
	case "dvi1":
		return "DVI"
	case "vga":
		return "VGA"
	}
	return key
}
