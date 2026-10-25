package tray

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/znahovo/switch-display/internal/application/displayapp"
	"github.com/znahovo/switch-display/internal/application/switchapp"
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
	"github.com/znahovo/switch-display/internal/domain/switching"
)

type nopLogger struct{}

func (nopLogger) Debug(string, ...any)        {}
func (nopLogger) Info(string, ...any)         {}
func (nopLogger) Error(string, error, ...any) {}

type nopEvents struct{ names []string }

func (e *nopEvents) Emit(name string, _ any) { e.names = append(e.names, name) }

// fakeSwitcher 记录被请求切换的源，并按脚本返回结果。
type fakeSwitcher struct {
	mu      sync.Mutex
	calls   []string
	outcome switching.Outcome
	err     error
	block   chan struct{} // 非 nil 时会一直阻塞到关闭，用来观察 busy 状态
}

func (s *fakeSwitcher) SwitchByID(_ context.Context, id string) (switchapp.Result, error) {
	s.mu.Lock()
	s.calls = append(s.calls, id)
	block := s.block
	s.mu.Unlock()

	if block != nil {
		<-block
	}
	if s.err != nil {
		return switchapp.Result{}, s.err
	}
	return switchapp.Result{Domain: switching.Result{
		SourceID: id,
		Outcome:  s.outcome,
	}}, nil
}

func (s *fakeSwitcher) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func newSwitchTray(sw Switcher, sources []source.SignalSource) *Tray {
	snap := source.EmptySnapshot()
	snap.Sources = sources
	return New(Deps{
		Config:   fakeConfig{snap: snap},
		Switcher: sw,
		Events:   &nopEvents{},
		Log:      nopLogger{},
	})
}

// waitIdle 等 busy 落回 false（切换在 goroutine 里跑）。
func waitIdle(t *testing.T, tr *Tray) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tr.mu.Lock()
		busy := tr.busy
		tr.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("tray stayed busy")
}

func TestSwitchToMarksBusyThenIdle(t *testing.T) {
	sw := &fakeSwitcher{outcome: switching.OutcomeConfirmed, block: make(chan struct{})}
	src := source.SignalSource{ID: "s1", Name: "A", Input: display.InputValue{Value: 15}}
	tr := newSwitchTray(sw, []source.SignalSource{src})

	tr.switchTo("s1", "A")

	// 切换在飞行中：busy 必须是 true，否则用户能连点造成重复写 DDC。
	tr.mu.Lock()
	busy := tr.busy
	tr.mu.Unlock()
	if !busy {
		t.Error("busy should be true while switching")
	}

	close(sw.block)
	waitIdle(t, tr)
	if sw.callCount() != 1 {
		t.Errorf("switcher calls = %d", sw.callCount())
	}
	// 成功后乐观地把打勾挪过去。
	tr.mu.Lock()
	current := tr.current
	tr.mu.Unlock()
	if current != "s1" {
		t.Errorf("current = %q, want s1", current)
	}
}

func TestSwitchToIgnoresClickWhileBusy(t *testing.T) {
	sw := &fakeSwitcher{outcome: switching.OutcomeConfirmed, block: make(chan struct{})}
	src := source.SignalSource{ID: "s1", Name: "A", Input: display.InputValue{Value: 15}}
	tr := newSwitchTray(sw, []source.SignalSource{src})

	tr.switchTo("s1", "A") // 第一次进入 busy
	tr.switchTo("s1", "A") // 这次应被丢掉
	close(sw.block)
	waitIdle(t, tr)

	if sw.callCount() != 1 {
		t.Errorf("switcher calls = %d, want 1 (second click must be dropped)", sw.callCount())
	}
}

func TestSwitchToDoesNotMoveCheckOnFailure(t *testing.T) {
	// 显示器拒绝了写入：打勾不能跟着挪，否则菜单会显示一个假的当前源。
	sw := &fakeSwitcher{outcome: switching.OutcomeFailed}
	src := source.SignalSource{ID: "s1", Name: "A", Input: display.InputValue{Value: 15}}
	tr := newSwitchTray(sw, []source.SignalSource{src})
	tr.current = "s0"

	tr.switchTo("s1", "A")
	waitIdle(t, tr)

	tr.mu.Lock()
	current := tr.current
	tr.mu.Unlock()
	if current != "s0" {
		t.Errorf("current = %q, want s0 (unchanged)", current)
	}
}

