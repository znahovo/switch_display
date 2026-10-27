//go:build windows

package tray

import "testing"

func TestLangIDIsChinese(t *testing.T) {
	// GetUserDefaultUILanguage 返回的 LANGID：低 10 位是主语言。
	// 这几个值来自 Windows 的真实取值，覆盖简体、繁体、香港、新加坡
	// 以及英文/日文——子语言不同但主语言都是 0x04 的必须全部认成中文。
	cases := []struct {
		name   string
		langID uintptr
		want   bool
	}{
		{"zh-CN 0x0804", 0x0804, true},
		{"zh-TW 0x0404", 0x0404, true},
		{"zh-HK 0x0C04", 0x0C04, true},
		{"zh-SG 0x1004", 0x1004, true},
		{"en-US 0x0409", 0x0409, false},
		{"ja-JP 0x0411", 0x0411, false},
		{"zero means call failed", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := langIDIsChinese(c.langID); got != c.want {
				t.Errorf("langIDIsChinese(0x%04X) = %v, want %v", c.langID, got, c.want)
			}
		})
	}
}

func TestSystemUILanguageDoesNotPanic(t *testing.T) {
	// 只是确保 kernel32 的 proc 能解析、调用不炸。返回值取决于跑测试的机器
	// 系统语言，所以不断言具体值。
	_ = systemUILanguageIsChinese()
}
