package discovery

import (
	"net"
	"net/netip"
)

// directedBroadcasts 返回每个「有 IPv4 地址的」网卡的定向广播地址。
//
// 只用 255.255.255.255 在某些企业网络/虚拟网卡环境下会被丢弃，
// 加上每网卡的 x.x.x.255 更可靠。
func directedBroadcasts(port int) []netip.AddrPort {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []netip.AddrPort
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			ip := netip.MustParseAddr(ipNet.IP.String()).As4()
			mask, ok := netip.AddrFromSlice(ipNet.Mask)
			if !ok || !mask.Is4() {
				continue
			}
			bcast := broadcastAddr(ip, mask.As4())
			out = append(out, netip.AddrPortFrom(bcast, uint16(port)))
		}
	}
	return out
}

// broadcastAddr 计算 ip | ^mask。
func broadcastAddr(ip, mask [4]byte) netip.Addr {
	var b [4]byte
	for i := 0; i < 4; i++ {
		b[i] = ip[i] | ^mask[i]
	}
	return netip.AddrFrom4(b)
}
