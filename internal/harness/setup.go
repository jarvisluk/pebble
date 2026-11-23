package harness

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SetupTask represents an active or recent install/login background task.
type SetupTask struct {
	Target     string   `json:"target"`
	Kind       string   `json:"kind"`  // "install" | "login"
	State      string   `json:"state"` // "idle" | "running" | "done" | "error"
	Logs       []string `json:"logs,omitempty"`
	Error      string   `json:"error,omitempty"`
	LoginURL   string   `json:"loginUrl,omitempty"`
	AuthStatus string   `json:"authStatus,omitempty"`
	AuthDetail string   `json:"authDetail,omitempty"`
	UpdatedAt  int64    `json:"updatedAt,omitempty"`
}

type setupEntry struct {
	task SetupTask
	cmd  *exec.Cmd
}

var (
	setupMu    sync.Mutex
	setupTasks = make(map[string]*setupEntry)
	urlRegex   = regexp.MustCompile(`https?://[^\s"'<>]+`)

	openBrowserFunc = defaultOpenBrowser
)

func defaultOpenBrowser(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "linux":
		return exec.Command("xdg-open", u).Start()
	default:
		return errors.New("unsupported OS for opening browser")
	}
}

func taskKey(target, kind string) string {
	return kind + ":" + target
}

func installCommandForTarget(target string) ([]string, error) {
	switch target {
	case "opencode":
		return []string{"bash", "-lc", "curl -fsSL https://opencode.ai/install | bash"}, nil
	case "qoder":
		return []string{"bash", "-lc", "curl -fsSL https://qoder.com.cn/install | bash"}, nil
	case "claude":
		return []string{"bash", "-lc", "curl -fsSL https://claude.ai/install.sh | bash"}, nil
	case "codebuddy":
		npmPath, ok := findBinary("npm")
		if !ok {
			return nil, errors.New("未检测到 npm，请先安装 Node.js 后重试")
		}
		return []string{npmPath, "install", "-g", "@tencent-ai/codebuddy-code"}, nil
	case "codex":
		if npmPath, ok := findBinary("npm"); ok {
			return []string{npmPath, "install", "-g", "@openai/codex"}, nil
		}
		if runtime.GOOS == "darwin" {
			if brewPath, ok := findBinary("brew"); ok {
				return []string{brewPath, "install", "--cask", "codex"}, nil
			}
		}
		return nil, errors.New("未检测到 npm 或 brew，请先安装 Node.js 后重试")
	case "magpie":
		return []string{"bash", "-lc", "curl -fsSL https://usemagpie.ai/install.sh | sh"}, nil
	default:
		return nil, fmt.Errorf("不支持一键安装 %q", target)
	}
}

func loginCommandForTarget(kind string) ([]string, error) {
	installed, binPath := DetectHarness(kind)
	if !installed {
		return nil, fmt.Errorf("%s 尚未安装", kind)
	}
	switch kind {
	case "claude":
		return []string{binPath, "auth", "login"}, nil
	case "codex":
		return []string{binPath, "login"}, nil
	case "codebuddy":
		return []string{binPath, "login"}, nil
	case "qoder":
		return []string{binPath, "login"}, nil
	default:
		return nil, fmt.Errorf("%s 无需或不支持自动登录", kind)
	}
}

func cloneTask(t SetupTask) SetupTask {
	t.Logs = append([]string(nil), t.Logs...)
	return t
}

func appendTaskLog(key, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	// Redact potential bearer/api tokens in output.
	if strings.Contains(strings.ToLower(line), "sk-") || strings.Contains(strings.ToLower(line), "bearer ") {
		line = "[redacted]"
	}
	setupMu.Lock()
	defer setupMu.Unlock()
	ent, ok := setupTasks[key]
	if !ok {
		return
	}
	ent.task.Logs = append(ent.task.Logs, line)
	if len(ent.task.Logs) > 40 {
		ent.task.Logs = ent.task.Logs[len(ent.task.Logs)-40:]
	}
	ent.task.UpdatedAt = time.Now().UnixMilli()
}

// StartInstall launches a whitelisted one-click installation for target.
func StartInstall(target string) (SetupTask, error) {
	argv, err := installCommandForTarget(target)
	if err != nil {
		return SetupTask{}, err
	}
	key := taskKey(target, "install")

	setupMu.Lock()
	if ent, ok := setupTasks[key]; ok && ent.task.State == "running" {
		res := cloneTask(ent.task)
		setupMu.Unlock()
		return res, nil
	}
	ent := &setupEntry{
		task: SetupTask{
			Target:    target,
			Kind:      "install",
			State:     "running",
			Logs:      []string{"正在启动安装..."},
			UpdatedAt: time.Now().UnixMilli(),
		},
	}
	setupTasks[key] = ent
	setupMu.Unlock()

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = augmentedEnv(nil)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return setTaskError(key, err), err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return setTaskError(key, err), err
	}
	if err := cmd.Start(); err != nil {
		return setTaskError(key, err), err
	}

	setupMu.Lock()
	ent.cmd = cmd
	setupMu.Unlock()

	go func() {
		var wg sync.WaitGroup
		readPipe := func(r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			for sc.Scan() {
				appendTaskLog(key, sc.Text())
			}
		}
		wg.Add(2)
		go readPipe(stdout)
		go readPipe(stderr)
		wg.Wait()

		waitErr := cmd.Wait()
		if target == "magpie" {
			InvalidateMagpieCache()
		}

		setupMu.Lock()
		defer setupMu.Unlock()
		cur, ok := setupTasks[key]
		if !ok {
			return
		}
		cur.cmd = nil
		cur.task.UpdatedAt = time.Now().UnixMilli()
		if waitErr != nil {
			cur.task.State = "error"
			cur.task.Error = waitErr.Error()
			return
		}
		if target == "magpie" && !MagpieInstalled() {
			cur.task.State = "error"
			cur.task.Error = "安装脚本已退出，但未检测到 magpie 命令"
			return
		}
		if target != "magpie" {
			if okInstalled, _ := DetectHarness(target); !okInstalled {
				cur.task.State = "error"
				cur.task.Error = fmt.Sprintf("安装完成但未检测到 %s 命令，请检查 PATH", target)
				return
			}
		}
		cur.task.State = "done"
		cur.task.Logs = append(cur.task.Logs, "安装完成")
	}()

	return cloneTask(ent.task), nil
}

