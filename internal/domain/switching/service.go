// Package switching 是「把显示器切到目标输入口」的领域服务。
//
// 它只依赖 domain/display 的 Port 与一个 Clock 抽象，因此可以在没有显示器、
// 不需要真 sleep 的情况下完整单测（architecture.md §7.1）。
package switching

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

// Outcome 是切换的最终判定，四态（ADR-003）。
//
// 分开四态的理由：DDC/CI 天生不可靠——写成功不代表显示器切过去了，
// 读回也不一定被支持。把「我发了指令」和「显示器确认了」混成一个 bool
// 会让 UI 无法给出诚实提示。
type Outcome string

const (
	// OutcomeConfirmed 写入成功且读回值等于目标值。
	OutcomeConfirmed Outcome = "confirmed"
	// OutcomeMismatch 写入成功，但读回的是另一个值（可能切到了别的口）。
	OutcomeMismatch Outcome = "mismatch"
	// OutcomeAppliedUnconfirmed 写入成功，但显示器不支持读回，无法确认。
	OutcomeAppliedUnconfirmed Outcome = "applied_unconfirmed"
	// OutcomeFailed 所有尝试都写失败。
	OutcomeFailed Outcome = "failed"
)

// Clock 抽象时间，使测试不必真等待。
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// Result 是一次切换的完整结果，直接映射到前端 DTO。
type Result struct {
	SourceID  string
	DisplayID display.DisplayID
	Input     display.InputValue
	Outcome   Outcome
	Attempts  int
	// ReadBack 是最后一次读回结果；OutcomeAppliedUnconfirmed 时为 nil。
	ReadBack *display.ReadBack
	// Err 只在 OutcomeFailed 时非 nil。
	Err error
}

// Service 执行切换流程。零值不可用，请用 NewService 构造。
type Service struct {
	port        display.Port
	clock       Clock
	maxAttempts int
	retryDelay  time.Duration
}

// 默认重试策略：DDC 写失败是常态（显示器忙、DPMS 休眠），值得重试两次。
const (
	defaultMaxAttempts = 2
	defaultRetryDelay  = 300 * time.Millisecond
	// readSettleDelay 是写入后、读回前的短暂等待，给显示器内部状态机一点时间。
	readSettleDelay = 120 * time.Millisecond
)

// NewService 构造切换服务。
func NewService(port display.Port, clock Clock) *Service {
	return &Service{
		port:        port,
		clock:       clock,
		maxAttempts: defaultMaxAttempts,
		retryDelay:  defaultRetryDelay,
	}
}

// Switch 把 src 指向的显示器切到 src 的输入口。
//
// 返回的 error 仅表示「流程本身无法进行」（比如显示器不在了）；切换失败是
// OutcomeFailed + Result.Err，因为它属于业务结果而非调用错误。
func (s *Service) Switch(ctx context.Context, src source.SignalSource, confirm bool) (Result, error) {
	res := Result{
		SourceID:  string(src.ID),
		DisplayID: src.DisplayID,
		Input:     src.Input,
	}

	attempts, err := s.writeWithRetry(ctx, src.DisplayID, src.Input)
	res.Attempts = attempts
	if err != nil {
		res.Outcome = OutcomeFailed
		res.Err = err
		return res, nil
	}

	if !confirm || src.ConfirmMs <= 0 {
		// 用户/配置明确不做确认：只能报「已发送」。
		res.Outcome = OutcomeAppliedUnconfirmed
		return res, nil
	}

	// 等待显示器内部切换完成再读回，否则几乎必然读到旧值。
	if err := s.clock.Sleep(ctx, time.Duration(src.ConfirmMs)*time.Millisecond); err != nil {
		return res, err
	}
	if err := s.clock.Sleep(ctx, readSettleDelay); err != nil {
		return res, err
	}

	rb, err := s.port.ReadVCP(ctx, src.DisplayID, src.Input.VCP, src.Input.Alt)
	if err != nil || !rb.Supported {
		// 读不到不代表没切过去——绝大多数情况是显示器不支持读 0x60。
		res.Outcome = OutcomeAppliedUnconfirmed
		return res, nil
	}

	res.ReadBack = &rb
	if rb.Current == src.Input.Value {
		res.Outcome = OutcomeConfirmed
	} else {
		res.Outcome = OutcomeMismatch
	}
	return res, nil
}

// writeWithRetry 最多尝试 maxAttempts 次，全部失败才返回错误。
func (s *Service) writeWithRetry(ctx context.Context, id display.DisplayID, v display.InputValue) (int, error) {
	var lastErr error
	for i := 1; i <= s.maxAttempts; i++ {
		err := s.port.WriteVCP(ctx, id, v)
		if err == nil {
			return i, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return i, ctx.Err()
		}
		if i < s.maxAttempts {
			if err := s.clock.Sleep(ctx, s.retryDelay); err != nil {
				return i, err
			}
		}
	}
	if errors.Is(lastErr, display.ErrUnsupported) {
		return s.maxAttempts, lastErr
	}
	return s.maxAttempts, fmt.Errorf("%w: %v", display.ErrWriteFailed, lastErr)
}
