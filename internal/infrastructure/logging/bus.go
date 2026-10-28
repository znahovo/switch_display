// Package logging 是进程内日志总线：环形缓冲 + 文件落盘 + 给前端的快照。
//
// 为什么要自己实现而不只往 stderr 打：用户在没有终端的场景（双击打开的
// .app / .exe）里出问题时，唯一能看到的就是界面。共享键鼠依赖一个外部
// 子进程（deskflow-core），它的失败原因常常只在子进程自己的输出里，
// 所以这里既要收本进程的 slog，也要收子进程的原始 stdout/stderr。
//
// 它属于基础设施层：实现的是「日志端口」，并且可以直接被 bootstrap 注入
// 到任何需要在界面上留痕的地方（architecture.md R3）。
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/znahovo/switch-display/internal/application"
)

// 级别字符串。前端按这些值过滤，所以是稳定标识而不是文案。
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
	// LevelRaw 是「无法识别级别」的外部输出（子进程的裸行）。
	LevelRaw = "raw"
)

// Entry 就是用例层的日志条目类型。
//
// 用类型别名而不是另立一套：这个包要实现 application.LogBuffer 端口，
// 两套类型会让每个方法都多一次转换，且很容易漏字段。
type Entry = application.LogEntry

// DefaultCapacity 是环形缓冲保留的条数。
//
// 800 条足够覆盖一次「启动 → 配对 → 失败」的全过程（实测一次完整启动
// 只有二十来行业务日志），内存占用在一百多 KB 量级。缓冲只放用户真正
// 会看的日志：框架噪音在源头就被挡掉了（见 bootstrap 的 WailsLogger），
// 不靠这里的大容量去兜。
const DefaultCapacity = 800

// NotifyInterval 是「有新日志」事件的最小间隔。
//
// 不做合并的话，子进程一次启动就能打出几十行，每行一次 IPC 会把
// 前端主线程刷爆；而日志页本身只需要 200ms 的刷新粒度。
const NotifyInterval = 200 * time.Millisecond

// Deps 是 Bus 的构造参数。
type Deps struct {
	// Capacity 是环形缓冲条数；<=0 用 DefaultCapacity。
	Capacity int
	// FilePath 是落盘路径；空表示不落盘。
	FilePath string
	// FileLimit 是单文件字节上限，超出后切成 <path>.1；<=0 用默认值。
	FileLimit int64
	// Events 用于广播 EventLogAppended；nil 表示不广播。
	Events application.EventPublisher
	// Now 便于测试；nil 用 time.Now。
	Now func() time.Time
}

// Bus 收日志、存日志、广播「有新日志」。
type Bus struct {
	mu       sync.Mutex
	entries  []Entry
	nextSeq  uint64
	dropped  int
	capacity int

	file   *FileWriter
	events application.EventPublisher
	now    func() time.Time

	// pending 保证 NotifyInterval 内只发一次事件。
	pending bool
}

// New 构造 Bus。FilePath 打不开记录在 FileError 里，不影响内存日志。
func New(d Deps) *Bus {
	if d.Capacity <= 0 {
		d.Capacity = DefaultCapacity
	}
	b := &Bus{
		capacity: d.Capacity,
		events:   d.Events,
		now:      d.Now,
		nextSeq:  1,
	}
	if b.now == nil {
		b.now = time.Now
	}
	if d.FilePath != "" {
		// 落盘失败不能阻断日志：界面上的日志才是用户能看到的，
		// 文件只是为了让用户能把日志发出来。
		b.file, _ = NewFileWriter(d.FilePath, d.FileLimit)
	}
	return b
}

// SetEvents 注入事件发布者。
//
// 单独一个方法而不是构造参数：日志总线在 wails.New 之前就要建好
// （Wails 在构造时固定自己的 logger），那时事件发布者还不存在。
func (b *Bus) SetEvents(events application.EventPublisher) {
	b.mu.Lock()
	b.events = events
	b.mu.Unlock()
}

// FilePath 实现 application.LogBuffer。即使当前写不进去也返回路径，
// 因为「日志该在哪」本身对用户有用。
func (b *Bus) FilePath() string {
	if b.file == nil {
		return ""
	}
	return b.file.Path()
}

