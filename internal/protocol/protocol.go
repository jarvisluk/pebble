// Package protocol defines the single source of truth for protocol version
// levels, capability identifiers, header names, and upgrade errors shared across
// the Hub HTTP API, remote device nodes, desktop host daemon, and clients.
package protocol

import (
	"errors"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	// CurrentProtocol is the active integer protocol generation.
	// Peers or clients omitting protocolVersion default to 1 (legacy).
	CurrentProtocol = 2

	// MinClientProtocol is the minimum client protocol still accepted by the hub.
	MinClientProtocol = 1

	// MinNodeProtocol is the minimum remote node protocol still accepted by the hub.
	MinNodeProtocol = 1
)

// AppVersion can be overridden at build time via:
// -ldflags "-X bot-hub/internal/protocol.AppVersion=..."
var AppVersion = "0.1.0"

const (
	HeaderProtocol = "X-Pebble-Protocol"
	HeaderClient   = "X-Pebble-Client"
	HeaderVersion  = "X-Pebble-Version"
)

const (
	ErrCodeClientUpgradeRequired = "client_upgrade_required"
	ErrCodeNodeUpgradeRequired   = "node_upgrade_required"
	ErrCodeCapabilityUnsupported = "capability_unsupported"
)

// Server capabilities advertised in GET /api/bots ("server") and GET /api/meta.
const (
	CapBotDesktopBinding   = "bot.desktop_binding"
	CapBotBrowserIsolation = "bot.browser_isolation"
	CapDesktopVirtualSlots = "desktop.virtual_slots"
	CapDesktopPermissions  = "desktop.permissions"
	CapDesktopWSH264       = "desktop.ws_h264"
	CapDesktopWSHEVC       = "desktop.ws_hevc"
	CapDesktopFramePolling = "desktop.frame_polling"
	CapStageFiles          = "stage.files"
	CapQuestions           = "questions"
	CapRoutines            = "routines"
	CapUpdateCheck         = "update.check"
)

// Remote node capabilities advertised in POST /api/nodes/poll.
const (
	CapNodeExecEnv            = "node.exec_env"
	CapNodeBrowserProfile     = "node.browser_profile"
	CapNodeDesktopPermissions = "node.desktop_permissions"
	CapNodeDesktopPower       = "node.desktop_power"
	CapNodeVirtualSlots       = "node.virtual_slots"
)

// Local desktop host-daemon capabilities advertised in the stdout handshake.
const (
	CapDaemonLocalNode = "daemon.local_node"
	CapDaemonFailover  = "daemon.failover"
	CapDaemonTailscale = "daemon.tailscale"
)

// ServerInfo describes the hub's protocol generation and enabled capabilities.
type ServerInfo struct {
	ProtocolVersion   int      `json:"protocolVersion"`
	MinClientProtocol int      `json:"minClientProtocol"`
	MinNodeProtocol   int      `json:"minNodeProtocol"`
	Version           string   `json:"version,omitempty"`
	Capabilities      []string `json:"capabilities,omitempty"`
}

// ClientMeta captures optional client identification headers sent on API requests.
type ClientMeta struct {
	ProtocolVersion int    `json:"protocolVersion"`
	Client          string `json:"client,omitempty"`
	Version         string `json:"version,omitempty"`
}

// NormalizeProtocol maps missing or non-positive protocol versions to 1 (legacy).
func NormalizeProtocol(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}

// ParseClientMeta extracts client protocol metadata from request headers or query params.
func ParseClientMeta(r *http.Request) ClientMeta {
	meta := ClientMeta{ProtocolVersion: 1}
	if r == nil {
		return meta
	}
	rawProto := strings.TrimSpace(r.Header.Get(HeaderProtocol))
	if rawProto == "" && r.URL != nil {
		rawProto = strings.TrimSpace(r.URL.Query().Get("proto"))
	}
	if rawProto != "" {
		if n, err := strconv.Atoi(rawProto); err == nil {
			meta.ProtocolVersion = NormalizeProtocol(n)
		}
	}
	meta.Client = strings.TrimSpace(r.Header.Get(HeaderClient))
	meta.Version = strings.TrimSpace(r.Header.Get(HeaderVersion))
	return meta
}

// NormalizeCapabilities trims, deduplicates, and sorts capability strings deterministically.
func NormalizeCapabilities(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		c := strings.TrimSpace(raw)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// HasCapability reports whether cap is present in caps.
func HasCapability(caps []string, capName string) bool {
	want := strings.TrimSpace(capName)
	if want == "" {
		return false
	}
	return slices.Contains(caps, want)
}

// ServerCapabilities builds the deterministic list of capabilities advertised by the hub.
func ServerCapabilities(virtualSupported bool) []string {
	caps := []string{
		CapBotBrowserIsolation,
		CapBotDesktopBinding,
		CapDesktopFramePolling,
		CapDesktopPermissions,
		CapDesktopWSH264,
		CapDesktopWSHEVC,
		CapQuestions,
		CapRoutines,
		CapStageFiles,
		CapUpdateCheck,
	}
	if virtualSupported {
		caps = append(caps, CapDesktopVirtualSlots)
	}
	return NormalizeCapabilities(caps)
}

// DaemonCapabilities builds the deterministic list of host-daemon capabilities.
func DaemonCapabilities() []string {
	return NormalizeCapabilities([]string{
		CapDaemonFailover,
		CapDaemonLocalNode,
		CapDaemonTailscale,
	})
}

// BuildServerInfo creates a ServerInfo snapshot for API responses.
func BuildServerInfo(virtualSupported bool) ServerInfo {
	return ServerInfo{
		ProtocolVersion:   CurrentProtocol,
		MinClientProtocol: MinClientProtocol,
		MinNodeProtocol:   MinNodeProtocol,
		Version:           AppVersion,
		Capabilities:      ServerCapabilities(virtualSupported),
	}
}

// UpgradeError carries structured capability/protocol mismatch context alongside a readable message.
type UpgradeError struct {
	Code               string
	Message            string
	RequiredCapability string
	MinProtocol        int
}

func (e *UpgradeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// WriteErrorPayload formats an error into the standard JSON response map,
// preserving "error" for legacy clients while exposing structured compatibility fields.
func WriteErrorPayload(err error) map[string]any {
	if err == nil {
		return map[string]any{"error": ""}
	}
	var ue *UpgradeError
	if errors.As(err, &ue) && ue != nil {
		out := map[string]any{
			"error": ue.Message,
		}
		if ue.Code != "" {
			out["code"] = ue.Code
		}
		if ue.RequiredCapability != "" {
			out["requiredCapability"] = ue.RequiredCapability
		}
		if ue.MinProtocol > 0 {
			out["minProtocol"] = ue.MinProtocol
		}
		return out
	}
	return map[string]any{"error": err.Error()}
}
