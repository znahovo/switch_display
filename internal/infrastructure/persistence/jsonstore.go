package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

// JSONStore 是 source.Repository 的文件实现。
type JSONStore struct {
	path string
}

// NewJSONStore 构造 store。path 为空时用 DefaultPath()。
func NewJSONStore(path string) (*JSONStore, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, fmt.Errorf("resolve config path: %w", err)
		}
		path = p
	}
	return &JSONStore{path: path}, nil
}

// Path 返回配置文件绝对路径。
func (s *JSONStore) Path() string { return s.path }

// Load 读取配置。文件不存在视为「首次运行」，返回空快照且不报错——
// 让调用方只需要处理「真的坏了」这一种错误。
func (s *JSONStore) Load(ctx context.Context) (source.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return source.EmptySnapshot(), err
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return source.EmptySnapshot(), nil
		}
		return source.EmptySnapshot(), fmt.Errorf("read config: %w", err)
	}

	// 空文件（比如上次写入中途断电留下的）同样按首次运行处理。
	if len(data) == 0 {
		return source.EmptySnapshot(), nil
	}

	var w wireSnapshot
	if err := json.Unmarshal(data, &w); err != nil {
		return source.EmptySnapshot(), fmt.Errorf("parse config %s: %w", s.path, err)
	}

	snap, err := w.toDomain()
	if err != nil {
		return source.EmptySnapshot(), fmt.Errorf("decode config %s: %w", s.path, err)
	}
	return snap, nil
}

// Save 原子地保存配置。
//
// 步骤：写临时文件 → fsync → 备份现有文件为 .bak → rename。
// rename 在同一文件系统内是原子操作，所以任何时刻磁盘上都只有完整的旧版本
// 或完整的新版本。
func (s *JSONStore) Save(ctx context.Context, snap source.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// 保存前必须校验：不允许把非法配置写进磁盘，否则下次启动直接读到坏数据。
	if err := snap.Validate(); err != nil {
		return fmt.Errorf("refuse to save invalid config: %w", err)
	}

	wire := fromDomain(snap)
	data, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n') // 结尾换行，便于 git diff / 手工编辑

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ConfigFileName+".tmp*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// 任何提前返回都清理临时文件；成功 rename 后这次 Remove 会失败，无副作用。
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	// fsync 是必须的：否则 rename 可能先落盘，内容还在页缓存里。
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// 备份旧文件。失败不影响主流程（首次保存时本来就没有旧文件）。
	if _, err := os.Stat(s.path); err == nil {
		_ = copyFile(s.path, s.path+".bak")
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// Export 把当前配置写到用户指定的路径（用于「导出配置」）。
func (s *JSONStore) Export(ctx context.Context, snap source.Snapshot, dest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := snap.Validate(); err != nil {
		return fmt.Errorf("refuse to export invalid config: %w", err)
	}
	data, err := json.MarshalIndent(fromDomain(snap), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}

// Import 读取并校验一个外部配置文件，返回解析后的快照（不落盘）。
func (s *JSONStore) Import(ctx context.Context, src string) (source.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return source.EmptySnapshot(), err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return source.EmptySnapshot(), fmt.Errorf("read %s: %w", src, err)
	}
	var w wireSnapshot
	if err := json.Unmarshal(data, &w); err != nil {
		return source.EmptySnapshot(), fmt.Errorf("parse %s: %w", src, err)
	}
	snap, err := w.toDomain()
	if err != nil {
		return source.EmptySnapshot(), err
	}
	if err := snap.Validate(); err != nil {
		return source.EmptySnapshot(), fmt.Errorf("validate %s: %w", src, err)
	}
	return snap, nil
}

// ---- 磁盘格式（与领域模型解耦，保证领域重构不会破坏已有配置文件） ----

// wireSnapshot 是配置文件里实际出现的结构。
type wireSnapshot struct {
	Version  int          `json:"version"`
	Settings wireSettings `json:"settings"`
	Sources  []wireSource `json:"sources"`
}

type wireSettings struct {
	Language           string `json:"language"`
	ConfirmAfterSwitch bool   `json:"confirmAfterSwitch"`
	DefaultConfirmMs   int    `json:"defaultConfirmMs"`
	HideToTrayOnClose  bool   `json:"hideToTrayOnClose"`
	SelfSourceID       string `json:"selfSourceId,omitempty"`
}

type wireSource struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color,omitempty"`
	DisplayID string    `json:"displayId"`
	Input     wireInput `json:"input"`
	Shortcut  string    `json:"shortcut,omitempty"`
	ConfirmMs int       `json:"confirmMs"`
}

type wireInput struct {
	VCP   uint8  `json:"vcp"`
	Value uint16 `json:"value"`
	Alt   bool   `json:"alt,omitempty"`
}

func fromDomain(s source.Snapshot) wireSnapshot {
	w := wireSnapshot{
		Version: s.Version,
		Settings: wireSettings{
			Language:           s.Settings.Language,
			ConfirmAfterSwitch: s.Settings.ConfirmAfterSwitch,
			DefaultConfirmMs:   s.Settings.DefaultConfirmMs,
			HideToTrayOnClose:  s.Settings.HideToTrayOnClose,
			SelfSourceID:       s.Settings.SelfSourceID,
		},
		Sources: make([]wireSource, 0, len(s.Sources)),
	}
	for _, src := range s.Sources {
		w.Sources = append(w.Sources, wireSource{
			ID:        string(src.ID),
			Name:      src.Name,
			Color:     src.Color,
			DisplayID: string(src.DisplayID),
			Input: wireInput{
				VCP:   uint8(src.Input.VCP),
				Value: src.Input.Value,
				Alt:   src.Input.Alt,
			},
			Shortcut:  src.Shortcut,
			ConfirmMs: src.ConfirmMs,
		})
	}
	return w
}

func (w wireSnapshot) toDomain() (source.Snapshot, error) {
	migrated, err := migrate(w)
	if err != nil {
		return source.EmptySnapshot(), err
	}

	snap := source.Snapshot{
		Version: migrated.Version,
		Settings: source.Settings{
			Language:           migrated.Settings.Language,
			ConfirmAfterSwitch: migrated.Settings.ConfirmAfterSwitch,
			DefaultConfirmMs:   migrated.Settings.DefaultConfirmMs,
			HideToTrayOnClose:  migrated.Settings.HideToTrayOnClose,
			SelfSourceID:       migrated.Settings.SelfSourceID,
		},
		Sources: make([]source.SignalSource, 0, len(migrated.Sources)),
	}
	for _, src := range migrated.Sources {
		snap.Sources = append(snap.Sources, source.SignalSource{
			ID:        source.ID(src.ID),
			Name:      src.Name,
			Color:     src.Color,
			DisplayID: display.DisplayID(src.DisplayID),
			Input: display.InputValue{
				VCP:   display.VCPCode(src.Input.VCP),
				Value: src.Input.Value,
				Alt:   src.Input.Alt,
			},
			Shortcut:  src.Shortcut,
			ConfirmMs: src.ConfirmMs,
		})
	}
	return snap, nil
}

// copyFile 用于生成 .bak；失败只丢备份，不影响主流程。
func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o644)
}
