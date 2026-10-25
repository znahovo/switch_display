// Package displayapp 是「显示器枚举与探测」用例。
package displayapp

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/application/dto"
	"github.com/znahovo/switch-display/internal/domain/display"
)

// Service 维护当前显示器列表的缓存。
//
// 为什么缓存：前端每次渲染、托盘每次重建都要显示器列表，而 DDC 枚举
// 要遍历整个 IORegistry（毫秒级但没必要每次都做）。只在启动、显示器
// 热插拔、用户手动刷新时重建。
type Service struct {
	mu   sync.RWMutex
	list []display.Display
	port display.Port
	log  application.Logger
}

// Deps 构造依赖。
type Deps struct {
	Port display.Port
	Log  application.Logger
}

// New 构造显示器用例。
func New(d Deps) *Service {
	return &Service{port: d.Port, log: d.Log}
}

// Refresh 重新枚举显示器并更新缓存。
func (s *Service) Refresh(ctx context.Context) ([]display.Display, error) {
	list, err := s.port.List(ctx)
	if err != nil {
		return nil, err
	}
	// 固定顺序，避免每次刷新后前端列表跳动。
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Builtin != list[j].Builtin {
			return list[j].Builtin // 外接排前面：本项目就是为外接显示器做的
		}
		return list[i].Name < list[j].Name
	})

	s.mu.Lock()
	s.list = list
	s.mu.Unlock()

	s.log.Info("displays refreshed", "count", len(list))
	return list, nil
}

// refreshQuiet 与 Refresh 相同但不打日志。
//
// 热插拔轮询每 5 秒跑一次，如果每次都打 Info 会把日志淹掉，
// 而真正需要看到的是「拓扑变了」那一条。
func (s *Service) refreshQuiet(ctx context.Context) ([]display.Display, error) {
	list, err := s.port.List(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.list = list
	s.mu.Unlock()
	return list, nil
}

// List 返回缓存的显示器列表；未初始化时尝试枚举一次。
func (s *Service) List(ctx context.Context) ([]display.Display, error) {
	s.mu.RLock()
	cached := s.list
	s.mu.RUnlock()
	if cached != nil {
		return append([]display.Display(nil), cached...), nil
	}
	return s.Refresh(ctx)
}

// ListDTO 返回给前端的显示器列表。
func (s *Service) ListDTO(ctx context.Context) ([]dto.DisplayDTO, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	return dto.FromDisplays(list), nil
}

// Probe 读取某台显示器某个 VCP 的当前值，用于「探测这个端口有没有信号」。
func (s *Service) Probe(ctx context.Context, id string, vcp byte, alt bool) (dto.InputDTO, error) {
	code := display.VCPCode(vcp)
	if code == 0 {
		code = display.VCPInputSource
	}
	rb, err := s.port.ReadVCP(ctx, display.DisplayID(id), code, alt)
	if err != nil {
		return dto.InputDTO{}, err
	}
	return dto.FromInput(display.InputValue{VCP: code, Value: rb.Current, Alt: alt}), nil
}

// Watcher 周期性轮询显示器数量变化，用于热插拔。
//
// 用轮询而不是 IOAVService 通知：DDC 层没有可靠的插拔回调，
// 而轮询成本极低（一次 IORegistry 遍历）。
type Watcher struct {
	svc      *Service
	events   application.EventPublisher
	log      application.Logger
	clock    application.Clock
	interval time.Duration
}

// NewWatcher 构造热插拔轮询器。interval 为零时用 5 秒。
func NewWatcher(svc *Service, events application.EventPublisher, log application.Logger, clock application.Clock, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Watcher{svc: svc, events: events, log: log, clock: clock, interval: interval}
}

// Run 阻塞轮询，直到 ctx 取消。由 bootstrap 放进 goroutine。
func (w *Watcher) Run(ctx context.Context) {
	last := w.fingerprint(ctx)
	for {
		if err := w.clock.Sleep(ctx, w.interval); err != nil {
			return
		}
		cur := w.fingerprint(ctx)
		if cur == last {
			continue
		}
		last = cur
		w.log.Info("display topology changed", "fingerprint", cur)
		w.events.Emit(application.EventStateChanged, nil)
	}
}

// fingerprint 用「ID + 名字」的拼接表示当前拓扑，够用且不必深比较。
func (w *Watcher) fingerprint(ctx context.Context) string {
	list, err := w.svc.refreshQuiet(ctx)
	if err != nil {
		w.log.Debug("display poll failed", "error", err)
		return ""
	}
	out := ""
	for _, d := range list {
		out += string(d.ID) + "|" + d.Name + ";"
	}
	return out
}
