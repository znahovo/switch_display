package tray

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	wails "github.com/wailsapp/wails/v3/pkg/application"

	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

// renderedAccel 给出 Wails 读完 accelerator 后 GetAccelerator 返回的字符串。
//
// 存盘格式是跨平台的（"Cmd+Option+9"），但 Wails 会按运行平台把修饰键重写成
// 本地名字并按字母序排列：macOS 是 "Cmd+Option+9"，Windows / Linux 把
// Cmd→Ctrl、Option→Alt，排序后变成 "Alt+Ctrl+9"。所以断言不能硬编码 macOS
// 的写法，否则这个用例只能在 macOS 上过。
func renderedAccel(shortcut string) string {
	parts := strings.Split(shortcut, "+")
	key := parts[len(parts)-1]
	if runtime.GOOS == "darwin" {
		return "Cmd+Option+" + key
	}
	return "Alt+Ctrl+" + key
}

func TestLabelsForPicksChineseByPrefix(t *testing.T) {
	// 显式设置环境变量：判断空 lang 的兜底行为时不能依赖跑测试的机器
	// 恰好是中文环境，否则这个用例在 CI 上是随机失败的。
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")

	cases := []struct {
		lang string
		want string
	}{
		{"zh-CN", "退出"},
		{"zh", "退出"},
		{"ZH-TW", "退出"},
		{"en", "Quit"},
		{"", "Quit"}, // 环境变量里没有 zh 时退回英文
	}
	for _, c := range cases {
		if got := labelsFor(c.lang).Quit; got != c.want {
			t.Errorf("labelsFor(%q).Quit = %q, want %q", c.lang, got, c.want)
		}
	}
}

func TestLabelsForFollowsOSLocale(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := labelsFor("").Quit; got != "退出" {
		t.Errorf("labelsFor(\"\").Quit = %q, want 退出 (LANG=zh_CN)", got)
	}
}

func TestSourceLabelIncludesPort(t *testing.T) {
	src := source.SignalSource{
		ID:        "s1",
		Name:      "Windows",
		DisplayID: "d1",
		Input:     display.InputValue{VCP: display.VCPInputSource, Value: 17},
	}
	if got, want := sourceLabel(src), "Windows (HDMI1)"; got != want {
		t.Errorf("sourceLabel = %q, want %q", got, want)
	}
}

func TestPortKeyLabelFallsBackToRawValue(t *testing.T) {
	// 27 是 USB-C 的标准值，但预设表里 usbc=27 只在非 alt 时匹配；
	// 这里用一个表里没有的值确认退回原始数字。
	src := source.SignalSource{Input: display.InputValue{VCP: display.VCPInputSource, Value: 99}}
	if got, want := portKeyLabel(src), "99"; got != want {
		t.Errorf("portKeyLabel = %q, want %q", got, want)
	}
}

func TestPortKeyLabelMapsAltPresets(t *testing.T) {
	src := source.SignalSource{Input: display.InputValue{VCP: display.VCPInputSourceAlt, Value: 208, Alt: true}}
	if got, want := portKeyLabel(src), "DP1"; got != want {
		t.Errorf("portKeyLabel = %q, want %q", got, want)
	}
}

// ---- 菜单结构 ----

// forceEnglish 把系统区域固定成英文，让断言不受跑测试的机器语言影响。
// 菜单文案走的是「配置语言 > 系统区域」的兜底，不打桩就变成环境依赖测试。
func forceEnglish(t *testing.T) {
	t.Helper()
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
}

type fakeConfig struct{ snap source.Snapshot }

func (c fakeConfig) Snapshot() source.Snapshot { return c.snap }

// menuLabels 把菜单拍平成 "label" / "---"（分隔线）序列，便于整体断言。
func menuLabels(m *wails.Menu) []string {
	var out []string
	for i := 0; ; i++ {
		item := m.ItemAt(i)
		if item == nil {
			break
		}
		if item.IsSeparator() {
			out = append(out, "---")
			continue
		}
		out = append(out, item.Label())
	}
	return out
}

