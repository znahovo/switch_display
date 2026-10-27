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

// bundleDirName 是随应用分发时 Deskflow 运行时的目录名。
//
// macOS 上是 `<App>.app/Contents/Resources/deskflow/`（里面是 Deskflow 自己的
// `Contents/{MacOS,Frameworks,PlugIns,Resources}`，因为 core 的 rpath 是
// `@loader_path/../Frameworks`，只有保持这套相对结构才能跑起来）；
// Windows 上是 `<exe 所在目录>\deskflow\`。
const bundleDirName = "deskflow"

// bundledCore 返回随应用分发（内置）的 deskflow-core 路径，找不到返回空串。
//
// 不用 `go:embed`：Deskflow 是 40–57MB 的原生程序且带一堆动态库，
// 塞进 Go 二进制只会让每次启动都被包体拖慢。按安装目录相对定位即可。
func bundledCore() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// 解析符号链接：macOS 从 .app 里启动时 os.Executable 已是真路径，
	// 但开发期 `go run` 与某些打包器会给出软链。
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return bundledCoreUnder(filepath.Dir(exe))
}

// bundledCoreUnder 在 exeDir（即可执行文件所在目录）下找内置的 deskflow-core。
//
// 单独拆出来是为了可测：bundledCore 依赖 os.Executable，测试里造不出
// 真实的 .app 布局，只能直接喂目录。
func bundledCoreUnder(exeDir string) string {
	candidates := make([]string, 0, 3)
	if runtime.GOOS == "darwin" {
		// <App>.app/Contents/MacOS/<binary> → Resources/deskflow/Contents/MacOS/
		candidates = append(candidates,
			filepath.Join(exeDir, "..", "Resources", bundleDirName, "Contents", "MacOS", coreBinName))
	}
	candidates = append(candidates,
		filepath.Join(exeDir, bundleDirName, coreBinName+exeSuffix()),
		filepath.Join(exeDir, bundleDirName, "Contents", "MacOS", coreBinName+exeSuffix()),
	)

	for _, cand := range candidates {
		if p, err := resolveExecutable(cand); err == nil {
			return p
		}
	}
	return ""
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// candidatePaths 返回各平台上 deskflow-core 的候选路径（按优先级）。
//
// 顺序理由：用户显式指定（调用方在 Locate 里优先处理）> 内置（随应用分发的
// 那份）> 系统安装的 Deskflow.app > 包管理器 > PATH。
// 内置版排在系统安装版之前，是因为我们打包的那套 Qt 依赖是配好的；
// 用户想用自己的 Deskflow 仍可在设置里填显式路径覆盖。
func candidatePaths() []string {
	paths := make([]string, 0, 8)
	if bundled := bundledCore(); bundled != "" {
		paths = append(paths, bundled)
	}

	switch runtime.GOOS {
	case "darwin":
		paths = append(paths,
			"/Applications/Deskflow.app/Contents/MacOS/"+coreBinName,
			filepath.Join(homeDir(), "Applications/Deskflow.app/Contents/MacOS", coreBinName),
			"/opt/homebrew/bin/"+coreBinName,
			"/usr/local/bin/"+coreBinName,
		)
	case "windows":
		paths = append(paths,
			filepath.Join(os.Getenv("ProgramFiles"), "Deskflow", coreBinName+".exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Deskflow", coreBinName+".exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Deskflow", coreBinName+".exe"),
		)
	default:
		paths = append(paths,
			"/usr/bin/"+coreBinName,
			"/usr/local/bin/"+coreBinName,
			"/opt/homebrew/bin/"+coreBinName,
			filepath.Join(homeDir(), ".local/bin", coreBinName),
		)
	}
	return paths
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
