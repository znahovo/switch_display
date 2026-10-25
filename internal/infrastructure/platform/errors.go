package platform

import "fmt"

// errUnsupportedShortcut 表示当前平台/运行时没有全局快捷键能力。
func errUnsupportedShortcut(accelerator string) error {
	return fmt.Errorf("global shortcuts are not available in this build (accelerator %q)", accelerator)
}