// FileError 返回落盘的错误说明（空串表示正常）。
func (b *Bus) FileError() string {
	if b.file == nil {
		return ""
	}
	return b.file.Error()
}

// Capacity 返回环形缓冲容量。
func (b *Bus) Capacity() int { return b.capacity }

// Append 记一条日志。它是所有入口（slog handler / 子进程输出）的唯一写点。
func (b *Bus) Append(e Entry) {
	if e.Time.IsZero() {
		e.Time = b.now()
	}
	if e.Level == "" {
		e.Level = LevelInfo
	}

	b.mu.Lock()
	e.Seq = b.nextSeq
	b.nextSeq++
	b.entries = append(b.entries, e)
	if over := len(b.entries) - b.capacity; over > 0 {
		// 拷贝而不是切片留引用：否则被裁掉的前缀会一直被底层数组拽着。
		kept := make([]Entry, b.capacity)
		copy(kept, b.entries[over:])
		b.entries = kept
		b.dropped += over
	}
	file := b.file
	b.mu.Unlock()

	if file != nil {
		file.Write(e)
	}
	b.notify()
}

// Tail 返回最近 limit 条（limit<=0 表示全要）。
func (b *Bus) Tail(limit int) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit <= 0 || limit > len(b.entries) {
		limit = len(b.entries)
	}
	out := make([]Entry, limit)
	copy(out, b.entries[len(b.entries)-limit:])
	return out
}

// Latest 返回已写入的最大序号（0 表示还没有任何日志）。
func (b *Bus) Latest() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nextSeq - 1
}

// Oldest 实现 application.LogBuffer：缓冲里还剩的最小序号。
func (b *Bus) Oldest() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) == 0 {
		return 0
	}
	return b.entries[0].Seq
}

// Since 返回序号大于 after 的条目。
//
// 如果 after 已经被环形缓冲挤掉，返回的就是缓冲里现存最旧的那批——
// 前端据此能看出「中间有缺失」，而不是静默地少显示一段。
func (b *Bus) Since(after uint64, limit int) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()

	start := 0
	for start < len(b.entries) && b.entries[start].Seq <= after {
		start++
	}
	out := b.entries[start:]
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	// 复制：调用方可能在锁外长时间使用，不能让它和后续 Append 共享底层数组。
	cp := make([]Entry, len(out))
	copy(cp, out)
	return cp
}

// Dropped 返回因容量上限被丢弃的条数。
func (b *Bus) Dropped() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// Clear 清空内存缓冲（磁盘文件不删，历史仍然可查）。
func (b *Bus) Clear() {
	b.mu.Lock()
	b.entries = nil
	b.dropped = 0
	b.mu.Unlock()
}

// Raw 记录一行外部进程的原始输出。
//
// 实现 deskflow.Sink 端口。级别从行里尽力解析（Deskflow 的格式是
// `[<ISO8601>] LEVEL: message`），解析不出来就记 raw。
func (b *Bus) Raw(source, line string) {
	if line == "" {
		return
	}
	b.Append(Entry{Level: rawLevel(line), Source: source, Message: line})
}

// notify 合并 200ms 内的多次写入，只发一次事件。
func (b *Bus) notify() {
	if b.events == nil {
		return
	}
	b.mu.Lock()
	if b.pending {
		b.mu.Unlock()
		return
	}
	b.pending = true
	b.mu.Unlock()

	time.AfterFunc(NotifyInterval, func() {
		b.mu.Lock()
		b.pending = false
		events := b.events
		b.mu.Unlock()
		if events != nil {
			events.Emit(application.EventLogAppended, nil)
		}
	})
}

// rawLevel 从外部日志行里猜级别。
func rawLevel(line string) string {
	idx := strings.Index(line, "] ")
	if idx < 0 {
		return LevelRaw
	}
	rest := line[idx+2:]
	c := strings.Index(rest, ": ")
	if c <= 0 {
		return LevelRaw
	}
	switch strings.ToUpper(strings.TrimSpace(rest[:c])) {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR", "ERR", "CRIT", "CRITICAL", "FATAL":
		return LevelError
	}
	return LevelRaw
}

// Handler 构造一个 write-only 的 slog.Handler，把记录写进 Bus。
//
// source 用来区分日志来源（app / wails / …），前端据此做过滤。
func (b *Bus) Handler(source string, level slog.Leveler) slog.Handler {
	if level == nil {
		level = slog.LevelInfo
	}
	return &handler{bus: b, source: source, level: level}
}

