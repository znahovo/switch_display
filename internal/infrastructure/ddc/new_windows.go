//go:build windows

package ddc

import (
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/infrastructure/ddc/windows"
)

func newPort() display.Port { return windows.New() }
