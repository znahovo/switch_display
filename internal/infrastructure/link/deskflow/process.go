package deskflow

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/znahovo/switch-display/internal/domain/link"
)

// 状态常量直接复用领域值，保证 UI 看到的状态字符串只有一处定义。
const (
	// StateStopped 表示没有 core 在跑。
	StateStopped = link.StateStopped
	// StateStarting 表示已 exec，但还没确认可用。
	StateStarting = link.StateStarting
	// StateRunning 表示 core 进程存活。
	StateRunning = link.StateRunning
	// StateFailed 表示 core 启动失败或异常退出。
	StateFailed = link.StateFailed
)

// 退出原因标识。同样交给前端翻译（见 ShareView 的 core 状态卡）。
const (
	// MsgExitArgs 对应 Deskflow 的 s_exitArgs（3）：参数被拒。
	MsgExitArgs = "exit_args"
	// MsgExitConfig 对应 s_exitConfig（4）：配置文件/布局不合法。
	MsgExitConfig = "exit_config"
	// MsgExitDuplicate 对应 s_exitDuplicate（5）：已有另一个 core。
	MsgExitDuplicate = "exit_duplicate"
)

// ErrBusy 表示已经有 core 在跑（或正在停止）。
var ErrBusy = errors.New("deskflow core is busy")

// 状态判定参数。
const (
	// startGrace 是「进程刚起还没退出」的判定窗口。core 的参数/配置错误
	// 会在几百毫秒内退出，所以熬过这段时间基本说明配置被接受了。
	startGrace = 1200 * time.Millisecond
	// readyProbeTimeout 是 server 端口探测的总超时。
	readyProbeTimeout = 5 * time.Second
	// readyProbeInterval 是端口探测间隔。
	readyProbeInterval = 200 * time.Millisecond
	// maxLogLines 是保留的日志行数（给 UI 显示最近发生了什么）。
	maxLogLines = 200
)

// 状态说明里我们自己产生的那几条用稳定标识，具体措辞交给前端 i18n。
//
// Deskflow 自己输出的 ERROR/CRIT 行是英文原文，无法翻译，原样透传。
const (
	// MsgClientStarted 表示 client 进程已起，正在等连上主机。
	MsgClientStarted = "client_started"
	// MsgClientStopped 表示 client 主动停止。
	MsgClientStopped = "client_stopped"
)

// Status 是 core 的运行状态快照（复用领域值对象）。
type Status = link.RuntimeStatus

// Runtime 管理一个 deskflow-core 子进程。
//
// 关键约束：Deskflow 用 QSharedMemory 做单实例互斥，所以同一时刻只能有一个
// core。我们仍然传 --new-instance（万一用户自己开着 Deskflow GUI），
// 但自身用 mu 保证不会并发起两个。
type Runtime struct {
	binary string
	log    Logger

	mu      sync.Mutex
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	status  Status
	logTail []string
	waitCh  chan struct{}
}

// Logger 是 Runtime 需要的日志端口（避免 import application/port.go 造成
// infrastructure → application 的反向依赖）。
type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Error(msg string, err error, kv ...any)
}

// NewRuntime 构造 Runtime。binary 必须是可执行文件的绝对路径。
func NewRuntime(binary string, log Logger) *Runtime {
	return &Runtime{
		binary: binary,
		log:    log,
		status: Status{State: StateStopped, Role: link.RoleIdle},
	}
}

// Status 返回当前状态快照。
func (r *Runtime) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// LogTail 返回最近的日志行（最多 maxLogLines 行）。
func (r *Runtime) LogTail() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.logTail))
	copy(out, r.logTail)
	return out
}

