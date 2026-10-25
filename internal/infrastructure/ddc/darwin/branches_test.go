//go:build darwin && cgo

package darwin

import (
	"context"
	"errors"
	"testing"

	"github.com/znahovo/switch-display/internal/domain/display"
)

// 这些用例只走「还没碰硬件就返回」的分支，所以不需要接显示器。

func TestListHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().List(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestReadVCPHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().ReadVCP(ctx, "whatever", display.VCPInputSource, false); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestWriteVCPHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in := display.InputValue{VCP: display.VCPInputSource, Value: 17}
	if err := New().WriteVCP(ctx, "whatever", in); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestReadUnknownDisplayIsNotFound(t *testing.T) {
	// 写错显示器 ID 必须报 ErrDisplayNotFound（而不是崩溃或沉默成功）。
	_, err := New().ReadVCP(context.Background(), "no-such-display", display.VCPInputSource, false)
	if !errors.Is(err, display.ErrDisplayNotFound) {
		t.Errorf("err = %v, want ErrDisplayNotFound", err)
	}
}

func TestWriteUnknownDisplayIsNotFound(t *testing.T) {
	in := display.InputValue{VCP: display.VCPInputSource, Value: 17}
	err := New().WriteVCP(context.Background(), "no-such-display", in)
	if !errors.Is(err, display.ErrDisplayNotFound) {
		t.Errorf("err = %v, want ErrDisplayNotFound", err)
	}
}

func TestBoolToC(t *testing.T) {
	if boolToC(true) != 1 || boolToC(false) != 0 {
		t.Error("boolToC mapping is wrong")
	}
}
