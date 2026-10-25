package displayapp

import (
	"context"
	"errors"
	"testing"
	"time"

	appcore "github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/domain/display"
)

type nopLogger struct{}

func (nopLogger) Debug(string, ...any)        {}
func (nopLogger) Info(string, ...any)         {}
func (nopLogger) Error(string, error, ...any) {}

type recordingEvents struct{ names []string }

func (e *recordingEvents) Emit(name string, _ any) { e.names = append(e.names, name) }

// fakePort 按顺序吐出预设的显示器列表，用完重复最后一个。
type fakePort struct {
	lists [][]display.Display
	calls int
	err   error
	// readValue 是 ReadVCP 的返回值；readErr 非空时读失败。
	readValue uint16
	readErr   error
	// gotVCP / gotAlt 记录 Probe 实际传下去的参数。
	gotVCP display.VCPCode
	gotAlt bool
}

func (p *fakePort) List(context.Context) ([]display.Display, error) {
	if p.err != nil {
		return nil, p.err
	}
	if len(p.lists) == 0 {
		return nil, nil
	}
	i := p.calls
	if i >= len(p.lists) {
		i = len(p.lists) - 1
	}
	p.calls++
	return p.lists[i], nil
}

func (p *fakePort) ReadVCP(_ context.Context, _ display.DisplayID, vcp display.VCPCode, alt bool) (display.ReadBack, error) {
	p.gotVCP, p.gotAlt = vcp, alt
	if p.readErr != nil {
		return display.ReadBack{}, p.readErr
	}
	return display.ReadBack{Supported: true, Current: p.readValue, Maximum: 255}, nil
}

func (p *fakePort) WriteVCP(context.Context, display.DisplayID, display.InputValue) error { return nil }

// scriptedClock 按脚本返回 Sleep 结果：先依次返回 nil，脚本用完后返回
// context.Canceled，这样 Watcher.Run 能自然退出，测试不会挂住。
//
// 注意不能用 ctx.Err()：未取消的 ctx 上它是 nil，Run 会无限循环。
type scriptedClock struct {
	remaining int
	slept     []time.Duration
}

func (c *scriptedClock) Now() time.Time { return time.Unix(0, 0) }
func (c *scriptedClock) Sleep(ctx context.Context, d time.Duration) error {
	c.slept = append(c.slept, d)
	if c.remaining <= 0 {
		return context.Canceled
	}
	c.remaining--
	return nil
}

func TestRefreshSortsExternalFirstThenByName(t *testing.T) {
	port := &fakePort{lists: [][]display.Display{{
		{ID: "b1", Name: "Zeta", Builtin: true},
		{ID: "e2", Name: "Beta"},
		{ID: "e1", Name: "Alpha"},
	}}}
	svc := New(Deps{Port: port, Log: nopLogger{}})

	list, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	want := []display.DisplayID{"e1", "e2", "b1"}
	for i, id := range want {
		if list[i].ID != id {
			t.Errorf("index %d = %q, want %q (full: %+v)", i, list[i].ID, id, list)
		}
	}
}

