package source

import "github.com/znahovo/switch-display/internal/domain/display"

// Preset 是一个常见输入口的预设，用于前端下拉与「快速新建」。
type Preset struct {
	Key   string // 稳定键，前端 i18n 用它取名字
	Value uint16 // VCP 0x60 的值
	Alt   bool   // 是否需要备用通道（LG）
}

// 标准 MCCS 输入源取值（见 m1ddc 的 usage）。
var (
	PresetDP1   = Preset{Key: "dp1", Value: 15}
	PresetDP2   = Preset{Key: "dp2", Value: 16}
	PresetHDMI1 = Preset{Key: "hdmi1", Value: 17}
	PresetHDMI2 = Preset{Key: "hdmi2", Value: 18}
	PresetUSBC  = Preset{Key: "usbc", Value: 27}
	PresetDVI1  = Preset{Key: "dvi1", Value: 3}
	PresetVGA   = Preset{Key: "vga", Value: 1}
)

// Presets 是前端展示的顺序；顺序固定，避免每次渲染抖动。
var Presets = []Preset{
	PresetDP1, PresetDP2, PresetHDMI1, PresetHDMI2, PresetUSBC, PresetDVI1, PresetVGA,
}

// AltPresets 是 LG 等显示器使用的备用通道取值。
var AltPresets = []Preset{
	{Key: "alt-dp1", Value: 208, Alt: true},
	{Key: "alt-dp2", Value: 209, Alt: true},
	{Key: "alt-hdmi1", Value: 144, Alt: true},
	{Key: "alt-hdmi2", Value: 145, Alt: true},
	{Key: "alt-usbc", Value: 210, Alt: true},
}

// ToInputValue 把预设转成领域值对象。
func (p Preset) ToInputValue() display.InputValue {
	vcp := display.VCPInputSource
	if p.Alt {
		vcp = display.VCPInputSourceAlt
	}
	return display.InputValue{VCP: vcp, Value: p.Value, Alt: p.Alt}
}

// MatchPreset 反查：给一个 InputValue，找出匹配的预设键（用于 UI 高亮）。
// 找不到返回空串，表示是用户手填的自定义值。
func MatchPreset(v display.InputValue) string {
	// 按 Alt 分表查：备用通道的取值（208/209/…）和标准取值（15/16/…）有重叠区，
	// 混在一张表里会让 alt-dp1 被误判成 dp1。
	if v.Alt {
		for _, p := range AltPresets {
			if p.Value == v.Value {
				return p.Key
			}
		}
		return ""
	}
	for _, p := range Presets {
		if p.Value == v.Value {
			return p.Key
		}
	}
	return ""
}
