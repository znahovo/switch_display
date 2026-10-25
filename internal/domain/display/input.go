package display

import "fmt"

// VCPCode 是 DDC/CI 的 VCP 代码（MCCS 标准）。
type VCPCode byte

const (
	// VCPInputSource 是「输入源」标准 VCP 代码。
	VCPInputSource VCPCode = 0x60
	// VCPInputSourceAlt 是 LG 等显示器使用的备用输入源代码。
	// 走 0x50 地址、0xF4 代码（见 m1ddc 的 INPUT_ALT）。
	VCPInputSourceAlt VCPCode = 0xF4
	// VCPLuminance / VCPContrast 目前只用于「探测 DDC 是否可用」。
	VCPLuminance VCPCode = 0x10
	VCPContrast  VCPCode = 0x12
)

// String 让日志更可读。
func (c VCPCode) String() string { return fmt.Sprintf("0x%02X", byte(c)) }

// InputValue 是「把显示器切到某个输入口」的完整描述。
//
// Alt=true 表示该显示器要求走备用通道（0x50 地址 + 0xF4）。领域层不关心
// 地址细节，只把「是不是备用」作为值的一部分传下去。
type InputValue struct {
	VCP   VCPCode
	Value uint16
	Alt   bool
}

// String 用于日志与错误信息。
func (v InputValue) String() string {
	if v.Alt {
		return fmt.Sprintf("alt vcp=%s value=%d(%#x)", v.VCP, v.Value, v.Value)
	}
	return fmt.Sprintf("vcp=%s value=%d(%#x)", v.VCP, v.Value, v.Value)
}

// ReadBack 是一次 VCP 读取的结果。
//
// Supported 表示显示器是否真的回了可用的值——本机实测有显示器对 0x60
// 回了个不可能的值（cur=18 max=14），所以调用方必须能区分「读到了但不可信」
// 与「读成功了」。
type ReadBack struct {
	Supported bool
	Current   uint16
	Maximum   uint16
	Raw       []byte // 原始帧，诊断用
}
