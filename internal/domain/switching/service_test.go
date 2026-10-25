package switching

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

// fakeClock 让「等 1.2 秒」在测试里瞬间完成。
type fakeClock struct{ slept []time.Duration }

func (c *fakeClock) Now() time.Time { return time.Unix(0, 0) }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.slept = append(c.slept, d)
	return ctx.Err()
}

// fakePort 记录调用，并可编程返回结果。
type fakePort struct {
	list       []display.Display
	writeErr   error
	writeCalls int
	readBack   display.ReadBack
	readErr    error
}

func (p *fakePort) List(context.Context) ([]display.Display, error) { return p.list, nil }

func (p *fakePort) ReadVCP(context.Context, display.DisplayID, display.VCPCode, bool) (display.ReadBack, error) {
	return p.readBack, p.readErr
}

func (p *fakePort) WriteVCP(context.Context, display.DisplayID, display.InputValue) error {
	p.writeCalls++
	if p.writeErr != nil {
		return p.writeErr
	}
	return nil
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

func TestSwitchConfirmed(t *testing.T) {
	port := &fakePort{readBack: display.ReadBack{Supported: true, Current: 17}}
	clock := &fakeClock{}
	svc := NewService(port, clock)

	res, err := svc.Switch(context.Background(), testSource(), true)
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if res.Outcome != OutcomeConfirmed {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if res.Attempts != 1 {
		t.Fatalf("attempts = %d", res.Attempts)
	}
	if len(clock.slept) != 2 {
		t.Fatalf("expected confirm + settle sleep, got %v", clock.slept)
	}
}

func TestSwitchMismatch(t *testing.T) {
	port := &fakePort{readBack: display.ReadBack{Supported: true, Current: 15}}
	svc := NewService(port, &fakeClock{})

	res, _ := svc.Switch(context.Background(), testSource(), true)
	if res.Outcome != OutcomeMismatch {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if res.ReadBack == nil || res.ReadBack.Current != 15 {
		t.Fatalf("readback = %+v", res.ReadBack)
	}
}

func TestSwitchUnsupportedReadIsAppliedUnconfirmed(t *testing.T) {
	port := &fakePort{readBack: display.ReadBack{Supported: false}}
	svc := NewService(port, &fakeClock{})

	res, _ := svc.Switch(context.Background(), testSource(), true)
	if res.Outcome != OutcomeAppliedUnconfirmed {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if res.ReadBack != nil {
		t.Fatal("readback should be nil when unsupported")
	}
}

func TestSwitchWithoutConfirmSkipsRead(t *testing.T) {
	port := &fakePort{readBack: display.ReadBack{Supported: true, Current: 17}}
	clock := &fakeClock{}
	svc := NewService(port, clock)

	res, _ := svc.Switch(context.Background(), testSource(), false)
	if res.Outcome != OutcomeAppliedUnconfirmed {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if len(clock.slept) != 0 {
		t.Fatalf("expected no sleep, got %v", clock.slept)
	}
}

func TestSwitchRetriesThenFails(t *testing.T) {
	port := &fakePort{writeErr: errors.New("i2c nack")}
	svc := NewService(port, &fakeClock{})

	res, err := svc.Switch(context.Background(), testSource(), true)
	if err != nil {
		t.Fatalf("switch should not return error for hardware failure: %v", err)
	}
	if res.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if res.Attempts != 2 {
		t.Fatalf("attempts = %d", res.Attempts)
	}
	if !errors.Is(res.Err, display.ErrWriteFailed) {
		t.Fatalf("err = %v", res.Err)
	}
}

func TestSwitchRetrySucceedsOnSecondAttempt(t *testing.T) {
	port := &flakyPort{
		fakePort:  fakePort{readBack: display.ReadBack{Supported: true, Current: 17}},
		failFirst: 1,
	}
	svc := NewService(port, &fakeClock{})

	res, _ := svc.Switch(context.Background(), testSource(), true)
	if res.Outcome != OutcomeConfirmed {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if res.Attempts != 2 {
		t.Fatalf("attempts = %d", res.Attempts)
	}
}

// flakyPort 前 n 次写失败，之后成功。
type flakyPort struct {
	fakePort
	failFirst int
	written   int
}

func (p *flakyPort) WriteVCP(ctx context.Context, id display.DisplayID, v display.InputValue) error {
	p.written++
	if p.written <= p.failFirst {
		return errors.New("busy")
	}
	return nil
}
