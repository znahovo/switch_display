//go:build linux

package ddc

import (
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/infrastructure/ddc/linux"
)

func newPort() display.Port { return linux.New() }