// Start 启动 core。settingsPath 是 `-s` 指向的配置文件。
//
// 返回 nil 只表示「进程起来了」，不代表已连上对端：server 模式还会
// 探测端口，client 模式则依赖 Deskflow 自己的重试循环。
func (r *Runtime) Start(ctx context.Context, mode Mode, settingsPath string, port int) error {
	if !mode.Validate() {
		return fmt.Errorf("invalid mode %q", mode)
	}

	r.mu.Lock()
	if r.cmd != nil {
		r.mu.Unlock()
		return ErrBusy
	}
	r.status = Status{State: StateStarting, Role: link.Role(mode), StartedAt: time.Now()}
	r.logTail = r.logTail[:0]
	// 子进程的生命周期独立于发起 Start 的那个请求，所以用 Background
	// 派生一个可取消的 context，由 Stop 负责取消。
	runCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.mu.Unlock()

	args := []string{string(mode), "-s", settingsPath, "--new-instance"}
	cmd := exec.CommandContext(runCtx, r.binary, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		r.markFailed(fmt.Errorf("stdout pipe: %w", err))
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		r.markFailed(fmt.Errorf("stderr pipe: %w", err))
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		e := fmt.Errorf("start deskflow-core: %w", err)
		r.markFailed(e)
		return e
	}

	waitCh := make(chan struct{})
	r.mu.Lock()
	r.cmd = cmd
	r.status.PID = cmd.Process.Pid
	r.waitCh = waitCh
	r.mu.Unlock()

	go r.pump(stdout)
	go r.pump(stderr)
	go r.wait(cmd, mode, waitCh)

	r.log.Info("deskflow core started", "pid", cmd.Process.Pid, "mode", string(mode))

	// server 模式必须等端口真的 listen：ServerApp 没有「已就绪」日志，
	// 端口能连上才是唯一可靠的信号。
	if mode == ModeServer {
		go r.probeServerReady(port)
	}
	return nil
}

// probeServerReady 轮询 TCP 端口，确认 server 已经在监听。
func (r *Runtime) probeServerReady(port int) {
	if port <= 0 {
		return
	}
	deadline := time.Now().Add(readyProbeTimeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		// 进程可能已经退出了，别白等。
		r.mu.Lock()
		gone := r.cmd == nil
		r.mu.Unlock()
		if gone {
			return
		}
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			r.mu.Lock()
			if r.status.State == StateStarting {
				r.status.State = StateRunning
				r.status.Message = ""
			}
			r.mu.Unlock()
			return
		}
		time.Sleep(readyProbeInterval)
	}
	// 端口没起来：进程还活着但没在监听，可能是端口被占用或在等配置。
	r.mu.Lock()
	if r.status.State == StateStarting {
		r.status.State = StateFailed
		r.status.Message = fmt.Sprintf("server 未在端口 %d 监听", port)
	}
	r.mu.Unlock()
}

// pump 逐行读取子进程输出并记进日志尾巴。
func (r *Runtime) pump(rd io.Reader) {
	scanner := bufio.NewScanner(rd)
	// Deskflow 单行日志可能较长（带堆栈），放宽上限。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		r.appendLog(line)
	}
}

// appendLog 把一行输出放进环形缓冲，并更新状态。
func (r *Runtime) appendLog(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logTail = append(r.logTail, line)
	if len(r.logTail) > maxLogLines {
		r.logTail = r.logTail[len(r.logTail)-maxLogLines:]
	}

	ev, ok := logEventFor(line)
	if !ok {
		return
	}
	if ev.Message != "" {
		r.status.Message = ev.Message
	}
	// 连上对端是唯一能自证「可用」的信号，从任何未停止状态都能提升。
	// 之所以不限制只在 starting 时提升：client 第一次连接可能被拒
	// （server 还没起来），我们已经按失败处理过，之后重试成功必须能翻回来。
	if ev.Ready && r.status.State != StateStopped {
		r.status.State = StateRunning
		r.status.Message = ""
		return
	}
	// 错误只把「还没成功」的启动判死。已经在跑之后冒出的 ERROR/WARN
	// （光标不可见、输入法映射之类）不影响可用性，等进程真的退出由
	// wait 统一收敛。
	if ev.Failed && r.status.State == StateStarting {
		r.status.State = StateFailed
	}
}

// logEvent 描述一行 Deskflow 日志对运行时状态的含义。
type logEvent struct {
	// Message 是要显示给用户的文字，空串表示不更新。
	Message string
	// Failed 表示这行日志说明启动失败。
	Failed bool
	// Ready 表示这行日志说明对端已经连上，可以算 running。
	Ready bool
}

// benignErrors 是「我们自己造成的」错误噪音，不能当成故障。
//
// 就绪探测靠 TCP 连一下再断开，Deskflow 会把它当成一次失败的 TLS
// 握手并报 `ERROR: failed to accept secure socket`（SecureSocket.cpp:425）。
var benignErrors = []string{
	"failed to accept secure socket",
}

