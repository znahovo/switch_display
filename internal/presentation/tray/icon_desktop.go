//go:build !darwin

package tray

import (
	wails "github.com/wailsapp/wails/v3/pkg/application"
)

// applyIcon 在 Windows / Linux 上直接给两张位图。
//
// 这两个平台没有 template image 的概念：Wails 在 Windows 上的
// SetTemplateIcon 是空实现（图标根本不会出现），Linux 的 GTK 托盘也一样
// 只是按原样画 PNG。所以必须显式给图，并且按深浅色各给一张——
// 深色任务栏上看黑色图标等于看不见。
func applyIcon(tray *wails.SystemTray) {
	tray.SetIcon(iconPNG)
	tray.SetDarkModeIcon(iconWhitePNG)
}
