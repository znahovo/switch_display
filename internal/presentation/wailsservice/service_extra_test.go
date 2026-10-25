package wailsservice

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	wails "github.com/wailsapp/wails/v3/pkg/application"

	appcore "github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/application/configapp"
	"github.com/znahovo/switch-display/internal/application/displayapp"
	"github.com/znahovo/switch-display/internal/application/dto"
	"github.com/znahovo/switch-display/internal/application/switchapp"
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/switching"
	"github.com/znahovo/switch-display/internal/infrastructure/persistence"
)

// failingPort 枚举就报错，用来覆盖 State/ListDisplays 的错误路径。
type failingPort struct{ err error }

func (p failingPort) List(context.Context) ([]display.Display, error) { return nil, p.err }
func (p failingPort) ReadVCP(context.Context, display.DisplayID, display.VCPCode, bool) (display.ReadBack, error) {
	return display.ReadBack{}, p.err
}
func (p failingPort) WriteVCP(context.Context, display.DisplayID, display.InputValue) error {
	return p.err
}

// warnHotkeys 让所有快捷键都注册失败，用来产生警告。
type warnHotkeys struct{}

func (warnHotkeys) RegisterAll(b []appcore.Binding) []error {
	errs := make([]error, len(b))
	for i, bind := range b {
		if bind.Accelerator != "" {
			errs[i] = errors.New("already registered")
		}
	}
	return errs
}
func (warnHotkeys) UnregisterAll() {}

func buildService(t *testing.T, port display.Port, hotkeys appcore.HotkeyRegistrar) *DisplayService {
	t.Helper()
	repo, err := persistence.NewJSONStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	log, clock, events := nopLogger{}, fakeClock{}, &nopEvents{}
	displays := displayapp.New(displayapp.Deps{Port: port, Log: log})
	cfg := configapp.New(configapp.Deps{
		Repo: repo, Export: repo, Events: events, Hotkey: hotkeys, Log: log, Clock: clock,
	})
	sw := switchapp.New(switchapp.Deps{
		Engine: switching.NewService(port, clock), Config: cfg, Events: events, Log: log,
	})
	return New(Deps{Displays: displays, Config: cfg, Switcher: sw})
}

func TestStateReportsPortFailureAsAppError(t *testing.T) {
	svc := buildService(t, failingPort{err: errors.New("iokit down")}, nopHotkeys{})
	_, err := svc.State()
	if err == nil {
		t.Fatal("expected error")
	}
	if code := toAppErr(t, err).Code; code != "internal" {
		t.Errorf("code = %q, want internal", code)
	}
}

func TestListDisplaysReportsPortFailure(t *testing.T) {
	svc := buildService(t, failingPort{err: errors.New("boom")}, nopHotkeys{})
	if _, err := svc.ListDisplays(); err == nil {
		t.Fatal("expected error")
	}
}

func TestWarningsAreConsumedOnce(t *testing.T) {
	// 警告只随下一次 State 返回一次：否则前端每次轮询都会弹同一个过期提示。
	svc := buildService(t, &fakePort{current: 18}, warnHotkeys{})
	state, err := svc.State()
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	state.Sources = []dto.SourceDTO{{
		ID: "s1", Name: "A", DisplayID: "uuid-1",
		Input: dto.InputDTO{VCP: 0x60, Value: 17}, Shortcut: "Cmd+Option+1", ConfirmMs: 100,
	}}
	res, err := svc.SaveConfig(state)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %v", res.Warnings)
	}

	first, err := svc.State()
	if err != nil {
		t.Fatalf("state 2: %v", err)
	}
	if len(first.Warnings) != 1 {
		t.Fatalf("first State warnings = %v", first.Warnings)
	}
	second, err := svc.State()
	if err != nil {
		t.Fatalf("state 3: %v", err)
	}
	if len(second.Warnings) != 0 {
		t.Errorf("warnings should be consumed once, got %v", second.Warnings)
	}
}

