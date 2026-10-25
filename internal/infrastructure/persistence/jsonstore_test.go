package persistence

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

func newStore(t *testing.T) (*JSONStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	s, err := NewJSONStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s, path
}

func sampleSnapshot() source.Snapshot {
	return source.Snapshot{
		Version:  source.CurrentVersion,
		Settings: source.DefaultSettings(),
		Sources: []source.SignalSource{
			{
				ID:        "s1",
				Name:      "工作 PC",
				Color:     "#3b82f6",
				DisplayID: "uuid-1",
				Input:     display.InputValue{VCP: display.VCPInputSource, Value: 17},
				Shortcut:  "Cmd+Option+1",
				ConfirmMs: 1200,
			},
			{
				ID:        "s2",
				Name:      "MacBook",
				DisplayID: "uuid-1",
				Input:     display.InputValue{VCP: display.VCPInputSource, Value: 27},
				ConfirmMs: 800,
			},
		},
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	s, _ := newStore(t)
	snap, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if snap.Version != source.CurrentVersion {
		t.Fatalf("version = %d", snap.Version)
	}
	if len(snap.Sources) != 0 {
		t.Fatalf("expected no sources, got %d", len(snap.Sources))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s, path := newStore(t)
	want := sampleSnapshot()
	if err := s.Save(context.Background(), want); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not written: %v", err)
	}

	got, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Sources) != len(want.Sources) {
		t.Fatalf("sources = %d, want %d", len(got.Sources), len(want.Sources))
	}
	if got.Sources[0].Name != "工作 PC" {
		t.Fatalf("name = %q", got.Sources[0].Name)
	}
	if got.Sources[0].Input.Value != 17 {
		t.Fatalf("value = %d", got.Sources[0].Input.Value)
	}
	if got.Settings.DefaultConfirmMs != want.Settings.DefaultConfirmMs {
		t.Fatalf("settings lost")
	}
}

func TestSaveWritesBackup(t *testing.T) {
	s, path := newStore(t)
	if err := s.Save(context.Background(), sampleSnapshot()); err != nil {
		t.Fatalf("first save: %v", err)
	}
	second := sampleSnapshot()
	second.Sources = second.Sources[:1]
	if err := s.Save(context.Background(), second); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	s, _ := newStore(t)
	bad := sampleSnapshot()
	bad.Sources[0].Name = ""
	if err := s.Save(context.Background(), bad); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadCorruptFileReportsError(t *testing.T) {
	s, path := newStore(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(context.Background()); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadEmptyFileTreatedAsFirstRun(t *testing.T) {
	s, path := newStore(t)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(snap.Sources) != 0 {
		t.Fatal("expected empty snapshot")
	}
}

func TestExportImport(t *testing.T) {
	s, _ := newStore(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "export.json")
	want := sampleSnapshot()
	if err := s.Export(context.Background(), want, dest); err != nil {
		t.Fatalf("export: %v", err)
	}

	other, _ := NewJSONStore(filepath.Join(dir, "other.json"))
	got, err := other.Import(context.Background(), dest)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(got.Sources) != len(want.Sources) {
		t.Fatalf("sources = %d", len(got.Sources))
	}
	if got.Sources[1].Input.Value != 27 {
		t.Fatalf("value = %d", got.Sources[1].Input.Value)
	}
}
