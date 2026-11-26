package config

import (
	"fmt"
	"strings"
	"time"
)

const (
	RoutineDeliveryReceipt = "receipt"
	RoutineDeliveryNotify  = "notify"
	RoutineDeliveryNone    = "none"

	RoutineFailoverAuto        = "auto"
	RoutineFailoverPrimaryOnly = "primary_only"
	RoutineFailoverManual      = "manual"

	PermissionModeAuto = "auto"
	PermissionModeEdit = "edit"
	PermissionModeAsk  = "ask"
	PermissionModePlan = "plan"
)

type ScheduleConfig struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name"`
	EveryMinutes   int    `json:"everyMinutes,omitempty"`
	DailyAt        string `json:"dailyAt,omitempty"` // "HH:MM" local time
	Cron           string `json:"cron,omitempty"`    // 5-field, 6-field (with seconds), or @every/@daily
	WebhookTrigger bool   `json:"webhookTrigger,omitempty"`
	WebhookSecret  string `json:"webhookSecret,omitempty"`
	WebhookURL     string `json:"webhookUrl,omitempty"`
	Prompt         string `json:"prompt"`
	Model          string `json:"model,omitempty"`
	MainDelivery   string `json:"mainDelivery,omitempty"`   // "receipt" (default) | "notify" | "none"
	FailoverPolicy string `json:"failoverPolicy,omitempty"` // "auto" (default) | "primary_only" | "manual"
	PermissionMode string `json:"permissionMode,omitempty"` // "" (follow bot) | "auto" | "plan"
	Enabled        *bool  `json:"enabled,omitempty"`
}

func (s ScheduleConfig) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// EffectiveID returns the routine's ID, or a normalized slug derived from Name.
func (s ScheduleConfig) EffectiveID() string {
	if id := strings.TrimSpace(s.ID); id != "" {
		return id
	}
	return slugifyRoutineID(s.Name)
}

// EffectiveMainDelivery returns the normalized main-session delivery mode ("receipt", "notify", or "none").
func (s ScheduleConfig) EffectiveMainDelivery() string {
	switch strings.ToLower(strings.TrimSpace(s.MainDelivery)) {
	case "", RoutineDeliveryReceipt:
		return RoutineDeliveryReceipt
	case RoutineDeliveryNotify:
		return RoutineDeliveryNotify
	case RoutineDeliveryNone:
		return RoutineDeliveryNone
	default:
		return strings.ToLower(strings.TrimSpace(s.MainDelivery))
	}
}

// EffectiveFailoverPolicy returns the normalized failover policy ("auto", "primary_only", or "manual").
func (s ScheduleConfig) EffectiveFailoverPolicy() string {
	switch strings.ToLower(strings.TrimSpace(s.FailoverPolicy)) {
	case "", RoutineFailoverAuto:
		return RoutineFailoverAuto
	case RoutineFailoverPrimaryOnly:
		return RoutineFailoverPrimaryOnly
	case RoutineFailoverManual:
		return RoutineFailoverManual
	default:
		return strings.ToLower(strings.TrimSpace(s.FailoverPolicy))
	}
}

type BotConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Color       string `json:"color,omitempty"`
	Workspace   string `json:"workspace"`
	Harness     string `json:"harness,omitempty"` // default: "opencode"
	Model       string `json:"model,omitempty"`
	// ModelInherited is true when Model came from the hub default rather
	// than the bot's own config.
	ModelInherited  bool              `json:"-"`
	Agent           string            `json:"agent,omitempty"`
	AutoApprove     *bool             `json:"autoApprove,omitempty"`    // legacy fallback: false -> "ask"
	PermissionMode  string            `json:"permissionMode,omitempty"` // "auto" (default) | "edit" | "ask" | "plan"
	TimeoutSec      int               `json:"timeoutSec,omitempty"`     // default: 300
	PrivateSessions bool              `json:"privateSessions,omitempty"`
	DesktopTarget         string            `json:"desktopTarget,omitempty"`
	BrowserMode           string            `json:"browserMode,omitempty"` // "" | "auto" | "isolated" | "attach"
	CoordinatorCanApprove *bool             `json:"coordinatorCanApprove,omitempty"`
	Env                   map[string]string `json:"env,omitempty"`
	Schedules             []ScheduleConfig  `json:"schedules,omitempty"`
}

