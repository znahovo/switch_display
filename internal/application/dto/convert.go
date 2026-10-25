package dto

import (
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
	"github.com/znahovo/switch-display/internal/domain/switching"
)

// FromDisplay 把领域显示器转成 DTO。
func FromDisplay(d display.Display) DisplayDTO {
	return DisplayDTO{
		ID:      string(d.ID),
		Name:    d.Label(),
		Vendor:  d.Vendor,
		Model:   d.Model,
		Serial:  d.Serial,
		EDID:    d.EDID,
		Builtin: d.Builtin,
	}
}

// FromDisplays 批量转换。
func FromDisplays(list []display.Display) []DisplayDTO {
	out := make([]DisplayDTO, 0, len(list))
	for _, d := range list {
		out = append(out, FromDisplay(d))
	}
	return out
}

// FromInput 把领域输入值转成 DTO，并补上预设键。
func FromInput(v display.InputValue) InputDTO {
	return InputDTO{
		VCP:       uint8(v.VCP),
		Value:     v.Value,
		Alt:       v.Alt,
		PresetKey: source.MatchPreset(v),
	}
}

// ToInput 把 DTO 转回领域值。
func ToInput(d InputDTO) display.InputValue {
	vcp := display.VCPCode(d.VCP)
	if vcp == 0 {
		vcp = display.VCPInputSource
	}
	return display.InputValue{VCP: vcp, Value: d.Value, Alt: d.Alt}
}

// FromSource 把领域信号源转成 DTO。
func FromSource(s source.SignalSource) SourceDTO {
	return SourceDTO{
		ID:        string(s.ID),
		Name:      s.Name,
		Color:     s.Color,
		DisplayID: string(s.DisplayID),
		Input:     FromInput(s.Input),
		Shortcut:  s.Shortcut,
		ConfirmMs: s.ConfirmMs,
	}
}

// FromSources 批量转换。
func FromSources(list []source.SignalSource) []SourceDTO {
	out := make([]SourceDTO, 0, len(list))
	for _, s := range list {
		out = append(out, FromSource(s))
	}
	return out
}

// ToSource 把 DTO 转回领域信号源（不校验，校验由领域负责）。
func ToSource(s SourceDTO) source.SignalSource {
	return source.SignalSource{
		ID:        source.ID(s.ID),
		Name:      s.Name,
		Color:     s.Color,
		DisplayID: display.DisplayID(s.DisplayID),
		Input:     ToInput(s.Input),
		Shortcut:  s.Shortcut,
		ConfirmMs: s.ConfirmMs,
	}
}

// FromSettings / ToSettings 转换设置。
func FromSettings(s source.Settings) SettingsDTO {
	return SettingsDTO{
		Language:           s.Language,
		ConfirmAfterSwitch: s.ConfirmAfterSwitch,
		DefaultConfirmMs:   s.DefaultConfirmMs,
		HideToTrayOnClose:  s.HideToTrayOnClose,
	}
}

// ToSettings 把 DTO 转回领域设置。
func ToSettings(s SettingsDTO) source.Settings {
	return source.Settings{
		Language:           s.Language,
		ConfirmAfterSwitch: s.ConfirmAfterSwitch,
		DefaultConfirmMs:   s.DefaultConfirmMs,
		HideToTrayOnClose:  s.HideToTrayOnClose,
	}
}

// FromPresets 转换预设表。
func FromPresets(list []source.Preset) []PresetDTO {
	out := make([]PresetDTO, 0, len(list))
	for _, p := range list {
		out = append(out, PresetDTO{Key: p.Key, Value: p.Value, Alt: p.Alt})
	}
	return out
}

// FromSwitchResult 把切换结果转成 DTO。
//
// Message 在这里就渲染成人类可读文本（而不是交给前端）：后端已经知道
// outcome 与读回值，前端只负责显示。
func FromSwitchResult(r switching.Result) SwitchResultDTO {
	current := -1
	if r.ReadBack != nil {
		current = int(r.ReadBack.Current)
	}
	return SwitchResultDTO{
		SourceID:  r.SourceID,
		DisplayID: string(r.DisplayID),
		Outcome:   string(r.Outcome),
		Attempts:  r.Attempts,
		Current:   current,
		Message:   messageFor(r),
	}
}

// messageFor 只给出稳定的英文/中性文案，具体语言由前端 i18n 负责；
// 这里保留一句话是为了日志与托盘通知也能直接用。
func messageFor(r switching.Result) string {
	switch r.Outcome {
	case switching.OutcomeConfirmed:
		return "input switched and confirmed by display"
	case switching.OutcomeMismatch:
		return "display reports a different input source"
	case switching.OutcomeAppliedUnconfirmed:
		return "input switch sent (display cannot confirm)"
	case switching.OutcomeFailed:
		if r.Err != nil {
			return "failed to switch input: " + r.Err.Error()
		}
		return "failed to switch input"
	default:
		return string(r.Outcome)
	}
}
