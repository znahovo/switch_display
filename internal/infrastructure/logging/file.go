package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultFileLimit 是单个日志文件的字节上限（2 MiB）。
//
// 共享键鼠出问题时用户往往连点好几次重试，日志长得很快；上限到了就
// 滚动成 `.1`，只保留上一份，避免把磁盘写满。
const DefaultFileLimit int64 = 2 << 20

// FileWriter 把日志追加到一个文本文件，超限时滚动。
//
// 所有写入都是「尽力而为」：日志落盘失败绝不能让业务失败，所以错误
// 只记录在 err 里供界面展示，不向上抛。
type FileWriter struct {
	mu    sync.Mutex
	path  string
	limit int64
	f     *os.File
	size  int64
	err   string
}

// NewFileWriter 打开（或创建）日志文件。失败时返回的 writer 仍然可用，
// 只是 Error() 非空——调用方据此在界面上提示。
func NewFileWriter(path string, limit int64) (*FileWriter, error) {
	if limit <= 0 {
		limit = DefaultFileLimit
	}
	w := &FileWriter{path: path, limit: limit}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// Path 返回目标路径。
func (w *FileWriter) Path() string { return w.path }

// Error 返回最近一次失败说明（空串表示正常）。
func (w *FileWriter) Error() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// Write 追加一条日志。
func (w *FileWriter) Write(e Entry) {
	w.mu.Lock()
	defer w.mu.Unlock()

	line := FormatLine(e)
	if w.f == nil {
		if err := w.open(); err != nil {
			return
		}
	}
	n, err := w.f.WriteString(line)
	if err != nil {
		w.err = err.Error()
		return
	}
	w.size += int64(n)
	if w.size < w.limit {
		return
	}
	w.roll()
}

// Close 关闭文件。
func (w *FileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// open（重新）打开文件，并把现有大小算进去，否则重启后会晚一步滚动。
func (w *FileWriter) open() error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		w.err = err.Error()
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		w.err = err.Error()
		return err
	}
	info, statErr := f.Stat()
	if statErr == nil {
		w.size = info.Size()
	}
	w.f = f
	w.err = ""
	return nil
}

// roll 把当前文件改名为 `<path>.1`（覆盖旧的那份）后重新开始。
func (w *FileWriter) roll() {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		// 改名失败（比如被占用）就截断继续写，总比不记日志强。
		w.err = err.Error()
	}
	w.size = 0
	if err := w.open(); err != nil {
		return
	}
	// 滚动后写一行分隔，方便用户把两份日志拼起来看。
	// 这行也计入 size，但几十字节对上限没有影响。
	sep := fmt.Sprintf("=== log rolled, previous is %s.1 ===\n", filepath.Base(w.path))
	if n, err := w.f.WriteString(sep); err == nil {
		w.size += int64(n)
	}
}

// FormatLine 把一条日志渲染成一行文本。
//
// 格式稳定且便于人读（带时区：用户会把两台机器的日志放在一起对比）：
//
//	2026-10-28T08:30:00.123+08:00 INFO  [deskflow] started client pid=123
func FormatLine(e Entry) string {
	if e.Time.IsZero() {
		return ""
	}
	line := fmt.Sprintf("%s %-5s [%s] %s",
		e.Time.Format("2006-01-02T15:04:05.000-07:00"),
		strings.ToUpper(e.Level),
		e.Source,
		e.Message,
	)
	if e.Fields != "" {
		line += " " + e.Fields
	}
	return line + "\n"
}
