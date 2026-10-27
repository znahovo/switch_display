//go:build !windows

package discovery

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

// listenUDP 绑定 UDP 端口，并打开地址复用。
//
// 为什么需要 SO_REUSEADDR/SO_REUSEPORT：广播包会被投递给「所有绑定该端口
// 的 socket」。开发时同时跑 `task dev` 与打包版，两个进程都要能收到对端
// 的 announce，否则调试时永远看不到对方。（已在本机实测可行。）
//
// SO_BROADCAST 必须显式打开，否则发广播会报 permission denied。
func listenUDP(port int) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var setErr error
			if err := c.Control(func(fd uintptr) {
				if e := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); e != nil {
					setErr = e
					return
				}
				if e := setReusePort(fd); e != nil {
					setErr = e
					return
				}
				if e := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); e != nil {
					setErr = e
					return
				}
			}); err != nil {
				return err
			}
			return setErr
		},
	}

	pc, err := lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("unexpected packet conn type %T", pc)
	}
	return conn, nil
}
