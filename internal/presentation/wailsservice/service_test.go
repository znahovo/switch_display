package wailsservice

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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

// fakePort 是一台假的 DDC 显示器：当前输入口存在变量里。
type fakePort struct {
	current uint16
	writes  []display.InputValue
}

func (p *fakePort) List(context.Context) ([]display.Display, error) {
	return []display.Display{{ID: "uuid-1", Name: "Fake Monitor"}}, nil
}

func (p *fakePort) ReadVCP(_ context.Context, _ display.DisplayID, vcp display.VCPCode, alt bool) (display.ReadBack, error) {
	return display.ReadBack{Supported: true, Current: p.current, Maximum: 255}, nil
}

func (p *fakePort) WriteVCP(_ context.Context, _ display.DisplayID, v display.InputValue) error {
	p.writes = append(p.writes, v)
	p.current = v.Value
	return nil
}

type nopEvents struct{ names []string }

func (e *nopEvents) Emit(name string, _ any) { e.names = append(e.names, name) }

// nopLogger 丢弃日志：测试只关心结果，不关心日志。
type nopLogger struct{}

func (nopLogger) Debug(string, ...any)        {}
func (nopLogger) Info(string, ...any)         {}
func (nopLogger) Error(string, error, ...any) {}

type nopHotkeys struct{}

func (nopHotkeys) RegisterAll(b []appcore.Binding) []error { return make([]error, len(b)) }
func (nopHotkeys) UnregisterAll()                          {}

type fakeClock struct{}

func (fakeClock) Now() time.Time                                   { return time.Unix(0, 0) }
func (fakeClock) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// newTestService 拼一套不依赖 Wails 的服务，用来验证 DTO 层的接线。
func newTestService(t *testing.T) (*DisplayService, *fakePort) {
	t.Helper()

	port := &fakePort{current: 18}
	repo, err := persistence.NewJSONStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	log := nopLogger{}
	clock := fakeClock{}
	events := &nopEvents{}

	displays := displayapp.New(displayapp.Deps{Port: port, Log: log})
	cfg := configapp.New(configapp.Deps{
		Repo: repo, Export: repo, Events: events, Hotkey: nopHotkeys{}, Log: log, Clock: clock,
	})
	sw := switchapp.New(switchapp.Deps{
		Engine: switching.NewService(port, clock), Config: cfg, Events: events, Log: log,
	})
	svc := New(Deps{Displays: displays, Config: cfg, Switcher: sw})
	if err := svc.ServiceStartup(context.Background(), wails.ServiceOptions{}); err != nil {
		t.Fatalf("startup: %v", err)
	}
	return svc, port
}

func TestSaveConfigAndState(t *testing.T) {
	svc, _ := newTestService(t)

	state, err := svc.State()
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if len(state.Displays) != 1 || state.Displays[0].Name != "Fake Monitor" {
		t.Fatalf("displays = %+v", state.Displays)
	}
	if state.ConfigPath == "" {
		t.Fatal("config path should be set")
	}

	next := state
	next.Sources = []dto.SourceDTO{{
		ID:        "s1",
		Name:      "Work PC",
		DisplayID: "uuid-1",
		Input:     dto.InputDTO{VCP: 0x60, Value: 17},
		ConfirmMs: 1200,
	}}
	res, err := svc.SaveConfig(next)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !res.OK {
		t.Fatalf("save not ok: %+v", res)
	}

	// 再取一次状态，确认真的持久化并回读了。
	again, err := svc.State()
	if err != nil {
		t.Fatalf("state 2: %v", err)
	}
	if len(again.Sources) != 1 || again.Sources[0].Name != "Work PC" {
		t.Fatalf("sources = %+v", again.Sources)
	}
	if again.Sources[0].Input.PresetKey != "hdmi1" {
		t.Fatalf("preset key = %q", again.Sources[0].Input.PresetKey)
	}
}

func TestSaveConfigRejectsDuplicatePort(t *testing.T) {
	svc, _ := newTestService(t)
	state, _ := svc.State()
	dup := dto.SourceDTO{
		ID: "s1", Name: "A", DisplayID: "uuid-1",
		Input: dto.InputDTO{VCP: 0x60, Value: 17}, ConfirmMs: 100,
	}
	dup2 := dup
	dup2.ID, dup2.Name = "s2", "B"
	state.Sources = []dto.SourceDTO{dup, dup2}

	if _, err := svc.SaveConfig(state); err == nil {
		t.Fatal("expected duplicate port error")
	} else if appErr := toAppErr(t, err); appErr.Code != "duplicate_port" {
		t.Fatalf("code = %q", appErr.Code)
	}
}

func TestSwitchSourceUpdatesDisplay(t *testing.T) {
	svc, port := newTestService(t)
	state, _ := svc.State()
	state.Sources = []dto.SourceDTO{{
		ID: "s1", Name: "Linux", DisplayID: "uuid-1",
		Input: dto.InputDTO{VCP: 0x60, Value: 17}, ConfirmMs: 0,
	}}
	if _, err := svc.SaveConfig(state); err != nil {
		t.Fatalf("save: %v", err)
	}

	res, err := svc.SwitchSource("s1")
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if res.Outcome != "applied_unconfirmed" {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if len(port.writes) != 1 || port.writes[0].Value != 17 {
		t.Fatalf("writes = %+v", port.writes)
	}
}

func TestSwitchUnknownSource(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.SwitchSource("nope"); err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestReadCurrentInput(t *testing.T) {
	svc, _ := newTestService(t)
	in, err := svc.ReadCurrentInput("uuid-1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if in.Value != 18 {
		t.Fatalf("value = %d", in.Value)
	}
	if in.PresetKey != "hdmi2" {
		t.Fatalf("preset = %q", in.PresetKey)
	}
}

// 辅助：从 *Error 里取 code（save 返回的是 error 接口）。
func toAppErr(t *testing.T, err error) *Error {
	t.Helper()
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("not *Error: %T %v", err, err)
	}
	return e
}
