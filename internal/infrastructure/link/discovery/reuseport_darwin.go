//go:build darwin

package discovery

import "syscall"

// setReusePort 打开 SO_REUSEPORT，让同机多个实例都能收到广播。
func setReusePort(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
}
