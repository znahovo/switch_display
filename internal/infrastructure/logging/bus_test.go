package logging

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// nopEvents 吞掉事件（这里不关心广播）。
type nopEvents struct {
	mu    sync.Mutex
	calls int
}

func (n *nopEvents) Emit(string, any) {
	n.mu.Lock()
	n.calls++
	n.mu.Unlock()
}

func (n *nopEvents) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

func TestBusAssignsIncrementingSeq(t *testing.T) {
	b := New(Deps{})
	b.Append(Entry{Level: LevelInfo, Message: "a"})
	b.Append(Entry{Level: LevelInfo, Message: "b"})

	got := b.Tail(0)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Seq != 1 || got[1].Seq != 2 {
		t.Errorf("seq = %d,%d, want 1,2", got[0].Seq, got[1].Seq)
	}
	if b.Latest() != 2 {
		t.Errorf("Latest = %d, want 2", b.Latest())
	}
}

func TestBusSinceReturnsOnlyNewer(t *testing.T) {
	b := New(Deps{})
	for i := 0; i < 5; i++ {
		b.Append(Entry{Level: LevelInfo, Message: "x"})
	}

	got := b.Since(3, 0)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Seq != 4 {
		t.Errorf("first seq = %d, want 4", got[0].Seq)
	}
	if got := b.Since(5, 0); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestBusSinceRespectsLimitKeepingNewest(t *testing.T) {
	// 落后太多时给最新的那几条，而不是最旧的——用户关心「刚刚发生了什么」。
	b := New(Deps{})
	for i := 0; i < 10; i++ {
		b.Append(Entry{Level: LevelInfo, Message: "x"})
	}
	got := b.Since(0, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[2].Seq != 10 {
		t.Errorf("last seq = %d, want 10", got[2].Seq)
	}
}

func TestBusDropsOldestWhenFull(t *testing.T) {
	b := New(Deps{Capacity: 3})
	for i := 0; i < 5; i++ {
		b.Append(Entry{Level: LevelInfo, Message: "x"})
	}
	got := b.Tail(0)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// 保留的是最新的三条（seq 3,4,5），Oldest 要如实反映。
	if got[0].Seq != 3 {
		t.Errorf("first seq = %d, want 3", got[0].Seq)
	}
	if b.Oldest() != 3 {
		t.Errorf("Oldest = %d, want 3", b.Oldest())
	}
	if b.Dropped() != 2 {
		t.Errorf("Dropped = %d, want 2", b.Dropped())
	}
}

func TestBusClearResetsEntriesButKeepsSeq(t *testing.T) {
	b := New(Deps{})
	b.Append(Entry{Level: LevelInfo, Message: "a"})
	b.Clear()
	if len(b.Tail(0)) != 0 {
		t.Fatal("expected empty buffer")
	}
	b.Append(Entry{Level: LevelInfo, Message: "b"})
	// 清空后序号必须继续往前走：前端游标才不会在清空后重复取到旧序号。
	if got := b.Tail(1)[0].Seq; got != 2 {
		t.Errorf("seq = %d, want 2", got)
	}
}

func TestBusRawParsesLevel(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"[2026-10-28T08:00:00] INFO: started client", LevelInfo},
		{"[2026-10-28T08:00:00] WARNING: failed to connect", LevelWarn},
		{"[2026-10-28T08:00:00] ERROR: nope", LevelError},
		{"[2026-10-28T08:00:00] DEBUG: detail", LevelDebug},
		{"[2026-10-28T08:00:00] FATAL: dead", LevelError},
		{"random line without level", LevelRaw},
		{"", LevelRaw},
	}
	for _, tc := range tests {
		if got := rawLevel(tc.line); got != tc.want {
			t.Errorf("rawLevel(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestBusRawStoresSource(t *testing.T) {
	b := New(Deps{})
	b.Raw("deskflow", "[2026-10-28T08:00:00] INFO: started client")
	got := b.Tail(1)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Source != "deskflow" {
		t.Errorf("source = %q, want deskflow", got[0].Source)
	}
	if !strings.Contains(got[0].Message, "started client") {
		t.Errorf("message = %q", got[0].Message)
	}
}

func TestBusRawIgnoresEmptyLine(t *testing.T) {
	b := New(Deps{})
	b.Raw("deskflow", "")
	if len(b.Tail(0)) != 0 {
		t.Error("empty line should be skipped")
	}
}

func TestBusHandlerWritesStructuredEntries(t *testing.T) {
	b := New(Deps{})
	l := slog.New(b.Handler("app", slog.LevelDebug))

	l.Info("pair request", "peer", "moe", "port", 24800)
	l.Error("boom", "err", errors.New("i2c nack"))

	got := b.Tail(0)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Level != LevelInfo || got[0].Source != "app" {
		t.Errorf("entry = %+v", got[0])
	}
	if !strings.Contains(got[0].Fields, "peer=moe") {
		t.Errorf("fields = %q, want peer=moe", got[0].Fields)
	}
	if !strings.Contains(got[1].Fields, "i2c nack") {
		t.Errorf("error value missing: %q", got[1].Fields)
	}
}

func TestBusHandlerRespectsLevel(t *testing.T) {
	b := New(Deps{})
	l := slog.New(b.Handler("app", slog.LevelWarn))
	l.Debug("dropped")
	l.Info("dropped")
	l.Warn("kept")

	got := b.Tail(0)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
	}
	if got[0].Level != LevelWarn {
		t.Errorf("level = %q, want warn", got[0].Level)
	}
}

func TestBusHandlerQuotesValuesWithSpaces(t *testing.T) {
	// 带空格的值不加引号会让人以为多了一个字段。
	b := New(Deps{})
	l := slog.New(b.Handler("app", slog.LevelDebug))
	l.Info("msg", "name", "Mac Book Pro")

	fields := b.Tail(1)[0].Fields
	if !strings.Contains(fields, `name="Mac Book Pro"`) {
		t.Errorf("fields = %q", fields)
	}
}

func TestBusHandlerWithAttrsAndGroup(t *testing.T) {
	b := New(Deps{})
	l := slog.New(b.Handler("app", slog.LevelDebug)).With("run", 1).WithGroup("core")
	l.Info("msg", "port", 24800)

	fields := b.Tail(1)[0].Fields
	if !strings.Contains(fields, "run=1") {
		t.Errorf("attr missing: %q", fields)
	}
	if !strings.Contains(fields, "core.port=24800") {
		t.Errorf("group prefix missing: %q", fields)
	}
}

func TestTeeWritesToBothHandlers(t *testing.T) {
	// 终端一个、总线一个：跑 dev 时两边都该有内容。
	b := New(Deps{})
	var termBuf strings.Builder
	term := slog.NewTextHandler(&termBuf, &slog.HandlerOptions{Level: slog.LevelDebug})

	l := slog.New(Tee(term, b.Handler("app", slog.LevelDebug)))
	l.Info("hello")

	if !strings.Contains(termBuf.String(), "hello") {
		t.Errorf("terminal handler got nothing: %q", termBuf.String())
	}
	if len(b.Tail(0)) != 1 {
		t.Errorf("bus got %d entries", len(b.Tail(0)))
	}
}

func TestTeeSkipsNilHandlers(t *testing.T) {
	b := New(Deps{})
	l := slog.New(Tee(nil, b.Handler("app", slog.LevelDebug)))
	l.Info("hello")
	if len(b.Tail(0)) != 1 {
		t.Error("nil handler broke the tee")
	}
}

func TestBusNotifiesOncePerInterval(t *testing.T) {
	// 一次启动能打出几十行，逐行广播会把前端刷爆。
	ev := &nopEvents{}
	b := New(Deps{Events: ev})

	for i := 0; i < 20; i++ {
		b.Append(Entry{Level: LevelInfo, Message: "x"})
	}
	// NotifyInterval 之后应当只收到一次。
	deadline := time.Now().Add(4 * NotifyInterval)
	for time.Now().Before(deadline) && ev.count() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(2 * NotifyInterval)

	if got := ev.count(); got != 1 {
		t.Errorf("events = %d, want 1", got)
	}
}

func TestFileWriterAppendsAndFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w, err := NewFileWriter(path, 0)
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	w.Write(Entry{Time: time.Date(2026, 10, 28, 8, 30, 0, 0, time.Local), Level: LevelInfo, Source: "app", Message: "started", Fields: "pid=1"})
	w.Write(Entry{Time: time.Date(2026, 10, 28, 8, 30, 1, 0, time.Local), Level: LevelError, Source: "deskflow", Message: "boom"})
	_ = w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "2026-10-28T08:30:00.000+08:00") {
		// 带时区是有意的：用户会把两台机器的日志放一起对比。
		t.Errorf("timestamp missing: %q", text)
	}
	if !strings.Contains(text, "INFO  [app] started pid=1") {
		t.Errorf("first line malformed: %q", text)
	}
	if !strings.Contains(text, "ERROR [deskflow] boom") {
		t.Errorf("second line malformed: %q", text)
	}
}

func TestFileWriterRollsWhenOverLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	w, err := NewFileWriter(path, 64)
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		w.Write(Entry{Time: time.Now(), Level: LevelInfo, Source: "app", Message: "abcdefghij"})
	}
	_ = w.Close()

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rolled file missing: %v", err)
	}
}

func TestFileWriterReportsUnwritablePath(t *testing.T) {
	// 目录不可写时不能 panic、也不能让业务失败，只把错误记下来给界面看。
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "app.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0o755) })

	w, err := NewFileWriter(path, 0)
	if err == nil {
		// 以 root 跑测试时写权限检查不生效，跳过即可。
		_ = w.Close()
		t.Skip("directory is writable for this user")
	}
	if err.Error() == "" {
		t.Error("expected an error message")
	}
}

func TestBusFilePathExposed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	b := New(Deps{FilePath: path})
	if b.FilePath() != path {
		t.Errorf("FilePath = %q, want %q", b.FilePath(), path)
	}
	b.Append(Entry{Level: LevelInfo, Message: "x"})

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}

func TestBusWithoutFileHasEmptyPath(t *testing.T) {
	b := New(Deps{})
	if b.FilePath() != "" {
		t.Errorf("FilePath = %q, want empty", b.FilePath())
	}
	b.Append(Entry{Level: LevelInfo, Message: "x"}) // 不能 panic
}

