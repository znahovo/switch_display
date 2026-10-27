//go:build windows

package discovery

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

// listenUDP 在 Windows 上绑定 UDP 端口。
//
// Windows 没有 SO_REUSEPORT；不过 Windows 的 SO_REUSEADDR 语义接近 Unix 的
// SO_REUSEPORT（允许同一端口多个 socket 共享），所以只设它即可。
// SO_BROADCAST 同样需要显式打开，否则发广播会失败。
func listenUDP(port int) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var setErr error
			if err := c.Control(func(fd uintptr) {
				if e := syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); e != nil {
					setErr = e
					return
				}
				if e := syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); e != nil {
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
