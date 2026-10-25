package configapp

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	appcore "github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
	"github.com/znahovo/switch-display/internal/infrastructure/persistence"
)

type nopLogger struct{}

func (nopLogger) Debug(string, ...any)        {}
func (nopLogger) Info(string, ...any)         {}
func (nopLogger) Error(string, error, ...any) {}

type nopClock struct{}

func (nopClock) Now() time.Time                                   { return time.Unix(0, 0) }
func (nopClock) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// recordingEvents 记下所有 Broadcast 的事件名。
type recordingEvents struct{ names []string }

func (e *recordingEvents) Emit(name string, _ any) { e.names = append(e.names, name) }

// fakeHotkeys 可编程让某几个 accelerator 注册失败。
type fakeHotkeys struct {
	registered []appcore.Binding
	unregister int
	failOn     map[string]error
}

func (h *fakeHotkeys) RegisterAll(b []appcore.Binding) []error {
	errs := make([]error, len(b))
	for i, bind := range b {
		if err, ok := h.failOn[bind.Accelerator]; ok {
			errs[i] = err
			continue
		}
		h.registered = append(h.registered, bind)
	}
	return errs
}

func (h *fakeHotkeys) UnregisterAll() { h.unregister++ }

// errorRepo 让 Save 失败，用来验证「校验通过但落盘失败」的分支。
type errorRepo struct{}

func (errorRepo) Load(context.Context) (source.Snapshot, error) { return source.EmptySnapshot(), nil }
func (errorRepo) Save(context.Context, source.Snapshot) error   { return errors.New("disk full") }
func (errorRepo) Path() string                                  { return "/dev/null/config.json" }

func newService(t *testing.T, hotkeys appcore.HotkeyRegistrar) (*Service, *recordingEvents, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	repo, err := persistence.NewJSONStore(path)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	events := &recordingEvents{}
	svc := New(Deps{
		Repo:   repo,
		Export: repo,
		Events: events,
		Hotkey: hotkeys,
		Log:    nopLogger{},
		Clock:  nopClock{},
	})
	return svc, events, path
}

func src(id, name string, value uint16) source.SignalSource {
	return source.SignalSource{
		ID:        source.ID(id),
		Name:      name,
		DisplayID: "uuid-1",
		Input:     display.InputValue{VCP: display.VCPInputSource, Value: value},
		ConfirmMs: 1200,
	}
}

func TestNewStartsWithEmptySnapshot(t *testing.T) {
	svc, _, _ := newService(t, &fakeHotkeys{})
	snap := svc.Snapshot()
	if snap.Version != source.CurrentVersion {
		t.Errorf("Version = %d", snap.Version)
	}
	if len(snap.Sources) != 0 {
		t.Errorf("Sources = %d", len(snap.Sources))
	}
	if !snap.Settings.ConfirmAfterSwitch {
		t.Error("default settings should confirm after switch")
	}
}

func TestLoadMissingFileKeepsEmpty(t *testing.T) {
	svc, _, _ := newService(t, &fakeHotkeys{})
	snap, err := svc.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(snap.Sources) != 0 {
		t.Errorf("Sources = %d", len(snap.Sources))
	}
}

func TestLoadRejectsInvalidConfigAndFallsBackToEmpty(t *testing.T) {
	svc, _, path := newService(t, &fakeHotkeys{})

	// 手工写一份不合法的配置（两个源指向同一个口）。
	bad := `{"version":1,"settings":{"defaultConfirmMs":1200},"sources":[
		{"id":"s1","name":"A","displayId":"uuid-1","input":{"vcp":96,"value":17},"confirmMs":1200},
		{"id":"s2","name":"B","displayId":"uuid-1","input":{"vcp":96,"value":17},"confirmMs":1200}]}`
	if err := writeFile(path, bad); err != nil {
		t.Fatalf("write: %v", err)
	}

	snap, err := svc.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// 坏配置不能让应用起不来，但也不能被当成好配置用。
	if len(snap.Sources) != 0 {
		t.Errorf("invalid config should fall back to empty, got %d sources", len(snap.Sources))
	}
}

