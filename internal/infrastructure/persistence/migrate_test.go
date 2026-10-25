package persistence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/znahovo/switch-display/internal/domain/source"
)

func TestMigrateFillsMissingVersion(t *testing.T) {
	// 手工编辑掉的 version 字段：当成最早的 v1，并补默认值。
	w, err := migrate(wireSnapshot{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if w.Version != source.CurrentVersion {
		t.Errorf("Version = %d", w.Version)
	}
	if w.Settings.DefaultConfirmMs != source.DefaultConfirmMs {
		t.Errorf("DefaultConfirmMs = %d", w.Settings.DefaultConfirmMs)
	}
}

func TestMigrateRejectsFutureVersion(t *testing.T) {
	// 降级安装时不能静默丢掉新版本里的字段，要明确报错。
	_, err := migrate(wireSnapshot{Version: source.CurrentVersion + 1})
	if err == nil {
		t.Fatal("expected error for future version")
	}
	if !strings.Contains(err.Error(), "unsupported config version") {
		t.Errorf("err = %v", err)
	}
}

func TestApplyDefaultsGivesSourcesFallbackConfirmMs(t *testing.T) {
	w := wireSnapshot{
		Version:  source.CurrentVersion,
		Settings: wireSettings{DefaultConfirmMs: 900},
		Sources: []wireSource{
			{ID: "s1", Name: "A", ConfirmMs: 0},   // 补 900
			{ID: "s2", Name: "B", ConfirmMs: 300}, // 保留
		},
	}
	got := applyDefaults(w)
	if got.Sources[0].ConfirmMs != 900 {
		t.Errorf("source 0 confirmMs = %d", got.Sources[0].ConfirmMs)
	}
	if got.Sources[1].ConfirmMs != 300 {
		t.Errorf("source 1 confirmMs = %d", got.Sources[1].ConfirmMs)
	}
}

func TestNewJSONStoreEmptyPathUsesDefault(t *testing.T) {
	// 传空串时必须回落到平台默认路径，而不是当前目录下的 config.json。
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	s, err := NewJSONStore("")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if !strings.Contains(s.Path(), AppDirName) {
		t.Errorf("Path() = %q, want it to contain %q", s.Path(), AppDirName)
	}
	if !strings.HasSuffix(s.Path(), ConfigFileName) {
		t.Errorf("Path() = %q, want suffix %q", s.Path(), ConfigFileName)
	}
}

func TestDefaultPathIsUnderUserConfigDir(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if filepath.Base(p) != ConfigFileName {
		t.Errorf("path = %q", p)
	}
	if !strings.Contains(p, AppDirName) {
		t.Errorf("path = %q", p)
	}
}

func TestSaveKeepsSelfSourceRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	snap := sampleSnapshot()
	snap.Settings.SelfSourceID = "s1"

	if err := s.Save(context.Background(), snap); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Settings.SelfSourceID != "s1" {
		t.Errorf("SelfSourceID = %q, want s1", got.Settings.SelfSourceID)
	}
}

func TestSaveRejectsDanglingSelfSource(t *testing.T) {
	s, _ := newStore(t)
	snap := sampleSnapshot()
	snap.Settings.SelfSourceID = "ghost"
	if err := s.Save(context.Background(), snap); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadCorruptJSONLeavesNoPartialState(t *testing.T) {
	s, path := newStore(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	snap, err := s.Load(context.Background())
	if err == nil {
		t.Fatal("expected parse error")
	}
	// 出错时返回的必须是可用的空快照，而不是零值（Version=0 会直接失败）。
	if snap.Version != source.CurrentVersion {
		t.Errorf("Version = %d, want %d", snap.Version, source.CurrentVersion)
	}
}

func TestSaveRespectsCancelledContext(t *testing.T) {
	s, _ := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Save(ctx, sampleSnapshot()); err == nil {
		t.Fatal("expected context error")
	}
}

func TestLoadRespectsCancelledContext(t *testing.T) {
	s, _ := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Load(ctx); err == nil {
		t.Fatal("expected context error")
	}
}

func TestExportImportRespectCancelledContext(t *testing.T) {
	s, _ := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Export(ctx, sampleSnapshot(), filepath.Join(t.TempDir(), "x.json")); err == nil {
		t.Fatal("export should fail on cancelled context")
	}
	if _, err := s.Import(ctx, filepath.Join(t.TempDir(), "x.json")); err == nil {
		t.Fatal("import should fail on cancelled context")
	}
}

func TestExportCreatesParentDir(t *testing.T) {
	s, _ := newStore(t)
	dest := filepath.Join(t.TempDir(), "deep", "nested", "config.json")
	if err := s.Export(context.Background(), sampleSnapshot(), dest); err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("stat: %v", err)
	}
}

func TestExportRejectsInvalidSnapshot(t *testing.T) {
	s, _ := newStore(t)
	bad := sampleSnapshot()
	bad.Sources[0].Name = ""
	if err := s.Export(context.Background(), bad, filepath.Join(t.TempDir(), "x.json")); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestImportMissingFile(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Import(context.Background(), filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error")
	}
}

func TestImportRejectsInvalidContent(t *testing.T) {
	s, _ := newStore(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	content := `{"version":1,"sources":[{"id":"s1","name":"","displayId":"uuid-1","input":{"vcp":96,"value":17}}]}`
	if err := os.WriteFile(bad, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.Import(context.Background(), bad); err == nil {
		t.Fatal("expected validation error")
	}
}
