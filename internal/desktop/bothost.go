package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

type rpcRequest struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ResolveBotHost locates a compiled opencode-remote-desktop `bot-host` binary if installed.
func ResolveBotHost() string {
	if p := strings.TrimSpace(os.Getenv("BOT_HOST_BIN")); p != "" {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}

	exeName := "bot-host"
	if runtime.GOOS == "windows" {
		exeName = "bot-host.exe"
	}
	if exe, err := os.Executable(); err == nil {
		for _, cand := range []string{
			filepath.Join(filepath.Dir(exe), exeName),
			filepath.Join(filepath.Dir(exe), "..", "Resources", "bin", exeName),
		} {
			if info, err := os.Stat(cand); err == nil && !info.IsDir() {
				return cand
			}
		}
	}
	if p, err := exec.LookPath("bot-host"); err == nil {
		return p
	}

	asset := botHostAssetName()
	if asset == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates := []string{
			filepath.Join(home, ".cache", "opencode-bot", "bin", asset),
			filepath.Join(home, "codespace", "opencode-bot", "dist", "bin", asset),
		}
		for _, cand := range candidates {
			if info, err := os.Stat(cand); err == nil && !info.IsDir() {
				return cand
			}
		}
	}
	return ""
}

func botHostAssetName() string {
	plat := map[string]string{
		"darwin":  "darwin",
		"linux":   "linux",
		"windows": "win32",
	}[runtime.GOOS]
	arch := map[string]string{
		"arm64": "arm64",
		"amd64": "x64",
	}[runtime.GOARCH]
	if plat == "" || arch == "" {
		return ""
	}
	ext := ""
	if plat == "win32" {
		ext = ".exe"
	}
	return fmt.Sprintf("bot-host-%s-%s%s", plat, arch, ext)
}

// botHostClient manages a lazy stdio JSON-RPC connection to `bot-host --stdio`.
type botHostClient struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	pending map[int64]chan rpcResponse
	nextID  atomic.Int64
	closed  bool
	envFn   func() []string
}

func newBotHostClient() *botHostClient {
	return &botHostClient{pending: make(map[int64]chan rpcResponse)}
}

func (c *botHostClient) Available() bool {
	return ResolveBotHost() != ""
}

func (c *botHostClient) Close() {
	c.mu.Lock()
	c.closed = true
	cmd := c.cmd
	stdin := c.stdin
	c.cmd = nil
	c.stdin = nil
	c.failAllLocked("desktop host closed")
	c.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

func (c *botHostClient) ensureStarted() (io.WriteCloser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("desktop host closed")
	}
	if c.stdin != nil && c.cmd != nil && c.cmd.ProcessState == nil {
		return c.stdin, nil
	}

	bin := ResolveBotHost()
	if bin == "" {
		return nil, errors.New("bot-host unavailable")
	}
	cmd := exec.Command(bin, "--stdio")
	if c.envFn != nil {
		if extra := c.envFn(); len(extra) > 0 {
			cmd.Env = mergeEnv(os.Environ(), extra)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	c.cmd = cmd
	c.stdin = stdin
	go c.readLoop(cmd, stdout)
	return stdin, nil
}

func (c *botHostClient) readLoop(cmd *exec.Cmd, stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var resp rpcResponse
		if json.Unmarshal([]byte(line), &resp) != nil || resp.ID == 0 {
			continue
		}
		c.mu.Lock()
		ch := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
	_ = cmd.Wait()

	c.mu.Lock()
	if c.cmd == cmd {
		c.cmd = nil
		if c.stdin != nil {
			_ = c.stdin.Close()
			c.stdin = nil
		}
	}
	c.failAllLocked("bot-host exited")
	c.mu.Unlock()
}

func (c *botHostClient) failAllLocked(msg string) {
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- rpcResponse{
			ID: id,
			Error: &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{Code: -32000, Message: msg},
		}
	}
}

func (c *botHostClient) Request(ctx context.Context, method string, params any, out any) error {
	stdin, err := c.ensureStarted()
	if err != nil {
		return err
	}
	id := c.nextID.Add(1)
	ch := make(chan rpcResponse, 1)

	c.mu.Lock()
	if c.closed || c.stdin != stdin {
		c.mu.Unlock()
		return errors.New("bot-host disconnected")
	}
	c.pending[id] = ch
	payload, err := json.Marshal(rpcRequest{ID: id, Method: method, Params: params})
	if err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}
	payload = append(payload, '\n')
	_, werr := stdin.Write(payload)
	if werr != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return werr
	}
	c.mu.Unlock()

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return errors.New(resp.Error.Message)
		}
		if out != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}