func TestSnapshotIsDeepCopy(t *testing.T) {
	svc, _, _ := newService(t, &fakeHotkeys{})
	snap := source.EmptySnapshot()
	snap.Sources = []source.SignalSource{src("s1", "A", 17)}
	if _, err := svc.Save(context.Background(), snap); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := svc.Snapshot()
	got.Sources[0].Name = "mutated"
	if svc.Snapshot().Sources[0].Name != "A" {
		t.Fatal("Snapshot() leaked its internal slice")
	}
}

func TestSavePersistsAndEmitsStateChanged(t *testing.T) {
	svc, events, _ := newService(t, &fakeHotkeys{})
	snap := source.EmptySnapshot()
	snap.Sources = []source.SignalSource{src("s1", "A", 17)}

	res, err := svc.Save(context.Background(), snap)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !res.OK {
		t.Error("OK = false")
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v", res.Warnings)
	}
	if len(svc.Snapshot().Sources) != 1 {
		t.Error("snapshot not updated")
	}
	found := false
	for _, n := range events.names {
		if n == appcore.EventStateChanged {
			found = true
		}
	}
	if !found {
		t.Errorf("events = %v, want %s", events.names, appcore.EventStateChanged)
	}
}

func TestSaveRejectsInvalidSnapshotWithoutTouchingDisk(t *testing.T) {
	svc, events, _ := newService(t, &fakeHotkeys{})
	bad := source.EmptySnapshot()
	bad.Sources = []source.SignalSource{src("s1", "", 17)} // 名字为空

	if _, err := svc.Save(context.Background(), bad); err == nil {
		t.Fatal("expected validation error")
	}
	if len(svc.Snapshot().Sources) != 0 {
		t.Error("invalid snapshot must not replace memory state")
	}
	if len(events.names) != 0 {
		t.Errorf("no event should be emitted, got %v", events.names)
	}
}

func TestSaveReportsRepoError(t *testing.T) {
	svc := New(Deps{Repo: errorRepo{}, Events: &recordingEvents{}, Hotkey: &fakeHotkeys{}, Log: nopLogger{}, Clock: nopClock{}})
	res, err := svc.Save(context.Background(), source.EmptySnapshot())
	if err == nil {
		t.Fatal("expected repo error")
	}
	if res.OK {
		t.Error("OK should be false on error")
	}
}

func TestSaveRebindsHotkeys(t *testing.T) {
	hk := &fakeHotkeys{}
	svc, _, _ := newService(t, hk)

	snap := source.EmptySnapshot()
	a := src("s1", "A", 17)
	a.Shortcut = "Cmd+Option+1"
	b := src("s2", "B", 18)
	b.Shortcut = "Cmd+Option+2"
	snap.Sources = []source.SignalSource{a, b}

	if _, err := svc.Save(context.Background(), snap); err != nil {
		t.Fatalf("save: %v", err)
	}
	if hk.unregister == 0 {
		t.Error("UnregisterAll should be called before rebinding")
	}
	if len(hk.registered) != 2 {
		t.Fatalf("registered = %+v", hk.registered)
	}
	// 绑定必须带上 sourceID，否则快捷键按下去不知道切哪个源。
	if hk.registered[0].SourceID != "s1" || hk.registered[1].SourceID != "s2" {
		t.Errorf("registered = %+v", hk.registered)
	}
}

func TestSaveSkipsSourcesWithoutShortcut(t *testing.T) {
	hk := &fakeHotkeys{}
	svc, _, _ := newService(t, hk)

	snap := source.EmptySnapshot()
	snap.Sources = []source.SignalSource{src("s1", "A", 17)} // 无快捷键
	if _, err := svc.Save(context.Background(), snap); err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(hk.registered) != 0 {
		t.Errorf("registered = %+v, want none", hk.registered)
	}
}

