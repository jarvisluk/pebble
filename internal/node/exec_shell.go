package node

import (
	"bytes"
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
	"time"
)

func (a *Agent) execShell(ctx context.Context, raw json.RawMessage) Result {
	var args struct {
		Command             string            `json:"command"`
		Cwd                 string            `json:"cwd"`
		Env                 map[string]string `json:"env"`
		DesktopSlot         string            `json:"desktopSlot"`
		BrowserProfileBotID string            `json:"browserProfileBotId"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Command) == "" {
		return Result{OK: false, Error: "command is required"}
	}
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", args.Command)
	} else {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
			if _, err := os.Stat(shell); err != nil {
				shell = "/bin/sh"
			}
		}
		c = exec.CommandContext(ctx, shell, "-lc", args.Command)
	}
	if cwd := expandHome(args.Cwd, a.homeDir); cwd != "" {
		c.Dir = cwd
	}
	extraEnvMap := make(map[string]string)
	if runtime.GOOS == "linux" {
		var dispEnvs []string
		if slot := strings.TrimSpace(args.DesktopSlot); slot != "" {
			if eng, err := a.desktopPool().ForSlot(slot); err == nil {
				dispEnvs = eng.Env()
			}
		} else {
			dispEnvs = a.desktopEngine().Env()
		}
		for _, kv := range dispEnvs {
			if k, v, ok := strings.Cut(kv, "="); ok && v != "" {
				extraEnvMap[k] = v
			}
		}
	}
	for k, v := range args.Env {
		if v == "" {
			continue
		}
		if (strings.HasPrefix(k, "AGENT_BROWSER_") && k != "AGENT_BROWSER_EXECUTABLE_PATH") || k == "PEBBLE_BOT_ID" || k == "BOT_HUB_BOT_ID" {
			extraEnvMap[k] = v
		}
	}
	if bid := strings.TrimSpace(args.BrowserProfileBotID); bid != "" && validNodeIDRe.MatchString(bid) && a.homeDir != "" {
		if extraEnvMap["AGENT_BROWSER_PROFILE"] == "" {
			dir := filepath.Join(a.homeDir, ".pebble", "browsers", bid)
			if _, err := os.Stat(dir); err != nil {
				legacyDir := filepath.Join(a.homeDir, ".bot-hub", "browsers", bid)
				if info, lerr := os.Stat(legacyDir); lerr == nil && info.IsDir() {
					dir = legacyDir
				}
			}
			if os.MkdirAll(dir, 0o700) == nil {
				extraEnvMap["AGENT_BROWSER_PROFILE"] = dir
			}
		}
	}
	if len(extraEnvMap) > 0 {
		env := append([]string(nil), os.Environ()...)
		for k, v := range extraEnvMap {
			env = setShellEnvVar(env, k, v)
		}
		c.Env = env
	}
	buf := &limitedBuffer{limit: maxExecOutputBytes}
	c.Stdout = buf
	c.Stderr = buf
	err := c.Run()
	out := buf.String()
	if buf.truncated {
		out += "\n[output truncated at 512KB]"
	}
	if err != nil {
		exitCode := 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
			return Result{OK: true, ExitCode: exitCode, Output: out}
		}
		return Result{OK: false, ExitCode: exitCode, Output: out, Error: err.Error()}
	}
	return Result{OK: true, ExitCode: 0, Output: out}
}

func (a *Agent) fileRead(raw json.RawMessage) Result {
	var args struct {
		Path     string `json:"path"`
		MaxBytes int    `json:"maxBytes"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Path) == "" {
		return Result{OK: false, Error: "path is required"}
	}
	p := expandHome(args.Path, a.homeDir)
	capBytes := args.MaxBytes
	if capBytes <= 0 {
		capBytes = defaultFileReadCap
	}
	if capBytes > maxFileReadBytes {
		capBytes = maxFileReadBytes
	}
	f, err := os.Open(p)
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(capBytes)+1))
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	truncated := len(data) > capBytes
	if truncated {
		data = data[:capBytes]
	}
	out := string(data)
	if truncated {
		out += fmt.Sprintf("\n[truncated at %d bytes]", capBytes)
	}
	return Result{OK: true, Output: out}
}

func (a *Agent) fileWrite(raw json.RawMessage) Result {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Path) == "" {
		return Result{OK: false, Error: "path is required"}
	}
	p := expandHome(args.Path, a.homeDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	if args.Append {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		n, werr := f.WriteString(args.Content)
		cerr := f.Close()
		if werr != nil {
			return Result{OK: false, Error: werr.Error()}
		}
		if cerr != nil {
			return Result{OK: false, Error: cerr.Error()}
		}
		return Result{OK: true, Output: fmt.Sprintf("appended %d bytes to %s", n, p)}
	}
	if err := os.WriteFile(p, []byte(args.Content), 0o644); err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	return Result{OK: true, Output: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), p)}
}

func (a *Agent) fileList(raw json.RawMessage) Result {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Path) == "" {
		return Result{OK: false, Error: "path is required"}
	}
	p := expandHome(args.Path, a.homeDir)
	entries, err := os.ReadDir(p)
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	type item struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Size    int64  `json:"size,omitempty"`
		ModTime string `json:"modTime,omitempty"`
	}
	limit := min(len(entries), 500)
	out := make([]item, 0, limit)
	for _, ent := range entries[:limit] {
		kind := "file"
		if ent.IsDir() {
			kind = "dir"
		} else if ent.Type()&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		it := item{Name: ent.Name(), Type: kind}
		if info, err := ent.Info(); err == nil {
			it.Size = info.Size()
			it.ModTime = info.ModTime().UTC().Format(time.RFC3339)
		}
		out = append(out, it)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	text := string(b)
	if len(entries) > limit {
		text += fmt.Sprintf("\n[showing first %d of %d entries]", limit, len(entries))
	}
	return Result{OK: true, Output: text}
}

func setShellEnvVar(env []string, key, val string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

func expandHome(p, home string) string {
	p = strings.TrimSpace(p)
	if p == "" || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rem := b.limit - b.buf.Len(); rem > 0 {
		if len(p) > rem {
			_, _ = b.buf.Write(p[:rem])
			b.truncated = true
		} else {
			_, _ = b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
