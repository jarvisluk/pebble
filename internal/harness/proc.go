package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bot-hub/internal/config"
)

// lineProc is a long-lived child process speaking newline-delimited JSON on
// stdin/stdout. Lines arrive on lines, which is closed when the process exits.
type lineProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan []byte
	done   chan struct{}
	stderr *tailWriter
	wmu    sync.Mutex
}

func startLineProc(cmd *exec.Cmd) (*lineProc, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &lineProc{cmd: cmd, stdin: stdin, lines: make(chan []byte, 1024), done: make(chan struct{}), stderr: &tailWriter{}}
	cmd.Stderr = p.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	go func() {
		r := bufio.NewReaderSize(stdout, 64*1024)
		for {
			// Split on LF only: JSON strings may contain U+2028/U+2029.
			line, err := r.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				p.lines <- line
			}
			if err != nil {
				break
			}
		}
		_ = cmd.Wait()
		close(p.lines)
		close(p.done)
	}()
	return p, nil
}

func (p *lineProc) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

func (p *lineProc) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *lineProc) kill() {
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
}

// exitDetail describes why the process ended, from its stderr tail.
func (p *lineProc) exitDetail() string {
	if s := strings.TrimSpace(p.stderr.String()); s != "" {
		return s
	}
	return "process exited"
}

// tailWriter keeps the last 4 KiB written to it.
type tailWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (w *tailWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, b...)
	if n := len(w.buf); n > 4096 {
		w.buf = w.buf[n-4096:]
	}
	return len(b), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

// splitModel splits "provider/model..." at the first slash.
func splitModel(m string) (provider, id string) {
	provider, id, _ = strings.Cut(m, "/")
	return provider, id
}

func newUUID() string {
	h := randHex(16)
	// version 4, variant 10
	b := []byte(h)
	b[12] = '4'
	b[16] = "89ab"[int(b[16])%4]
	return string(b[0:8]) + "-" + string(b[8:12]) + "-" + string(b[12:16]) + "-" + string(b[16:20]) + "-" + string(b[20:32])
}

func procEnv(cmd *exec.Cmd, opts RunOptions) []string {
	env := cmd.Environ()
	for k, v := range opts.Bot.Env {
		env = append(env, k+"="+v)
	}
	for k, v := range opts.ExtraEnv {
		env = append(env, k+"="+v)
	}
	return env
}

func augmentedEnv(extra map[string]string) []string {
	env := os.Environ()
	pathVal := os.Getenv("PATH")
	extraPaths := defaultExtraBinDirs()
	if len(extraPaths) > 0 {
		joined := strings.Join(extraPaths, string(os.PathListSeparator))
		if pathVal != "" {
			pathVal = pathVal + string(os.PathListSeparator) + joined
		} else {
			pathVal = joined
		}
		env = setEnvVar(env, "PATH", pathVal)
	}
	for k, v := range extra {
		env = setEnvVar(env, k, v)
	}
	return env
}

func setEnvVar(env []string, key, val string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

// mergedEnv builds an environment from augmentedEnv, applying botEnv first and
// extraEnv overrides second.
func mergedEnv(botEnv, extraEnv map[string]string) []string {
	env := augmentedEnv(nil)
	for k, v := range botEnv {
		env = setEnvVar(env, k, v)
	}
	for k, v := range extraEnv {
		env = setEnvVar(env, k, v)
	}
	return env
}

func withBotTimeout(ctx context.Context, timeoutSec int) (context.Context, context.CancelFunc) {
	return withTurnTimeout(ctx, timeoutSec, RunOptions{})
}

type imageAttachment struct {
	MimeType string
	DataB64  string
}

func readImageAttachments(atts []Attachment) []imageAttachment {
	var out []imageAttachment
	for _, att := range atts {
		if att.Kind != "image" || att.Path == "" {
			continue
		}
		raw, err := os.ReadFile(att.Path)
		if err != nil || len(raw) == 0 {
			continue
		}
		mt := att.MimeType
		if mt == "" {
			mt = "image/png"
		}
		out = append(out, imageAttachment{
			MimeType: mt,
			DataB64:  base64.StdEncoding.EncodeToString(raw),
		})
	}
	return out
}

var mcpConfigMu sync.Mutex

// upsertMCPConfig merges the pebble MCP server entry into path while preserving
// any user-configured servers. It skips rewriting when the file is unchanged and
// writes atomically with 0600 permissions because MCPEnv may hold API tokens.
func upsertMCPConfig(path string, opts RunOptions) error {
	if len(opts.MCPCommand) == 0 {
		return nil
	}
	mcpConfigMu.Lock()
	defer mcpConfigMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	doc := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &doc)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	delete(servers, config.LegacyMCPServerName)
	server := map[string]any{
		"command": opts.MCPCommand[0],
		"args":    opts.MCPCommand[1:],
	}
	if len(opts.MCPEnv) > 0 {
		server["env"] = opts.MCPEnv
	}
	servers[config.MCPServerName] = server
	doc["mcpServers"] = servers
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	want := append(b, '\n')
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, want) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, want, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

