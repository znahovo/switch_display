// Package display 定义显示器身份与 DDC/CI 读写端口。
//
// 本包是领域层：只允许 import 标准库（architecture.md R1）。
// 它不知道 cgo、IORegistry、Wails 或前端的存在。
package display

import "fmt"

// DisplayID 是显示器在当前系统里的稳定标识。
//
//   - macOS: 显示器的 System UUID（跨重启稳定，换接口也不变）
//   - Windows: monitor device path（待实现）
//   - Linux: connector + EDID hash（待实现）
type DisplayID string

// Display 是枚举得到的一台显示器的只读快照。
type Display struct {
	ID   DisplayID
	Name string // 用户可读名字，可能来自显示器 EDID
	// 以下字段用于诊断与「同一个显示器」的兜底匹配，均可为空。
	Vendor string
	Model  string
	Serial string
	EDID   string
	// Builtin 为 true 表示是笔记本内屏（内屏通常没有 DDC/CI 输入源可切）。
	Builtin bool
}

// Label 返回 UI 上显示的短名字，形如 "Mi Monitor"。
func (d Display) Label() string {
	if d.Name != "" {
		return d.Name
	}
	if d.ID != "" {
		return fmt.Sprintf("Display %s", d.ID)
	}
	return "Unknown Display"
}
