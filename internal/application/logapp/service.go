// Package logapp 是「日志」用例层：按游标读出日志、清空缓冲、提供文件位置。
//
// 它只做编排，不实现缓冲（在 infrastructure/logging）也不做渲染（在前端）。
package logapp

import (
	"sort"

	"github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/application/dto"
)

// MaxLimit 是一次最多返回的条数。
//
// 原先跟缓冲容量同级（2000）；缓冲降到 800 后这里也同步，否则会出现
// 「允许要 2000 条、后端只存 800 条」的错配，且一次 IPC 仍可能搬
// 一份比缓冲还大的数组。
const MaxLimit = 800

// Deps 是构造参数。
type Deps struct {
	// Buffer 是日志缓冲（实现见 infrastructure/logging.Bus）。
	Buffer application.LogBuffer
}

// Service 是日志用例。
type Service struct {
	buf application.LogBuffer
}

// New 构造日志服务。
func New(d Deps) *Service { return &Service{buf: d.Buffer} }

// Query 返回游标之后的日志片段。
//
// after 是前端上一次收到的最大序号；首次传 0。返回的 Latest 是缓冲里
// 当前最大的序号，前端下次原样回传即可。
func (s *Service) Query(after uint64, limit int) dto.LogBatchDTO {
	if limit <= 0 || limit > MaxLimit {
		limit = MaxLimit
	}
	if s.buf == nil {
		return dto.LogBatchDTO{}
	}

	entries := s.buf.Since(after, limit)

	out := make([]dto.LogEntryDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, dto.LogEntryDTO{
			Seq:     e.Seq,
			Time:    e.Time.Format("2006-01-02T15:04:05.000"),
			Level:   e.Level,
			Source:  e.Source,
			Message: e.Message,
			Fields:  e.Fields,
		})
	}

	latest := s.buf.Latest()
	// Oldest 是缓冲里还剩的最小序号（空缓冲时为 0）。前端拿它和
	// 自己的游标比对，就能发现「我落后太多、中间那段已经没了」。
	oldest := s.buf.Oldest()

	return dto.LogBatchDTO{
		Entries:   out,
		Latest:    latest,
		Oldest:    oldest,
		Dropped:   s.buf.Dropped(),
		Capacity:  s.buf.Capacity(),
		FilePath:  s.buf.FilePath(),
		FileError: s.buf.FileError(),
		// Lost 表示前端落后到「中间有缺失」，界面该提示而不是假装连续。
		Lost: after > 0 && oldest > 0 && after+1 < oldest,
	}
}

// Sources 返回出现过的来源列表，供前端做筛选下拉。
func (s *Service) Sources() []string {
	if s.buf == nil {
		return nil
	}
	entries := s.buf.Since(0, 0)
	seen := make(map[string]struct{}, 4)
	for _, e := range entries {
		if e.Source == "" {
			continue
		}
		seen[e.Source] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Clear 清空内存缓冲（磁盘日志保留）。
func (s *Service) Clear() {
	if s.buf != nil {
		s.buf.Clear()
	}
}

// FilePath 返回日志文件路径。
func (s *Service) FilePath() string {
	if s.buf == nil {
		return ""
	}
	return s.buf.FilePath()
}