// handler 是 slog.Handler 的最小实现。
type handler struct {
	bus    *Bus
	source string
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func (h *handler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level.Level() }

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	attrs := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	h.bus.Append(Entry{
		Time:    r.Time,
		Level:   levelName(r.Level),
		Source:  h.source,
		Message: r.Message,
		Fields:  renderAttrs(h.groups, attrs),
	})
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *handler) WithGroup(name string) slog.Handler {
	next := *h
	next.groups = append(append([]string{}, h.groups...), name)
	return &next
}

// levelName 把 slog 级别映射成前端认的字符串。
func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return LevelError
	case l >= slog.LevelWarn:
		return LevelWarn
	case l >= slog.LevelInfo:
		return LevelInfo
	default:
		return LevelDebug
	}
}

// renderAttrs 把 kv 渲染成一行。带空格的字符串加引号，否则读起来会歧义。
func renderAttrs(groups []string, attrs []slog.Attr) string {
	if len(attrs) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, a := range attrs {
		if i > 0 {
			sb.WriteByte(' ')
		}
		if len(groups) > 0 {
			sb.WriteString(strings.Join(groups, "."))
			sb.WriteByte('.')
		}
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		sb.WriteString(renderValue(a.Value))
	}
	return sb.String()
}

// renderValue 渲染单个值。err 类型的值直接取 Error()。
func renderValue(v slog.Value) string {
	v = v.Resolve()
	if v.Kind() == slog.KindAny {
		if err, ok := v.Any().(error); ok && err != nil {
			return err.Error()
		}
	}
	s := fmt.Sprint(v.Any())
	if strings.ContainsAny(s, " \t") {
		return strconv.Quote(s)
	}
	return s
}

// Filtered 在 h 之上再按 (级别, 消息) 做一层筛选。
//
// 用途：框架（Wails）自己的日志要么吵到没用（每个静态资源请求一条），
// 要么恰恰是排障关键（Build Info / Platform Info）。只按级别调没法
// 同时满足这两点，所以让调用方给一个判定函数。
//
// 注意它恒为 Enabled：判定要看到消息内容才能做，而 slog 在 Enabled
// 阶段拿不到消息。代价只是让上游多做一次调用，比「放宽级别」便宜得多。
func Filtered(h slog.Handler, keep func(level slog.Level, msg string) bool) slog.Handler {
	if h == nil {
		return nil
	}
	return &filteredHandler{next: h, keep: keep}
}

type filteredHandler struct {
	next slog.Handler
	keep func(level slog.Level, msg string) bool
}

func (f *filteredHandler) Enabled(context.Context, slog.Level) bool { return true }

func (f *filteredHandler) Handle(ctx context.Context, r slog.Record) error {
	if f.keep != nil && !f.keep(r.Level, r.Message) {
		return nil
	}
	// 下游自己的级别门槛仍要生效：Filtered 只做「额外」筛选。
	if !f.next.Enabled(ctx, r.Level) {
		return nil
	}
	return f.next.Handle(ctx, r.Clone())
}

func (f *filteredHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &filteredHandler{next: f.next.WithAttrs(attrs), keep: f.keep}
}

func (f *filteredHandler) WithGroup(name string) slog.Handler {
	return &filteredHandler{next: f.next.WithGroup(name), keep: f.keep}
}

// Tee 把一条记录同时写到多个 handler。
//
// 用途：界面上要留痕，开发时终端也要能看（否则跑 dev 时日志页和终端
// 各有一半信息）。
func Tee(handlers ...slog.Handler) slog.Handler {
	out := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			out = append(out, h)
		}
	}
	return teeHandler(out)
}

type teeHandler []slog.Handler

func (t teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range t {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range t {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		// 复制一份 record 再交给下游：handler 之间不该互相影响。
		_ = h.Handle(ctx, r.Clone())
	}
	return nil
}

func (t teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make(teeHandler, len(t))
	for i, h := range t {
		next[i] = h.WithAttrs(attrs)
	}
	return next
}

func (t teeHandler) WithGroup(name string) slog.Handler {
	next := make(teeHandler, len(t))
	for i, h := range t {
		next[i] = h.WithGroup(name)
	}
	return next
}
