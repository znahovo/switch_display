//go:build linux

package discovery

import "golang.org/x/sys/unix"

// setReusePort 打开 SO_REUSEPORT，让同机多个实例都能收到广播。
//
// Linux 的 SO_REUSEPORT 不在 syscall 包里（syscall.SO_REUSEPORT 未定义），
// 只能走 x/sys/unix。
func setReusePort(fd uintptr) error {
	return unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
}
