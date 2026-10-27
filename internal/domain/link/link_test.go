package link

import (
	"net/netip"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNewDeviceID(t *testing.T) {
	id, err := NewDeviceID()
	if err != nil {
		t.Fatalf("NewDeviceID() error = %v", err)
	}
	if len(id) != DeviceIDLen {
		t.Fatalf("len(id) = %d, want %d", len(id), DeviceIDLen)
	}
	if !ValidDeviceID(id) {
		t.Fatalf("ValidDeviceID(%q) = false, want true", id)
	}
	other, err := NewDeviceID()
	if err != nil {
		t.Fatalf("NewDeviceID() error = %v", err)
	}
	if id == other {
		t.Fatalf("two generated IDs are equal: %q", id)
	}
}

func TestValidDeviceID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid", "0123456789abcdef0123456789abcdef", true},
		{"too short", "0123456789abcdef", false},
		{"uppercase", "0123456789ABCDEF0123456789ABCDEF", false},
		{"non hex", "0123456789abcdef0123456789abcdeg", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidDeviceID(tt.id); got != tt.want {
				t.Fatalf("ValidDeviceID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestValidFingerprint(t *testing.T) {
	valid := "v2:sha256:" + strings.Repeat("a", 64)
	if !ValidFingerprint(valid) {
		t.Fatalf("ValidFingerprint(%q) = false, want true", valid)
	}
	tests := []struct {
		name string
		fp   string
	}{
		{"uppercase hex", "v2:sha256:" + strings.Repeat("A", 64)},
		{"short hex", "v2:sha256:" + strings.Repeat("a", 32)},
		{"missing prefix", strings.Repeat("a", 64)},
		{"sha1 style", "AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ValidFingerprint(tt.fp) {
				t.Fatalf("ValidFingerprint(%q) = true, want false", tt.fp)
			}
		})
	}
}

func TestCleanDeviceName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "MacBook Pro", "MacBook Pro"},
		{"trims", "  mac  ", "mac"},
		{"collapses spaces", "a    b", "a b"},
		{"drops control chars", "a\x00b\x1fc", "abc"},
		{"keeps tabs as space", "a\tb", "a b"},
		{"truncates", strings.Repeat("x", 100), strings.Repeat("x", MaxDeviceNameLen)},
		{"empty", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanDeviceName(tt.in); got != tt.want {
				t.Fatalf("CleanDeviceName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRoleValid(t *testing.T) {
	for _, r := range []Role{RoleIdle, RoleServer, RoleClient} {
		if !r.Valid() {
			t.Fatalf("%q.Valid() = false, want true", r)
		}
	}
	for _, r := range []Role{"", "host", "master"} {
		if r.Valid() {
			t.Fatalf("%q.Valid() = true, want false", r)
		}
	}
}

func TestPeerValidate(t *testing.T) {
	good := Peer{
		ID:   "0123456789abcdef0123456789abcdef",
		Name: "MacBook Pro",
		Addr: netip.MustParseAddrPort("192.168.1.20:24801"),
		Role: RoleServer,
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good peer Validate() error = %v", err)
	}

	badID := good
	badID.ID = "nope"
	if err := badID.Validate(); err == nil {
		t.Fatal("bad ID Validate() = nil, want error")
	}

	badName := good
	badName.Name = ""
	if err := badName.Validate(); err == nil {
		t.Fatal("empty name Validate() = nil, want error")
	}

	badRole := good
	badRole.Role = "boss"
	if err := badRole.Validate(); err == nil {
		t.Fatal("bad role Validate() = nil, want error")
	}

	badFP := good
	badFP.Fingerprint = "sha256:abc"
	if err := badFP.Validate(); err == nil {
		t.Fatal("bad fingerprint Validate() = nil, want error")
	}
}

func TestPeerStale(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	p := Peer{LastSeen: now.Add(-30 * time.Second)}
	if p.Stale(now, 10*time.Second) != true {
		t.Fatal("expected stale peer")
	}
	if p.Stale(now, 60*time.Second) != false {
		t.Fatal("expected fresh peer")
	}
	if p.Stale(now, 0) {
		t.Fatal("ttl 0 should disable staleness")
	}
}

func TestMessageRoundTrip(t *testing.T) {
	m := Announce(Self{
		ID:          "0123456789abcdef0123456789abcdef",
		Name:        "Studio Mac",
		Role:        RoleServer,
		Fingerprint: "v2:sha256:" + strings.Repeat("b", 64),
		Port:        DefaultPort,
	})
	data, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.ID != m.ID || got.Name != m.Name || got.Role != m.Role || got.Port != m.Port {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, m)
	}
	if got.Fingerprint != m.Fingerprint {
		t.Fatalf("fingerprint mismatch: got %q, want %q", got.Fingerprint, m.Fingerprint)
	}
}

func TestDecodeRejects(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"bad json", `{`},
		{"unknown version", `{"v":2,"type":"announce","id":"0123456789abcdef0123456789abcdef"}`},
		{"unknown type", `{"v":1,"type":"hello","id":"0123456789abcdef0123456789abcdef"}`},
		{"bad id", `{"v":1,"type":"announce","id":"xyz"}`},
		{"bad role", `{"v":1,"type":"announce","id":"0123456789abcdef0123456789abcdef","role":"boss"}`},
		{"bad fp", `{"v":1,"type":"announce","id":"0123456789abcdef0123456789abcdef","fp":"sha256:aa"}`},
		{"bad port", `{"v":1,"type":"announce","id":"0123456789abcdef0123456789abcdef","port":70000}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode([]byte(tt.data)); err == nil {
				t.Fatalf("Decode(%s) = nil error, want error", tt.data)
			}
		})
	}
}

func TestPeerFrom(t *testing.T) {
	m := Message{
		V:    MessageVersion,
		Type: TypeAnnounce,
		ID:   "0123456789abcdef0123456789abcdef",
		Name: "Desk Mini",
		Role: RoleClient,
	}
	addr := netip.MustParseAddrPort("10.0.0.5:24801")
	p := PeerFrom(m, addr)
	if p.ID != m.ID || p.Name != m.Name || p.Role != m.Role {
		t.Fatalf("PeerFrom() = %+v", p)
	}
	if p.Addr != addr {
		t.Fatalf("PeerFrom().Addr = %v, want %v", p.Addr, addr)
	}
}

func TestSnapshotValidate(t *testing.T) {
	devID, err := NewDeviceID()
	if err != nil {
		t.Fatalf("NewDeviceID() error = %v", err)
	}
	fp := "v2:sha256:" + strings.Repeat("c", 64)

	s := Snapshot{
		Version: CurrentVersion,
		Settings: Settings{
			DeviceID:   devID,
			DeviceName: "My Mac",
		},
		Trusted: []TrustedPeer{{
			ID:          "0123456789abcdef0123456789abcdef",
			Name:        "Office PC",
			Fingerprint: fp,
			PairedAt:    time.Now(),
		}},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	t.Run("unsupported version", func(t *testing.T) {
		bad := s
		bad.Version = 99
		if err := bad.Validate(); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("duplicate peer id", func(t *testing.T) {
		bad := s
		bad.Trusted = append(append([]TrustedPeer{}, s.Trusted...), s.Trusted[0])
		if err := bad.Validate(); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("duplicate fingerprint", func(t *testing.T) {
		bad := s
		second := s.Trusted[0]
		second.ID = "ffffffffffffffffffffffffffffffff"
		bad.Trusted = []TrustedPeer{s.Trusted[0], second}
		if err := bad.Validate(); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("last host must be valid", func(t *testing.T) {
		bad := s
		bad.Settings.LastHostID = "bogus"
		if err := bad.Validate(); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestFindTrusted(t *testing.T) {
	s := EmptySnapshot()
	s.Trusted = []TrustedPeer{{
		ID:          "0123456789abcdef0123456789abcdef",
		Name:        "Office PC",
		Fingerprint: "v2:sha256:" + strings.Repeat("d", 64),
	}}
	got, ok := s.FindTrusted("0123456789abcdef0123456789abcdef")
	if !ok || got.Name != "Office PC" {
		t.Fatalf("FindTrusted() = %+v, %v", got, ok)
	}
	if _, ok := s.FindTrusted("ffffffffffffffffffffffffffffffff"); ok {
		t.Fatal("FindTrusted(unknown) = true, want false")
	}
}

func TestComputerNameFor(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef"
	got := ComputerNameFor(id)
	if got != "sd-0123456789" {
		t.Fatalf("ComputerNameFor() = %q, want %q", got, "sd-0123456789")
	}
	// 必须满足 Deskflow 的 isValidComputerName 语义。
	if !regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_-]*[A-Za-z0-9_])?$`).MatchString(got) {
		t.Fatalf("ComputerNameFor() = %q is not a valid Deskflow computer name", got)
	}
	// 稳定：同一 ID 永远同一名字。
	if ComputerNameFor(id) != got {
		t.Fatal("ComputerNameFor is not deterministic")
	}
	// 不同 ID 不同名字。
	if ComputerNameFor("ffffffffffffffffffffffffffffffff") == got {
		t.Fatal("different IDs produced the same computer name")
	}
	// 过短 ID 返回空串。
	if ComputerNameFor("abc") != "" {
		t.Fatal("short ID should yield empty name")
	}
}
