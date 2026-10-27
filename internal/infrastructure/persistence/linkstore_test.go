package persistence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/znahovo/switch-display/internal/domain/link"
)

func newLinkStore(t *testing.T) (*LinkStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), LinkFileName)
	s, err := NewLinkStore(path)
	if err != nil {
		t.Fatalf("NewLinkStore() error = %v", err)
	}
	return s, path
}

func TestLinkStoreLoadMissing(t *testing.T) {
	s, _ := newLinkStore(t)
	snap, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snap.Version != link.CurrentVersion {
		t.Fatalf("Version = %d, want %d", snap.Version, link.CurrentVersion)
	}
	if snap.Settings.DeviceID != "" {
		t.Fatalf("DeviceID = %q, want empty", snap.Settings.DeviceID)
	}
}

func TestLinkStoreRoundTrip(t *testing.T) {
	s, path := newLinkStore(t)

	id, err := link.NewDeviceID()
	if err != nil {
		t.Fatalf("NewDeviceID() error = %v", err)
	}
	peerID, err := link.NewDeviceID()
	if err != nil {
		t.Fatalf("NewDeviceID() error = %v", err)
	}
	fp := "v2:sha256:" + strings.Repeat("a", 64)

	want := link.Snapshot{
		Version: link.CurrentVersion,
		Settings: link.Settings{
			DeviceID:   id,
			DeviceName: "Studio Mac",
			LastHostID: peerID,
		},
		Trusted: []link.TrustedPeer{{
			ID:          peerID,
			Name:        "Office PC",
			Fingerprint: fp,
			PairedAt:    time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
		}},
	}
	if err := s.Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Settings.DeviceID != want.Settings.DeviceID {
		t.Fatalf("DeviceID = %q, want %q", got.Settings.DeviceID, want.Settings.DeviceID)
	}
	if got.Settings.DeviceName != want.Settings.DeviceName {
		t.Fatalf("DeviceName = %q, want %q", got.Settings.DeviceName, want.Settings.DeviceName)
	}
	if got.Settings.LastHostID != want.Settings.LastHostID {
		t.Fatalf("LastHostID = %q, want %q", got.Settings.LastHostID, want.Settings.LastHostID)
	}
	if len(got.Trusted) != 1 {
		t.Fatalf("len(Trusted) = %d, want 1", len(got.Trusted))
	}
	if got.Trusted[0].ID != peerID || got.Trusted[0].Fingerprint != fp {
		t.Fatalf("Trusted[0] = %+v", got.Trusted[0])
	}
	if !got.Trusted[0].PairedAt.Equal(want.Trusted[0].PairedAt) {
		t.Fatalf("PairedAt = %v, want %v", got.Trusted[0].PairedAt, want.Trusted[0].PairedAt)
	}

	// 第二次保存应产生 .bak。
	if err := s.Save(context.Background(), want); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("missing .bak: %v", err)
	}
}

func TestLinkStoreCorruptFileFallsBack(t *testing.T) {
	s, path := newLinkStore(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	snap, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snap.Settings.DeviceID != "" {
		t.Fatalf("DeviceID = %q, want empty after corrupt file", snap.Settings.DeviceID)
	}
}

func TestLinkStoreRejectsInvalid(t *testing.T) {
	s, _ := newLinkStore(t)
	bad := link.Snapshot{Version: 99}
	if err := s.Save(context.Background(), bad); err == nil {
		t.Fatal("Save(invalid) = nil error, want error")
	}
}

func TestLinkStoreEmptyFile(t *testing.T) {
	s, path := newLinkStore(t)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	snap, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snap.Version != link.CurrentVersion {
		t.Fatalf("Version = %d, want %d", snap.Version, link.CurrentVersion)
	}
}