func TestSaveShortcutFailureBecomesWarningNotRollback(t *testing.T) {
	// ADR-007：快捷键注册失败只报警告，配置必须照常保存。
	hk := &fakeHotkeys{failOn: map[string]error{"Cmd+Option+1": errors.New("already registered")}}
	svc, _, path := newService(t, hk)

	snap := source.EmptySnapshot()
	a := src("s1", "A", 17)
	a.Shortcut = "Cmd+Option+1"
	snap.Sources = []source.SignalSource{a}

	res, err := svc.Save(context.Background(), snap)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !res.OK {
		t.Error("save should still succeed")
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	// 配置确实落盘了：重新 Load 应该能读回来。
	snap2, err := svc.Load(context.Background())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(snap2.Sources) != 1 {
		t.Errorf("config not persisted, sources = %d", len(snap2.Sources))
	}
	if _, err := readFile(path); err != nil {
		t.Errorf("config file missing: %v", err)
	}
}

func TestRebindHotkeysDoesNotWriteFile(t *testing.T) {
	hk := &fakeHotkeys{}
	svc, _, path := newService(t, hk)

	// 先放一份配置进内存（不落盘的做法：直接 Save 一次再删文件太绕，
	// 这里用 Save 落盘后记录 mtime，再调 RebindHotkeys 确认没被改）。
	snap := source.EmptySnapshot()
	a := src("s1", "A", 17)
	a.Shortcut = "Cmd+Option+1"
	snap.Sources = []source.SignalSource{a}
	if _, err := svc.Save(context.Background(), snap); err != nil {
		t.Fatalf("save: %v", err)
	}
	before, err := statModTime(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// Save 已经注册过一轮，这里清空记录，只看 RebindHotkeys 干了什么。
	hk.registered = nil

	warnings := svc.RebindHotkeys()
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	if len(hk.registered) != 1 {
		t.Errorf("registered = %+v", hk.registered)
	}
	after, err := statModTime(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !before.Equal(after) {
		t.Error("RebindHotkeys must not rewrite the config file")
	}
}

func TestRebindHotkeysWithoutHotkeysIsNoop(t *testing.T) {
	svc, _, _ := newService(t, nil) // 显式不注入快捷键端口
	if warnings := svc.RebindHotkeys(); len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestFindSource(t *testing.T) {
	svc, _, _ := newService(t, &fakeHotkeys{})
	snap := source.EmptySnapshot()
	snap.Sources = []source.SignalSource{src("s1", "A", 17)}
	if _, err := svc.Save(context.Background(), snap); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, ok := svc.FindSource("s1")
	if !ok || got.Name != "A" {
		t.Fatalf("FindSource = %+v, %v", got, ok)
	}
	if _, ok := svc.FindSource("missing"); ok {
		t.Error("should not find unknown id")
	}
}

func TestPathAndExportImport(t *testing.T) {
	svc, _, path := newService(t, &fakeHotkeys{})
	if svc.Path() != path {
		t.Errorf("Path() = %q, want %q", svc.Path(), path)
	}

	snap := source.EmptySnapshot()
	snap.Sources = []source.SignalSource{src("s1", "工作 PC", 17)}
	if _, err := svc.Save(context.Background(), snap); err != nil {
		t.Fatalf("save: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "exported.json")
	if err := svc.Export(context.Background(), dest); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Import 只解析不落盘，所以必须能在不改变内存状态的前提下读到内容。
	imported, err := svc.Import(context.Background(), dest)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(imported.Sources) != 1 || imported.Sources[0].Name != "工作 PC" {
		t.Errorf("imported = %+v", imported.Sources)
	}
	if len(svc.Snapshot().Sources) != 1 {
		t.Error("Import must not change memory state")
	}
}

func TestExportImportUnsupportedWithoutExporter(t *testing.T) {
	svc := New(Deps{Repo: errorRepo{}, Events: &recordingEvents{}, Hotkey: &fakeHotkeys{}, Log: nopLogger{}, Clock: nopClock{}})
	if err := svc.Export(context.Background(), "/tmp/x.json"); err == nil {
		t.Error("expected export error without exporter")
	}
	if _, err := svc.Import(context.Background(), "/tmp/x.json"); err == nil {
		t.Error("expected import error without exporter")
	}
}
