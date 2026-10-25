// Package source 定义「信号源」聚合：一个信号源 = 显示器 + 一个输入口 + 显示名。
//
// 领域层：只 import 标准库。不读文件、不注册快捷键、不碰硬件。
package source

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/znahovo/switch-display/internal/domain/display"
)

// ID 是信号源的稳定标识（本地生成，导入导出时保持不变）。
type ID string

// SignalSource 是用户配置的「切到这个系统的显示口」。
type SignalSource struct {
	ID        ID
	Name      string // 显示名，如「工作 PC」
	Color     string // 卡片强调色 #rrggbb
	DisplayID display.DisplayID
	Input     display.InputValue
	Shortcut  string // 全局快捷键，空串表示没绑
	ConfirmMs int    // 切换后等多久读回确认，0 = 不确认
}

// 不变量（architecture.md §6）。
const (
	MaxNameLen       = 32
	MaxConfirmMs     = 10000
	DefaultConfirmMs = 1200
)

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// 领域错误。
var (
	ErrInvalidName  = errors.New("invalid source name")
	ErrInvalidColor = errors.New("invalid color")
	ErrInvalidDelay = errors.New("invalid confirm delay")
	ErrInvalidInput = errors.New("invalid input value")
	ErrDuplicateID  = errors.New("duplicate source id")
)

// New 构造一个通过校验的信号源。
//
// 这里顺手做三件归一化，让调用方（尤其是导入外部配置的前端）不必重复：
//   - 去掉名字首尾空白（用户手输时几乎总带空格）
//   - VCP 缺省为 0x60（输入源），因为绝大多数源就是切输入源
//   - 生成一个本地唯一 ID：ID 只是引用，不做业务含义
func New(name string, displayID display.DisplayID, input display.InputValue) (SignalSource, error) {
	if input.VCP == 0 {
		input.VCP = display.VCPInputSource
		if input.Alt {
			input.VCP = display.VCPInputSourceAlt
		}
	}
	s := SignalSource{
		ID:        newID(),
		Name:      strings.TrimSpace(name),
		DisplayID: displayID,
		Input:     input,
		ConfirmMs: DefaultConfirmMs,
	}
	return s, s.Validate()
}

// newID 生成 8 字符随机 ID。
//
// 用随机而不是自增序号：导入两份配置时不会撞 ID，
// 也让「删掉中间一个源」不会导致后面的源被迫改名。
func newID() ID {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 熵源不可用是系统级故障，但这里退化成固定值也比 panic 好：
		// 上层 Validate 会把重复 ID 报成用户可见的配置错误。
		return ID("src-fallback")
	}
	return ID("src-" + hex.EncodeToString(b[:]))
}

// Validate 校验不变量，返回第一个违反的领域错误。
//
// 只在「进入系统」与「变更」时调用；application 层还会额外校验跨源唯一性
// （快捷键、目标口），因为那需要看整个 Snapshot。
func (s SignalSource) Validate() error {
	if strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > MaxNameLen {
		return fmt.Errorf("%w: %q", ErrInvalidName, s.Name)
	}
	if s.Color != "" && !colorRe.MatchString(s.Color) {
		return fmt.Errorf("%w: %q", ErrInvalidColor, s.Color)
	}
	if s.ConfirmMs < 0 || s.ConfirmMs > MaxConfirmMs {
		return fmt.Errorf("%w: %d", ErrInvalidDelay, s.ConfirmMs)
	}
	if s.Input.VCP == 0 {
		return fmt.Errorf("%w: missing vcp code", ErrInvalidInput)
	}
	if s.DisplayID == "" {
		return fmt.Errorf("%w: missing display id", ErrInvalidInput)
	}
	return nil
}

// Rename 返回改名后的副本（不可变风格，便于 application 层做「改完再整体保存」）。
func (s SignalSource) Rename(name string) (SignalSource, error) {
	s.Name = name
	return s, s.Validate()
}