// logEventFor 从 Deskflow 日志行里挑出值得关注的信息。
//
// 日志格式（src/lib/base/Log.cpp:91）：`[<ISO8601>] LEVEL: message`。
// 配置里 level=info，所以 DEBUG 行根本不会出现——判断连接状态必须
// 依赖 INFO 级别的那些行（见下表），不能用 `connected to server`
// 这种 DEBUG 文案。
func logEventFor(line string) (logEvent, bool) {
	idx := strings.Index(line, "] ")
	if idx < 0 {
		return logEvent{}, false
	}
	rest := line[idx+2:]
	c := strings.Index(rest, ": ")
	if c < 0 {
		return logEvent{}, false
	}
	level, msg := rest[:c], rest[c+2:]

	for _, benign := range benignErrors {
		if strings.Contains(msg, benign) {
			return logEvent{}, false
		}
	}

	switch level {
	// WARNING 不算失败：client 连不上 server 时会反复打
	// `failed to connect to server`，然后自己重试——把它判死会让
	// 「先起 client、后起 server」的常见顺序永远起不来。
	case "WARN":
		return logEvent{Message: msg}, true
	case "ERROR", "CRIT", "FATAL":
		return logEvent{Message: msg, Failed: true}, true
	}

	// INFO 级别里只挑和连接有关的那几条，否则会把日志尾巴刷满。
	switch {
	// 客户端：TLS 握手成功（SecureSocket.cpp:497）。
	case strings.Contains(msg, "connected to secure socket"):
		return logEvent{Ready: true}, true
	// 服务端：接受了客户端（SecureSocket.cpp:439 / ClientListener.cpp:158）。
	case strings.Contains(msg, "accepted secure socket"),
		strings.Contains(msg, "accepted client connection"):
		return logEvent{Ready: true}, true
	case strings.Contains(msg, "started client"):
		return logEvent{Message: MsgClientStarted}, true
	case strings.Contains(msg, "stopped client"):
		return logEvent{Message: MsgClientStopped}, true
	}
	return logEvent{}, false
}

// wait 等子进程退出并回收。
func (r *Runtime) wait(cmd *exec.Cmd, mode Mode, waitCh chan struct{}) {
	err := cmd.Wait()

	r.mu.Lock()
	// 只有当 cmd 还是本次的 cmd 时才清理（可能已经被 Stop 换掉了）。
	if r.cmd == cmd {
		r.cmd = nil
		if r.cancel != nil {
			r.cancel()
			r.cancel = nil
		}
		stopped := r.status.State == StateStopped
		if !stopped {
			if err != nil {
				r.status.State = StateFailed
				if r.status.Message == "" {
					r.status.Message = exitMessage(err)
				}
			} else {
				r.status.State = StateStopped
			}
		}
		r.status.PID = 0
	}
	r.mu.Unlock()
	close(waitCh)

	r.log.Info("deskflow core exited", "mode", string(mode), "err", err)
}

// exitMessage 把退出错误翻译成用户能理解的说明。
func exitMessage(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		switch ee.ExitCode() {
		// 退出码的含义见 Deskflow 的 src/lib/common/ExitCodes.h。
		case 3:
			return MsgExitArgs
		case 4:
			return MsgExitConfig
		case 5:
			return MsgExitDuplicate
		default:
			return fmt.Sprintf("Deskflow core exited with code %d", ee.ExitCode())
		}
	}
	if errors.Is(err, context.Canceled) {
		return ""
	}
	return fmt.Sprintf("Deskflow core 退出：%v", err)
}

// Stop 停止 core。多次调用是安全的。
func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	cmd := r.cmd
	cancel := r.cancel
	waitCh := r.waitCh
	r.status = Status{State: StateStopped, Role: link.RoleIdle}
	r.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// 先礼后兵：SIGTERM 让 core 自己清理监听、恢复键盘钩子；
	// 2 秒不退再 Kill。
	_ = terminate(cmd.Process)

	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		r.log.Info("deskflow core did not exit in time, killing")
		_ = cmd.Process.Kill()
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
		}
	}
	if cancel != nil {
		cancel()
	}
	// 取消 Stop 自己的 ctx 时也要把状态标成已停止。
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// markFailed 记录一次启动失败。
func (r *Runtime) markFailed(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.State = StateFailed
	r.status.Message = err.Error()
	r.log.Error("deskflow core failed", err)
}
