package wailsservice

import (
	"context"

	wails "github.com/wailsapp/wails/v3/pkg/application"

	"github.com/znahovo/switch-display/internal/application/dto"
	"github.com/znahovo/switch-display/internal/application/linkapp"
)

// LinkService 是「局域网共享鼠标键盘」的前端 API 表面。
//
// 与 DisplayService 并列而不是塞进它：两者生命周期一致，但关注点无关，
// 拆开后前端可以只订阅 link:changed，显示器热插拔不会触发共享页重渲染。
type LinkService struct {
	svc *linkapp.Service

	ctx context.Context

	// onStartup 是启动钩子（bootstrap 注入），语义同 DisplayService。
	onStartup func(context.Context) error
}

// LinkDeps 构造依赖。
type LinkDeps struct {
	Link *linkapp.Service
	// OnStartup 在 Wails 服务启动后调用（bootstrap 注入）。
	OnStartup func(context.Context) error
}

// NewLink 构造共享服务。
func NewLink(d LinkDeps) *LinkService {
	s := &LinkService{svc: d.Link}
	if d.OnStartup != nil {
		s.onStartup = d.OnStartup
	}
	return s
}

// ServiceName 实现 Wails Service 接口的可选命名。
func (s *LinkService) ServiceName() string { return "LinkService" }

// ServiceStartup 在应用启动时由 Wails 调用。
func (s *LinkService) ServiceStartup(ctx context.Context, _ wails.ServiceOptions) error {
	s.ctx = ctx
	if s.onStartup == nil {
		return nil
	}
	return s.onStartup(ctx)
}

// ServiceShutdown 在应用退出时由 Wails 调用：停 core、关 socket。
func (s *LinkService) ServiceShutdown() error {
	s.svc.Shutdown(context.Background())
	return nil
}

// State 返回共享页所需的全部状态。
func (s *LinkService) State() dto.LinkStateDTO { return s.svc.State() }

// 说明：下面各方法返回的 PairResultDTO.Message 只放稳定标识
// （pair_invite_sent / pair_join_sent / name_updated / sharing_stopped /
// peer_unpaired），具体文案由前端 i18n 渲染，后端不掺语言。

// SetDeviceName 修改本机展示名。
func (s *LinkService) SetDeviceName(name string) (dto.PairResultDTO, error) {
	if err := s.svc.SetDeviceName(s.lifeCtx(), name); err != nil {
		return dto.PairResultDTO{OK: false}, newError(err, "set device name")
	}
	return dto.PairResultDTO{OK: true, Message: "name_updated"}, nil
}

// HostWith 让本机成为主机，邀请指定对端加入。
func (s *LinkService) HostWith(peerID string) (dto.PairResultDTO, error) {
	res, err := s.svc.HostWith(s.lifeCtx(), peerID)
	if err != nil {
		return dto.PairResultDTO{OK: false}, newError(err, "host with peer")
	}
	return res, nil
}

// JoinHost 让本机成为客机，请求加入指定主机。
func (s *LinkService) JoinHost(peerID string) (dto.PairResultDTO, error) {
	res, err := s.svc.JoinHost(s.lifeCtx(), peerID)
	if err != nil {
		return dto.PairResultDTO{OK: false}, newError(err, "join host")
	}
	return res, nil
}

// Stop 停止共享。
func (s *LinkService) Stop() (dto.PairResultDTO, error) {
	if err := s.svc.Stop(s.lifeCtx()); err != nil {
		return dto.PairResultDTO{OK: false}, newError(err, "stop sharing")
	}
	return dto.PairResultDTO{OK: true, Message: "sharing_stopped"}, nil
}

// Unpair 删除一条配对记录。
func (s *LinkService) Unpair(peerID string) (dto.PairResultDTO, error) {
	if err := s.svc.Unpair(s.lifeCtx(), peerID); err != nil {
		return dto.PairResultDTO{OK: false}, newError(err, "unpair")
	}
	return dto.PairResultDTO{OK: true, Message: "peer_unpaired"}, nil
}

// RefreshDeskflow 重新查找 deskflow-core（用户刚装完 Deskflow 时用）。
func (s *LinkService) RefreshDeskflow() bool { return s.svc.RefreshDeskflow() }

// lifeCtx 与 DisplayService 相同语义：ctx 未注入时退回 Background。
func (s *LinkService) lifeCtx() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}
