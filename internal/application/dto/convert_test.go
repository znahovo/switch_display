package dto

import (
	"errors"
	"testing"

	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
	"github.com/znahovo/switch-display/internal/domain/switching"
)

func TestFromDisplayUsesLabel(t *testing.T) {
	d := display.Display{ID: "uuid-1", Name: "", Vendor: "XMI", Builtin: true}
	got := FromDisplay(d)
	if got.ID != "uuid-1" {
		t.Errorf("ID = %q", got.ID)
	}
	// Name 留空时前端收到的是 Label() 的兜底，而不是空串。
	if got.Name != "Display uuid-1" {
		t.Errorf("Name = %q, want label fallback", got.Name)
	}
	if !got.Builtin {
		t.Error("Builtin should be carried over")
	}
}

func TestFromDisplaysAlwaysReturnsSlice(t *testing.T) {
	// 空输入要返回空切片（非 nil），否则前端拿到 null 还要额外判空。
	if got := FromDisplays(nil); got == nil {
		t.Fatal("FromDisplays(nil) should be non-nil empty slice")
	}
}

func TestFromInputFillsPresetKey(t *testing.T) {
	got := FromInput(display.InputValue{VCP: display.VCPInputSource, Value: 17})
	if got.PresetKey != "hdmi1" {
		t.Errorf("PresetKey = %q, want hdmi1", got.PresetKey)
	}
	if got.VCP != 0x60 {
		t.Errorf("VCP = %#x", got.VCP)
	}

	custom := FromInput(display.InputValue{VCP: display.VCPInputSource, Value: 4242})
	if custom.PresetKey != "" {
		t.Errorf("custom PresetKey = %q, want empty", custom.PresetKey)
	}
}

func TestToInputDefaultsVCP(t *testing.T) {
	// 前端老数据/手写配置可能没有 vcp 字段：必须兜底成 0x60，
	// 否则会往 0x00 这个不存在的代码写。
	got := ToInput(InputDTO{Value: 17})
	if got.VCP != display.VCPInputSource {
		t.Errorf("VCP = %v, want 0x60", got.VCP)
	}
	if got.Value != 17 {
		t.Errorf("Value = %d", got.Value)
	}
}

func TestInputRoundTrip(t *testing.T) {
	cases := []display.InputValue{
		{VCP: display.VCPInputSource, Value: 17},
		{VCP: display.VCPInputSourceAlt, Value: 208, Alt: true},
		{VCP: display.VCPInputSource, Value: 65535},
	}
	for _, in := range cases {
		if got := ToInput(FromInput(in)); got != in {
			t.Errorf("round trip %+v -> %+v", in, got)
		}
	}
}

func TestSourceRoundTrip(t *testing.T) {
	in := source.SignalSource{
		ID:        "s1",
		Name:      "工作 PC",
		Color:     "#3b82f6",
		DisplayID: "uuid-1",
		Input:     display.InputValue{VCP: display.VCPInputSource, Value: 17},
		Shortcut:  "Cmd+Option+1",
		ConfirmMs: 1200,
	}
	if got := ToSource(FromSource(in)); got != in {
		t.Errorf("round trip\n got %+v\nwant %+v", got, in)
	}
}

func TestSettingsRoundTripPreservesSelfSource(t *testing.T) {
	in := source.Settings{
		Language:           "zh-CN",
		ConfirmAfterSwitch: false,
		DefaultConfirmMs:   900,
		HideToTrayOnClose:  false,
		SelfSourceID:       "s1",
	}
	if got := ToSettings(FromSettings(in)); got != in {
		t.Errorf("round trip\n got %+v\nwant %+v", got, in)
	}
}

func TestFromPresetsKeepsOrder(t *testing.T) {
	got := FromPresets(source.Presets)
	if len(got) != len(source.Presets) {
		t.Fatalf("len = %d, want %d", len(got), len(source.Presets))
	}
	for i := range got {
		if got[i].Key != source.Presets[i].Key || got[i].Value != source.Presets[i].Value {
			t.Errorf("index %d = %+v, want %+v", i, got[i], source.Presets[i])
		}
	}
}

func TestFromSwitchResultCurrentIsMinusOneWhenNoReadBack(t *testing.T) {
	// -1 是「没读到」的约定值，前端据此决定是否显示「当前」徽标。
	got := FromSwitchResult(switching.Result{Outcome: switching.OutcomeAppliedUnconfirmed})
	if got.Current != -1 {
		t.Errorf("Current = %d, want -1", got.Current)
	}
	if got.Outcome != "applied_unconfirmed" {
		t.Errorf("Outcome = %q", got.Outcome)
	}
}

func TestFromSwitchResultCarriesReadBack(t *testing.T) {
	rb := display.ReadBack{Supported: true, Current: 17, Maximum: 255}
	got := FromSwitchResult(switching.Result{
		SourceID: "s1", DisplayID: "uuid-1", Outcome: switching.OutcomeConfirmed,
		Attempts: 2, ReadBack: &rb,
	})
	if got.Current != 17 {
		t.Errorf("Current = %d, want 17", got.Current)
	}
	if got.Attempts != 2 {
		t.Errorf("Attempts = %d", got.Attempts)
	}
	if got.Message == "" {
		t.Error("Message should be filled")
	}
}

func TestMessageForEachOutcome(t *testing.T) {
	cases := []struct {
		in   switching.Result
		want string
	}{
		{switching.Result{Outcome: switching.OutcomeConfirmed}, "input switched and confirmed by display"},
		{switching.Result{Outcome: switching.OutcomeMismatch}, "display reports a different input source"},
		{switching.Result{Outcome: switching.OutcomeAppliedUnconfirmed}, "input switch sent (display cannot confirm)"},
		{switching.Result{Outcome: switching.OutcomeFailed}, "failed to switch input"},
		{
			switching.Result{Outcome: switching.OutcomeFailed, Err: errors.New("i2c timeout")},
			"failed to switch input: i2c timeout",
		},
		// 未知 outcome 直接透出原值，方便以后新增状态时至少不丢信息。
		{switching.Result{Outcome: switching.Outcome("weird")}, "weird"},
	}
	for _, c := range cases {
		if got := messageFor(c.in); got != c.want {
			t.Errorf("messageFor(%q) = %q, want %q", c.in.Outcome, got, c.want)
		}
	}
}
