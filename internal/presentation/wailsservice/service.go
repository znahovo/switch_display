// Package wailsservice 是对外（前端）的唯一 API 表面。
//
// 它是「薄适配器」：只做参数解包、调用用例、把领域错误翻译成前端能显示的
// 文案。任何流程控制、校验、重试都不属于这一层（见 architecture.md §4）。
package wailsservice

import (
	"context"
	"errors"
	"sync"

	wails "github.com/wailsapp/wails/v3/pkg/application"

	"github.com/znahovo/switch-display/internal/application/configapp"
	"github.com/znahovo/switch-display/internal/application/displayapp"
	"github.com/znahovo/switch-display/internal/application/dto"
	"github.com/znahovo/switch-display/internal/application/switchapp"
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

// DisplayService 是暴露给前端的服务。方法名即前端 API 名（Wails 直接映射）。
type DisplayService struct {
	displays *displayapp.Service
	config   *configapp.Service
	switcher *switchapp.Service

	mu       sync.RWMutex
	warnings []string // 最近一次操作的警告（快捷键占用等），随 State 返回

	ctx context.Context // Wails 注入的生命周期 context

	// onStartup 是启动钩子（由 bootstrap 注入），在 Wails 完成服务启动后
	// 执行一次性初始化：读配置、枚举显示器、注册快捷键、启动热插拔轮询。
	//
	// 用回调而不是让 wailsservice import bootstrap：表现层不该知道装配细节，
	// 否则依赖方向就反了（R1）。
	onStartup func(context.Context) error
}

// Deps 构造依赖。
type Deps struct {
	Displays  *displayapp.Service
	Config    *configapp.Service
	Switcher  *switchapp.Service
	OnStartup func(context.Context) error
}

// New 构造前端服务。
func New(d Deps) *DisplayService {
	return &DisplayService{
		displays:  d.Displays,
		config:    d.Config,
		switcher:  d.Switcher,
		onStartup: d.OnStartup,
	}
}

// ServiceName 实现 Wails Service 接口的可选命名（显示在日志里）。
func (s *DisplayService) ServiceName() string { return "DisplayService" }

// ServiceStartup 在应用启动时由 Wails 调用。
//
// 这里只记下 context：Wails 传进来的 context 在应用退出时取消，
// 让「切换中退出」能立刻中断等待。
func (s *DisplayService) ServiceStartup(ctx context.Context, _ wails.ServiceOptions) error {
	s.ctx = ctx
	if s.onStartup == nil {
		return nil
	}
	return s.onStartup(ctx)
}

// ServiceShutdown 在应用退出时由 Wails 调用。
func (s *DisplayService) ServiceShutdown() error { return nil }

// ---- 读状态 ----

// State 返回前端渲染所需的全部状态。
//
// 一次给全量而不是分多个方法：前端 store 只需要一个 refresh()，
// 也不会出现「显示器列表更新了但配置还是旧的」这种中间态。
func (s *DisplayService) State() (dto.StateDTO, error) {
	ctx := s.lifeCtx()

	displays, err := s.displays.ListDTO(ctx)
	if err != nil {
		return dto.StateDTO{}, newError(err, "list displays")
	}

	snap := s.config.Snapshot()
	return dto.StateDTO{
		Displays:   displays,
		Sources:    dto.FromSources(snap.Sources),
		Settings:   dto.FromSettings(snap.Settings),
		ConfigPath: s.config.Path(),
		Warnings:   s.takeWarnings(),
	}, nil
}

// ListDisplays 强制重新枚举显示器（用户点「重新检测」时调用）。
func (s *DisplayService) ListDisplays() ([]dto.DisplayDTO, error) {
	list, err := s.displays.Refresh(s.lifeCtx())
	if err != nil {
		return nil, newError(err, "list displays")
	}
	return dto.FromDisplays(list), nil
}

// Presets 返回内置输入口预设表。
func (s *DisplayService) Presets() []dto.PresetDTO {
	return dto.FromPresets(source.Presets)
}

// AltPresets 返回 LG 等显示器使用的备用预设表（VCP 0xF4）。
func (s *DisplayService) AltPresets() []dto.PresetDTO {
	return dto.FromPresets(source.AltPresets)
}

// ---- 切换 ----

// SwitchSource 切换到指定信号源。
//
// 注意返回值：硬件失败不是 Go error，而是 Outcome=failed 的 DTO，
// 因为「显示器没响应」是正常业务结果，前端应当提示而不是当作异常。
func (s *DisplayService) SwitchSource(id string) (dto.SwitchResultDTO, error) {
	res, err := s.switcher.SwitchByID(s.lifeCtx(), id)
	if err != nil {
		var nf *switchapp.NotFoundError
		if errors.As(err, &nf) {
			return dto.SwitchResultDTO{}, newError(err, "switch source")
		}
		return dto.SwitchResultDTO{}, newError(err, "switch source")
	}
	return res.DTO, nil
}

// ReadCurrentInput 读取某台显示器当前的输入源（用于高亮）。
//
// best-effort：很多显示器读不回 0x60（本机 Mi Monitor 就是），
// 所以返回 supported=false 而不是 error。
func (s *DisplayService) ReadCurrentInput(displayID string) (dto.InputDTO, error) {
	in, err := s.displays.Probe(s.lifeCtx(), displayID, byte(display.VCPInputSource), false)
	if err != nil {
		return dto.InputDTO{}, newError(err, "read current input")
	}
	return in, nil
}

// ---- 配置写 ----

// SaveConfig 保存完整配置。
//
// 入参用「全量快照」而不是「改哪存哪」：配置很小，全量保存让后端不需要
// 处理增量合并，也就没有并发合并的 bug 空间。
func (s *DisplayService) SaveConfig(state dto.StateDTO) (dto.SaveResultDTO, error) {
	next := source.Snapshot{
		Version:  source.CurrentVersion,
		Settings: dto.ToSettings(state.Settings),
		Sources:  make([]source.SignalSource, 0, len(state.Sources)),
	}
	for _, src := range state.Sources {
		next.Sources = append(next.Sources, dto.ToSource(src))
	}

	res, err := s.config.Save(s.lifeCtx(), next)
	if err != nil {
		return dto.SaveResultDTO{OK: false}, newError(err, "save config")
	}
	s.setWarnings(res.Warnings)
	return res, nil
}

// ExportConfig 把配置写到 dest。
func (s *DisplayService) ExportConfig(dest string) error {
	if err := s.config.Export(s.lifeCtx(), dest); err != nil {
		return newError(err, "export config")
	}
	return nil
}

// ImportConfig 读取外部配置并返回内容（不落盘，由前端确认后再 SaveConfig）。
func (s *DisplayService) ImportConfig(src string) (dto.StateDTO, error) {
	snap, err := s.config.Import(s.lifeCtx(), src)
	if err != nil {
		return dto.StateDTO{}, newError(err, "import config")
	}
	current, _ := s.State()
	current.Sources = dto.FromSources(snap.Sources)
	current.Settings = dto.FromSettings(snap.Settings)
	return current, nil
}

// ConfigPath 返回配置文件路径，便于前端展示「配置在哪」。
func (s *DisplayService) ConfigPath() string { return s.config.Path() }

// ---- 内部 ----

// lifeCtx 返回 Wails 生命周期 context；未注入时退回 Background。
//
// 为什么需要：ServiceStartup 之前调用方法（比如前端首屏）时 ctx 还是 nil，
// 直接传给用例会 panic。
func (s *DisplayService) lifeCtx() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *DisplayService) setWarnings(w []string) {
	s.mu.Lock()
	s.warnings = append([]string(nil), w...)
	s.mu.Unlock()
}

// takeWarnings 取出并清空：警告只随下一次 State 返回一次，
// 否则前端每次刷新都会弹出同一个过期警告。
func (s *DisplayService) takeWarnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.warnings
	s.warnings = nil
	return out
}
