package tray

import (
	"testing"

	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

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
