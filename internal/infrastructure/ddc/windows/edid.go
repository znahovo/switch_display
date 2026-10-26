//go:build windows

package windows

import (
	"fmt"
	"strings"
)

// 本文件只解析 EDID 字节，不碰任何 Win32：纯函数便于在没有显示器的机器上单测。
//
// EDID 是显示器给自己写的「身份证」：厂商、型号、序列号、可读名字都在里面。
// 这些字段在本项目里只用于显示与诊断（真正切换输入源只靠 VCP 0x60），
// 所以解析失败的代价只是「名字不好看」，绝不能因此让 List 失败。

// edidMinLength 是 EDID 1.x 基础块的长度；扩展块每块再加 128 字节。
const edidMinLength = 128

// EDID 基础块里的固定偏移。
const (
	edidHeaderLen       = 8
	edidOffVendor       = 8  // 厂商 ID，2 字节大端
	edidOffProduct      = 10 // 产品码，2 字节小端
	edidOffSerial       = 12 // 序列号，4 字节小端
	edidOffDescriptor   = 54 // 4 个 18 字节描述符从这里开始
	edidDescriptorLen   = 18
	edidDescriptorCount = 4
)

// 描述符类型：描述符的前 3 字节是 00 00 00，第 4 字节是类型标签。
const (
	edidDescSerial = 0xFF // ASCII 序列号
	edidDescName   = 0xFC // ASCII 显示器名
)

// edidInfo 是从 EDID 里能稳定拿到的那几个字段。全部可能为空。
type edidInfo struct {
	Name   string
	Vendor string
	Model  string
	Serial string
}

// parseEDID 解析基础块。data 不足 128 字节或头部不对时返回 false。
//
// 只解析第一块：扩展块（CEA-861 等）里没有额外的名字/序列号，
// 多读一块只会让函数变复杂。
func parseEDID(data []byte) (edidInfo, bool) {
	if len(data) < edidMinLength {
		return edidInfo{}, false
	}
	// 头部固定 00 FF FF FF FF FF FF 00：厂商 ID 的第一字节是 0x00，
	// 紧跟着 6 个 0xFF 和一个 0x00。校验它能挡掉绝大多数脏数据。
	if data[0] != 0x00 || data[1] != 0xFF || data[2] != 0xFF ||
		data[3] != 0xFF || data[4] != 0xFF || data[5] != 0xFF ||
		data[6] != 0xFF || data[7] != 0x00 {
		return edidInfo{}, false
	}

	info := edidInfo{
		Vendor: pnpID(uint16(data[edidOffVendor])<<8 | uint16(data[edidOffVendor+1])),
		Model:  fmt.Sprintf("%d", uint16(data[edidOffProduct])|uint16(data[edidOffProduct+1])<<8),
	}

	// 数值序列号是 0 或 0xFFFFFFFF 时视为「厂商没填」，此时只认描述符里的文本。
	if n := uint32(data[edidOffSerial]) | uint32(data[edidOffSerial+1])<<8 |
		uint32(data[edidOffSerial+2])<<16 | uint32(data[edidOffSerial+3])<<24; n != 0 && n != 0xFFFFFFFF {
		info.Serial = fmt.Sprintf("%d", n)
	}

	for i := 0; i < edidDescriptorCount; i++ {
		off := edidOffDescriptor + i*edidDescriptorLen
		if off+edidDescriptorLen > edidMinLength {
			break
		}
		d := data[off : off+edidDescriptorLen]
		// 只有「显示器描述符」有空隙，文本描述符前两字节为 0。
		if d[0] != 0x00 || d[1] != 0x00 {
			continue
		}
		switch d[3] {
		case edidDescName:
			if info.Name == "" {
				info.Name = edidText(d[5:])
			}
		case edidDescSerial:
			if info.Serial == "" {
				info.Serial = edidText(d[5:])
			}
		}
	}
	return info, true
}

// edidText 解码描述符里的文本段：ASCII + 0x0A 结尾，后面补空格。
func edidText(b []byte) string {
	end := len(b)
	for i, c := range b {
		if c == 0x0A {
			end = i
			break
		}
	}
	return strings.TrimSpace(string(b[:end]))
}

// pnpID 把 EDID 的厂商 ID 解成三字母 PNP ID（"DEL"、"LHC"、"XMI"）。
//
// 编码方式：16 位里高 15 位分成 5 位一组，每组加 'A'-1。
// 高位的 bit15 是保留位，必须为 0——不为 0 说明数据不对，返回空串。
func pnpID(v uint16) string {
	if v == 0 || v&0x8000 != 0 {
		return ""
	}
	var out [3]byte
	for i := 0; i < 3; i++ {
		// 从高位往低位取：0b0111_1100_0000_0000 是第一个字母。
		shift := uint(10 - i*5)
		letter := byte((v>>shift)&0x1F) + '@'
		if letter < 'A' || letter > 'Z' {
			return ""
		}
		out[i] = letter
	}
	return string(out[:])
}

// fallbackName 在没有 EDID 名字时给一个短名字。
//
// 取设备接口路径的第二段（\\?\DISPLAY#XMI27B1#... 里的 XMI27B1），
// 它通常是「厂商+型号」的 PnP 组合名，比完整的 100+ 字符路径好读得多。
// 拿不到就用空串，交给 display.Label() 的兜底逻辑。
func fallbackName(id string) string {
	instance, ok := deviceInstance(id)
	if !ok {
		return ""
	}
	parts := strings.Split(instance, `\`)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
