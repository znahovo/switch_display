//go:build !darwin

package tray

// selfDevice 是「本机」在英文文案里的说法（Windows / Linux）。
//
// 中文用「切回本机」，本来就是中性的；英文在 macOS 上是 "This Mac"，
// 在其它平台必须是 "This PC"，否则用户会以为这个菜单项是切到另一台机器。
const selfDevice = "This PC"
