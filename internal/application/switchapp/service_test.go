package switchapp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	appcore "github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
	"github.com/znahovo/switch-display/internal/domain/switching"
)

type nopLogger struct{}

func (nopLogger) Debug(string, ...any)        {}
func (nopLogger) Info(string, ...any)         {}
func (nopLogger) Error(string, error, ...any) {}

// recordingEvents 记录事件名与 switch:finished 的 payload。
type recordingEvents struct {
	names    []string
	finished []any
}

func (e *recordingEvents) Emit(name string, payload any) {
	e.names = append(e.names, name)
	if name == appcore.EventSwitchFinished {
		e.finished = append(e.finished, payload)
	}
}

// fakePort 是一台假显示器：WriteVCP 把值写进 current，ReadVCP 读回来。
type fakePort struct {
	current   uint16
	writeErr  error
	writes    int
	readErr   error
	readSuppt bool
}

func (p *fakePort) List(context.Context) ([]display.Display, error) { return nil, nil }

func (p *fakePort) ReadVCP(context.Context, display.DisplayID, display.VCPCode, bool) (display.ReadBack, error) {
	if p.readErr != nil {
		return display.ReadBack{}, p.readErr
	}
	return display.ReadBack{Supported: p.readSuppt, Current: p.current, Maximum: 255}, nil
}

func (p *fakePort) WriteVCP(_ context.Context, _ display.DisplayID, v display.InputValue) error {
	p.writes++
	if p.writeErr != nil {
		return p.writeErr
	}
	p.current = v.Value
	return nil
}

type fakeClock struct{}

func (fakeClock) Now() time.Time                                   { return time.Unix(0, 0) }
func (fakeClock) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// fakeConfig 是最小配置读取能力。
type fakeConfig struct {
	sources []source.SignalSource
	confirm bool
}

func (c fakeConfig) FindSource(id string) (source.SignalSource, bool) {
	for _, s := range c.sources {
		if string(s.ID) == id {
			return s, true
		}
	}
	return source.SignalSource{}, false
}

func (c fakeConfig) Snapshot() source.Snapshot {
	return source.Snapshot{
		Version:  source.CurrentVersion,
		Settings: source.Settings{ConfirmAfterSwitch: c.confirm},
		Sources:  c.sources,
	}
}

func testSource() source.SignalSource {
	return source.SignalSource{
		ID:        "s1",
		Name:      "PC",
		DisplayID: "uuid-1",
		Input:     display.InputValue{VCP: display.VCPInputSource, Value: 17},
		ConfirmMs: 1200,
	}
}

func newService(port *fakePort, cfg fakeConfig) (*Service, *recordingEvents) {
	events := &recordingEvents{}
	svc := New(Deps{
		Engine: switching.NewService(port, fakeClock{}),
		Config: cfg,
		Events: events,
		Log:    nopLogger{},
	})
	return svc, events
}

func TestSwitchByIDUnknownSource(t *testing.T) {
	svc, events := newService(&fakePort{}, fakeConfig{})
	_, err := svc.SwitchByID(context.Background(), "nope")

	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v, want NotFoundError", err)
	}
	if nf.ID != "nope" {
		t.Errorf("ID = %q", nf.ID)
	}
	// 源都不存在时不该广播「开始切换」。
	if len(events.names) != 0 {
		t.Errorf("events = %v", events.names)
	}
}

func TestSwitchByIDEmitsStartedAndFinished(t *testing.T) {
	port := &fakePort{readSuppt: true}
	src := testSource()
	svc, events := newService(port, fakeConfig{sources: []source.SignalSource{src}, confirm: true})

	res, err := svc.SwitchByID(context.Background(), "s1")
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if res.Domain.Outcome != switching.OutcomeConfirmed {
		t.Errorf("outcome = %q", res.Domain.Outcome)
	}
	if len(events.names) != 2 {
		t.Fatalf("events = %v", events.names)
	}
	// 顺序很重要：前端靠 started 先进入 loading，再靠 finished 收尾。
	if events.names[0] != appcore.EventSwitchStarted || events.names[1] != appcore.EventSwitchFinished {
		t.Errorf("events = %v", events.names)
	}
	// finished 的 payload 必须是 DTO，而不是领域对象或 nil。
	if events.finished[0] == nil {
		t.Error("switch:finished payload should not be nil")
	}
}