func TestSwitchToErrorKeepsCheck(t *testing.T) {
	sw := &fakeSwitcher{err: errors.New("boom")}
	src := source.SignalSource{ID: "s1", Name: "A", Input: display.InputValue{Value: 15}}
	tr := newSwitchTray(sw, []source.SignalSource{src})
	tr.current = "s0"

	tr.switchTo("s1", "A")
	waitIdle(t, tr)

	tr.mu.Lock()
	current := tr.current
	tr.mu.Unlock()
	if current != "s0" {
		t.Errorf("current = %q, want s0", current)
	}
	// 出错也必须退出 busy，否则菜单永远禁用。
}

// stubPort 让 displayapp 能返回固定的输入源读数。
type stubPort struct {
	value   uint16
	support bool
	err     error
}

func (p *stubPort) List(context.Context) ([]display.Display, error) {
	return []display.Display{{ID: "uuid-1", Name: "Fake"}}, nil
}

func (p *stubPort) ReadVCP(context.Context, display.DisplayID, display.VCPCode, bool) (display.ReadBack, error) {
	if p.err != nil {
		return display.ReadBack{}, p.err
	}
	return display.ReadBack{Supported: p.support, Current: p.value, Maximum: 255}, nil
}

func (p *stubPort) WriteVCP(context.Context, display.DisplayID, display.InputValue) error { return nil }

func TestResyncCurrentUsesReadBack(t *testing.T) {
	// 用户用显示器按键切走了：读回值应该纠正打勾。
	port := &stubPort{value: 18, support: true}
	a := source.SignalSource{ID: "s1", Name: "A", DisplayID: "uuid-1", Input: display.InputValue{Value: 15}}
	b := source.SignalSource{ID: "s2", Name: "B", DisplayID: "uuid-1", Input: display.InputValue{Value: 18}}
	tr := New(Deps{
		Config: fakeConfig{snap: source.Snapshot{
			Version: source.CurrentVersion, Settings: source.DefaultSettings(),
			Sources: []source.SignalSource{a, b},
		}},
		Displays: displayapp.New(displayapp.Deps{Port: port, Log: nopLogger{}}),
		Log:      nopLogger{},
	})
	tr.current = "s1"

	tr.resyncCurrent()

	tr.mu.Lock()
	current := tr.current
	tr.mu.Unlock()
	if current != "s2" {
		t.Errorf("current = %q, want s2", current)
	}
}

func TestResyncCurrentKeepsCheckWhenReadFails(t *testing.T) {
	// 输入源读回不可靠的显示器上：读失败不能把打勾清空。
	port := &stubPort{err: errors.New("nack")}
	a := source.SignalSource{ID: "s1", Name: "A", DisplayID: "uuid-1", Input: display.InputValue{Value: 15}}
	tr := New(Deps{
		Config: fakeConfig{snap: source.Snapshot{
			Version: source.CurrentVersion, Settings: source.DefaultSettings(),
			Sources: []source.SignalSource{a},
		}},
		Displays: displayapp.New(displayapp.Deps{Port: port, Log: nopLogger{}}),
		Log:      nopLogger{},
	})
	tr.current = "s1"

	tr.resyncCurrent()

	tr.mu.Lock()
	current := tr.current
	tr.mu.Unlock()
	if current != "s1" {
		t.Errorf("current = %q, want s1 (unchanged)", current)
	}
}

func TestResyncCurrentWithoutDisplaysIsNoop(t *testing.T) {
	src := source.SignalSource{ID: "s1", Name: "A", DisplayID: "uuid-1"}
	tr := newSwitchTray(nil, []source.SignalSource{src})
	tr.current = "s1"
	tr.resyncCurrent() // 不该 panic
	if tr.current != "s1" {
		t.Errorf("current = %q", tr.current)
	}
}