// CanCoordinatorApprove reports whether the bot may approve/reject its own worker tasks' permission prompts.
func (b BotConfig) CanCoordinatorApprove(hubCfg *HubConfig) bool {
	if b.CoordinatorCanApprove != nil {
		return *b.CoordinatorCanApprove
	}
	if hubCfg != nil && hubCfg.CoordinatorCanApprove != nil {
		return *hubCfg.CoordinatorCanApprove
	}
	return false
}

// EffectivePermissionMode returns the normalized permission mode ("auto", "edit", "ask", or "plan").
func (b BotConfig) EffectivePermissionMode() string {
	if pm, err := NormalizePermissionMode(b.PermissionMode); err == nil && pm != "" {
		return pm
	}
	if b.AutoApprove != nil && !*b.AutoApprove {
		return PermissionModeAsk
	}
	return PermissionModeAuto
}

func (b BotConfig) ShouldAutoApprove() bool {
	return b.EffectivePermissionMode() == PermissionModeAuto
}

type HTTPConfig struct {
	Enabled       *bool  `json:"enabled,omitempty"` // default: true
	Host          string `json:"host,omitempty"`    // default: "127.0.0.1"
	Port          int    `json:"port,omitempty"`    // default: 3789
	WebhookSecret string `json:"webhookSecret,omitempty"`
	// APIToken guards the Web UI and /api/*. Required when Host is not loopback.
	APIToken string `json:"apiToken,omitempty"`
	// Tailcat exposes the same server through a Tailcat tunnel (no Tailscale account needed).
	Tailcat *TailcatConfig `json:"tailcat,omitempty"`
	// Tailscale exposes the same server inside the user's Tailscale tailnet via tsnet.
	Tailscale *TailscaleConfig `json:"tailscale,omitempty"`
}

type TailcatConfig struct {
	Enabled    bool   `json:"enabled"`
	DERPMapURL string `json:"derpMapURL,omitempty"` // optional self-hosted DERP map (https)
}

type TailscaleConfig struct {
	Enabled    bool   `json:"enabled"`
	Hostname   string `json:"hostname,omitempty"`
	ControlURL string `json:"controlUrl,omitempty"`
	AuthKey    string `json:"authKey,omitempty"`
}

func (h HTTPConfig) TailcatEnabled() bool { return h.Tailcat != nil && h.Tailcat.Enabled }

func (h HTTPConfig) TailscaleEnabled() bool { return h.Tailscale != nil && h.Tailscale.Enabled }

