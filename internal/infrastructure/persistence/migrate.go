package persistence

import (
	"fmt"

	"github.com/znahovo/switch-display/internal/domain/source"
)

// migrate 把磁盘上的旧版本配置升级到当前版本。
//
// 当前只有 v1，所以这里只做「补默认值 + 版本校验」。保留这个函数是为了给
// 未来留明确的扩展点：新增版本时在这里加 case，而不是散落在 toDomain 里。
func migrate(w wireSnapshot) (wireSnapshot, error) {
	switch w.Version {
	case 0:
		// 缺 version 字段：当成最早的 v1 处理，并补齐默认设置。
		w.Version = source.CurrentVersion
	case 1:
		// 当前版本，无需迁移。
	case -1:
		fallthrough
	default:
		if w.Version < 1 || w.Version > source.CurrentVersion {
			return w, fmt.Errorf("unsupported config version %d (this build supports %d)",
				w.Version, source.CurrentVersion)
		}
	}
	return applyDefaults(w), nil
}

// applyDefaults 填充零值设置，避免手工编辑过的配置把开关全变成 false。
func applyDefaults(w wireSnapshot) wireSnapshot {
	def := source.DefaultSettings()
	if w.Settings.DefaultConfirmMs == 0 {
		w.Settings.DefaultConfirmMs = def.DefaultConfirmMs
	}
	if w.Settings.Language == "" {
		w.Settings.Language = def.Language
	}
	for i := range w.Sources {
		if w.Sources[i].ConfirmMs <= 0 {
			w.Sources[i].ConfirmMs = w.Settings.DefaultConfirmMs
		}
	}
	return w
}
