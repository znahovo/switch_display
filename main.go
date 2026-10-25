// Switch Display —— 一台显示器接多个系统时，一键切到对应信号源。
//
// 这个文件是**唯一的装配根**（architecture.md §2 R5）：它只做三件事——
// 构造 Wails 应用、把 bootstrap 装配好的服务挂上去、启动。不含任何业务逻辑。
package main

import (
	"embed"
	"log"

	wails "github.com/wailsapp/wails/v3/pkg/application"

	"github.com/znahovo/switch-display/internal/bootstrap"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := wails.New(wails.Options{
		Name:        "Switch Display",
		Description: "Switch monitor input source with one click",
		Assets: wails.AssetOptions{
			Handler: wails.AssetFileServerFS(assets),
		},
		Mac: wails.MacOptions{
			// 关掉最后一个窗口就退出会让「关闭窗口 = 隐藏到托盘」失效，
			// 所以这里必须为 false（托盘第二步会用到）。
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	wire, err := bootstrap.Build(app)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	app.RegisterService(wails.NewService(wire.DisplayService))

	win := app.Window.NewWithOptions(wails.WebviewWindowOptions{
		Name:   "main",
		Title:  "Switch Display",
		Width:  880,
		Height: 620,
		Mac: wails.MacWindow{
			InvisibleTitleBarHeight: 40,
			Backdrop:                wails.MacBackdropTranslucent,
			TitleBar:                wails.MacTitleBarHiddenInset,
		},
		BackgroundColour: wails.NewRGBA(0, 0, 0, 0),
		URL:              "/",
	})

	// 把窗口交给托盘，并安装「关窗口 = 收进菜单栏」的拦截。
	wire.AttachWindow(win)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