func TestSwitchStartedPayloadIsPending(t *testing.T) {
	port := &fakePort{readSuppt: true}
	src := testSource()
	events := &payloadEvents{}
	svc := New(Deps{
		Engine: switching.NewService(port, fakeClock{}),
		Config: fakeConfig{sources: []source.SignalSource{src}, confirm: true},
		Events: events,
		Log:    nopLogger{},
	})
	if _, err := svc.SwitchByID(context.Background(), "s1"); err != nil {
		t.Fatalf("switch: %v", err)
	}

	started, ok := events.byName(appcore.EventSwitchStarted)
	if !ok {
		t.Fatal("missing switch:started")
	}
	// outcome 用 "pending" 字面量（前端 EventName.switchStarted 的约定）。
	if got := started["outcome"]; got != "pending" {
		t.Errorf("outcome = %v, want pending", got)
	}

	finished, ok := events.byName(appcore.EventSwitchFinished)
	if !ok {
		t.Fatal("missing switch:finished")
	}
	if got := finished["outcome"]; got != "confirmed" {
		t.Errorf("finished outcome = %v, want confirmed", got)
	}
	if got := finished["sourceId"]; got != "s1" {
		t.Errorf("finished sourceId = %v", got)
	}
}

// payloadEvents 把 payload 反射成 map，便于按字段断言（不用 import dto 的具体类型）。
type payloadEvents struct{ payloads []namedPayload }

type namedPayload struct {
	name string
	data map[string]any
}

func (e *payloadEvents) Emit(name string, payload any) {
	e.payloads = append(e.payloads, namedPayload{name: name, data: toMap(payload)})
}

func (e *payloadEvents) byName(name string) (map[string]any, bool) {
	for _, p := range e.payloads {
		if p.name == name {
			return p.data, true
		}
	}
	return nil, false
}

// toMap 用 JSON 往返把 DTO 变成 map：这样断言写的是 json tag（也就是
// 前端的字段名），而不是 Go 字段名，避免 DTO 改名后测试假绿。
func toMap(v any) map[string]any {
	out := map[string]any{}
	data, err := json.Marshal(v)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

func TestSwitchRespectsConfirmAfterSwitch(t *testing.T) {
	// confirm=false 时不该读回（不进 confirmed 分支），只报 applied_unconfirmed。
	port := &fakePort{readSuppt: true}
	src := testSource()
	svc, _ := newService(port, fakeConfig{sources: []source.SignalSource{src}, confirm: false})

	res, err := svc.SwitchByID(context.Background(), "s1")
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if res.Domain.Outcome != switching.OutcomeAppliedUnconfirmed {
		t.Errorf("outcome = %q, want applied_unconfirmed", res.Domain.Outcome)
	}
}

func TestSwitchWriteFailureIsOutcomeNotError(t *testing.T) {
	// 硬件写失败是业务结果（outcome=failed），不是调用错误。
	port := &fakePort{writeErr: errors.New("i2c nack")}
	src := testSource()
	svc, events := newService(port, fakeConfig{sources: []source.SignalSource{src}, confirm: true})

	res, err := svc.SwitchByID(context.Background(), "s1")
	if err != nil {
		t.Fatalf("switch returned error: %v", err)
	}
	if res.Domain.Outcome != switching.OutcomeFailed {
		t.Errorf("outcome = %q", res.Domain.Outcome)
	}
	if res.Domain.Err == nil {
		t.Error("Domain.Err should describe the failure")
	}
	// 失败也要发 finished，否则前端的 loading 态永远转圈。
	if len(events.names) != 2 {
		t.Errorf("events = %v", events.names)
	}
}

func TestSwitchPassesConfirmMsFromSource(t *testing.T) {
	port := &fakePort{readSuppt: true}
	src := testSource()
	src.ConfirmMs = 0 // 0 = 不确认，即使全局开关是 true
	svc, _ := newService(port, fakeConfig{sources: []source.SignalSource{src}, confirm: true})

	res, err := svc.SwitchByID(context.Background(), "s1")
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if res.Domain.Outcome != switching.OutcomeAppliedUnconfirmed {
		t.Errorf("outcome = %q, want applied_unconfirmed", res.Domain.Outcome)
	}
}

func TestSwitchSerializesConcurrentCalls(t *testing.T) {
	// DDC 通道独占：两个切换不能同时进行。这里用「写次数」间接验证串行
	// （并行的话 fakePort 的 writes 计数会乱，且 current 会被交错覆盖）。
	port := &fakePort{readSuppt: true}
	src := testSource()
	svc, _ := newService(port, fakeConfig{sources: []source.SignalSource{src}, confirm: false})

	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, _ = svc.SwitchByID(context.Background(), "s1")
			done <- struct{}{}
		}()
	}
	<-done
	<-done
	if port.writes != 2 {
		t.Errorf("writes = %d, want 2", port.writes)
	}
}

func TestNotFoundErrorMessage(t *testing.T) {
	err := &NotFoundError{ID: "s9"}
	if got, want := err.Error(), "signal source not found: s9"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
