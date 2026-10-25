package source

import (
	"errors"
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
