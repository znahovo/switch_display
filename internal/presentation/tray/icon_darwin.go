//go:build darwin

package tray

import (
	wails "github.com/wailsapp/wails/v3/pkg/application"
)

// applyIcon 在 macOS 上用 template icon。
//
// macOS 会把 template image 的 alpha 当作蒙版，按菜单栏明暗自动反色，
// 所以这里只需要那份黑色图形；换成 SetIcon 反而会失去自动反色。
func applyIcon(tray *wails.SystemTray) {
	tray.SetTemplateIcon(iconPNG)
}
