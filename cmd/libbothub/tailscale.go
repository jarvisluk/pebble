package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/tunnel"
)

var (
	tsMu   sync.Mutex
	tsNode *tunnel.TailscaleNode
)

func marshalTSState(st tunnel.TailscaleState) string {
	b, _ := json.Marshal(st)
	return string(b)
}

func marshalTSError(err error) string {
	return marshalTSState(tunnel.TailscaleState{
		State: "error",
		Error: err.Error(),
	})
}

func tailscaleStartJSON(stateDir, hostname, controlURL, authKey string) string {
	stateDir = strings.TrimSpace(stateDir)
	hostname = strings.TrimSpace(hostname)
	controlURL = strings.TrimSpace(controlURL)
	authKey = strings.TrimSpace(authKey)

	if stateDir == "" {
		return marshalTSError(errors.New("tailscale stateDir is required"))
	}
	if hostname == "" {
		hostname = tunnel.DefaultTailscaleHostname("bothub-mobile")
	}

	tsMu.Lock()
	cur := tsNode
	if cur != nil {
		opts := cur.Options()
		st := cur.Status(context.Background(), false)
		sameTarget := opts.StateDir == stateDir && opts.ControlURL == controlURL && (hostname == "" || opts.Hostname == hostname)
		if sameTarget && authKey == "" && st.State != "error" && st.State != "disabled" {
			tsMu.Unlock()
			return marshalTSState(st)
		}
		tsNode = nil
		tsMu.Unlock()
		_ = cur.Close()
	} else {
		tsMu.Unlock()
	}

	node := tunnel.NewTailscaleNode(tunnel.TailscaleOptions{
		StateDir:   stateDir,
		Hostname:   hostname,
		ControlURL: controlURL,
		AuthKey:    authKey,
	})
	if err := node.Start(); err != nil {
		return marshalTSError(err)
	}

	tsMu.Lock()
	tsNode = node
	tsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	st := node.WaitStateChange(ctx)
	cancel()
	return marshalTSState(st)
}

func tailscaleStatusJSON(includePeers bool) string {
	tsMu.Lock()
	node := tsNode
	tsMu.Unlock()
	if node == nil {
		return marshalTSState(tunnel.TailscaleState{State: "disabled"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return marshalTSState(node.Status(ctx, includePeers))
}

func tailscaleLogoutJSON(stateDir string) string {
	stopTailscaleTunnelIfActive()

	tsMu.Lock()
	node := tsNode
	tsNode = nil
	tsMu.Unlock()

	if node != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = node.Logout(ctx)
		cancel()
	} else if dir := strings.TrimSpace(stateDir); dir != "" {
		_ = os.RemoveAll(dir)
	}
	return marshalTSState(tunnel.TailscaleState{State: "disabled"})
}

func dialTailscale(ctx context.Context, targetAddr string) (net.Conn, error) {
	tsMu.Lock()
	node := tsNode
	tsMu.Unlock()
	if node == nil {
		return nil, errors.New("tailscale is not bound")
	}
	if err := node.WaitRunning(ctx); err != nil {
		return nil, err
	}
	return node.Dial(ctx, targetAddr)
}
