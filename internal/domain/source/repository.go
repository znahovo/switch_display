package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/znahovo/switch-display/internal/domain/display"
)

// CurrentVersion 是当前配置格式版本，迁移时用。
const CurrentVersion = 1

// Settings 是全局设置。
type Settings struct {
	// Language 是界面语言："" = 跟随系统，否则 "zh-CN" / "en-US"。
	Language string
	// ConfirmAfterSwitch 为 true 时，切换后等待 ConfirmMs 再读回确认。
	ConfirmAfterSwitch bool
	// DefaultConfirmMs 是新建信号源时的默认等待时长。
	DefaultConfirmMs int
	// HideToTrayOnClose 关闭窗口时隐藏到菜单栏而不是退出。
	HideToTrayOnClose bool
	// SelfSourceID 是「本机自己所在的那个输入口」对应的信号源 ID。
	//
	// 为什么需要：切走之后这块屏幕就没有画面了，用户很可能需要一键切回。
	// 空串表示用户还没标记，此时托盘不显示「切回本机」。
	SelfSourceID string
}

// DefaultSettings 返回首次运行时的设置。
func DefaultSettings() Settings {
	return Settings{
		Language:           "",
		ConfirmAfterSwitch: true,
		DefaultConfirmMs:   DefaultConfirmMs,
		HideToTrayOnClose:  true,
	}
}

// Snapshot 是持久化的全部内容。
//
// 注意：显示器列表**不进配置**——它是硬件现状，每次启动重新枚举。信号源只
// 持有 DisplayID 引用，这样换显示器后再枚举也不会把配置写坏。
type Snapshot struct {
	Version  int
	Settings Settings
	Sources  []SignalSource
}

// EmptySnapshot 返回一个可用的空配置（不读磁盘）。
func EmptySnapshot() Snapshot {
	return Snapshot{Version: CurrentVersion, Settings: DefaultSettings()}
}

// Repository 是持久化端口（由 infrastructure/persistence 实现）。
type Repository interface {
	// Load 读取配置。文件不存在时返回 EmptySnapshot() 与 nil error。
	Load(ctx context.Context) (Snapshot, error)
	// Save 原子地写入配置。
	Save(ctx context.Context, s Snapshot) error
	// Path 返回配置文件路径，用于「在 Finder 中显示」。
	Path() string
}

// 跨源校验错误。
var (
	ErrDuplicateShortcut = errors.New("duplicate shortcut")
	ErrDuplicatePort     = errors.New("duplicate input port on same display")
	ErrDuplicateSourceID = errors.New("duplicate source id")
)

// Validate 校验整个快照：单源不变量 + 跨源唯一性。
//
// 跨源校验放在这里而不是单源 Validate 里，因为它需要看整体。
func (s Snapshot) Validate() error {
	if s.Version < 1 {
		return fmt.Errorf("invalid snapshot version: %d", s.Version)
	}

	shortcuts := map[string]ID{}
	ports := map[string]ID{}
	ids := map[ID]struct{}{}

	for _, src := range s.Sources {
		if err := src.Validate(); err != nil {
			return fmt.Errorf("source %q: %w", src.Name, err)
		}
		if _, dup := ids[src.ID]; dup {
			return fmt.Errorf("%w: %q", ErrDuplicateSourceID, src.ID)
		}
		ids[src.ID] = struct{}{}

		if src.Shortcut != "" {
			if prev, dup := shortcuts[src.Shortcut]; dup {
				return fmt.Errorf("%w: %q used by %q and %q", ErrDuplicateShortcut, src.Shortcut, prev, src.Name)
			}
			shortcuts[src.Shortcut] = src.ID
		}

		key := portKey(src.DisplayID, src.Input)
		if prev, dup := ports[key]; dup {
			return fmt.Errorf("%w: %q and %q both target %s", ErrDuplicatePort, prev, src.Name, src.Input)
		}
		ports[key] = src.ID
	}

	// SelfSourceID 必须指向一个真实存在的源，否则托盘的「切回本机」会指向空气。
	// 允许为空：用户还没标记哪台机器是「本机」。
	if id := s.Settings.SelfSourceID; id != "" {
		if _, ok := ids[ID(id)]; !ok {
			return fmt.Errorf("self source %q does not exist", id)
		}
	}
	return nil
}

func portKey(id display.DisplayID, v display.InputValue) string {
	alt := "0"
	if v.Alt {
		alt = "1"
	}
	return fmt.Sprintf("%s|%d|%d|%s", id, v.VCP, v.Value, alt)
}

// FindByID 在快照里按 ID 找信号源。
func (s Snapshot) FindByID(id ID) (SignalSource, bool) {
	for _, src := range s.Sources {
		if src.ID == id {
			return src, true
		}
	}
	return SignalSource{}, false
}
