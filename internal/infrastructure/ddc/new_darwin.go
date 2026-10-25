//go:build darwin && cgo

package ddc

import (
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/infrastructure/ddc/darwin"
)

func newPort() display.Port { return darwin.New() }
