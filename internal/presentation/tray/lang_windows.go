//go:build windows

package tray

import "golang.org/x/sys/windows"

var (
	modKernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetUserDefaultUILanguage = modKernel32.NewProc("GetUserDefaultUILanguage")
)

// langIDChinese 是中文的主语言 ID：LANGID 低 10 位为 0x04 代表 Chinese，
// 0x0403/0x0404/0x0804/0x0C04 等（zh-CN/zh-TW/zh-HK）都落在这个主语言里。
const langIDChinese = 0x04

// systemUILanguageIsChinese 问系统自己的 UI 语言。
//
// Windows 不设 LC_ALL / LC_MESSAGES / LANG，Unix 那套在 Windows 上取不到任何
// 信息，所以必须走 GetUserDefaultUILanguage：它返回用户在「区域和语言」里排
// 第一的显示语言，正是「跟随系统」要的语义。
func systemUILanguageIsChinese() bool {
	langID, _, _ := procGetUserDefaultUILanguage.Call()
	return langIDIsChinese(langID)
}

// langIDIsChinese 只做位运算，单独拆出来是为了能脱机测。
//
// LANGID 的低 10 位是主语言，高 6 位是子语言；0 表示调用失败（系统没给值），
// 按英文处理——宁可少翻译一次，也不要在英文系统上强塞中文。
func langIDIsChinese(langID uintptr) bool {
	if langID == 0 {
		return false
	}
	return langID&0x3ff == langIDChinese
}
