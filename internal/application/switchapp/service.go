// Package switchapp 是「切换信号源」用例。
//
// 它是切换流程的唯一入口：前端按钮、托盘菜单项、全局快捷键都走这条路，
// 所以切换的并发保护、事件广播只需要在这里做一次。
package switchapp

import (
	"context"
	"sync"

	"github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/application/dto"
	"github.com/znahovo/switch-display/internal/domain/source"
	"github.com/znahovo/switch-display/internal/domain/switching"
)

// ConfigReader 是本用例需要的配置读取能力（由 configapp.Service 实现）。
type ConfigReader interface {
	FindSource(id string) (source.SignalSource, bool)
	Snapshot() source.Snapshot
}

// Service 执行切换用例。
type Service struct {
	mu     sync.Mutex // 串行化：DDC 通道不能被两次切换同时占用
	engine *switching.Service
	config ConfigReader
	events application.EventPublisher
	log    application.Logger
}

// Deps 构造依赖。
type Deps struct {
	Engine *switching.Service
	Config ConfigReader
	Events application.EventPublisher
	Log    application.Logger
}

// New 构造切换用例。
func New(d Deps) *Service {
	return &Service{engine: d.Engine, config: d.Config, events: d.Events, log: d.Log}
}

// Result 同时返回 DTO（给前端）与领域结果（给托盘/日志）。
type Result struct {
	DTO    dto.SwitchResultDTO
	Domain switching.Result
}

// SwitchByID 按信号源 ID 执行切换。
func (s *Service) SwitchByID(ctx context.Context, id string) (Result, error) {
	src, ok := s.config.FindSource(id)
	if !ok {
		return Result{}, &NotFoundError{ID: id}
	}
	return s.Switch(ctx, src)
}

// Switch 执行一次切换。
//
// 返回 error 只表示「这次调用本身不成立」（源不存在、上下文取消）；
// 硬件层面的失败是 DTO 里的 outcome=failed，因为它属于业务结果。
func (s *Service) Switch(ctx context.Context, src source.SignalSource) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	confirm := s.config.Snapshot().Settings.ConfirmAfterSwitch

	s.events.Emit(application.EventSwitchStarted, dto.SwitchResultDTO{
		SourceID:  string(src.ID),
		DisplayID: string(src.DisplayID),
		Outcome:   "pending",
	})

	result, err := s.engine.Switch(ctx, src, confirm)
	if err != nil {
		s.log.Error("switch aborted", err, "source", src.Name)
		return Result{}, err
	}

	out := Result{DTO: dto.FromSwitchResult(result), Domain: result}
	s.log.Info("switch finished",
		"source", src.Name,
		"outcome", string(result.Outcome),
		"attempts", result.Attempts)

	s.events.Emit(application.EventSwitchFinished, out.DTO)
	return out, nil
}

// NotFoundError 表示给定的信号源 ID 不在配置里。
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return "signal source not found: " + e.ID }