// GetInstallStatus returns the current installation task state for target.
func GetInstallStatus(target string) SetupTask {
	key := taskKey(target, "install")
	setupMu.Lock()
	defer setupMu.Unlock()
	if ent, ok := setupTasks[key]; ok {
		return cloneTask(ent.task)
	}
	return SetupTask{Target: target, Kind: "install", State: "idle"}
}

// StartLogin launches the CLI login flow for kind and attempts to open the browser URL.
func StartLogin(kind string) (SetupTask, error) {
	argv, err := loginCommandForTarget(kind)
	if err != nil {
		return SetupTask{}, err
	}
	key := taskKey(kind, "login")

	setupMu.Lock()
	if ent, ok := setupTasks[key]; ok && ent.task.State == "running" {
		res := cloneTask(ent.task)
		setupMu.Unlock()
		return res, nil
	}
	ent := &setupEntry{
		task: SetupTask{
			Target:     kind,
			Kind:       "login",
			State:      "running",
			AuthStatus: "needs_login",
			Logs:       []string{"正在拉起登录流程..."},
			UpdatedAt:  time.Now().UnixMilli(),
		},
	}
	setupTasks[key] = ent
	setupMu.Unlock()

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = augmentedEnv(nil)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return setTaskError(key, err), err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return setTaskError(key, err), err
	}
	if err := cmd.Start(); err != nil {
		return setTaskError(key, err), err
	}

	setupMu.Lock()
	ent.cmd = cmd
	setupMu.Unlock()

	go func() {
		var wg sync.WaitGroup
		var openedOnce sync.Once
		readPipe := func(r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			for sc.Scan() {
				line := sc.Text()
				appendTaskLog(key, line)
				if u := urlRegex.FindString(line); u != "" {
					u = strings.TrimRight(u, ".,;)")
					setupMu.Lock()
					if cur, ok := setupTasks[key]; ok && cur.task.LoginURL == "" {
						cur.task.LoginURL = u
						cur.task.UpdatedAt = time.Now().UnixMilli()
					}
					setupMu.Unlock()
					openedOnce.Do(func() {
						_ = openBrowserFunc(u)
					})
				}
			}
		}
		wg.Add(2)
		go readPipe(stdout)
		go readPipe(stderr)
		wg.Wait()

		waitErr := cmd.Wait()
		status, detail := DetectHarnessAuth(context.Background(), kind)

		setupMu.Lock()
		defer setupMu.Unlock()
		cur, ok := setupTasks[key]
		if !ok {
			return
		}
		cur.cmd = nil
		cur.task.AuthStatus = status
		cur.task.AuthDetail = detail
		cur.task.UpdatedAt = time.Now().UnixMilli()
		if cur.task.State == "done" {
			return
		}
		if status == "ready" {
			cur.task.State = "done"
			cur.task.Logs = append(cur.task.Logs, "登录已验证完成")
			return
		}
		if waitErr != nil {
			cur.task.State = "error"
			cur.task.Error = waitErr.Error()
			return
		}
		cur.task.State = "error"
		cur.task.Error = "登录流程已退出，但尚未检测到有效登录凭证"
	}()

	return cloneTask(ent.task), nil
}

// GetLoginStatus verifies real-time auth status for kind and updates the login task accordingly.
func GetLoginStatus(ctx context.Context, kind string) SetupTask {
	key := taskKey(kind, "login")
	authStatus, authDetail := DetectHarnessAuth(ctx, kind)

	setupMu.Lock()
	defer setupMu.Unlock()
	ent, ok := setupTasks[key]
	if !ok {
		state := "idle"
		if authStatus == "ready" {
			state = "done"
		}
		return SetupTask{
			Target:     kind,
			Kind:       "login",
			State:      state,
			AuthStatus: authStatus,
			AuthDetail: authDetail,
		}
	}
	ent.task.AuthStatus = authStatus
	ent.task.AuthDetail = authDetail
	if authStatus == "ready" && ent.task.State == "running" {
		ent.task.State = "done"
		ent.task.UpdatedAt = time.Now().UnixMilli()
		ent.task.Logs = append(ent.task.Logs, "登录已验证完成")
		if ent.cmd != nil && ent.cmd.Process != nil {
			_ = syscall.Kill(-ent.cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	return cloneTask(ent.task)
}

func setTaskError(key string, err error) SetupTask {
	setupMu.Lock()
	defer setupMu.Unlock()
	if ent, ok := setupTasks[key]; ok {
		ent.task.State = "error"
		ent.task.Error = err.Error()
		ent.task.UpdatedAt = time.Now().UnixMilli()
		return cloneTask(ent.task)
	}
	return SetupTask{State: "error", Error: err.Error()}
}
