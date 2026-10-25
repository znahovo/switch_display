package platform

import (
	"log/slog"

	"github.com/znahovo/switch-display/internal/application"
)

// Logger 用 slog 实现 application.Logger。
//
// kv 约定是「成对出现」，这里容错处理落单的键（写成 key="!DANGLING"），
// 避免一个笔误 panic 掉整个应用。
type Logger struct {
	l *slog.Logger
}

// NewLogger 构造日志端口。l 为 nil 时用 slog.Default()。
func NewLogger(l *slog.Logger) *Logger {
	if l == nil {
		l = slog.Default()
	}
	return &Logger{l: l}
}

// Debug 实现 application.Logger。
func (g *Logger) Debug(msg string, kv ...any) { g.l.Debug(msg, sanitize(kv)...) }

// Info 实现 application.Logger。
func (g *Logger) Info(msg string, kv ...any) { g.l.Info(msg, sanitize(kv)...) }

// Error 实现 application.Logger。
func (g *Logger) Error(msg string, err error, kv ...any) {
	args := sanitize(kv)
	if err != nil {
		args = append(args, "error", err)
	}
	g.l.Error(msg, args...)
}

// sanitize 把落单的键补成 key/value 对，否则 slog 会把它们渲染成 !BADKEY。
func sanitize(kv []any) []any {
	if len(kv)%2 == 0 {
		return kv
	}
	out := make([]any, len(kv)+1)
	copy(out, kv)
	out[len(kv)-1] = "!DANGLING"
	out[len(kv)] = kv[len(kv)-1]
	return out
}

var _ application.Logger = (*Logger)(nil)
