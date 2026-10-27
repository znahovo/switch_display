package deskflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 托管配置文件名。core 用 `-s` 指向第一个文件，server 布局由
// `[server] externalConfigFile` 指向第二个文件。
const (
	// SettingsFileName 是 core 的 QSettings INI 文件。
	SettingsFileName = "deskflow.conf"
	// ServerConfigFileName 是 server 模式的布局文件。
	ServerConfigFileName = "deskflow-server.conf"
)

// SettingsPaths 是一次运行的配置文件路径集合。
type SettingsPaths struct {
	// Settings 是 deskflow.conf。
	Settings string
	// ServerConfig 是 deskflow-server.conf。
	ServerConfig string
	// TLS 是证书与信任库路径。
	TLS CertPaths
}

// Paths 返回 dir 下的配置文件路径集合。
func Paths(dir string) SettingsPaths {
	return SettingsPaths{
		Settings:     filepath.Join(dir, SettingsFileName),
		ServerConfig: filepath.Join(dir, ServerConfigFileName),
		TLS:          TLSPaths(dir),
	}
}

// Mode 是 core 的运行模式。
type Mode string

const (
	// ModeServer 让本机共享出自己的鼠标键盘。
	ModeServer Mode = "server"
	// ModeClient 让本机使用对端的鼠标键盘。
	ModeClient Mode = "client"
)

// Validate 报告模式是否是 server/client 之一。
func (m Mode) Validate() bool { return m == ModeServer || m == ModeClient }

// CoreConfig 是写 deskflow.conf 所需的全部输入。
type CoreConfig struct {
	// Mode 是 server 或 client。
	Mode Mode
	// ComputerName 是本机在 Deskflow 里的主机名；server 布局文件必须用它。
	// 必须已被归一化（见 CleanComputerName）。
	ComputerName string
	// Port 是 TCP 端口。
	Port int
	// RemoteHost 是 client 模式下的主机地址（可逗号分隔多个）。
	RemoteHost string
	// SettingsPath 是 deskflow.conf 的路径（写进注释，便于排查）。
	SettingsPath string
	// ServerConfigPath 是布局文件路径。
	ServerConfigPath string
	// CertPath 是证书 PEM 路径。
	CertPath string
	// Interface 是本机绑定地址；留空表示所有网卡。
	Interface string
	// Computers 是 server 模式下要写进 deskflow.conf 的机器名列表
	// （含本机与所有 client）。**必须写**：Deskflow 的 Config::read 用
	// `Settings::knownComputers()`（即 `computer_*` 段里的 `name`）来判定
	// 布局文件里的主机名是否已知，缺一个就报 `unknown computer name`。
	Computers []string
}

