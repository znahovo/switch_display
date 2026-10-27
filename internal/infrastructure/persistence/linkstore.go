package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/znahovo/switch-display/internal/domain/link"
)

// LinkStore 是 link.Repository 的文件实现。
//
// 与 JSONStore 同样采用「临时文件 → fsync → .bak → rename」，但多一层
// 容错：link.json 纯粹是运维态，读坏了与其让用户打不开软件，不如当作
// 首次运行重新生成设备 ID。
type LinkStore struct {
	path string
}

// NewLinkStore 构造 store。path 为空时用 DefaultLinkPath()。
func NewLinkStore(path string) (*LinkStore, error) {
	if path == "" {
		p, err := DefaultLinkPath()
		if err != nil {
			return nil, fmt.Errorf("resolve link path: %w", err)
		}
		path = p
	}
	return &LinkStore{path: path}, nil
}

// Path 返回文件绝对路径。
func (s *LinkStore) Path() string { return s.path }

// Load 读取 link.json。文件不存在、为空、解析失败或校验失败时都退回
// EmptySnapshot()，只把「读不动」这种真错误返回。
func (s *LinkStore) Load(ctx context.Context) (link.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return link.EmptySnapshot(), err
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return link.EmptySnapshot(), nil
		}
		return link.EmptySnapshot(), fmt.Errorf("read link config: %w", err)
	}
	if len(data) == 0 {
		return link.EmptySnapshot(), nil
	}

	var w wireLink
	if err := json.Unmarshal(data, &w); err != nil {
		return link.EmptySnapshot(), nil
	}
	snap := w.toDomain()
	if err := snap.Validate(); err != nil {
		return link.EmptySnapshot(), nil
	}
	return snap, nil
}

// Save 原子保存 link.json。
func (s *LinkStore) Save(ctx context.Context, snap link.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := snap.Validate(); err != nil {
		return fmt.Errorf("refuse to save invalid link config: %w", err)
	}

	data, err := json.MarshalIndent(fromLinkDomain(snap), "", "  ")
	if err != nil {
		return fmt.Errorf("encode link config: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create link config dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, LinkFileName+".tmp*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if _, err := os.Stat(s.path); err == nil {
		_ = copyFile(s.path, s.path+".bak")
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace link config: %w", err)
	}
	return nil
}

// ---- 磁盘格式 ----

type wireLink struct {
	Version  int             `json:"version"`
	DeviceID string          `json:"deviceId"`
	Name     string          `json:"deviceName"`
	LastHost string          `json:"lastHostId,omitempty"`
	Trusted  []wireTrustPeer `json:"trusted,omitempty"`
}

type wireTrustPeer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	PairedAt    string `json:"pairedAt,omitempty"`
}

func fromLinkDomain(s link.Snapshot) wireLink {
	w := wireLink{
		Version:  s.Version,
		DeviceID: s.Settings.DeviceID,
		Name:     s.Settings.DeviceName,
		LastHost: s.Settings.LastHostID,
		Trusted:  make([]wireTrustPeer, 0, len(s.Trusted)),
	}
	for _, t := range s.Trusted {
		p := wireTrustPeer{ID: t.ID, Name: t.Name, Fingerprint: t.Fingerprint}
		if !t.PairedAt.IsZero() {
			p.PairedAt = t.PairedAt.UTC().Format(time.RFC3339)
		}
		w.Trusted = append(w.Trusted, p)
	}
	return w
}

func (w wireLink) toDomain() link.Snapshot {
	snap := link.Snapshot{
		Version: w.Version,
		Settings: link.Settings{
			DeviceID:   w.DeviceID,
			DeviceName: w.Name,
			LastHostID: w.LastHost,
		},
		Trusted: make([]link.TrustedPeer, 0, len(w.Trusted)),
	}
	if snap.Version == 0 {
		snap.Version = link.CurrentVersion
	}
	for _, t := range w.Trusted {
		var pairedAt time.Time
		if t.PairedAt != "" {
			if ts, err := time.Parse(time.RFC3339, t.PairedAt); err == nil {
				pairedAt = ts
			}
		}
		snap.Trusted = append(snap.Trusted, link.TrustedPeer{
			ID:          t.ID,
			Name:        t.Name,
			Fingerprint: t.Fingerprint,
			PairedAt:    pairedAt,
		})
	}
	return snap
}
