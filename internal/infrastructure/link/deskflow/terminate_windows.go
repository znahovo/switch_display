//go:build windows

package deskflow

import "os"

// terminate 在 Windows 上没有优雅终止信号，直接 Kill。
//
// core 进程整体退出时由内核回收其监听端口，所以这里可接受。
func terminate(p *os.Process) error {
	return p.Kill()
}
