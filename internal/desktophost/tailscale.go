package desktophost

import (
	"bot-hub/internal/tunnel"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CloseTailscale shuts down the embedded Tailscale node if active.
func (h *Host) CloseTailscale() {
	h.mu.Lock()
	n := h.tsNode
	h.tsNode = nil
	h.mu.Unlock()
	if n != nil {
		_ = n.Close()
	}
}

// TailscaleStart starts or reconfigures the embedded Tailscale node.
func (h *Host) TailscaleStart(ctx context.Context, authKey, controlURL string, openBrowser bool) (tunnel.TailscaleState, error) {
	ctrl := strings.TrimSpace(controlURL)
	key := strings.TrimSpace(authKey)

	h.mu.Lock()
	if ctrl == "" {
		ctrl = h.prefs.TailscaleControlURL
	}
	existing := h.tsNode
	needRecreate := existing != nil && (ctrl != existing.Options().ControlURL || key != "")
	if needRecreate {
		h.tsNode = nil
	}
	h.mu.Unlock()

	if needRecreate && existing != nil {
		_ = existing.Close()
		existing = nil
	}

	if existing == nil {
		n := tunnel.NewTailscaleNode(tunnel.TailscaleOptions{
			StateDir:   filepath.Join(h.dir, "tailscale-client"),
			Hostname:   tunnel.DefaultTailscaleHostname("pebble-desktop"),
			ControlURL: ctrl,
			AuthKey:    key,
		})
		if err := n.Start(); err != nil {
			return tunnel.TailscaleState{State: "error", Error: err.Error()}, err
		}
		h.mu.Lock()
		h.tsNode = n
		h.prefs.TailscaleBound = true
		h.prefs.TailscaleControlURL = ctrl
		h.mu.Unlock()
		h.Save()
		h.notify()
		existing = n
	}

	wctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	st := existing.WaitStateChange(wctx)
	if st.State == "running" {
		st = existing.Status(ctx, true)
	}
	if openBrowser && st.State == "needs_login" && st.AuthURL != "" && h.hooks.OnOpenURL != nil {
		h.hooks.OnOpenURL(st.AuthURL)
	}
	return st, nil
}

// TailscaleStatus returns the current embedded Tailscale node status.
func (h *Host) TailscaleStatus(ctx context.Context, includePeers bool) tunnel.TailscaleState {
	h.mu.Lock()
	n := h.tsNode
	h.mu.Unlock()
	if n == nil {
		return tunnel.TailscaleState{State: "disabled"}
	}
	return n.Status(ctx, includePeers)
}

// TailscaleLogout logs out and removes the embedded Tailscale client state.
func (h *Host) TailscaleLogout(ctx context.Context) tunnel.TailscaleState {
	h.mu.Lock()
	n := h.tsNode
	h.tsNode = nil
	h.prefs.TailscaleBound = false
	h.mu.Unlock()
	h.Save()
	h.notify()
	if n != nil {
		_ = n.Logout(ctx)
	} else {
		_ = os.RemoveAll(filepath.Join(h.dir, "tailscale-client"))
	}
	return tunnel.TailscaleState{State: "disabled"}
}
