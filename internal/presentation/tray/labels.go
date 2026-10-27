package tray

import (
	"os"
	"strings"
)

// labels 是托盘菜单里用到的全部文案。
//
// 为什么自己在托盘层做一份而不复用前端 i18n：托盘菜单是原生菜单，
// 由 Go 侧渲染，拿不到 vue-i18n 的 message。文案量很小（不到 10 条），
// 重复一份比给前端塞一个「把翻译推给后端」的接口简单得多。
type labels struct {
	Open        string // 打开切换台
	NoSources   string // 还没有信号源
	BackToSelf  string // 切回本机
	Settings    string // 设置…
	Rescan      string // 重新检测显示器
	Quit        string // 退出
	SwitchingTo string // 正在切换到 %s
}

// labelsFor 按配置里的语言挑文案；lang 为空时跟随系统区域。
func labelsFor(lang string) labels {
	if isChinese(lang) {
		return labels{
			Open:        "打开切换台",
			NoSources:   "还没有信号源",
			BackToSelf:  "切回本机",
			Settings:    "设置…",
			Rescan:      "重新检测显示器",
			Quit:        "退出",
			SwitchingTo: "正在切换到 %s",
		}
	}
	return labels{
		Open:        "Open Switch Display",
		NoSources:   "No signal sources yet",
		BackToSelf:  "Back to " + selfDevice,
		Settings:    "Settings…",
		Rescan:      "Rescan Displays",
		Quit:        "Quit",
		SwitchingTo: "Switching to %s",
	}
}

// isChinese 判断该用中文还是英文文案。
//
// 优先级：配置里的语言 > locale 环境变量 > 系统 UI 语言。
// 中间那层是为 Unix 留的（LC_ALL/LANG 是那边的惯例）；Windows 不设这些变量，
// 所以那边会一路落到 systemUILanguageIsChinese。注意只要环境变量里出现了
// 语言就当场定案，不再问系统——否则显式设成 en_US 的进程在中文 Windows 上
// 会被系统语言覆盖回去。
func isChinese(lang string) bool {
	if lang != "" {
		return strings.HasPrefix(strings.ToLower(lang), "zh")
	}
	sawLocale := false
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		value := strings.ToLower(os.Getenv(key))
		// "C" / "POSIX" 是「没有指定区域」的哨兵值，不算有效信息：有些
		// 环境（ssh、CI、部分终端）会强行设成它，不能让它把系统语言挡掉。
		if value == "" || value == "c" || value == "posix" {
			continue
		}
		sawLocale = true
		if strings.Contains(value, "zh") {
			return true
		}
	}
	if sawLocale {
		return false
	}
	return systemUILanguageIsChinese()
}
