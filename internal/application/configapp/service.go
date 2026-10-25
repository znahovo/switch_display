// Package configapp 是「配置读写」用例：加载、保存、导入、导出。
//
// 一次保存就是一个事务边界：校验 → 落盘 → 重建快捷键 → 通知前端。
package configapp

import (
	"context"
	"fmt"
	"sync"

	"github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/application/dto"
	"github.com/znahovo/switch-display/internal/domain/source"
)

// Exporter 是可选的导入导出能力（由 persistence.JSONStore 实现）。
type Exporter interface {
	Export(ctx context.Context, snap source.Snapshot, dest string) error
	Import(ctx context.Context, src string) (source.Snapshot, error)
}

// Service 持有当前配置的内存副本。
//
// 为什么在内存里留一份：切换流程、托盘菜单、前端状态全都需要它，
// 每次读磁盘会让「点一下切源」变成一次 IO。磁盘只是持久化，内存才是真相。
type Service struct {
	mu     sync.RWMutex
	snap   source.Snapshot
	repo   source.Repository
	export Exporter

	events application.EventPublisher
	hotkey application.HotkeyRegistrar
	log    application.Logger
	clock  application.Clock
}

// Deps 是构造依赖，集中传参避免构造函数过长。
type Deps struct {
	Repo   source.Repository
	Export Exporter
	Events application.EventPublisher
	Hotkey application.HotkeyRegistrar
	Log    application.Logger
	Clock  application.Clock
}

// New 构造配置用例服务。
func New(d Deps) *Service {
	return &Service{
		snap:   source.EmptySnapshot(),
		repo:   d.Repo,
		export: d.Export,
		events: d.Events,
		hotkey: d.Hotkey,
		log:    d.Log,
		clock:  d.Clock,
	}
}

// Load 从磁盘读取配置并放进内存。启动时调用一次。
func (s *Service) Load(ctx context.Context) (source.Snapshot, error) {
	snap, err := s.repo.Load(ctx)
	if err != nil {
		return source.EmptySnapshot(), err
	}
	// 磁盘上的配置可能引用了已不存在的显示器，这里不拦——那是「警告」
	// 不是「错误」，等前端显示时再标记为失效。
	if err := snap.Validate(); err != nil {
		s.log.Error("loaded config failed validation, continuing with empty config", err)
		snap = source.EmptySnapshot()
	}

	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
	return snap, nil
}

// Snapshot 返回当前配置的副本。
//
// 返回切片是深拷贝：调用方改它不会污染内存中的真相（避免难查的串改 bug）。
func (s *Service) Snapshot() source.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.snap
	out.Sources = append([]source.SignalSource(nil), s.snap.Sources...)
	return out
}

// FindSource 按 ID 找信号源。
func (s *Service) FindSource(id string) (source.SignalSource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap.FindByID(source.ID(id))
}

// Save 保存一次完整配置：校验 → 落盘 → 重绑快捷键 → 广播。
//
// 快捷键注册失败不回滚配置（ADR-007）：用户精心填的端口表不该因为
// 某个组合键被别的 App 占用而全部丢失。失败项放在 warnings 里返回。
func (s *Service) Save(ctx context.Context, next source.Snapshot) (dto.SaveResultDTO, error) {
	if err := next.Validate(); err != nil {
		return dto.SaveResultDTO{OK: false}, err
	}
	if err := s.repo.Save(ctx, next); err != nil {
		return dto.SaveResultDTO{OK: false}, err
	}

	s.mu.Lock()
	s.snap = next
	s.mu.Unlock()

	warnings := s.rebindHotkeys(next)

	s.events.Emit(application.EventStateChanged, nil)
	return dto.SaveResultDTO{OK: true, Warnings: warnings}, nil
}

// rebindHotkeys 先全清再全注册，避免残留旧绑定。
func (s *Service) rebindHotkeys(snap source.Snapshot) []string {
	if s.hotkey == nil {
		return nil
	}
	s.hotkey.UnregisterAll()

	bindings := make([]application.Binding, 0, len(snap.Sources))
	for _, src := range snap.Sources {
		if src.Shortcut == "" {
			continue
		}
		bindings = append(bindings, application.Binding{
			Accelerator: src.Shortcut,
			SourceID:    string(src.ID),
		})
	}
	if len(bindings) == 0 {
		return nil
	}

	errs := s.hotkey.RegisterAll(bindings)
	warnings := make([]string, 0, len(errs))
	for i, err := range errs {
		if err == nil {
			continue
		}
		msg := fmt.Sprintf("shortcut %q could not be registered: %v", bindings[i].Accelerator, err)
		s.log.Error("hotkey registration failed", err, "accelerator", bindings[i].Accelerator)
		warnings = append(warnings, msg)
	}
	return warnings
}

// RebindHotkeys 只重建快捷键、不落盘。
//
// 启动时需要它：配置已经从磁盘读好了，没必要为了注册快捷键再写一遍文件
// （那会让「首次启动」也创建配置文件，也让文件 mtime 每次启动都变）。
func (s *Service) RebindHotkeys() []string {
	return s.rebindHotkeys(s.Snapshot())
}

// Path 返回配置文件路径。
func (s *Service) Path() string { return s.repo.Path() }

// Export 把当前配置导出到 dest。
func (s *Service) Export(ctx context.Context, dest string) error {
	if s.export == nil {
		return fmt.Errorf("export not supported")
	}
	return s.export.Export(ctx, s.Snapshot(), dest)
}

// Import 读取外部配置并**不落盘**地替换内存状态，返回结果供用户确认。
//
// 分两步（先 Import 再 Save）是刻意的：导入是个破坏性操作，用户应该看到
// 内容后再决定保存。
func (s *Service) Import(ctx context.Context, src string) (source.Snapshot, error) {
	if s.export == nil {
		return source.EmptySnapshot(), fmt.Errorf("import not supported")
	}
	return s.export.Import(ctx, src)
}