func newTestTray(snap source.Snapshot) *Tray {
	return New(Deps{Config: fakeConfig{snap: snap}})
}

func snapshotWith(t *testing.T, sources []source.SignalSource, mutate func(*source.Settings)) source.Snapshot {
	t.Helper()
	snap := source.EmptySnapshot()
	snap.Sources = sources
	if mutate != nil {
		mutate(&snap.Settings)
	}
	return snap
}

func TestBuildMenuEmptyHasNoSourceItems(t *testing.T) {
	forceEnglish(t)
	tr := newTestTray(snapshotWith(t, nil, nil))
	labels := menuLabels(tr.buildMenu())

	// 空配置：打开 + 分隔 + 「还没有信号源」 + 分隔 + 设置/重新检测 + 分隔 + 退出
	want := []string{
		"Open Switch Display", "---", "No signal sources yet",
		"---", "Settings…", "Rescan Displays", "---", "Quit",
	}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("labels = %v\n want %v", labels, want)
	}
	// 「还没有信号源」必须禁用，否则点了没反应像 bug。
	item := tr.buildMenu().FindByLabel("No signal sources yet")
	if item == nil || item.Enabled() {
		t.Errorf("placeholder item = %+v", item)
	}
}

func TestBuildMenuListsSourcesAsRadios(t *testing.T) {
	forceEnglish(t)
	a := source.SignalSource{ID: "s1", Name: "Windows",
		Input: display.InputValue{VCP: display.VCPInputSource, Value: 15}}
	b := source.SignalSource{ID: "s2", Name: "Mac",
		Input: display.InputValue{VCP: display.VCPInputSource, Value: 18}}
	tr := newTestTray(snapshotWith(t, []source.SignalSource{a, b}, nil))
	menu := tr.buildMenu()

	win := menu.FindByLabel("Windows (DP1)")
	if win == nil {
		t.Fatal("Windows item missing")
	}
	if !win.IsRadio() {
		t.Error("source items must be radio (single choice)")
	}
	if win.Enabled() != true {
		t.Error("source item should be enabled when idle")
	}
	if menu.FindByLabel("Mac (HDMI2)") == nil {
		t.Error("Mac item missing")
	}
	// 两个源必须连续排列，否则 macOS 的 Radio 分组会把它们拆成两组
	// （分组规则见 Wails menu.processRadioGroups）。
	labels := menuLabels(menu)
	if labels[2] != "Windows (DP1)" || labels[3] != "Mac (HDMI2)" {
		t.Errorf("sources are not contiguous: %v", labels)
	}
}

func TestBuildMenuChecksCurrentSource(t *testing.T) {
	forceEnglish(t)
	a := source.SignalSource{ID: "s1", Name: "A", Input: display.InputValue{Value: 15}}
	b := source.SignalSource{ID: "s2", Name: "B", Input: display.InputValue{Value: 18}}
	tr := newTestTray(snapshotWith(t, []source.SignalSource{a, b}, nil))
	tr.current = "s2"

	menu := tr.buildMenu()
	if got := menu.FindByLabel("A (DP1)"); got.Checked() {
		t.Error("A should not be checked")
	}
	if got := menu.FindByLabel("B (HDMI2)"); !got.Checked() {
		t.Error("B should be checked")
	}
}

