package logapp

import (
	"testing"
	"time"

	"github.com/znahovo/switch-display/internal/application"
)

// fakeBuffer 实现 application.LogBuffer，用来验证用例层的游标语义。
type fakeBuffer struct {
	entries   []application.LogEntry
	dropped   int
	capacity  int
	path      string
	fileErr   string
	clearCall int
}

func (f *fakeBuffer) Since(after uint64, limit int) []application.LogEntry {
	start := 0
	for start < len(f.entries) && f.entries[start].Seq <= after {
		start++
	}
	out := f.entries[start:]
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (f *fakeBuffer) Latest() uint64 {
	if len(f.entries) == 0 {
		return 0
	}
	return f.entries[len(f.entries)-1].Seq
}

func (f *fakeBuffer) Oldest() uint64 {
	if len(f.entries) == 0 {
		return 0
	}
	return f.entries[0].Seq
}

func (f *fakeBuffer) Dropped() int      { return f.dropped }
func (f *fakeBuffer) Capacity() int     { return f.capacity }
func (f *fakeBuffer) FilePath() string  { return f.path }
func (f *fakeBuffer) FileError() string { return f.fileErr }
func (f *fakeBuffer) Clear()            { f.clearCall++; f.entries = nil }

// seed 造 n 条从 seq=1 开始的日志。
func seed(buf *fakeBuffer, n int) {
	for i := 1; i <= n; i++ {
		buf.entries = append(buf.entries, application.LogEntry{
			Seq:     uint64(i),
			Time:    time.Date(2026, 10, 28, 8, 0, i, 0, time.Local),
			Level:   "info",
			Source:  "app",
			Message: "line",
		})
	}
}

func TestQueryFromZeroReturnsAll(t *testing.T) {
	buf := &fakeBuffer{capacity: 100}
	seed(buf, 3)

	got := New(Deps{Buffer: buf}).Query(0, 0)
	if len(got.Entries) != 3 {
		t.Fatalf("len = %d, want 3", len(got.Entries))
	}
	if got.Latest != 3 {
		t.Errorf("Latest = %d, want 3", got.Latest)
	}
	if got.Lost {
		t.Error("first query must not report data loss")
	}
}

func TestQueryWithCursorReturnsOnlyNewer(t *testing.T) {
	buf := &fakeBuffer{capacity: 100}
	seed(buf, 5)

	got := New(Deps{Buffer: buf}).Query(2, 0)
	if len(got.Entries) != 3 {
		t.Fatalf("len = %d, want 3", len(got.Entries))
	}
	if got.Entries[0].Seq != 3 {
		t.Errorf("first seq = %d, want 3", got.Entries[0].Seq)
	}
	if got.Latest != 5 {
		t.Errorf("Latest = %d, want 5", got.Latest)
	}
}

func TestQueryLatestReflectsBufferNotSlice(t *testing.T) {
	// limit 截断时 Latest 仍必须是缓冲里真实的最大序号，
	// 否则前端会以为后面没有新日志、永远停在半截。
	buf := &fakeBuffer{capacity: 100}
	seed(buf, 10)

	got := New(Deps{Buffer: buf}).Query(0, 3)
	if len(got.Entries) != 3 {
		t.Fatalf("len = %d, want 3", len(got.Entries))
	}
	if got.Latest != 10 {
		t.Errorf("Latest = %d, want 10", got.Latest)
	}
}

func TestQueryReportsLostWhenCursorFellBehind(t *testing.T) {
	// 缓冲只留 3..10，而前端游标停在 1：中间那段已经没了，要如实告知。
	buf := &fakeBuffer{capacity: 8}
	for i := 3; i <= 10; i++ {
		buf.entries = append(buf.entries, application.LogEntry{Seq: uint64(i), Time: time.Now(), Level: "info", Message: "x"})
	}

	got := New(Deps{Buffer: buf}).Query(1, 0)
	if !got.Lost {
		t.Error("expected Lost = true")
	}
	if got.Oldest != 3 {
		t.Errorf("Oldest = %d, want 3", got.Oldest)
	}
}

func TestQueryClampsLimit(t *testing.T) {
	buf := &fakeBuffer{capacity: 100}
	seed(buf, 10)

	// 超上限的 limit 会被压到 MaxLimit（这里条目不足，验证不炸即可）。
	got := New(Deps{Buffer: buf}).Query(0, MaxLimit+100)
	if len(got.Entries) != 10 {
		t.Errorf("len = %d, want 10", len(got.Entries))
	}
}

func TestQueryEmptyBufferIsSafe(t *testing.T) {
	got := New(Deps{Buffer: &fakeBuffer{}}).Query(0, 100)
	if len(got.Entries) != 0 || got.Latest != 0 || got.Lost {
		t.Errorf("got %+v", got)
	}
}

func TestQueryWithNilBufferIsSafe(t *testing.T) {
	// 装配出错时不能让日志页把整个界面搞崩。
	got := New(Deps{}).Query(0, 100)
	if len(got.Entries) != 0 {
		t.Errorf("len = %d, want 0", len(got.Entries))
	}
	if got := New(Deps{}).Sources(); got != nil {
		t.Errorf("Sources = %v, want nil", got)
	}
	if got := New(Deps{}).FilePath(); got != "" {
		t.Errorf("FilePath = %q, want empty", got)
	}
}

func TestQueryFormatsTime(t *testing.T) {
	buf := &fakeBuffer{capacity: 10}
	buf.entries = append(buf.entries, application.LogEntry{
		Seq:     1,
		Time:    time.Date(2026, 10, 28, 8, 30, 5, 123000000, time.Local),
		Level:   "info",
		Source:  "app",
		Message: "hi",
		Fields:  "pid=1",
	})

	got := New(Deps{Buffer: buf}).Query(0, 0)
	if got.Entries[0].Time != "2026-10-28T08:30:05.123" {
		t.Errorf("time = %q", got.Entries[0].Time)
	}
	if got.Entries[0].Fields != "pid=1" {
		t.Errorf("fields = %q", got.Entries[0].Fields)
	}
}

func TestQueryPassesThroughFileInfo(t *testing.T) {
	buf := &fakeBuffer{capacity: 10, path: "/tmp/app.log", fileErr: "disk full", dropped: 7}
	got := New(Deps{Buffer: buf}).Query(0, 0)

	if got.FilePath != "/tmp/app.log" {
		t.Errorf("FilePath = %q", got.FilePath)
	}
	if got.FileError != "disk full" {
		t.Errorf("FileError = %q", got.FileError)
	}
	if got.Dropped != 7 {
		t.Errorf("Dropped = %d, want 7", got.Dropped)
	}
}

func TestSourcesAreSortedAndDeduped(t *testing.T) {
	buf := &fakeBuffer{capacity: 10}
	buf.entries = append(buf.entries,
		application.LogEntry{Seq: 1, Source: "wails"},
		application.LogEntry{Seq: 2, Source: "app"},
		application.LogEntry{Seq: 3, Source: "app"},
		application.LogEntry{Seq: 4, Source: ""},
	)

	got := New(Deps{Buffer: buf}).Sources()
	if len(got) != 2 {
		t.Fatalf("sources = %v, want 2", got)
	}
	if got[0] != "app" || got[1] != "wails" {
		t.Errorf("sources = %v, want [app wails]", got)
	}
}

func TestClearDelegatesToBuffer(t *testing.T) {
	buf := &fakeBuffer{}
	New(Deps{Buffer: buf}).Clear()
	if buf.clearCall != 1 {
		t.Errorf("clearCall = %d, want 1", buf.clearCall)
	}
	// nil buffer 也不能 panic。
	New(Deps{}).Clear()
}

func TestFilePathDelegatesToBuffer(t *testing.T) {
	buf := &fakeBuffer{path: "/tmp/x.log"}
	if got := New(Deps{Buffer: buf}).FilePath(); got != "/tmp/x.log" {
		t.Errorf("FilePath = %q", got)
	}
}
