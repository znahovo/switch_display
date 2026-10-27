package deskflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanComputerName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "my-mac", "my-mac"},
		{"underscore", "my_mac", "my_mac"},
		{"space to hyphen", "MacBook Pro", "MacBook-Pro"},
		{"chinese dropped", "我的电脑", ""},
		{"mixed", "Mac 我的 Pro", "Mac-Pro"},
		{"dots to hyphen then trimmed", ".mac.", "mac"},
		{"leading hyphen trimmed", "-mac-", "mac"},
		{"empty", "", ""},
		{"only symbols", "@#$", ""},
		{"long truncated", strings.Repeat("a", 100), strings.Repeat("a", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanComputerName(tt.in)
			if got != tt.want {
				t.Fatalf("CleanComputerName(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// 结果必须匹配 Deskflow 的 isValidComputerName。
			if got != "" && !computerNameRe.MatchString(got) {
				t.Fatalf("CleanComputerName(%q) = %q, not a valid computer name", tt.in, got)
			}
		})
	}
}

func TestWriteCoreConfigServer(t *testing.T) {
	dir := t.TempDir()
	paths := Paths(dir)
	err := WriteCoreConfig(CoreConfig{
		Mode:             ModeServer,
		ComputerName:     "alpha",
		Port:             24800,
		SettingsPath:     paths.Settings,
		ServerConfigPath: paths.ServerConfig,
		CertPath:         paths.TLS.Cert,
	})
	if err != nil {
		t.Fatalf("WriteCoreConfig() error = %v", err)
	}

	data, err := os.ReadFile(paths.Settings)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)

	wants := []string{
		"[core]",
		"coreMode=2",
		"computerName=alpha",
		"port=24800",
		"[server]",
		"externalConfig=true",
		"externalConfigFile=" + paths.ServerConfig,
		"[security]",
		"tlsEnabled=true",
		"checkPeerFingerprints=true",
		"certificate=" + paths.TLS.Cert,
		"keySize=2048",
	}
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Fatalf("config missing %q:\n%s", w, text)
		}
	}
	// server 模式不应出现 [client] 段。
	if strings.Contains(text, "[client]") {
		t.Fatalf("server config must not contain [client]:\n%s", text)
	}
}

func TestWriteCoreConfigClient(t *testing.T) {
	dir := t.TempDir()
	paths := Paths(dir)
	err := WriteCoreConfig(CoreConfig{
		Mode:         ModeClient,
		ComputerName: "beta",
		Port:         24800,
		RemoteHost:   "192.168.1.20",
		SettingsPath: paths.Settings,
		CertPath:     paths.TLS.Cert,
	})
	if err != nil {
		t.Fatalf("WriteCoreConfig() error = %v", err)
	}
	data, err := os.ReadFile(paths.Settings)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "coreMode=1") {
		t.Fatalf("client config missing coreMode=1:\n%s", text)
	}
	if !strings.Contains(text, "[client]") || !strings.Contains(text, "remoteHost=192.168.1.20") {
		t.Fatalf("client config missing remoteHost:\n%s", text)
	}
	if strings.Contains(text, "[server]") {
		t.Fatalf("client config must not contain [server]:\n%s", text)
	}
}

func TestWriteCoreConfigRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	paths := Paths(dir)

	cases := []struct {
		name string
		cfg  CoreConfig
	}{
		{"bad mode", CoreConfig{Mode: "host", ComputerName: "a", Port: 1, SettingsPath: paths.Settings}},
		{"empty name", CoreConfig{Mode: ModeServer, ComputerName: "中文", Port: 1, SettingsPath: paths.Settings}},
		{"bad port", CoreConfig{Mode: ModeServer, ComputerName: "a", Port: 0, SettingsPath: paths.Settings}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := WriteCoreConfig(tt.cfg); err == nil {
				t.Fatal("WriteCoreConfig() = nil error, want error")
			}
		})
	}
}

func TestWriteServerLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ServerConfigFileName)

	if err := WriteServerLayout(path, HorizontalLinks("alpha", "beta")); err != nil {
		t.Fatalf("WriteServerLayout() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)

	// 必须双向：否则鼠标过去回不来。
	for _, want := range []string{
		"section: links",
		"alpha:",
		"right = beta",
		"beta:",
		"left = alpha",
		"end",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("layout missing %q:\n%s", want, text)
		}
	}
}