// IsLoopback reports whether the server only listens on a loopback address.
func (h HTTPConfig) IsLoopback() bool {
	switch h.Host {
	case "", "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func (h HTTPConfig) IsEnabled() bool {
	return h.Enabled == nil || *h.Enabled
}

func (h HTTPConfig) BaseURL() string {
	host := h.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := h.Port
	if port == 0 {
		port = 3789
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

type DesktopConfig struct {
	Mode                  string `json:"mode,omitempty"`                  // "auto" (default) | "native" | "virtual"
	AutoInstall           bool   `json:"autoInstall,omitempty"`           // auto install headless deps on startup
	Resolution            string `json:"resolution,omitempty"`            // default "1440x900"
	IdleFreezeMinutes     int    `json:"idleFreezeMinutes,omitempty"`     // default 5; <0 disables auto-freeze
	IdleStopMinutes       int    `json:"idleStopMinutes,omitempty"`       // default 30; <0 disables auto-stop
	StopOnlyWhenNoWindows *bool  `json:"stopOnlyWhenNoWindows,omitempty"` // default true
}

func (d DesktopConfig) EffectiveMode() string {
	switch strings.ToLower(strings.TrimSpace(d.Mode)) {
	case "native", "virtual":
		return strings.ToLower(strings.TrimSpace(d.Mode))
	default:
		return "auto"
	}
}

func (d DesktopConfig) EffectiveResolution() string {
	res := strings.TrimSpace(d.Resolution)
	if res == "" {
		return "1440x900"
	}
	return res
}

func (d DesktopConfig) EffectiveIdleFreezeMinutes() int {
	if d.IdleFreezeMinutes < 0 {
		return 0
	}
	if d.IdleFreezeMinutes == 0 {
		return 5
	}
	return d.IdleFreezeMinutes
}

func (d DesktopConfig) EffectiveIdleStopMinutes() int {
	if d.IdleStopMinutes < 0 {
		return 0
	}
	if d.IdleStopMinutes == 0 {
		return 30
	}
	return d.IdleStopMinutes
}

func (d DesktopConfig) ShouldStopOnlyWhenNoWindows() bool {
	return d.StopOnlyWhenNoWindows == nil || *d.StopOnlyWhenNoWindows
}

type SyncGitConfig struct {
	Enabled         bool   `json:"enabled"`
	RepoURL         string `json:"repoUrl,omitempty"`
	Branch          string `json:"branch,omitempty"`          // default: "main"
	IntervalMinutes int    `json:"intervalMinutes,omitempty"` // default: 10
}

func (g SyncGitConfig) EffectiveBranch() string {
	if b := strings.TrimSpace(g.Branch); b != "" {
		return b
	}
	return "main"
}

func (g SyncGitConfig) EffectiveIntervalMinutes() int {
	if g.IntervalMinutes <= 0 {
		return 10
	}
	return g.IntervalMinutes
}

type SyncWebDAVConfig struct {
	Enabled  bool   `json:"enabled"`
	Prefix   string `json:"prefix,omitempty"`   // default: "/webdav"
	ReadOnly *bool  `json:"readOnly,omitempty"` // default: true
}

func (w SyncWebDAVConfig) EffectivePrefix() string {
	p := strings.TrimSpace(w.Prefix)
	if p == "" {
		return "/webdav"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/webdav"
	}
	return p
}

func (w SyncWebDAVConfig) IsReadOnly() bool {
	return w.ReadOnly == nil || *w.ReadOnly
}

type SyncPeerConfig struct {
	ID       string `json:"id,omitempty"`
	Label    string `json:"label,omitempty"`
	URL      string `json:"url"`
	Token    string `json:"token,omitempty"`
	Priority int    `json:"priority,omitempty"` // smaller number = higher priority (e.g. 10 primary, 20 standby)
}

type SyncConfig struct {
	Role               string            `json:"role,omitempty"`     // "primary" (default) | "standby"
	Priority           int               `json:"priority,omitempty"` // default: primary=10, standby=20
	PeerURL            string            `json:"peerUrl,omitempty"`
	PeerToken          string            `json:"peerToken,omitempty"`
	Peers              []SyncPeerConfig  `json:"peers,omitempty"`
	FailoverTimeoutSec int               `json:"failoverTimeoutSec,omitempty"` // default: 90
	Git                *SyncGitConfig    `json:"git,omitempty"`
	WebDAV             *SyncWebDAVConfig `json:"webdav,omitempty"`
}

func (s *SyncConfig) EffectivePriority() int {
	if s != nil && s.Priority > 0 {
		return s.Priority
	}
	if s.EffectiveRole() == "standby" {
		return 20
	}
	return 10
}

// HigherPriorityPeers returns all configured peers whose priority is strictly
// higher (smaller number) than the local host's effective priority.
func (s *SyncConfig) HigherPriorityPeers() []SyncPeerConfig {
	if s == nil {
		return nil
	}
	selfPrio := s.EffectivePriority()
	var out []SyncPeerConfig
	seen := make(map[string]bool)
	for _, p := range s.Peers {
		u := strings.TrimRight(strings.TrimSpace(p.URL), "/")
		if u == "" || seen[u] {
			continue
		}
		prio := p.Priority
		if prio <= 0 {
			prio = 10
		}
		if prio < selfPrio {
			p.URL = u
			p.Priority = prio
			seen[u] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 && s.EffectiveRole() == "standby" {
		if u := strings.TrimRight(strings.TrimSpace(s.PeerURL), "/"); u != "" {
			out = append(out, SyncPeerConfig{
				ID:       "primary",
				URL:      u,
				Token:    s.PeerToken,
				Priority: 10,
			})
		}
	}
	return out
}

func (s *SyncConfig) EffectiveFailoverTimeout() time.Duration {
	if s == nil || s.FailoverTimeoutSec <= 0 {
		return 90 * time.Second
	}
	return time.Duration(s.FailoverTimeoutSec) * time.Second
}

func (s *SyncConfig) EffectiveRole() string {
	if s == nil {
		return "primary"
	}
	switch strings.ToLower(strings.TrimSpace(s.Role)) {
	case "standby":
		return "standby"
	default:
		return "primary"
	}
}

func (s *SyncConfig) GitEnabled() bool {
	return s != nil && s.Git != nil && s.Git.Enabled
}

func (s *SyncConfig) WebDAVEnabled() bool {
	return s != nil && s.WebDAV != nil && s.WebDAV.Enabled
}

func (s *SyncConfig) WebDAVReadOnly() bool {
	if s == nil || s.WebDAV == nil {
		return true
	}
	return s.WebDAV.IsReadOnly()
}

type APNsConfig struct {
	KeyPath    string `json:"keyPath,omitempty"`
	KeyPEM     string `json:"keyPem,omitempty"`
	KeyID      string `json:"keyId,omitempty"`
	TeamID     string `json:"teamId,omitempty"`
	BundleID   string `json:"bundleId,omitempty"`
	DefaultEnv string `json:"defaultEnv,omitempty"` // "production" | "sandbox"
}

type PushConfig struct {
	Enabled  *bool      `json:"enabled,omitempty"` // default: true
	RelayURL string     `json:"relayUrl,omitempty"`
	APNs     APNsConfig `json:"apns,omitempty"`
}

func (p PushConfig) IsEnabled() bool {
	return p.Enabled == nil || *p.Enabled
}

type HubConfig struct {
	ProjectRoot string `json:"-"`
	ConfigPath  string `json:"-"`
	// DefaultHarness selects the default backend harness when creating a bot.
	DefaultHarness        string `json:"defaultHarness,omitempty"`
	DefaultPermissionMode string `json:"defaultPermissionMode,omitempty"`
	CoordinatorCanApprove *bool  `json:"coordinatorCanApprove,omitempty"`
	// DefaultModel applies to opencode and pi bots (they share provider/model
	// IDs); DefaultModels overrides it per harness.
	DefaultModel        string            `json:"defaultModel,omitempty"`
	DefaultModels       map[string]string `json:"defaultModels,omitempty"`
	OnboardingCompleted bool              `json:"onboardingCompleted,omitempty"`
	DesktopHarness      string            `json:"desktopHarness,omitempty"`
	DesktopModel        string            `json:"desktopModel,omitempty"`
	Desktop             DesktopConfig     `json:"desktop,omitempty"`
	// MagpieWebURL is where `magpie web` is reachable from other devices;
	// clients away from the host open it instead of the local app.
	MagpieWebURL      string      `json:"magpieWebUrl,omitempty"`
	OpenCodeServerURL string      `json:"opencodeServerUrl,omitempty"`
	SharedDir         string      `json:"sharedDir,omitempty"`
	StateFile         string      `json:"stateFile,omitempty"`
	MessagesDir       string      `json:"messagesDir,omitempty"`
	HTTP              HTTPConfig  `json:"http,omitempty"`
	Sync              *SyncConfig `json:"sync,omitempty"`
	Push              PushConfig  `json:"push,omitempty"`
	Bots              []BotConfig `json:"bots"`
}

// EffectiveBrowserMode returns "isolated", "attach", or "auto".
func (b BotConfig) EffectiveBrowserMode() string {
	switch strings.ToLower(strings.TrimSpace(b.BrowserMode)) {
	case "isolated":
		return "isolated"
	case "attach":
		return "attach"
	default:
		return "auto"
	}
}

// EffectiveDefaultPermissionMode returns the hub-wide default permission mode (falling back to "auto").
func (c *HubConfig) EffectiveDefaultPermissionMode() string {
	if c != nil {
		if pm, err := NormalizePermissionMode(c.DefaultPermissionMode); err == nil && pm != "" {
			return pm
		}
	}
	return PermissionModeAuto
}

// EffectiveDefaultHarness returns the configured default harness, falling back to "opencode".
func (c *HubConfig) EffectiveDefaultHarness() string {
	if c != nil {
		if h := strings.TrimSpace(c.DefaultHarness); h != "" {
			return h
		}
	}
	return "opencode"
}

// DefaultModelFor returns the model a bot of this harness inherits; empty
// means the harness's own default.
func (c *HubConfig) DefaultModelFor(harness string) string {
	if m := c.DefaultModels[harness]; m != "" {
		return expandEnv(m)
	}
	if c.DefaultModel == "" {
		return ""
	}
	// A magpie model works in every harness magpie wires up (claude takes
	// the bare gateway ID); a bare ID/alias works across all harnesses;
	// provider/model only fits opencode and pi.
	if strings.HasPrefix(c.DefaultModel, "magpie/") {
		if harness == "claude" {
			return strings.TrimPrefix(c.DefaultModel, "magpie/")
		}
		return c.DefaultModel
	}
	if !strings.Contains(c.DefaultModel, "/") {
		return c.DefaultModel
	}
	switch harness {
	case "", "opencode", "pi":
		return c.DefaultModel
	}
	return ""
}