func TestNewSkipZeroCapacity(t *testing.T) {
	b := New(Deps{Capacity: -5})
	if b.Capacity() != DefaultCapacity {
		t.Errorf("Capacity = %d, want %d", b.Capacity(), DefaultCapacity)
	}
}

func TestFilteredDropsByPredicate(t *testing.T) {
	// Wails 的噪音级别写死在它自己源码里，只能在这一层按消息拦。
	bus := New(Deps{Capacity: 10})
	h := Filtered(bus.Handler("wails", slog.LevelInfo), func(_ slog.Level, msg string) bool {
		return !strings.Contains(msg, "Handling request")
	})
	l := slog.New(h)
	l.Info("Handling request", "url", "/assets/index.js")
	l.Info("Build Info:")
	if got := bus.Tail(10); len(got) != 1 || got[0].Message != "Build Info:" {
		t.Fatalf("filtered = %+v", got)
	}
}

func TestFilteredKeepsDownstreamLevelGate(t *testing.T) {
	// Filtered 恒 Enabled，但下游自己的级别门槛仍要生效，
	// 否则「只要 Warn」的 handler 会被 Debug 记录灌满。
	bus := New(Deps{Capacity: 10})
	l := slog.New(Filtered(bus.Handler("wails", slog.LevelWarn), nil))
	l.Debug("debug line")
	l.Warn("warn line")
	got := bus.Tail(10)
	if len(got) != 1 || got[0].Level != LevelWarn {
		t.Fatalf("got = %+v", got)
	}
}

func TestFilteredNilHandler(t *testing.T) {
	if got := Filtered(nil, func(slog.Level, string) bool { return true }); got != nil {
		t.Errorf("Filtered(nil) = %v, want nil", got)
	}
}

func TestFilteredWithAttrsAndGroup(t *testing.T) {
	bus := New(Deps{Capacity: 10})
	h := Filtered(bus.Handler("wails", slog.LevelInfo), func(slog.Level, string) bool { return true })
	l := slog.New(h).With("pid", 7).WithGroup("core")
	l.Info("msg")
	got := bus.Tail(10)
	// slog 的组前缀作用于 handler 上挂的全部 attr（含 With 加进来的），
	// 这条断言同时确认 Filtered 没有破坏这一点。
	if len(got) != 1 || got[0].Fields != "core.pid=7" {
		t.Fatalf("fields = %q", got[0].Fields)
	}
}
