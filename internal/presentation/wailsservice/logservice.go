package wailsservice

import (
	"github.com/znahovo/switch-display/internal/application/dto"
	"github.com/znahovo/switch-display/internal/application/logapp"
)

// LogService 是「日志」页的前端 API 表面。
//
// 它没有 Start/Shutdown 钩子：日志缓冲在装配阶段就建好了，
// 早于任何界面出现——启动初期的日志恰恰是排障最需要的。
type LogService struct {
	svc *logapp.Service
}

// LogDeps 构造依赖。
type LogDeps struct {
	Logs *logapp.Service
}

// NewLogService 构造日志服务。
func NewLogService(d LogDeps) *LogService { return &LogService{svc: d.Logs} }

// ServiceName 实现 Wails Service 接口的可选命名。
func (s *LogService) ServiceName() string { return "LogService" }

// Query 返回游标之后的日志片段。after 为 0 表示从头（缓冲里现存的）开始。
func (s *LogService) Query(after uint64, limit int) dto.LogBatchDTO {
	return s.svc.Query(after, limit)
}

// Sources 返回出现过的来源列表，供前端筛选。
func (s *LogService) Sources() []string { return s.svc.Sources() }

// Clear 清空界面上的日志（磁盘文件保留，事后仍可查看）。
func (s *LogService) Clear() bool {
	s.svc.Clear()
	return true
}

// FilePath 返回日志文件路径，空表示未落盘。
func (s *LogService) FilePath() string { return s.svc.FilePath() }