func TestBuildMenuAddsBackToSelfOnlyWhenConfigured(t *testing.T) {
	forceEnglish(t)
	self := source.SignalSource{ID: "s1", Name: "Mac", Shortcut: "Cmd+Option+9",
		Input: display.InputValue{VCP: display.VCPInputSource, Value: 18}}
	other := source.SignalSource{ID: "s2", Name: "PC",
		Input: display.InputValue{VCP: display.VCPInputSource, Value: 15}}

	// 文案按平台变（macOS 是 "Back to This Mac"，其它平台是 "Back to This PC"），
	// 所以断言用 labelsFor 取，而不是硬编码——否则这个用例只能在 macOS 上过。
	backToSelf := labelsFor("en").BackToSelf

	// 未标记本机源：不出现「切回本机」。
	tr := newTestTray(snapshotWith(t, []source.SignalSource{self, other}, nil))
	if tr.buildMenu().FindByLabel(backToSelf) != nil {
		t.Error("back-to-self should be hidden when unset")
	}

	// 标记之后出现，并且继承该源的快捷键（菜单上显示提示）。
	tr = newTestTray(snapshotWith(t, []source.SignalSource{self, other}, func(s *source.Settings) {
		s.SelfSourceID = "s1"
	}))
	item := tr.buildMenu().FindByLabel(backToSelf)
	if item == nil {
		t.Fatal("back-to-self missing")
	}
	if got, want := item.GetAccelerator(), renderedAccel("Cmd+Option+9"); got != want {
		t.Errorf("accelerator = %q, want %q", got, want)
	}
	if item.IsRadio() {
		t.Error("back-to-self is a plain item, not part of the radio group")
	}
}

func TestBuildMenuDanglingSelfSourceIsIgnored(t *testing.T) {
	forceEnglish(t)
	// 配置里引用了不存在的源：菜单不能崩，也必须不显示该项。
	tr := newTestTray(snapshotWith(t, []source.SignalSource{
		{ID: "s1", Name: "A", Input: display.InputValue{Value: 15}},
	}, func(s *source.Settings) { s.SelfSourceID = "ghost" }))

	if tr.buildMenu().FindByLabel(labelsFor("en").BackToSelf) != nil {
		t.Error("dangling self source should be skipped")
	}
}

func TestBuildMenuAcceleratorOnlyWhenSet(t *testing.T) {
	forceEnglish(t)
	// 没绑快捷键的源不能让 Wails 去 parse 空串（会打错误日志）。
	plain := source.SignalSource{ID: "s1", Name: "A", Input: display.InputValue{Value: 15}}
	tr := newTestTray(snapshotWith(t, []source.SignalSource{plain}, nil))
	if got := tr.buildMenu().FindByLabel("A (DP1)").GetAccelerator(); got != "" {
		t.Errorf("accelerator = %q, want empty", got)
	}

	bound := plain
	bound.Shortcut = "Cmd+Option+2"
	tr = newTestTray(snapshotWith(t, []source.SignalSource{bound}, nil))
	if got, want := tr.buildMenu().FindByLabel("A (DP1)").GetAccelerator(), renderedAccel("Cmd+Option+2"); got != want {
		t.Errorf("accelerator = %q, want %q", got, want)
	}
}

func TestBuildMenuDisablesSourcesWhenBusy(t *testing.T) {
	forceEnglish(t)
	a := source.SignalSource{ID: "s1", Name: "A", Input: display.InputValue{Value: 15}}
	tr := newTestTray(snapshotWith(t, []source.SignalSource{a}, nil))
	tr.busy = true

	menu := tr.buildMenu()
	if item := menu.FindByLabel("A (DP1)"); item.Enabled() {
		t.Error("source items must be disabled while a switch is in flight")
	}
	// 退出永远可用：卡住时用户还得能关掉程序。
	if item := menu.FindByLabel("Quit"); !item.Enabled() {
		t.Error("Quit must stay enabled while busy")
	}
}

func TestBuildMenuUsesConfiguredLanguage(t *testing.T) {
	tr := newTestTray(snapshotWith(t, nil, func(s *source.Settings) { s.Language = "zh-CN" }))
	labels := menuLabels(tr.buildMenu())
	if labels[0] != "打开切换台" {
		t.Errorf("labels = %v, want Chinese labels", labels)
	}
	if labels[len(labels)-1] != "退出" {
		t.Errorf("last label = %q, want 退出", labels[len(labels)-1])
	}
}

func TestBuildMenuWithoutTrayDoesNotPanic(t *testing.T) {
	forceEnglish(t)
	// rebuild 在 tray 还没建起来时（启动早期）必须静默返回。
	tr := newTestTray(snapshotWith(t, nil, nil))
	tr.rebuild() // t.tray == nil
}
