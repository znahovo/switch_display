//go:build darwin

package tray

// selfDevice 是「本机」在英文文案里的说法。
//
// 分开按平台定义而不是写死 "Mac"：这个应用本来就是在一台 Mac 和一台 PC
// 之间切显示器，Windows 版说 "Back to This Mac" 会让人以为切错了机器。
const selfDevice = "This Mac"
