package harness

import (
	"bot-hub/internal/config"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (h *OpenCodeHarness) Models(ctx context.Context) ([]string, error) {
	var fetchErr error
	ids := h.modelsCache.get(func() ([]string, bool) {
		h.mu.Lock()
		serverActive := h.external != "" || h.baseURL != ""
		h.mu.Unlock()

		if !serverActive {
			bin, ok := h.resolveBinary()
			if !ok {
				return nil, false
			}
			fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(fctx, bin, "models")
			cmd.Env = augmentedEnv(nil)
			if out, err := cmd.Output(); err == nil {
				if list := parseSlashModelLines(string(out)); len(list) > 0 {
					return list, true
				}
			}
		}

		base, pw, err := h.ensure(ctx)
		if err != nil {
			if serverActive {
				fetchErr = err
			}
			return nil, false
		}
		var out struct {
			Data []struct {
				ID         string `json:"id"`
				ProviderID string `json:"providerID"`
			} `json:"data"`
		}
		if _, err := h.apiClient(base, pw).do(ctx, http.MethodGet, "/api/model", nil, &out); err != nil {
			if serverActive {
				fetchErr = err
			}
			return nil, false
		}
		list := make([]string, 0, len(out.Data))
		for _, m := range out.Data {
			list = append(list, m.ProviderID+"/"+m.ID)
		}
		return list, len(list) > 0
	})
	if len(ids) == 0 && fetchErr != nil {
		return nil, fetchErr
	}
	return append([]string(nil), ids...), nil
}

func decodeOpenCodeDataList[T any](raw json.RawMessage) ([]T, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var wrapped struct {
		Data []T `json:"data"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Data != nil {
		return wrapped.Data, true
	}
	var arr []T
	if json.Unmarshal(raw, &arr) == nil {
		return arr, true
	}
	return nil, false
}

func (h *OpenCodeHarness) ListCommands(ctx context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "Compact current session context", Source: "harness"},
		{Name: "init", Description: "Initialize project AGENTS.md", Source: "harness"},
		{Name: "review", Description: "Review current code changes", Source: "harness"},
		{Name: "undo", Description: "Undo last turn changes", Source: "harness"},
		{Name: "redo", Description: "Redo undone changes", Source: "harness"},
		{Name: "new", Description: "Start a new session", Source: "harness"},
		{Name: "share", Description: "Share current session", Source: "harness"},
		{Name: "unshare", Description: "Unshare current session", Source: "harness"},
		{Name: "models", Description: "View or switch model", Source: "harness"},
		{Name: "agents", Description: "View or switch agent", Source: "harness"},
		{Name: "mcp", Description: "Show MCP server status", Source: "harness"},
		{Name: "sessions", Description: "List previous sessions", Source: "harness"},
		{Name: "export", Description: "Export current session", Source: "harness"},
		{Name: "help", Description: "Show available commands", Source: "harness"},
	}
	h.mu.Lock()
	ready := h.baseURL != "" || h.external != ""
	h.mu.Unlock()

	var remoteCmds, remoteSkills []SlashCommand
	if ready {
		if base, pw, err := h.ensure(ctx); err == nil {
			api := h.apiClient(base, pw)
			q := ""
			if bot.Workspace != "" {
				q = "?location[directory]=" + url.QueryEscape(bot.Workspace)
			}
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				var raw json.RawMessage
				type ocCmd struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				}
				if _, err := api.do(ctx, http.MethodGet, "/api/command"+q, nil, &raw); err == nil {
					if items, ok := decodeOpenCodeDataList[ocCmd](raw); ok {
						for _, c := range items {
							remoteCmds = append(remoteCmds, SlashCommand{
								Name:        c.Name,
								Description: c.Description,
								Source:      "harness",
							})
						}
					}
				}
			}()
			go func() {
				defer wg.Done()
				var raw json.RawMessage
				type ocSkill struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				}
				if _, err := api.do(ctx, http.MethodGet, "/api/skill"+q, nil, &raw); err == nil {
					if items, ok := decodeOpenCodeDataList[ocSkill](raw); ok {
						for _, s := range items {
							remoteSkills = append(remoteSkills, SlashCommand{
								Name:        s.Name,
								Description: s.Description,
								Source:      "skill",
							})
						}
					}
				}
			}()
			wg.Wait()
		}
	}

	var localCmds, localSkills []SlashCommand
	if bot.Workspace != "" {
		localCmds = append(localCmds, scanMarkdownCommands(filepath.Join(bot.Workspace, ".opencode", "commands"), "harness")...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(bot.Workspace, ".opencode", "skills"))...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(bot.Workspace, ".claude", "skills"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		localCmds = append(localCmds, scanMarkdownCommands(filepath.Join(home, ".config", "opencode", "commands"), "harness")...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(home, ".config", "opencode", "skills"))...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(home, ".claude", "skills"))...)
	}
	localSkills = append(localSkills, scanSharedSkills(bot.Workspace)...)

	return mergeSlashCommands(builtins, remoteCmds, remoteSkills, localCmds, localSkills), nil
}

func (h *OpenCodeHarness) ListAgents(ctx context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	h.mu.Lock()
	base, pw := h.baseURL, h.password
	h.mu.Unlock()

	var remote []AgentInfo
	if base != "" {
		q := ""
		if bot.Workspace != "" {
			q = "?location[directory]=" + url.QueryEscape(bot.Workspace)
		}
		var raw json.RawMessage
		type ocAgent struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Hidden      bool   `json:"hidden"`
		}
		if _, err := h.apiClient(base, pw).do(ctx, http.MethodGet, "/api/agent"+q, nil, &raw); err == nil {
			if items, ok := decodeOpenCodeDataList[ocAgent](raw); ok {
				for _, a := range items {
					if a.Hidden || a.Name == "" {
						continue
					}
					remote = append(remote, AgentInfo{Name: a.Name, Description: a.Description})
				}
			}
		}
	}

	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".opencode", "agents"))...)
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".claude", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".config", "opencode", "agents"))...)
		local = append(local, scanAgentsDir(filepath.Join(home, ".claude", "agents"))...)
	}

	return mergeAgents(remote, local), nil
}

type ocAPI struct {
	base, pw string
	client   *http.Client
}

func (a *ocAPI) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, rd)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(serverUser(), a.pw)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := a.client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Message != "" {
			return resp.StatusCode, errors.New(e.Message)
		}
		return resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return resp.StatusCode, json.Unmarshal(raw, out)
	}
	return resp.StatusCode, nil
}

func waitHealthy(ctx context.Context, base, pw string) error {
	api := &ocAPI{base: base, pw: pw}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if code, err := api.do(ctx, http.MethodGet, "/api/info", nil, nil); err == nil && code == http.StatusOK {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("opencode server at %s not reachable", base)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func serverUser() string {
	if u := os.Getenv("OPENCODE_SERVER_USERNAME"); u != "" {
		return u
	}
	return "opencode"
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type ttlCache[T any] struct {
	mu      sync.Mutex
	val     T
	fetched time.Time
	ttl     time.Duration
}

func (c *ttlCache[T]) get(load func() (T, bool)) T {
	c.mu.Lock()
	if !c.fetched.IsZero() && time.Since(c.fetched) < c.ttl {
		v := c.val
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()

	fresh, ok := load()
	c.mu.Lock()
	defer c.mu.Unlock()
	if ok {
		c.val = fresh
		c.fetched = time.Now()
		return fresh
	}
	if !c.fetched.IsZero() {
		return c.val
	}
	return fresh
}

func (c *ttlCache[T]) invalidate() {
	c.mu.Lock()
	c.fetched = time.Time{}
	c.mu.Unlock()
}

func parseSlashModelLines(raw string) []string {
	var parsed []string
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && strings.Contains(f[0], "/") && !strings.Contains(f[0], "://") {
			parsed = append(parsed, f[0])
		}
	}
	return parsed
}
