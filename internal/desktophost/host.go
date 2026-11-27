package desktophost

import (
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
	"bot-hub/internal/protocol"
	"bot-hub/internal/tunnel"
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Prefs mirrors desktop.json persisted in the data directory.
type Prefs struct {
	Mode                string   `json:"mode,omitempty"` // "" (ask) | local | remote
	Link                string   `json:"link,omitempty"`
	RemoteToken         string   `json:"remoteToken,omitempty"`
	NodeID              string   `json:"nodeId,omitempty"`
	NodeName            string   `json:"nodeName,omitempty"`
	AllowNode           bool     `json:"allowNode,omitempty"`
	NodePerms           []string `json:"nodePerms"`
	TailscaleBound      bool     `json:"tailscaleBound,omitempty"`
	TailscaleControlURL string   `json:"tailscaleControlUrl,omitempty"`
}

// Snapshot represents the live state of the desktop host exported to UI shells.
type Snapshot struct {
	Mode                string   `json:"mode"`
	Link                string   `json:"link"`
	DataDir             string   `json:"dataDir"`
	ActiveURL           string   `json:"activeUrl"`
	ActiveToken         string   `json:"activeToken"`
	Status              string   `json:"status"`
	StatusState         string   `json:"statusState"`
	StatusError         string   `json:"statusError"`
	AllowNode           bool     `json:"allowNode"`
	NodeConnected       bool     `json:"nodeConnected"`
	NodeStatus          string   `json:"nodeStatus"`
	NodeError           string   `json:"nodeError"`
	NodeID              string   `json:"nodeId"`
	NodeName            string   `json:"nodeName"`
	NodePerms           []string `json:"nodePerms"`
	CanFailover         bool     `json:"canFailover"`
	CanFailback         bool     `json:"canFailback"`
	TailscaleBound      bool     `json:"tailscaleBound"`
	TailscaleControlURL string   `json:"tailscaleControlUrl"`
	DaemonProtocol      int      `json:"daemonProtocol,omitempty"`
}

// RemoteTarget describes a parsed remote connection link.
type RemoteTarget struct {
	Kind    string // "tailcat" | "tailscale" | "http"
	Link    string // cleaned link without #token=
	Token   string
	Addr    string // host:port for tailscale dialing
	BaseURL string // for Kind == "http" or "tailscale"
}

// Hooks connects the desktop host engine to a UI shell (Wails or Swift daemon).
type Hooks struct {
	ServeLocal func(ctx context.Context, dir string) error
	OnChange   func(Snapshot)
	OnModeURL  func(url string)
	OnReset    func()
	OnOpenURL  func(url string)
}

// Host manages local/remote mode lifecycle, local node agent, failover/failback, and Tailscale.
type Host struct {
	dir   string
	hooks Hooks

	mu          sync.Mutex
	prefs       Prefs
	activeURL   string
	activeToken string
	status      string
	statusState string
	statusError string
	stop        func()
	nodeAgent   *node.Agent
	tsNode      *tunnel.TailscaleNode
}

// New creates a Host rooted at dir with optional lifecycle hooks.
func New(dir string, hooks Hooks) *Host {
	return &Host{dir: dir, hooks: hooks}
}

// Dir returns the host data directory.
func (h *Host) Dir() string {
	return h.dir
}

// Prefs returns a copy of the current preferences.
func (h *Host) Prefs() Prefs {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := h.prefs
	cp.NodePerms = append([]string(nil), h.prefs.NodePerms...)
	return cp
}

// Snapshot returns a thread-safe snapshot of the current desktop host state.
func (h *Host) Snapshot() Snapshot {
	h.mu.Lock()
	p := h.prefs
	perms := append([]string(nil), h.prefs.NodePerms...)
	activeURL := h.activeURL
	activeToken := h.activeToken
	status := h.status
	statusState := h.statusState
	statusError := h.statusError
	agent := h.nodeAgent
	h.mu.Unlock()

	nodeConnected := false
	nodeStatus := "disabled"
	nodeError := ""
	if agent != nil {
		st := agent.Status()
		nodeConnected = st.Connected
		nodeStatus = st.Status
		nodeError = st.Error
		if st.NodeID != "" {
			p.NodeID = st.NodeID
		}
		if st.Name != "" {
			p.NodeName = st.Name
		}
		if st.Permissions != nil {
			perms = append([]string(nil), st.Permissions...)
		}
	} else if p.AllowNode {
		nodeStatus = "idle"
	}

	linkTrimmed := strings.TrimSpace(p.Link)
	return Snapshot{
		Mode:                p.Mode,
		Link:                p.Link,
		DataDir:             h.dir,
		ActiveURL:           activeURL,
		ActiveToken:         activeToken,
		Status:              status,
		StatusState:         statusState,
		StatusError:         statusError,
		AllowNode:           p.AllowNode,
		NodeConnected:       nodeConnected,
		NodeStatus:          nodeStatus,
		NodeError:           nodeError,
		NodeID:              p.NodeID,
		NodeName:            p.NodeName,
		NodePerms:           perms,
		CanFailover:         p.Mode == "remote" && linkTrimmed != "",
		CanFailback:         p.Mode == "local" && linkTrimmed != "",
		TailscaleBound:      p.TailscaleBound,
		TailscaleControlURL: p.TailscaleControlURL,
		DaemonProtocol:      protocol.CurrentProtocol,
	}
}

func (h *Host) notify() {
	if h.hooks.OnChange != nil {
		h.hooks.OnChange(h.Snapshot())
	}
}

// Load reads desktop.json and fills default node identity/permissions.
func (h *Host) Load() {
	h.mu.Lock()
	if raw, err := os.ReadFile(filepath.Join(h.dir, "desktop.json")); err == nil {
		_ = json.Unmarshal(raw, &h.prefs)
	}
	defID, defName := node.DefaultIdentity()
	if strings.TrimSpace(h.prefs.NodeID) == "" {
		h.prefs.NodeID = defID
	}
	if strings.TrimSpace(h.prefs.NodeName) == "" {
		h.prefs.NodeName = defName
	}
	if h.prefs.NodePerms == nil {
		h.prefs.NodePerms = []string{node.PermFiles, node.PermExec, node.PermDesktop}
	} else {
		h.prefs.NodePerms = node.ValidPerms(h.prefs.NodePerms)
	}
	h.mu.Unlock()
}

// Save writes desktop.json with 0600 permissions.
func (h *Host) Save() {
	h.mu.Lock()
	raw, _ := json.MarshalIndent(h.prefs, "", "  ")
	h.mu.Unlock()
	_ = os.WriteFile(filepath.Join(h.dir, "desktop.json"), raw, 0o600)
}

// WarmupCaches pre-warms harness auth caches while on the setup screen.
func (h *Host) WarmupCaches() {
	wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, id := range []string{"opencode", "codebuddy", "qoder", "codex", "claude", "grok", "trae", "pi", "dsh", "agy"} {
		if ok, _ := harness.DetectHarness(id); ok {
			k := id
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = harness.DetectHarnessAuthCached(wctx, k)
			}()
		}
	}
	wg.Wait()
}

