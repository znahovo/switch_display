//go:build !windows

package tray

import (
	"os"
	"strings"
)

// systemUILanguageIsChinese 用 POSIX locale 环境变量判断是否中文。
//
// 三个变量都要认：只设 LANG 的环境（多数 Linux 桌面）不查就会误判成英文。
func systemUILanguageIsChinese() bool {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if strings.Contains(strings.ToLower(os.Getenv(key)), "zh") {
			return true
		}
	}
	return false
}
