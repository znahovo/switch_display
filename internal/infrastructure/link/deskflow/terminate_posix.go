//go:build !windows

package deskflow

import (
	"os"
	"syscall"
)

// terminate 先送 SIGTERM 让 core 自己清理监听与键盘钩子；
// 超时后由调用方 Kill。
func terminate(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
