package source

import (
	"errors"
	"strings"
	"testing"

	"github.com/znahovo/switch-display/internal/domain/display"
)

func TestNewTrimsNameAndDefaults(t *testing.T) {
	s, err := New("  Work PC  ", "uuid-1", display.InputValue{VCP: display.VCPInputSource, Value: 17})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if s.Name != "Work PC" {
		t.Fatalf("name = %q", s.Name)
	}
	if s.ConfirmMs != DefaultConfirmMs {
		t.Fatalf("confirmMs = %d", s.ConfirmMs)
	}
	if s.ID == "" {
		t.Fatal("id should be generated")
	}
}

func TestNewRejectsEmptyName(t *testing.T) {
	if _, err := New("   ", "uuid-1", display.InputValue{Value: 17}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateRejectsBadColor(t *testing.T) {
	s, _ := New("PC", "uuid-1", display.InputValue{Value: 17})
	s.Color = "blue"
	if err := s.Validate(); !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("err = %v", err)
	}
	s.Color = "#3B82F6"
	if err := s.Validate(); err != nil {
		t.Fatalf("valid color rejected: %v", err)
	}
}

func TestValidateRejectsDelayOutOfRange(t *testing.T) {
	s, _ := New("PC", "uuid-1", display.InputValue{Value: 17})
	s.ConfirmMs = MaxConfirmMs + 1
	if err := s.Validate(); !errors.Is(err, ErrInvalidDelay) {
		t.Fatalf("err = %v", err)
	}
}

func TestSnapshotRejectsDuplicatePort(t *testing.T) {
	a, _ := New("A", "uuid-1", display.InputValue{VCP: display.VCPInputSource, Value: 17})
	b, _ := New("B", "uuid-1", display.InputValue{VCP: display.VCPInputSource, Value: 17})
	snap := Snapshot{Version: CurrentVersion, Settings: DefaultSettings(), Sources: []SignalSource{a, b}}
	if err := snap.Validate(); !errors.Is(err, ErrDuplicatePort) {
		t.Fatalf("err = %v", err)
	}
}

func TestSnapshotRejectsDuplicateShortcut(t *testing.T) {
	a, _ := New("A", "uuid-1", display.InputValue{Value: 17})
	b, _ := New("B", "uuid-1", display.InputValue{Value: 18})
	a.Shortcut, b.Shortcut = "Cmd+Option+1", "Cmd+Option+1"
	snap := Snapshot{Version: CurrentVersion, Settings: DefaultSettings(), Sources: []SignalSource{a, b}}
	if err := snap.Validate(); !errors.Is(err, ErrDuplicateShortcut) {
		t.Fatalf("err = %v", err)
	}
}

func TestMatchPreset(t *testing.T) {
	cases := []struct {
		v    display.InputValue
		want string
	}{
		{display.InputValue{Value: 15}, "dp1"},
		{display.InputValue{Value: 17}, "hdmi1"},
		{display.InputValue{Value: 27}, "usbc"},
		{display.InputValue{Value: 210, Alt: true}, "alt-usbc"},
		{display.InputValue{Value: 1234}, ""},
	}
	for _, c := range cases {
		if got := MatchPreset(c.v); got != c.want {
			t.Errorf("MatchPreset(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestEmptySnapshotIsValid(t *testing.T) {
	if err := EmptySnapshot().Validate(); err != nil {
		t.Fatalf("empty snapshot invalid: %v", err)
	}
}

func TestNewNormalizesVCPCode(t *testing.T) {
	// 缺省 VCP 时按 Alt 补对应代码：否则「只有端口值」的调用会写出 0x00。
	std, err := New("PC", "uuid-1", display.InputValue{Value: 17})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if std.Input.VCP != display.VCPInputSource {
		t.Errorf("vcp = %v, want %v", std.Input.VCP, display.VCPInputSource)
	}

	alt, err := New("PC", "uuid-1", display.InputValue{Value: 208, Alt: true})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if alt.Input.VCP != display.VCPInputSourceAlt {
		t.Errorf("vcp = %v, want %v", alt.Input.VCP, display.VCPInputSourceAlt)
	}
}

func TestValidateRejectsEmptyNameAndLongName(t *testing.T) {
	s, _ := New("PC", "uuid-1", display.InputValue{Value: 17})

	s.Name = "   "
	if err := s.Validate(); !errors.Is(err, ErrInvalidName) {
		t.Errorf("blank name err = %v", err)
	}

	// 边界：刚好 32 个字符（按 rune 算，中文也一样）
	s.Name = strings.Repeat("ä", MaxNameLen)
	if err := s.Validate(); err != nil {
		t.Errorf("32-rune name rejected: %v", err)
	}
	s.Name = strings.Repeat("ä", MaxNameLen+1)
	if err := s.Validate(); !errors.Is(err, ErrInvalidName) {
		t.Errorf("33-rune name err = %v", err)
	}
}

func TestValidateRejectsMissingInputOrDisplay(t *testing.T) {
	s, _ := New("PC", "uuid-1", display.InputValue{Value: 17})

	s.Input.VCP = 0
	if err := s.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("missing vcp err = %v", err)
	}

	s, _ = New("PC", "uuid-1", display.InputValue{Value: 17})
	s.DisplayID = ""
	if err := s.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("missing display err = %v", err)
	}
}

func TestValidateAcceptsZeroConfirmMs(t *testing.T) {
	// 0 是合法值，含义是「发完就算，不读回确认」。
	s, _ := New("PC", "uuid-1", display.InputValue{Value: 17})
	s.ConfirmMs = 0
	if err := s.Validate(); err != nil {
		t.Fatalf("confirmMs=0 rejected: %v", err)
	}
}

func TestRename(t *testing.T) {
	s, _ := New("PC", "uuid-1", display.InputValue{Value: 17})

	renamed, err := s.Rename("  MacBook  ")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "  MacBook  " {
		// Rename 只改字段不做 trim（校验会 TrimSpace 判断非空）：
		// 保留用户输入，避免「改名后空格莫名消失」。
		t.Errorf("name = %q", renamed.Name)
	}

	if _, err := s.Rename("   "); !errors.Is(err, ErrInvalidName) {
		t.Errorf("blank rename err = %v", err)
	}
}

func TestSnapshotRejectsDuplicateID(t *testing.T) {
	a, _ := New("A", "uuid-1", display.InputValue{Value: 17})
	b, _ := New("B", "uuid-1", display.InputValue{Value: 18})
	b.ID = a.ID
	snap := Snapshot{Version: CurrentVersion, Settings: DefaultSettings(), Sources: []SignalSource{a, b}}
	if err := snap.Validate(); !errors.Is(err, ErrDuplicateSourceID) {
		t.Fatalf("err = %v", err)
	}
}

func TestSnapshotRejectsBadVersion(t *testing.T) {
	snap := EmptySnapshot()
	snap.Version = 0
	if err := snap.Validate(); err == nil {
		t.Fatal("version 0 should be rejected")
	}
}

func TestSnapshotAllowsSamePortOnDifferentDisplays(t *testing.T) {
	// 端口唯一性只在「同一台显示器」上成立：两台显示器各自有 HDMI1 是正常的。
	a, _ := New("A", "uuid-1", display.InputValue{Value: 17})
	b, _ := New("B", "uuid-2", display.InputValue{Value: 17})
	snap := Snapshot{Version: CurrentVersion, Settings: DefaultSettings(), Sources: []SignalSource{a, b}}
	if err := snap.Validate(); err != nil {
		t.Fatalf("same port on different displays rejected: %v", err)
	}
}

func TestSnapshotRejectsDanglingSelfSource(t *testing.T) {
	a, _ := New("A", "uuid-1", display.InputValue{Value: 17})
	snap := Snapshot{Version: CurrentVersion, Settings: DefaultSettings(), Sources: []SignalSource{a}}
	snap.Settings.SelfSourceID = "nope"
	if err := snap.Validate(); err == nil {
		t.Fatal("dangling selfSourceId should be rejected")
	}

	// 指向真实存在的源就可以。
	snap.Settings.SelfSourceID = string(a.ID)
	if err := snap.Validate(); err != nil {
		t.Fatalf("valid selfSourceId rejected: %v", err)
	}
}

func TestSnapshotFindByID(t *testing.T) {
	a, _ := New("A", "uuid-1", display.InputValue{Value: 17})
	snap := Snapshot{Version: CurrentVersion, Settings: DefaultSettings(), Sources: []SignalSource{a}}

	got, ok := snap.FindByID(a.ID)
	if !ok || got.Name != "A" {
		t.Fatalf("FindByID(%q) = %+v, %v", a.ID, got, ok)
	}
	if _, ok := snap.FindByID("missing"); ok {
		t.Fatal("FindByID should miss unknown id")
	}
}

func TestPresetToInputValue(t *testing.T) {
	dp1 := PresetDP1.ToInputValue()
	if dp1.VCP != display.VCPInputSource || dp1.Value != 15 || dp1.Alt {
		t.Errorf("PresetDP1.ToInputValue() = %+v", dp1)
	}
	// alt 预设必须同时带 Alt=true 和 0xF4 的 VCP，否则会写到错误的地址。
	for _, p := range AltPresets {
		v := p.ToInputValue()
		if !v.Alt || v.VCP != display.VCPInputSourceAlt {
			t.Errorf("preset %q ToInputValue() = %+v, want alt + 0xF4", p.Key, v)
		}
	}
}

func TestMatchPresetDistinguishesAltTable(t *testing.T) {
	// 208 只在 alt 表里；不带 Alt 时应视为自定义值，不能误报成 alt-dp1。
	if got := MatchPreset(display.InputValue{Value: 208}); got != "" {
		t.Errorf("MatchPreset(208, alt=false) = %q, want empty", got)
	}
	if got := MatchPreset(display.InputValue{Value: 208, Alt: true}); got != "alt-dp1" {
		t.Errorf("MatchPreset(208, alt=true) = %q, want alt-dp1", got)
	}
}

func TestPresetsHaveUniqueKeys(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range append(append([]Preset{}, Presets...), AltPresets...) {
		if seen[p.Key] {
			t.Errorf("duplicate preset key %q", p.Key)
		}
		seen[p.Key] = true
	}
}