// TestWriteServerLayoutEmpty：空 links 段是合法的。
//
// Deskflow 的 readSectionLinks 会一直读到 `end`，只有本机一台时
// 布局文件就必须是「有 section 但没条目」。
func TestWriteServerLayoutEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ServerConfigFileName)

	if err := WriteServerLayout(path, nil); err != nil {
		t.Fatalf("WriteServerLayout(nil) error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "section: links") {
		t.Fatalf("layout missing 'section: links':\n%s", text)
	}
	if !strings.Contains(text, "end") {
		t.Fatalf("layout missing 'end':\n%s", text)
	}
}

func TestWriteServerLayoutRejects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ServerConfigFileName)

	cases := []struct {
		name  string
		links []LayoutLink
	}{
		{"self link", []LayoutLink{{From: "a", Side: "right", To: "a"}}},
		{"bad side", []LayoutLink{{From: "a", Side: "middle", To: "b"}}},
		{"empty name", []LayoutLink{{From: "", Side: "right", To: "b"}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := WriteServerLayout(path, tt.links); err == nil {
				t.Fatal("WriteServerLayout() = nil error, want error")
			}
		})
	}
}

func TestTrustDBWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trusted-clients")
	fp1 := "v2:sha256:" + strings.Repeat("a", 64)
	fp2 := "v2:sha256:" + strings.Repeat("b", 64)

	if err := WriteTrustedClients(path, []string{fp2, fp1, fp1, "  "}); err != nil {
		t.Fatalf("WriteTrustedClients() error = %v", err)
	}

	got, err := ReadTrustDB(path)
	if err != nil {
		t.Fatalf("ReadTrustDB() error = %v", err)
	}
	// 去重 + 排序。
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(got), got)
	}
	if got[0] != fp1 || got[1] != fp2 {
		t.Fatalf("entries = %v, want [fp1 fp2]", got)
	}

	// 每行一个指纹，行尾换行（Deskflow 按行读）。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("trust db must end with newline: %q", string(data))
	}
}

func TestTrustDBEmptyRemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trusted-servers")

	// 先写一条。
	fp := "v2:sha256:" + strings.Repeat("c", 64)
	if err := WriteTrustedServers(path, []string{fp}); err != nil {
		t.Fatalf("WriteTrustedServers() error = %v", err)
	}
	// 再清空：文件必须被删掉，而不是留一个空文件。
	// Deskflow 的 verifyCertFingerprint 对「存在但没有指纹」的文件会直接拒绝连接。
	if err := WriteTrustedServers(path, nil); err != nil {
		t.Fatalf("WriteTrustedServers(empty) error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty trust db should be removed, stat err = %v", err)
	}
}

func TestHorizontalLinks(t *testing.T) {
	links := HorizontalLinks("alpha", "beta")
	if len(links) != 2 {
		t.Fatalf("len = %d, want 2", len(links))
	}
	if links[0].From != "alpha" || links[0].Side != "right" || links[0].To != "beta" {
		t.Fatalf("links[0] = %+v", links[0])
	}
	if links[1].From != "beta" || links[1].Side != "left" || links[1].To != "alpha" {
		t.Fatalf("links[1] = %+v", links[1])
	}
}

func TestModeValidate(t *testing.T) {
	if !ModeServer.Validate() || !ModeClient.Validate() {
		t.Fatal("server/client must be valid")
	}
	if Mode("").Validate() || Mode("host").Validate() {
		t.Fatal("invalid modes accepted")
	}
}

func TestWriteCoreConfigServerComputers(t *testing.T) {
	dir := t.TempDir()
	paths := Paths(dir)
	err := WriteCoreConfig(CoreConfig{
		Mode:             ModeServer,
		ComputerName:     "alpha",
		Port:             24800,
		SettingsPath:     paths.Settings,
		ServerConfigPath: paths.ServerConfig,
		CertPath:         paths.TLS.Cert,
		Computers:        []string{"beta", "alpha", "beta", "中文"},
	})
	if err != nil {
		t.Fatalf("WriteCoreConfig() error = %v", err)
	}
	data, err := os.ReadFile(paths.Settings)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)

	// 布局文件里的主机名必须在 computer_<name>/name 里声明，
	// 否则 Deskflow 报 unknown computer name 并以 s_exitConfig 退出。
	for _, want := range []string{
		"[computer_alpha]\nname=alpha",
		"[computer_beta]\nname=beta",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
	}
	// 去重：beta 只出现一次。
	if n := strings.Count(text, "[computer_beta]"); n != 1 {
		t.Fatalf("[computer_beta] appears %d times, want 1", n)
	}
	// 中文名被丢弃，不应产生空名段。
	if strings.Contains(text, "computer_]\n") {
		t.Fatalf("config contains empty computer section:\n%s", text)
	}
}