// WriteCoreConfig 写出 deskflow.conf。
//
// 只写我们用到的键：coreMode / computerName / port / certificate 等。
// 全部显式写死而不依赖默认值——Deskflow 的默认值随版本变化过，
// 显式写才能保证换版本后行为不变。
func WriteCoreConfig(cfg CoreConfig) error {
	if !cfg.Mode.Validate() {
		return fmt.Errorf("invalid core mode %q", cfg.Mode)
	}
	name := CleanComputerName(cfg.ComputerName)
	if name == "" {
		return fmt.Errorf("empty computer name")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("invalid port %d", cfg.Port)
	}

	var b strings.Builder
	b.WriteString("# 由 Switch Display 生成，请勿手工编辑（每次启动/切换角色都会重写）。\n")
	fmt.Fprintf(&b, "# settings file: %s\n\n", cfg.SettingsPath)

	b.WriteString("[core]\n")
	if cfg.Mode == ModeServer {
		b.WriteString("coreMode=2\n")
	} else {
		b.WriteString("coreMode=1\n")
	}
	fmt.Fprintf(&b, "computerName=%s\n", name)
	fmt.Fprintf(&b, "port=%d\n", cfg.Port)
	if cfg.Interface != "" {
		fmt.Fprintf(&b, "interface=%s\n", cfg.Interface)
	}
	b.WriteString("\n")

	if cfg.Mode == ModeClient {
		b.WriteString("[client]\n")
		fmt.Fprintf(&b, "remoteHost=%s\n", cfg.RemoteHost)
		b.WriteString("languageSync=false\n")
		b.WriteString("\n")
	}

	if cfg.Mode == ModeServer {
		b.WriteString("[server]\n")
		// 显式指向我们自己的布局文件，避免 core 去猜默认路径。
		b.WriteString("externalConfig=true\n")
		fmt.Fprintf(&b, "externalConfigFile=%s\n", cfg.ServerConfigPath)
		b.WriteString("enableClipboard=true\n")
		b.WriteString("\n")
	}

	b.WriteString("[security]\n")
	b.WriteString("tlsEnabled=true\n")
	// 保持 true：我们通过预置信任库来实现零点击配对，
	// 而不是关掉指纹校验（见 ADR-010）。
	b.WriteString("checkPeerFingerprints=true\n")
	fmt.Fprintf(&b, "certificate=%s\n", cfg.CertPath)
	fmt.Fprintf(&b, "keySize=%d\n", certKeyBits)
	b.WriteString("\n")

	b.WriteString("[log]\n")
	b.WriteString("level=info\n")
	b.WriteString("toFile=false\n")

	// server 模式的计算机段：布局文件里的每个主机名都必须在
	// `computer_<name>/name` 里声明过（见 CoreConfig.Computers 的注释）。
	if cfg.Mode == ModeServer {
		names := cleanComputerNames(append([]string{name}, cfg.Computers...))
		for _, n := range names {
			b.WriteString("\n")
			fmt.Fprintf(&b, "[computer_%s]\n", n)
			fmt.Fprintf(&b, "name=%s\n", n)
		}
	}

	return writeFileAtomic(cfg.SettingsPath, []byte(b.String()), 0o600)
}

