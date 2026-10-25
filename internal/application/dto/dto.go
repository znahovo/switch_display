// Package dto 定义 application 层返回给表现层的传输对象。
//
// 为什么单独一个包：层与层之间的数据形状是契约，集中放一处便于 Wails
// bindings 生成稳定的 TypeScript 类型；也避免把领域对象直接暴露给前端
// （领域对象的方法与内部字段不应成为对外 API）。
package dto

// DisplayDTO 是一台显示器。
type DisplayDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Vendor  string `json:"vendor"`
	Model   string `json:"model"`
	Serial  string `json:"serial"`
	EDID    string `json:"edid"`
	Builtin bool   `json:"builtin"`
}

// InputDTO 是一个输入口取值。
type InputDTO struct {
	VCP   uint8  `json:"vcp"`
	Value uint16 `json:"value"`
	Alt   bool   `json:"alt"`
	// PresetKey 是匹配到的预设键（"dp1"/"hdmi1"/…），自定义值为空串。
	// 放在这里是为了前端不用再维护一份端口表。
	PresetKey string `json:"presetKey"`
}

// SourceDTO 是一个信号源。
type SourceDTO struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Color     string   `json:"color"`
	DisplayID string   `json:"displayId"`
	Input     InputDTO `json:"input"`
	Shortcut  string   `json:"shortcut"`
	ConfirmMs int      `json:"confirmMs"`
}

// SettingsDTO 是全局设置。
type SettingsDTO struct {
	Language           string `json:"language"`
	ConfirmAfterSwitch bool   `json:"confirmAfterSwitch"`
	DefaultConfirmMs   int    `json:"defaultConfirmMs"`
	HideToTrayOnClose  bool   `json:"hideToTrayOnClose"`
	// SelfSourceID 是「本机」对应的信号源 ID；空串表示未标记。
	// 「切回本机」菜单项与设置页都靠它；不存端口值本身而存源 ID，
	// 这样用户改源端口时「本机」会跟着变。
	SelfSourceID string `json:"selfSourceId"`
}

// StateDTO 是前端一次渲染所需的全部状态。
//
// 故意做成「一次性快照」而不是增量补丁：状态量小（几十个源），
// 全量刷新让前端 store 逻辑简单到不需要 reducer。
type StateDTO struct {
	Displays []DisplayDTO `json:"displays"`
	Sources  []SourceDTO  `json:"sources"`
	Settings SettingsDTO  `json:"settings"`
	// ConfigPath 让用户知道配置在哪，也方便「导入/导出」提示。
	ConfigPath string `json:"configPath"`
	// Warnings 是「不致命但用户该知道」的问题，如快捷键注册失败。
	Warnings []string `json:"warnings"`
}

// SwitchResultDTO 是一次切换的结果。
type SwitchResultDTO struct {
	SourceID  string `json:"sourceId"`
	DisplayID string `json:"displayId"`
	// Outcome 取值：confirmed / mismatch / applied_unconfirmed / failed。
	Outcome string `json:"outcome"`
	// Attempts 是实际写了几次（含重试）。
	Attempts int `json:"attempts"`
	// Current 是读回的当前值，-1 表示没读到。
	Current int `json:"current"`
	// Message 是可直接展示给用户的一句话。
	Message string `json:"message"`
}

// PresetDTO 是输入口预设。
type PresetDTO struct {
	Key   string `json:"key"`
	Value uint16 `json:"value"`
	Alt   bool   `json:"alt"`
}

// SaveResultDTO 是保存配置的结果。
type SaveResultDTO struct {
	OK bool `json:"ok"`
	// Warnings 是保存成功但需要提示的问题（例如某条快捷键被占用）。
	Warnings []string `json:"warnings"`
}
