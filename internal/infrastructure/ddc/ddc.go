// Package ddc 是 display.Port 的平台工厂。
//
// 平台差异只在「怎么读写 DDC/CI」：macOS 走 IOKit/IOAVService（cgo），
// Windows 走 Dxva2，Linux 走 /dev/i2c-*。上层用例完全不知道这些差别，
// 这也是把它们放在 infrastructure 的原因（architecture.md §2 R3）。
package ddc

import (
	"github.com/znahovo/switch-display/internal/domain/display"
)

// New 返回当前平台的 DDC 实现。
//
// 每个平台一个文件（new_darwin.go / new_windows.go / new_linux.go），
// 靠 build tag 选择——增删平台只动一个文件，不需要改这里。
func New() display.Port {
	return newPort()
}
