package wailsservice

import (
	"errors"
	"fmt"
	"testing"

	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

func TestNewErrorNil(t *testing.T) {
	if got := newError(nil, "op"); got != nil {
		t.Errorf("newError(nil) = %+v, want nil", got)
	}
}

func TestNewErrorMapsDomainErrorsToCodes(t *testing.T) {
	// 前端按 code 做 i18n，所以这张映射表是对外契约：改错就等于前端显示英文原文。
	cases := []struct {
		err      error
		wantCode string
	}{
		{source.ErrInvalidName, "invalid_name"},
		{source.ErrInvalidColor, "invalid_color"},
		{source.ErrInvalidDelay, "invalid_delay"},
		{source.ErrInvalidInput, "invalid_input"},
		{source.ErrDuplicateShortcut, "duplicate_shortcut"},
		{source.ErrDuplicatePort, "duplicate_port"},
		{source.ErrDuplicateSourceID, "duplicate_source"},
		{display.ErrDisplayNotFound, "display_not_found"},
		{display.ErrUnsupported, "unsupported"},
		{display.ErrWriteFailed, "write_failed"},
		{errors.New("something else"), "internal"},
	}
	for _, c := range cases {
		got := newError(c.err, "op")
		if got == nil {
			t.Fatalf("%v -> nil", c.err)
		}
		if got.Code != c.wantCode {
			t.Errorf("%v -> code %q, want %q", c.err, got.Code, c.wantCode)
		}
		if got.Message == "" {
			t.Errorf("%v -> empty message", c.err)
		}
		if got.Op != "op" {
			t.Errorf("%v -> op %q", c.err, got.Op)
		}
	}
}

func TestNewErrorUnwrapsWrappedErrors(t *testing.T) {
	// 领域错误几乎总是被 fmt.Errorf("%w") 包一层，必须 errors.Is 才能命中。
	wrapped := fmt.Errorf("source %q: %w", "A", source.ErrInvalidName)
	if got := newError(wrapped, "save"); got.Code != "invalid_name" {
		t.Errorf("code = %q, want invalid_name", got.Code)
	}

	wrapped = fmt.Errorf("open: %w", display.ErrUnsupported)
	if got := newError(wrapped, "read"); got.Code != "unsupported" {
		t.Errorf("code = %q, want unsupported", got.Code)
	}
}

func TestErrorMessageIncludesOp(t *testing.T) {
	e := &Error{Code: "internal", Op: "SaveConfig", Message: "boom"}
	if got, want := e.Error(), "SaveConfig: boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	// Op 为空时不要留下 ": boom" 这种多余前缀（日志里会显得很怪）。
	e = &Error{Code: "internal", Message: "boom"}
	if got, want := e.Error(), "boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
