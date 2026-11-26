package harness

import (
	"bot-hub/internal/config"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// OpenCodeHarness keeps one long-lived `opencode serve` (or a configured
// external server) and drives sessions over its HTTP API, receiving output
// from a single shared SSE event stream.
type OpenCodeHarness struct {
	binary      string
	external    string
	httpClient  *http.Client
	modelsCache ttlCache[[]string]

	mu       sync.Mutex
	baseURL  string
	password string
	cmd      *exec.Cmd
	stopEv   context.CancelFunc
	closed   bool

	subsMu sync.Mutex
	subs   map[string]chan ocEvent

	applied      sync.Map // sessionID -> model last applied
	appliedAgent sync.Map // sessionID -> agent last applied
}

type ocEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

var ocEventTypes = []string{
	"session.text.delta",
	"session.text.ended",
	"session.tool.input.started",
	"session.step.ended",
	"session.execution.succeeded",
	"session.execution.failed",
	"session.execution.interrupted",
	"permission.asked",
	"permission.replied",
	"form.created",
	"form.replied",
	"form.cancelled",
}

func NewOpenCodeHarness(externalURL string) *OpenCodeHarness {
	binary := os.Getenv("OPENCODE_BIN")
	if binary == "" {
		binary = "opencode"
	}
	return &OpenCodeHarness{
		binary:      binary,
		external:    strings.TrimRight(externalURL, "/"),
		httpClient:  &http.Client{Timeout: 60 * time.Second},
		modelsCache: ttlCache[[]string]{ttl: time.Minute},
		subs:        make(map[string]chan ocEvent),
	}
}

func (h *OpenCodeHarness) apiClient(base, pw string) *ocAPI {
	return &ocAPI{base: base, pw: pw, client: h.httpClient}
}

func (h *OpenCodeHarness) Kind() string { return "opencode" }

func (h *OpenCodeHarness) Label() string { return "OpenCode" }

func (h *OpenCodeHarness) PermissionCapabilities() PermissionCapabilities {
	return PermissionCapabilities{
		Modes: []string{
			config.PermissionModeAuto,
			config.PermissionModeEdit,
			config.PermissionModeAsk,
			config.PermissionModePlan,
		},
		InteractiveApproval: true,
		SupportsAllowAlways: true,
	}
}

func (h *OpenCodeHarness) resolveBinary() (string, bool) {
	return resolveBinaryFor("OPENCODE_BIN", "opencode", h.binary)
}

// ensure returns a ready server (started on first use) with its event stream connected.
func (h *OpenCodeHarness) ensure(ctx context.Context) (string, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return "", "", errors.New("opencode harness closed")
	}
	if h.baseURL != "" {
		return h.baseURL, h.password, nil
	}

	base, pw := h.external, os.Getenv("OPENCODE_SERVER_PASSWORD")
	var cmd *exec.Cmd
	if base == "" {
		port, err := freePort()
		if err != nil {
			return "", "", err
		}
		pw = randHex(16)
		base = fmt.Sprintf("http://127.0.0.1:%d", port)
		bin, ok := h.resolveBinary()
		if !ok {
			bin = h.binary
		}
		cmd = exec.Command(bin, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
		cmd.Env = augmentedEnv(map[string]string{"OPENCODE_SERVER_PASSWORD": pw})
		// Own process group so Close also stops the MCP servers it spawns.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return "", "", fmt.Errorf("start opencode serve: %w", err)
		}
		go func() {
			err := cmd.Wait()
			h.mu.Lock()
			if h.cmd == cmd {
				log.Printf("[opencode] server exited: %v", err)
				h.resetLocked()
			}
			h.mu.Unlock()
		}()
	}

	if err := waitHealthy(ctx, base, pw); err != nil {
		if cmd != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return "", "", err
	}

	evCtx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	go h.eventLoop(evCtx, base, pw, ready)
	select {
	case <-ready:
	case <-time.After(15 * time.Second):
		cancel()
		if cmd != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return "", "", errors.New("opencode event stream did not connect")
	}

	h.baseURL, h.password, h.cmd, h.stopEv = base, pw, cmd, cancel
	if cmd != nil {
		log.Printf("[opencode] server ready at %s (pid %d)", base, cmd.Process.Pid)
	}
	return base, pw, nil
}

func (h *OpenCodeHarness) resetLocked() {
	if h.stopEv != nil {
		h.stopEv()
	}
	h.baseURL, h.password, h.cmd, h.stopEv = "", "", nil, nil
}

func (h *OpenCodeHarness) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	if h.cmd != nil {
		_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGTERM)
	}
	h.resetLocked()
}

// Reload terminates the idle opencode serve process (keeping the harness open)
// so subsequent turns restart it with updated CLI/provider configs.
func (h *OpenCodeHarness) Reload() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	if h.cmd != nil && h.cmd.Process != nil {
		_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGTERM)
	}
	h.resetLocked()
	h.modelsCache.invalidate()
	h.applied.Range(func(k, _ any) bool {
		h.applied.Delete(k)
		return true
	})
	h.appliedAgent.Range(func(k, _ any) bool {
		h.appliedAgent.Delete(k)
		return true
	})
}

func (h *OpenCodeHarness) eventLoop(ctx context.Context, base, pw string, ready chan struct{}) {
	q := url.Values{}
	for _, t := range ocEventTypes {
		q.Add("types", t)
	}
	var once sync.Once
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/event?"+q.Encode(), nil)
		req.SetBasicAuth(serverUser(), pw)
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
			for sc.Scan() {
				line := sc.Text()
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				var evt ocEvent
				if json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &evt) != nil {
					continue
				}
				if evt.Type == "server.connected" {
					once.Do(func() { close(ready) })
					continue
				}
				h.dispatch(evt)
			}
		}
		if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (h *OpenCodeHarness) dispatch(evt ocEvent) {
	var d struct {
		SessionID string `json:"sessionID"`
		Form      *struct {
			SessionID string `json:"sessionID"`
		} `json:"form"`
	}
	if json.Unmarshal(evt.Data, &d) != nil {
		return
	}
	sid := d.SessionID
	if sid == "" && d.Form != nil {
		sid = d.Form.SessionID
	}
	if sid == "" {
		return
	}
	h.subsMu.Lock()
	ch := h.subs[sid]
	h.subsMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- evt:
	default:
		log.Printf("[opencode] dropped %s for %s (consumer lagging)", evt.Type, sid)
	}
}

func (h *OpenCodeHarness) subscribe(sid string) chan ocEvent {
	ch := make(chan ocEvent, 1024)
	h.subsMu.Lock()
	h.subs[sid] = ch
	h.subsMu.Unlock()
	return ch
}

func (h *OpenCodeHarness) unsubscribe(sid string) {
	h.subsMu.Lock()
	delete(h.subs, sid)
	h.subsMu.Unlock()
}