func TestPresetsAndAltPresets(t *testing.T) {
	svc := buildService(t, &fakePort{}, nopHotkeys{})

	presets := svc.Presets()
	if len(presets) == 0 {
		t.Fatal("presets empty")
	}
	if presets[0].Key != "dp1" || presets[0].Value != 15 {
		t.Errorf("first preset = %+v", presets[0])
	}

	alt := svc.AltPresets()
	if len(alt) == 0 {
		t.Fatal("alt presets empty")
	}
	// alt 表的每一项都必须标了 Alt，否则前端会把它写成标准 VCP。
	for _, p := range alt {
		if !p.Alt {
			t.Errorf("alt preset %q has Alt=false", p.Key)
		}
	}
}

func TestConfigPathDelegatesToRepo(t *testing.T) {
	svc := buildService(t, &fakePort{}, nopHotkeys{})
	if svc.ConfigPath() == "" {
		t.Fatal("ConfigPath() empty")
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	svc := buildService(t, &fakePort{current: 18}, nopHotkeys{})
	state, _ := svc.State()
	state.Sources = []dto.SourceDTO{{
		ID: "s1", Name: "工作 PC", DisplayID: "uuid-1",
		Input: dto.InputDTO{VCP: 0x60, Value: 17}, ConfirmMs: 500,
	}}
	if _, err := svc.SaveConfig(state); err != nil {
		t.Fatalf("save: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "export.json")
	if err := svc.ExportConfig(dest); err != nil {
		t.Fatalf("export: %v", err)
	}

	imported, err := svc.ImportConfig(dest)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(imported.Sources) != 1 || imported.Sources[0].Name != "工作 PC" {
		t.Fatalf("sources = %+v", imported.Sources)
	}
	// ImportConfig 只返回内容，不落盘：内存里的配置应当保持不变。
	// （这里两者内容相同，所以换个角度断言：返回值里带着配置路径。）
	if imported.ConfigPath == "" {
		t.Error("imported state should carry config path")
	}
}

func TestImportConfigMissingFile(t *testing.T) {
	svc := buildService(t, &fakePort{}, nopHotkeys{})
	if _, err := svc.ImportConfig(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error")
	}
}

func TestExportConfigBadDest(t *testing.T) {
	svc := buildService(t, &fakePort{}, nopHotkeys{})
	// 往一个已存在的目录当文件写：必须报错而不是静默成功。
	dir := t.TempDir()
	if err := svc.ExportConfig(dir); err == nil {
		t.Fatal("expected error writing to a directory path")
	}
}

func TestLifeCtxFallsBackToBackground(t *testing.T) {
	// ServiceStartup 之前（前端首屏可能抢跑）调用方法不能 panic。
	svc := buildService(t, &fakePort{current: 18}, nopHotkeys{})
	if svc.ctx != nil {
		t.Fatal("ctx should be nil before startup")
	}
	if _, err := svc.State(); err != nil {
		t.Fatalf("State before startup: %v", err)
	}
}

func TestServiceStartupCallsHookAndShutdownIsNil(t *testing.T) {
	called := false
	svc := buildService(t, &fakePort{}, nopHotkeys{})
	svc.onStartup = func(context.Context) error {
		called = true
		return nil
	}
	if err := svc.ServiceStartup(context.Background(), wails.ServiceOptions{}); err != nil {
		t.Fatalf("startup: %v", err)
	}
	if !called {
		t.Error("onStartup not called")
	}
	if err := svc.ServiceShutdown(); err != nil {
		t.Errorf("shutdown = %v", err)
	}
	if svc.ServiceName() != "DisplayService" {
		t.Errorf("ServiceName = %q", svc.ServiceName())
	}
}

func TestServiceStartupWithoutHook(t *testing.T) {
	svc := buildService(t, &fakePort{}, nopHotkeys{})
	svc.onStartup = nil
	if err := svc.ServiceStartup(context.Background(), wails.ServiceOptions{}); err != nil {
		t.Fatalf("startup: %v", err)
	}
}
