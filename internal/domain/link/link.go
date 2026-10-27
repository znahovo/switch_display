// Package link 定义「局域网键盘鼠标共享」的领域模型：本机身份、对端设备、角色与信令。
//
// 领域层：只 import 标准库。不做网络 IO、不跑子进程、不读文件——那些在
// infrastructure/link 里。这里只负责「什么是合法的一个对端、一条报文」。
package link

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// Role 是本机在共享关系里的角色。
//
// 注意 idle 不是「未初始化」而是「空闲」：发现服务仍在跑，只是没有跑
// deskflow-core。这样 UI 才能区分「没开共享」与「开共享失败」。
type Role string

const (
	// RoleIdle 表示本机没有参与共享（不跑 core，但仍在广播、仍能看到对端）。
	RoleIdle Role = "idle"
	// RoleServer 表示本机是主机：把自己的鼠标键盘共享出去。
	RoleServer Role = "server"
	// RoleClient 表示本机是客机：使用别的机器的鼠标键盘。
	RoleClient Role = "client"
)

// Valid 报告角色是否是三个合法值之一。
func (r Role) Valid() bool {
	switch r {
	case RoleIdle, RoleServer, RoleClient:
		return true
	}
	return false
}

// 不变量（见 docs/deskflow-integration.md §5）。
const (
	// DeviceIDLen 是设备 ID 的字符数（128 bit 随机数的 hex 表示）。
	DeviceIDLen = 32
	// MaxDeviceNameLen 是设备名最大字符数。
	MaxDeviceNameLen = 64
	// DefaultPort 是 Deskflow core 监听的 TCP 端口。
	DefaultPort = 24800
)

var (
	deviceIDRe    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	fingerprintRe = regexp.MustCompile(`^v2:sha256:[0-9a-f]{64}$`)
)

// computerNamePrefix 是派生 Deskflow 主机名的前缀。
const computerNamePrefix = "sd-"

// ComputerNameFor 由设备 ID 派生 Deskflow 里的主机名。
//
// 为什么派生而不是用用户填的设备名：server 布局文件与 client 的
// `[core] computerName` 必须是同一个字符串，而两侧只有设备 ID 是天然一致的。
// 设备 ID 是 32 位随机 hex，取前 10 位做后缀足够避免碰撞，且满足
// Deskflow 的 isValidComputerName（首字符字母、只含 [-_A-Za-z0-9]）。
func ComputerNameFor(deviceID string) string {
	if len(deviceID) < computerNameSuffixLen {
		return ""
	}
	return computerNamePrefix + deviceID[:computerNameSuffixLen]
}

// computerNameSuffixLen 是从设备 ID 取来当主机名后缀的字符数。
const computerNameSuffixLen = 10

// NewDeviceID 生成一个新的设备 ID（16 字节随机数的 hex）。
//
// 为什么不用 hostname 当 ID：同一间办公室有两台叫 MacBook-Pro 的机器很常见，
// 而配对与信任库都必须精确到一台设备。
func NewDeviceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate device id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// ValidDeviceID 报告 id 是否是合法设备 ID。
func ValidDeviceID(id string) bool { return deviceIDRe.MatchString(id) }

// ValidFingerprint 报告 fp 是否是 Deskflow 信任库认得的指纹格式。
//
// 格式来自 `Fingerprint::toDbLine()`：`v2:sha256:<64 位小写 hex>`。
// 大写或带冒号的形式是 GUI 给人看的，写进信任库会被判为非法行。
func ValidFingerprint(fp string) bool { return fingerprintRe.MatchString(fp) }

// CleanDeviceName 归一化用户输入或系统 hostname 得到的设备名：
// 去掉首尾空白、控制字符，把连续空白压成一个空格，并截断到上限。
func CleanDeviceName(name string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range name {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if lastSpace || b.Len() == 0 {
				continue
			}
			lastSpace = true
			b.WriteByte(' ')
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		lastSpace = false
		if b.Len() >= MaxDeviceNameLen {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// Peer 是局域网里发现的另一台正在运行本软件的设备。
type Peer struct {
	// ID 是对端的设备 ID（不是 hostname）。
	ID string
	// Name 是对端展示名。
	Name string
	// Addr 是发现服务的来源地址（UDP 包里的来源，用于回复信令）。
	Addr netip.AddrPort
	// Fingerprint 是对端 Deskflow 证书指纹；空串表示对端还没生成证书。
	Fingerprint string
	// Role 是对端自报的角色。
	Role Role
	// LastSeen 是最后一次收到它对端广播的时间。
	LastSeen time.Time
}

// Validate 校验对端的基本不变量。
func (p Peer) Validate() error {
	if !ValidDeviceID(p.ID) {
		return fmt.Errorf("%w: %q", ErrInvalidDeviceID, p.ID)
	}
	if p.Name == "" || len([]rune(p.Name)) > MaxDeviceNameLen {
		return fmt.Errorf("%w: %q", ErrInvalidName, p.Name)
	}
	if !p.Role.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidRole, string(p.Role))
	}
	if p.Fingerprint != "" && !ValidFingerprint(p.Fingerprint) {
		return fmt.Errorf("%w: %q", ErrInvalidFingerprint, p.Fingerprint)
	}
	return nil
}

// Stale 报告该对端是否已经超过 ttl 没有广播（应当从列表里移除）。
func (p Peer) Stale(now time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	return now.Sub(p.LastSeen) > ttl
}

// Self 是本机身份 + 当前广播内容。它决定 announce 报文的内容。
type Self struct {
	ID          string
	Name        string
	Role        Role
	Fingerprint string
	// Port 是 Deskflow core 的 TCP 端口，随 announce 告知对端。
	Port int
}

// Validate 校验本机身份。
func (s Self) Validate() error {
	if !ValidDeviceID(s.ID) {
		return fmt.Errorf("%w: %q", ErrInvalidDeviceID, s.ID)
	}
	if s.Name == "" || len([]rune(s.Name)) > MaxDeviceNameLen {
		return fmt.Errorf("%w: %q", ErrInvalidName, s.Name)
	}
	if !s.Role.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidRole, string(s.Role))
	}
	if s.Fingerprint != "" && !ValidFingerprint(s.Fingerprint) {
		return fmt.Errorf("%w: %q", ErrInvalidFingerprint, s.Fingerprint)
	}
	if s.Port < 0 || s.Port > 65535 {
		return fmt.Errorf("%w: %d", ErrInvalidPort, s.Port)
	}
	return nil
}