func TestRefreshPropagatesPortError(t *testing.T) {
	port := &fakePort{err: errors.New("iokit exploded")}
	svc := New(Deps{Port: port, Log: nopLogger{}})
	if _, err := svc.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestListCachesAfterFirstRefresh(t *testing.T) {
	port := &fakePort{lists: [][]display.Display{{{ID: "e1", Name: "A"}}}}
	svc := New(Deps{Port: port, Log: nopLogger{}})

	// 第一次 List 缓存为空 → 触发一次枚举。
	if _, err := svc.List(context.Background()); err != nil {
		t.Fatalf("list: %v", err)
	}
	// 第二次不应再碰端口（calls 不再增长）。
	before := port.calls
	if _, err := svc.List(context.Background()); err != nil {
		t.Fatalf("list: %v", err)
	}
	if port.calls != before {
		t.Errorf("port called again: %d -> %d", before, port.calls)
	}
}

func TestListReturnsCopy(t *testing.T) {
	port := &fakePort{lists: [][]display.Display{{{ID: "e1", Name: "A"}}}}
	svc := New(Deps{Port: port, Log: nopLogger{}})

	first, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	first[0].Name = "mutated"
	second, _ := svc.List(context.Background())
	if second[0].Name != "A" {
		t.Error("List leaked its internal slice")
	}
}

func TestListDTOLabelsDisplays(t *testing.T) {
	port := &fakePort{lists: [][]display.Display{{{ID: "e1", Name: ""}}}}
	svc := New(Deps{Port: port, Log: nopLogger{}})

	got, err := svc.ListDTO(context.Background())
	if err != nil {
		t.Fatalf("list dto: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Display e1" {
		t.Errorf("ListDTO = %+v", got)
	}
}

func TestProbeDefaultsVCPAndMapsPreset(t *testing.T) {
	port := &fakePort{readValue: 18}
	svc := New(Deps{Port: port, Log: nopLogger{}})

	got, err := svc.Probe(context.Background(), "uuid-1", 0, false)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// vcp=0 必须兜底成 0x60，否则会去读一个不存在的代码。
	if port.gotVCP != display.VCPInputSource {
		t.Errorf("vcp = %v, want 0x60", port.gotVCP)
	}
	if got.Value != 18 {
		t.Errorf("Value = %d", got.Value)
	}
	if got.PresetKey != "hdmi2" {
		t.Errorf("PresetKey = %q, want hdmi2", got.PresetKey)
	}
}

func TestProbePassesAltThrough(t *testing.T) {
	port := &fakePort{readValue: 208}
	svc := New(Deps{Port: port, Log: nopLogger{}})

	got, err := svc.Probe(context.Background(), "uuid-1", 0xF4, true)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !port.gotAlt {
		t.Error("alt flag not passed to the port")
	}
	if got.PresetKey != "alt-dp1" {
		t.Errorf("PresetKey = %q, want alt-dp1", got.PresetKey)
	}
}

func TestProbePropagatesReadError(t *testing.T) {
	port := &fakePort{readErr: errors.New("i2c nack")}
	svc := New(Deps{Port: port, Log: nopLogger{}})
	if _, err := svc.Probe(context.Background(), "uuid-1", 0x60, false); err == nil {
		t.Fatal("expected error")
	}
}

func TestWatcherDefaultsToFiveSeconds(t *testing.T) {
	w := NewWatcher(nil, nil, nopLogger{}, &scriptedClock{}, 0)
	if w.interval != 5*time.Second {
		t.Errorf("interval = %v, want 5s", w.interval)
	}
	// 负数同样走默认值，避免配置写错导致忙等。
	w = NewWatcher(nil, nil, nopLogger{}, &scriptedClock{}, -time.Second)
	if w.interval != 5*time.Second {
		t.Errorf("interval = %v, want 5s", w.interval)
	}
}

func TestWatcherEmitsWhenTopologyChanges(t *testing.T) {
	port := &fakePort{lists: [][]display.Display{
		{{ID: "e1", Name: "A"}},                        // 首次 fingerprint
		{{ID: "e1", Name: "A"}},                        // 第一轮：没变
		{{ID: "e1", Name: "A"}, {ID: "e2", Name: "B"}}, // 第二轮：多了一台
	}}
	svc := New(Deps{Port: port, Log: nopLogger{}})
	events := &recordingEvents{}
	clock := &scriptedClock{remaining: 3}
	w := NewWatcher(svc, events, nopLogger{}, clock, time.Second)

	w.Run(context.Background()) // 脚本用完后立刻返回，不会挂住

	if len(events.names) != 1 {
		t.Fatalf("events = %v, want exactly one state change", events.names)
	}
	if events.names[0] != appcore.EventStateChanged {
		t.Errorf("event = %q", events.names[0])
	}
	// 每轮都睡 interval，而不是别的时长。
	for _, d := range clock.slept {
		if d != time.Second {
			t.Errorf("slept %v, want 1s", d)
		}
	}
}

func TestWatcherStaysQuietWhenTopologyIsStable(t *testing.T) {
	port := &fakePort{lists: [][]display.Display{{{ID: "e1", Name: "A"}}}}
	svc := New(Deps{Port: port, Log: nopLogger{}})
	events := &recordingEvents{}
	w := NewWatcher(svc, events, nopLogger{}, &scriptedClock{remaining: 2}, time.Second)

	w.Run(context.Background())
	if len(events.names) != 0 {
		t.Errorf("events = %v, want none", events.names)
	}
}

func TestWatcherExitsOnContextCancel(t *testing.T) {
	port := &fakePort{lists: [][]display.Display{{{ID: "e1", Name: "A"}}}}
	svc := New(Deps{Port: port, Log: nopLogger{}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		NewWatcher(svc, &recordingEvents{}, nopLogger{}, &scriptedClock{remaining: 99}, time.Second).Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
