package link

import (
	"context"
	"fmt"
	"time"
)

// CurrentVersion 是 link.json 的格式版本。
const CurrentVersion = 1

// TrustedPeer 是用户明确同意过配对的一台设备。
//
// 存的是「对方证书指纹」，因为 Deskflow 的信任库是纯指纹列表；
// 我们额外记住 ID 与名字，只为了在 UI 里显示「已配对设备」并支持解绑。
type TrustedPeer struct {
	// ID 是对端设备 ID。
	ID string
	// Name 是对端设备名（配对时的快照）。
	Name string
	// Fingerprint 是对端 Deskflow 证书指纹，`v2:sha256:<hex>`。
	Fingerprint string
	// PairedAt 是配对成功的时间。
	PairedAt time.Time
}

// Validate 校验一条已配对记录。
func (t TrustedPeer) Validate() error {
	if !ValidDeviceID(t.ID) {
		return fmt.Errorf("%w: %q", ErrInvalidDeviceID, t.ID)
	}
	if t.Name == "" || len([]rune(t.Name)) > MaxDeviceNameLen {
		return fmt.Errorf("%w: %q", ErrInvalidName, t.Name)
	}
	if !ValidFingerprint(t.Fingerprint) {
		return fmt.Errorf("%w: %q", ErrInvalidFingerprint, t.Fingerprint)
	}
	return nil
}

// Settings 是本机的共享身份设置（跨重启稳定）。
type Settings struct {
	// DeviceID 是本机设备 ID，首次运行生成后不再改变。
	DeviceID string
	// DeviceName 是本机展示名，用户可改。
	DeviceName string
	// LastHostID 是上次作为客机时配对的主机 ID，仅用于 UI 快捷方式。
	LastHostID string
}

// DefaultSettings 返回出厂设置（DeviceID 为空，由用例层首次运行时补齐）。
func DefaultSettings() Settings {
	return Settings{DeviceID: "", DeviceName: "", LastHostID: ""}
}

// Snapshot 是 link.json 的完整内容。
type Snapshot struct {
	// Version 是格式版本。
	Version int
	// Settings 是本机身份设置。
	Settings Settings
	// Trusted 是已配对设备列表。
	Trusted []TrustedPeer
}

// EmptySnapshot 返回一份空快照（首次运行、文件不存在时使用）。
func EmptySnapshot() Snapshot {
	return Snapshot{Version: CurrentVersion, Settings: DefaultSettings(), Trusted: nil}
}

// Validate 校验快照的不变量：
//   - 版本在支持范围内；
//   - DeviceID 非空时必须是合法 ID；
//   - DeviceName 非空时必须是合法名；
//   - 已配对列表里 ID 与指纹都不重复。
func (s Snapshot) Validate() error {
	if s.Version < 1 || s.Version > CurrentVersion {
		return fmt.Errorf("%w: %d", ErrInvalidMessage, s.Version)
	}
	if s.Settings.DeviceID != "" && !ValidDeviceID(s.Settings.DeviceID) {
		return fmt.Errorf("%w: %q", ErrInvalidDeviceID, s.Settings.DeviceID)
	}
	if s.Settings.DeviceName != "" {
		if len([]rune(s.Settings.DeviceName)) > MaxDeviceNameLen {
			return fmt.Errorf("%w: %q", ErrInvalidName, s.Settings.DeviceName)
		}
	}
	if s.Settings.LastHostID != "" && !ValidDeviceID(s.Settings.LastHostID) {
		return fmt.Errorf("%w: %q", ErrInvalidDeviceID, s.Settings.LastHostID)
	}

	seenID := make(map[string]struct{}, len(s.Trusted))
	seenFP := make(map[string]struct{}, len(s.Trusted))
	for _, t := range s.Trusted {
		if err := t.Validate(); err != nil {
			return err
		}
		if _, dup := seenID[t.ID]; dup {
			return fmt.Errorf("%w: duplicate peer %q", ErrInvalidDeviceID, t.ID)
		}
		if _, dup := seenFP[t.Fingerprint]; dup {
			return fmt.Errorf("%w: duplicate fingerprint for %q", ErrInvalidFingerprint, t.Name)
		}
		seenID[t.ID] = struct{}{}
		seenFP[t.Fingerprint] = struct{}{}
	}
	return nil
}

// FindTrusted 按设备 ID 查找已配对记录。
func (s Snapshot) FindTrusted(id string) (TrustedPeer, bool) {
	for _, t := range s.Trusted {
		if t.ID == id {
			return t, true
		}
	}
	return TrustedPeer{}, false
}

// Repository 是 link.json 的持久化端口，实现方在 infrastructure/persistence。
type Repository interface {
	// Load 读取快照；文件不存在时返回 EmptySnapshot。
	Load(ctx context.Context) (Snapshot, error)
	// Save 原子写入快照。
	Save(ctx context.Context, s Snapshot) error
	// Path 返回文件路径（用于 UI 展示）。
	Path() string
}