// cleanComputerNames 归一化并去重一组主机名，保持出现顺序。
func cleanComputerNames(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		n := CleanComputerName(raw)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// computerNameRe 对应 Deskflow 的 isValidComputerName：每段
// `[_A-Za-z0-9][-_A-Za-z0-9]*[_A-Za-z0-9]`，段之间用 `.` 分隔。
var computerNameRe = regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_-]*[A-Za-z0-9_])?(\.[A-Za-z0-9_]([A-Za-z0-9_-]*[A-Za-z0-9_])?)*$`)

// CleanComputerName 把任意字符串归一化成 Deskflow 能接受的主机名。
//
// 之所以必须自己归一化：Deskflow 的 `cleanComputerName` 会保留空格为下划线，
// 而 UI 上用户可能输入中文或表情；server 布局文件里主机名不合法会让 core
// 以 s_exitConfig=4 退出，报错信息还很难懂。
func CleanComputerName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
			lastDash = false
		case r == '-':
			if lastDash || b.Len() == 0 {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		case r == ' ' || r == '\t' || r == '.':
			// 空格与点都当分隔符处理（点是合法的段分隔，但这里用连字符统一）。
			if lastDash || b.Len() == 0 {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		default:
			// 其它字符（中文、表情、标点）直接丢弃：主机名只用于 Deskflow 内部标识，
			// 用户看到的是我们自己的 DeviceName。
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return ""
	}
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-_.")
	}
	if !computerNameRe.MatchString(out) {
		// 兜底：出现了意外字符，退化成合法形式。
		var safe strings.Builder
		for _, r := range out {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				safe.WriteRune(r)
			}
		}
		out = strings.Trim(safe.String(), "_")
	}
	return out
}

// LayoutLink 是一条布局边：from 的 side 方向是 to。
type LayoutLink struct {
	// From 是源主机名（必须是 server 布局里声明的规范名）。
	From string
	// Side 是 left/right/up/down。
	Side string
	// To 是目标主机名。
	To string
}

// WriteServerLayout 写 server 布局文件。
//
// 必须双向声明：只写 A right=B 时，鼠标能从 A 过去但回不来。
//
// 允许空 links：只有本机一台时 Deskflow 仍需要一个 `section: links ... end`
// 的空段才能通过解析（readSectionLinks 会一直读到 `end`）。
func WriteServerLayout(path string, links []LayoutLink) error {

	// 按 From 分组，保持稳定顺序（便于 diff 与人工检查）。
	byFrom := make(map[string][]LayoutLink, len(links))
	order := make([]string, 0, len(links))
	for _, l := range links {
		if l.From == "" || l.To == "" {
			return fmt.Errorf("link with empty host name: %+v", l)
		}
		if l.From == l.To {
			return fmt.Errorf("link from %q to itself", l.From)
		}
		switch l.Side {
		case "left", "right", "up", "down":
		default:
			return fmt.Errorf("invalid side %q", l.Side)
		}
		if _, ok := byFrom[l.From]; !ok {
			order = append(order, l.From)
		}
		byFrom[l.From] = append(byFrom[l.From], l)
	}
	sort.Strings(order)

	var b strings.Builder
	b.WriteString("# 由 Switch Display 生成，请勿手工编辑。\n")
	b.WriteString("# 语法见 Deskflow src/lib/server/Config.cpp:readSectionLinks。\n\n")
	b.WriteString("section: links\n")
	for _, from := range order {
		fmt.Fprintf(&b, "\t%s:\n", from)
		for _, l := range byFrom[from] {
			fmt.Fprintf(&b, "\t\t%s = %s\n", l.Side, l.To)
		}
	}
	b.WriteString("end\n")
	return writeFileAtomic(path, []byte(b.String()), 0o600)
}

// HorizontalLinks 返回两台机器「左右相邻」的双向链接。
//
// left 在左、right 在右，鼠标从 left 的右缘进入 right，反之亦然。
func HorizontalLinks(left, right string) []LayoutLink {
	return []LayoutLink{
		{From: left, Side: "right", To: right},
		{From: right, Side: "left", To: left},
	}
}

// WriteTrustedClients 写 server 侧信任库（信任哪些客机的证书）。
func WriteTrustedClients(path string, fingerprints []string) error {
	return writeTrustDB(path, fingerprints)
}

// WriteTrustedServers 写 client 侧信任库（信任哪些主机的证书）。
func WriteTrustedServers(path string, fingerprints []string) error {
	return writeTrustDB(path, fingerprints)
}

// writeTrustDB 写指纹库。
//
// Deskflow 的 `verifyCertFingerprint` 有个坑：文件存在但解析出 0 条指纹时
// 直接报 "failed to open trusted fingerprints file" 并拒绝连接。所以宁可
// 不创建文件，也不要写一个空文件——因此空列表时删掉文件。
func writeTrustDB(path string, fingerprints []string) error {
	cleaned := make([]string, 0, len(fingerprints))
	seen := make(map[string]struct{}, len(fingerprints))
	for _, fp := range fingerprints {
		fp = strings.TrimSpace(fp)
		if fp == "" {
			continue
		}
		if _, dup := seen[fp]; dup {
			continue
		}
		seen[fp] = struct{}{}
		cleaned = append(cleaned, fp)
	}
	sort.Strings(cleaned)

	if len(cleaned) == 0 {
		// 不留下空文件（见函数注释）。
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove empty trust db: %w", err)
		}
		return nil
	}

	var b strings.Builder
	for _, fp := range cleaned {
		b.WriteString(fp)
		b.WriteString("\n")
	}
	return writeFileAtomic(path, []byte(b.String()), 0o600)
}

// ReadTrustDB 读取指纹库（不存在返回空）。
func ReadTrustDB(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}
