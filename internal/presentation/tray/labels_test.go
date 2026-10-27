package tray

import (
	"testing"
)

func TestIsChinese(t *testing.T) {
	// 显式语言优先：即使用户系统是英文，配置成 zh-CN 也要中文。
	for _, lang := range []string{"zh", "zh-CN", "ZH-TW", "zh-Hans"} {
		if !isChinese(lang) {
			t.Errorf("isChinese(%q) = false, want true", lang)
		}
	}
	for _, lang := range []string{"en", "en-US", "ja", "fr"} {
		if isChinese(lang) {
			t.Errorf("isChinese(%q) = true, want false", lang)
		}
	}
}

func TestIsChineseFallsBackToLocale(t *testing.T) {
	// 配置里没选语言时跟随系统区域。三个变量都要认，否则只设 LANG 的环境会失效。
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("LC_ALL", "")
			t.Setenv("LC_MESSAGES", "")
			t.Setenv("LANG", "")
			t.Setenv(key, "zh_CN.UTF-8")
			if !isChinese("") {
				t.Errorf("%s=zh_CN not detected", key)
			}
		})
	}
}

func TestIsChineseDefaultsToEnglish(t *testing.T) {
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	if isChinese("") {
		t.Error("should default to English")
	}
}

func TestLabelsForBothLanguages(t *testing.T) {
	zh := labelsFor("zh-CN")
	if zh.Quit != "退出" || zh.Open != "打开切换台" {
		t.Errorf("zh labels = %+v", zh)
	}
	en := labelsFor("en")
	if en.Quit != "Quit" || en.Open != "Open Switch Display" {
		t.Errorf("en labels = %+v", en)
	}
	// 任何一条文案都不能为空：空 label 会让菜单出现空白项。
	for _, l := range []labels{zh, en} {
		for name, v := range map[string]string{
			"Open": l.Open, "NoSources": l.NoSources, "BackToSelf": l.BackToSelf,
			"Settings": l.Settings, "Rescan": l.Rescan, "Quit": l.Quit,
			"SwitchingTo": l.SwitchingTo,
		} {
			if v == "" {
				t.Errorf("label %s is empty", name)
			}
		}
	}
}
func TestIsChineseIgnoresCLocale(t *testing.T) {
	// "C" / "POSIX" 是「没指定区域」的哨兵值，不能当成一个有效语言把它
	// 挡在系统语言前面——否则 ssh、CI、部分终端里托盘会莫名其妙变英文。
	// 这种情况下应该一路落到系统语言，结果与直接问系统一致。
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("LC_ALL", "")
			t.Setenv("LC_MESSAGES", "")
			t.Setenv("LANG", "")
			t.Setenv(key, "C")
			if got, want := isChinese(""), systemUILanguageIsChinese(); got != want {
				t.Errorf("isChinese(\"\") with %s=C = %v, want %v (system language)", key, got, want)
			}
		})
	}
}

func TestIsChineseLocaleBeatsSystemLanguage(t *testing.T) {
	// 显式设了非中文 locale 时就要听它的，不能被系统语言翻回去。
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	if isChinese("") {
		t.Error("explicit en_US.UTF-8 must win over system language")
	}
}