// SetLocalToken updates the active token when running in local mode and notifies UI shells.
func (h *Host) SetLocalToken(tok string) {
	tok = strings.TrimSpace(tok)
	h.mu.Lock()
	changed := h.prefs.Mode == "local" && h.activeToken != tok
	if changed {
		h.activeToken = tok
	}
	h.mu.Unlock()
	if changed {
		h.notify()
	}
}

// SetLocalNode updates local node settings and reconfigures the live node agent if running.
func (h *Host) SetLocalNode(enabled *bool, name *string, perms []string) map[string]any {
	h.mu.Lock()
	if enabled != nil {
		h.prefs.AllowNode = *enabled
	}
	if name != nil {
		if n := strings.TrimSpace(*name); n != "" {
			h.prefs.NodeName = n
		}
	}
	if perms != nil {
		h.prefs.NodePerms = node.ValidPerms(perms)
	}
	allow := h.prefs.AllowNode
	nodeID := h.prefs.NodeID
	nodeName := h.prefs.NodeName
	curPerms := append([]string(nil), h.prefs.NodePerms...)
	agent := h.nodeAgent
	h.mu.Unlock()

	h.Save()
	defer h.notify()
	if agent != nil {
		agent.Configure(allow, nodeName, curPerms)
		st := agent.Status()
		return map[string]any{
			"available":   true,
			"enabled":     st.Enabled,
			"connected":   st.Connected,
			"status":      st.Status,
			"error":       st.Error,
			"nodeId":      st.NodeID,
			"name":        st.Name,
			"permissions": st.Permissions,
		}
	}
	return map[string]any{
		"available":   false,
		"enabled":     allow,
		"nodeId":      nodeID,
		"name":        nodeName,
		"permissions": curPerms,
	}
}

// HandleLocalNode serves GET/POST /api/local-node on the remote proxy.
func (h *Host) HandleLocalNode(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h2, _, err := net.SplitHostPort(host); err == nil {
		host = h2
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		http.Error(w, `{"error":"invalid host"}`, http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			http.Error(w, `{"error":"cross-origin request rejected"}`, http.StatusForbidden)
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		h.mu.Lock()
		agent := h.nodeAgent
		h.mu.Unlock()
		if agent == nil {
			WriteJSON(w, map[string]any{"available": false})
			return
		}
		st := agent.Status()
		WriteJSON(w, map[string]any{
			"available":   true,
			"enabled":     st.Enabled,
			"connected":   st.Connected,
			"status":      st.Status,
			"error":       st.Error,
			"nodeId":      st.NodeID,
			"name":        st.Name,
			"permissions": st.Permissions,
		})

	case http.MethodPost:
		if tok := h.remoteToken(); tok != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if got == "" {
				if c, err := r.Cookie("bot_hub_token"); err == nil {
					got = c.Value
				}
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(tok)) != 1 {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		var req struct {
			Enabled     *bool    `json:"enabled"`
			Name        *string  `json:"name"`
			Permissions []string `json:"permissions"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		WriteJSON(w, h.SetLocalNode(req.Enabled, req.Name, req.Permissions))

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// Halt stops the active local server or remote proxy.
func (h *Host) Halt() {
	h.mu.Lock()
	stop := h.stop
	h.stop = nil
	h.activeURL = ""
	h.activeToken = ""
	h.mu.Unlock()
	if stop != nil {
		stop()
	}
	h.notify()
}

// Reset stops any active mode and returns the host to mode selection.
func (h *Host) Reset() {
	h.Halt()
	h.mu.Lock()
	h.prefs.Mode = ""
	h.activeURL = ""
	h.activeToken = ""
	h.status = ""
	h.statusState = ""
	h.statusError = ""
	h.mu.Unlock()
	h.Save()
	h.notify()
	if h.hooks.OnReset != nil {
		h.hooks.OnReset()
	}
}

func (h *Host) setStatus(s string) {
	h.mu.Lock()
	h.status = s
	h.statusState = ""
	h.statusError = ""
	h.mu.Unlock()
	h.notify()
}

func (h *Host) setStatusState(state, errMsg string) {
	h.mu.Lock()
	h.status = ""
	h.statusState = state
	h.statusError = errMsg
	h.mu.Unlock()
	h.notify()
}

// WriteJSON writes v as JSON with HTTP 200.
func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
