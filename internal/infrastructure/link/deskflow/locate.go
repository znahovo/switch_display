package deskflow

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

// ErrNoCore 表示找不到 deskflow-core 可执行文件。
//
// 表现层把它映射成 `deskflow_not_found`，UI 据此显示安装引导而不是
// 一个看不懂的 exec 错误。
var ErrNoCore = errors.New("deskflow-core not found")

// coreBinName 是 Deskflow core 的可执行文件名。
const coreBinName = "deskflow-core"

// candidatePaths 返回各平台上 deskflow-core 的候选路径（按优先级）。
//
// 顺序理由：先用户显式配置（由调用方在前面插入），再应用包内，
// 再包管理器，最后 PATH。这样「用户装了新版 Deskflow.app」总是优先。
func candidatePaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Deskflow.app/Contents/MacOS/" + coreBinName,
			filepath.Join(homeDir(), "Applications/Deskflow.app/Contents/MacOS", coreBinName),
			"/opt/homebrew/bin/" + coreBinName,
			"/usr/local/bin/" + coreBinName,
		}
	case "windows":
		return []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Deskflow", coreBinName+".exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Deskflow", coreBinName+".exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Deskflow", coreBinName+".exe"),
		}
	default:
		return []string{
			"/usr/bin/" + coreBinName,
			"/usr/local/bin/" + coreBinName,
			"/opt/homebrew/bin/" + coreBinName,
			filepath.Join(homeDir(), ".local/bin", coreBinName),
		}
	}
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// Locate 找到 deskflow-core 的可执行路径。
//
// 优先用调用方给的 explicit（用户在设置里指定的），为空则按
// 平台候选路径 + PATH 查找。找不到返回 ErrNoCore。
func Locate(explicit string) (string, error) {
	if explicit != "" {
		if p, err := resolveExecutable(explicit); err == nil {
			return p, nil
		} else {
			return "", fmt.Errorf("%w: %s: %v", ErrNoCore, explicit, err)
		}
	}

	for _, cand := range candidatePaths() {
		if cand == "" {
			continue
		}
		if p, err := resolveExecutable(cand); err == nil {
			return p, nil
		}
	}

	if p, err := exec.LookPath(coreBinName); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("%w: 请安装 Deskflow（https://github.com/deskflow/deskflow/releases）", ErrNoCore)
}

// resolveExecutable 校验路径确实是一个可执行文件。
func resolveExecutable(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", abs)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", abs)
	}
	return abs, nil
}

// Finder 是带缓存的可执行文件查找器（避免每次启动都扫一遍目录）。
type Finder struct {
	mu       sync.Mutex
	explicit string
	cached   string
	err      error
	done     bool
}

// NewFinder 构造查找器；explicit 为空表示自动查找。
func NewFinder(explicit string) *Finder { return &Finder{explicit: explicit} }

// Find 返回可执行路径，首次调用后缓存结果。
func (f *Finder) Find() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.done {
		return f.cached, f.err
	}
	f.cached, f.err = Locate(f.explicit)
	f.done = true
	return f.cached, f.err
}

// Reset 清空缓存（用户改了设置、或刚安装完 Deskflow 时调用）。
func (f *Finder) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = false
	f.cached = ""
	f.err = nil
}
