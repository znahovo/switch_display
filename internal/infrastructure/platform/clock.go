package platform

import (
	"context"
	"time"
)

// Clock 是 application.Clock 的真实实现。
type Clock struct{}

// NewClock 构造系统时钟。
func NewClock() Clock { return Clock{} }

// Now 实现 switching.Clock。
func (Clock) Now() time.Time { return time.Now() }

// Sleep 实现 switching.Clock。
//
// 用 timer 而不是 time.Sleep：切换过程中用户可能关窗退出，
// 这时 ctx 取消应当立刻返回，而不是傻等一秒多。
func (Clock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