const (
	// Idle reaping is disabled by default so long-lived Prompt Caches are never evicted.
	defaultProcIdleTTL      = 0
	defaultProcReapInterval = 5 * time.Minute
)

func configuredProcIdleTTL() time.Duration {
	if raw := config.LookupEnv("PEBBLE_HARNESS_IDLE_MINUTES", "BOT_HUB_HARNESS_IDLE_MINUTES"); raw != "" {
		if mins, err := strconv.Atoi(raw); err == nil && mins > 0 {
			return time.Duration(mins) * time.Minute
		}
	}
	return defaultProcIdleTTL
}

type poolProc interface {
	comparable
	alive() bool
	kill()
}

type procEntry[P poolProc] struct {
	proc     P
	inUse    int
	lastUsed time.Time
}

// procPool manages long-lived per-bot CLI processes. Idle reaping is off by
// default to preserve Prompt Caches, and can be enabled via PEBBLE_HARNESS_IDLE_MINUTES.
type procPool[P poolProc] struct {
	mu           sync.Mutex
	entries      map[string]*procEntry[P]
	idleTTL      *time.Duration // nil = read configuredProcIdleTTL()
	reapInterval time.Duration  // 0 = defaultProcReapInterval
	stopReaper   chan struct{}
	closed       bool
}

func (p *procPool[P]) effectiveIdleTTL() time.Duration {
	if p.idleTTL != nil {
		return *p.idleTTL
	}
	return configuredProcIdleTTL()
}

func (p *procPool[P]) effectiveReapInterval() time.Duration {
	if p.reapInterval > 0 {
		return p.reapInterval
	}
	return defaultProcReapInterval
}

func (p *procPool[P]) ensureReaperLocked() {
	if p.stopReaper != nil || p.closed {
		return
	}
	ttl := p.effectiveIdleTTL()
	if ttl <= 0 {
		return
	}
	interval := p.effectiveReapInterval()
	stop := make(chan struct{})
	p.stopReaper = stop
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				p.reapIdle(now)
			}
		}
	}()
}

func (p *procPool[P]) acquire(botID string, reusable func(P) bool, start func() (P, error)) (P, func(), error) {
	var old P
	var hasOld bool

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		var zero P
		return zero, nil, errors.New("harness closed")
	}
	if e := p.entries[botID]; e != nil {
		if e.proc.alive() && (reusable == nil || reusable(e.proc)) {
			e.inUse++
			proc := e.proc
			p.mu.Unlock()
			return proc, p.releaseFunc(botID, e), nil
		}
		old = e.proc
		hasOld = true
		delete(p.entries, botID)
	}
	p.mu.Unlock()

	if hasOld {
		old.kill()
	}

	proc, err := start()
	if err != nil {
		var zero P
		return zero, nil, err
	}

	var replaced P
	var hasReplaced bool

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		proc.kill()
		var zero P
		return zero, nil, errors.New("harness closed")
	}
	if p.entries == nil {
		p.entries = make(map[string]*procEntry[P])
	}
	if prev := p.entries[botID]; prev != nil {
		replaced = prev.proc
		hasReplaced = true
	}
	entry := &procEntry[P]{
		proc:     proc,
		inUse:    1,
		lastUsed: time.Now(),
	}
	p.entries[botID] = entry
	p.ensureReaperLocked()
	p.mu.Unlock()

	if hasReplaced {
		replaced.kill()
	}
	return proc, p.releaseFunc(botID, entry), nil
}

func (p *procPool[P]) releaseFunc(botID string, entry *procEntry[P]) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			if cur := p.entries[botID]; cur == entry {
				if cur.inUse > 0 {
					cur.inUse--
				}
				cur.lastUsed = time.Now()
			}
			p.mu.Unlock()
		})
	}
}

func (p *procPool[P]) reapIdle(now time.Time) {
	ttl := p.effectiveIdleTTL()
	if ttl <= 0 {
		return
	}
	var toKill []P
	p.mu.Lock()
	for id, e := range p.entries {
		if e.inUse == 0 && now.Sub(e.lastUsed) >= ttl {
			toKill = append(toKill, e.proc)
			delete(p.entries, id)
		}
	}
	p.mu.Unlock()
	for _, proc := range toKill {
		proc.kill()
	}
}

func (p *procPool[P]) drop(botID string, target P) {
	p.mu.Lock()
	if e := p.entries[botID]; e != nil && e.proc == target {
		delete(p.entries, botID)
	}
	p.mu.Unlock()
	target.kill()
}

func (p *procPool[P]) clear() {
	var toKill []P
	p.mu.Lock()
	for id, e := range p.entries {
		toKill = append(toKill, e.proc)
		delete(p.entries, id)
	}
	p.mu.Unlock()
	for _, proc := range toKill {
		proc.kill()
	}
}

func (p *procPool[P]) close() {
	var toKill []P
	p.mu.Lock()
	p.closed = true
	stop := p.stopReaper
	p.stopReaper = nil
	for id, e := range p.entries {
		toKill = append(toKill, e.proc)
		delete(p.entries, id)
	}
	p.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	for _, proc := range toKill {
		proc.kill()
	}
}
