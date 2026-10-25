//go:build linux

// Package linux 是 Linux 上的 DDC/CI 实现（尚未编写）。
//
// 计划：直接操作 /dev/i2c-*，按 DDC/CI 协议组帧（与 macOS 实现同构），
// 或调用 ddcutil 作为后备实现。
// 现在只返回 ErrUnsupported，让上层以「不支持」正常降级而不是崩溃。
package linux

import (
	"context"

	"github.com/znahovo/switch-display/internal/domain/display"
)

// Port 是 Linux DDC 端口。
type Port struct{}

// New 构造 Linux DDC 端口。
func New() *Port { return &Port{} }

// List 实现 display.Port。
func (p *Port) List(ctx context.Context) ([]display.Display, error) {
	return nil, display.ErrUnsupported
}

// ReadVCP 实现 display.Port。
func (p *Port) ReadVCP(ctx context.Context, id display.DisplayID, vcp display.VCPCode, alt bool) (display.ReadBack, error) {
	return display.ReadBack{}, display.ErrUnsupported
}

// WriteVCP 实现 display.Port。
func (p *Port) WriteVCP(ctx context.Context, id display.DisplayID, v display.InputValue) error {
	return display.ErrUnsupported
}
