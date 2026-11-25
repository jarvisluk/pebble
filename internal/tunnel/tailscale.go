package tunnel

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

// TailscaleOptions configures a user-space Tailscale node via tsnet.
type TailscaleOptions struct {
	StateDir   string
	Hostname   string
	ControlURL string
	AuthKey    string
	Ephemeral  bool
}

// TailscalePeer describes another device visible on the same tailnet.
type TailscalePeer struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	DNSName  string `json:"dnsName,omitempty"`
	IP       string `json:"ip,omitempty"`
	OS       string `json:"os,omitempty"`
	Address  string `json:"address"`
	Online   bool   `json:"online"`
	IsBotHub bool   `json:"isBotHub"`
}

// TailscaleState is the current status snapshot of a TailscaleNode.
type TailscaleState struct {
	State   string          `json:"state"` // disabled | starting | needs_login | running | error
	AuthURL string          `json:"authUrl,omitempty"`
	Hostname string         `json:"hostname,omitempty"`
	DNSName string          `json:"dnsName,omitempty"`
	IP      string          `json:"ip,omitempty"`
	Tailnet string          `json:"tailnet,omitempty"`
	Link    string          `json:"link,omitempty"`
	Error   string          `json:"error,omitempty"`
	Peers   []TailscalePeer `json:"peers,omitempty"`
}

// DefaultTailscaleHostname returns a sanitized Tailscale node name such as "pebble-mac-mini".
func DefaultTailscaleHostname(prefix string) string {
	p := sanitizeDNSLabel(prefix)
	if p == "" {
		p = "pebble"
	}
	host, err := os.Hostname()
	if err != nil {
		return p
	}
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	h := sanitizeDNSLabel(host)
	if h == "" {
		return p
	}
	out := p + "-" + h
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}

func sanitizeDNSLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// ParseTailscaleLink parses a tailscale://<host>[:port] link (defaulting to Port)
// and returns host:port suitable for tsnet dialing.
func ParseTailscaleLink(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "tailscale") {
		return "", errors.New("invalid tailscale link")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		host = strings.TrimSpace(strings.TrimPrefix(u.Path, "/"))
	}
	if host == "" {
		return "", errors.New("invalid tailscale host")
	}
	port := uint16(Port)
	if ps := u.Port(); ps != "" {
		p, perr := strconv.ParseUint(ps, 10, 16)
		if perr != nil || p == 0 {
			return "", errors.New("invalid tailscale port")
		}
		port = uint16(p)
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// TailscaleNode manages a single in-process tsnet.Server lifecycle.
type TailscaleNode struct {
	opts TailscaleOptions

	mu          sync.Mutex
	srv         *tsnet.Server
	lc          *local.Client
	state       TailscaleState
	loginAsked  bool
	notifyCh    chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	closed      bool
}

// NewTailscaleNode constructs an unstarted TailscaleNode with opts.
func NewTailscaleNode(opts TailscaleOptions) *TailscaleNode {
	if strings.TrimSpace(opts.Hostname) == "" {
		opts.Hostname = DefaultTailscaleHostname("pebble")
	}
	return &TailscaleNode{
		opts: opts,
		state: TailscaleState{
			State:    "starting",
			Hostname: opts.Hostname,
		},
		notifyCh: make(chan struct{}, 1),
	}
}

// Options returns a copy of the node's options (without exposing AuthKey in state).
func (n *TailscaleNode) Options() TailscaleOptions {
	return n.opts
}

// Start initializes the tsnet.Server and begins watching its IPN state bus.
func (n *TailscaleNode) Start() error {
	n.mu.Lock()
	if n.srv != nil {
		n.mu.Unlock()
		return nil
	}
	if n.closed {
		n.mu.Unlock()
		return errors.New("tailscale node closed")
	}
	if strings.TrimSpace(n.opts.StateDir) != "" {
		if err := os.MkdirAll(n.opts.StateDir, 0o700); err != nil {
			n.state.State = "error"
			n.state.Error = err.Error()
			n.mu.Unlock()
			return err
		}
	}
	srv := &tsnet.Server{
		Dir:        n.opts.StateDir,
		Hostname:   n.opts.Hostname,
		ControlURL: strings.TrimSpace(n.opts.ControlURL),
		AuthKey:    strings.TrimSpace(n.opts.AuthKey),
		Ephemeral:  n.opts.Ephemeral,
		Logf:       tsLogf,
		UserLogf:   tsLogf,
	}
	ctx, cancel := context.WithCancel(context.Background())
	n.srv = srv
	n.ctx = ctx
	n.cancel = cancel
	n.done = make(chan struct{})
	n.state.State = "starting"
	n.state.Error = ""
	n.mu.Unlock()

	if err := srv.Start(); err != nil {
		cancel()
		_ = srv.Close()
		n.mu.Lock()
		n.srv = nil
		n.state.State = "error"
		n.state.Error = err.Error()
		close(n.done)
		n.broadcastLocked()
		n.mu.Unlock()
		return err
	}

	lc, err := srv.LocalClient()
	if err != nil {
		cancel()
		_ = srv.Close()
		n.mu.Lock()
		n.srv = nil
		n.state.State = "error"
		n.state.Error = err.Error()
		close(n.done)
		n.broadcastLocked()
		n.mu.Unlock()
		return err
	}

	n.mu.Lock()
	n.lc = lc
	n.mu.Unlock()

	go n.watchLoop(ctx, lc)
	return nil
}

func (n *TailscaleNode) broadcastLocked() {
	select {
	case n.notifyCh <- struct{}{}:
	default:
	}
}

func (n *TailscaleNode) watchLoop(ctx context.Context, lc *local.Client) {
	defer close(n.done)

	watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil {
		if ctx.Err() == nil {
			n.mu.Lock()
			n.state.State = "error"
			n.state.Error = err.Error()
			n.broadcastLocked()
			n.mu.Unlock()
		}
		return
	}
	defer watcher.Close()

	for {
		note, err := watcher.Next()
		if err != nil {
			if ctx.Err() == nil {
				n.mu.Lock()
				if n.state.State != "disabled" {
					n.state.State = "error"
					n.state.Error = err.Error()
				}
				n.broadcastLocked()
				n.mu.Unlock()
			}
			return
		}

		var needInteractiveLogin bool
		var needRefreshRunning bool

		n.mu.Lock()
		if note.ErrMessage != nil && strings.TrimSpace(*note.ErrMessage) != "" {
			n.state.Error = strings.TrimSpace(*note.ErrMessage)
		}
		if note.BrowseToURL != nil && strings.TrimSpace(*note.BrowseToURL) != "" {
			n.state.State = "needs_login"
			n.state.AuthURL = strings.TrimSpace(*note.BrowseToURL)
			n.state.Error = ""
		}
		if note.State != nil {
			switch *note.State {
			case ipn.NeedsLogin:
				n.state.State = "needs_login"
				n.state.Error = ""
				if strings.TrimSpace(n.opts.AuthKey) == "" && !n.loginAsked {
					n.loginAsked = true
					needInteractiveLogin = true
				}
			case ipn.Running:
				needRefreshRunning = true
			case ipn.Starting:
				if n.state.State != "needs_login" {
					n.state.State = "starting"
				}
			case ipn.Stopped, ipn.NoState:
				if n.state.State != "needs_login" && n.state.State != "error" {
					n.state.State = "starting"
				}
			}
		}
		n.broadcastLocked()
		n.mu.Unlock()

		if needInteractiveLogin {
			go func() {
				lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				if err := lc.StartLoginInteractive(lctx); err != nil && ctx.Err() == nil {
					tsLogf("StartLoginInteractive: %v", err)
				}
			}()
		}
		if needRefreshRunning {
			n.refreshRunningStatus(ctx, lc)
		}
	}
}

func (n *TailscaleNode) refreshRunningStatus(ctx context.Context, lc *local.Client) {
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st, err := lc.Status(sctx)
	if err != nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return
	}
	n.applyStatusLocked(st)
	n.state.State = "running"
	n.state.AuthURL = ""
	n.state.Error = ""
	n.loginAsked = false
	n.broadcastLocked()
}

func (n *TailscaleNode) applyStatusLocked(st *ipnstate.Status) {
	if st == nil {
		return
	}
	if st.CurrentTailnet != nil {
		n.state.Tailnet = st.CurrentTailnet.Name
	}
	if st.Self != nil {
		if h := strings.TrimSpace(st.Self.HostName); h != "" {
			n.state.Hostname = h
		}
		n.state.DNSName = strings.TrimSuffix(strings.TrimSpace(st.Self.DNSName), ".")
	}
	n.state.IP = pickPreferredIP(st.TailscaleIPs)
	target := n.state.DNSName
	if target == "" {
		target = n.state.IP
	}
	if target != "" {
		n.state.Link = fmt.Sprintf("tailscale://%s:%d", target, Port)
	}
}

func pickPreferredIP(ips []netip.Addr) string {
	var fallback string
	for _, ip := range ips {
		if !ip.IsValid() {
			continue
		}
		if ip.Is4() {
			return ip.String()
		}
		if fallback == "" {
			fallback = ip.String()
		}
	}
	return fallback
}

// WaitStateChange waits until either the state leaves "starting" or ctx expires.
func (n *TailscaleNode) WaitStateChange(ctx context.Context) TailscaleState {
	for {
		n.mu.Lock()
		st := n.state
		ch := n.notifyCh
		n.mu.Unlock()
		if st.State != "starting" {
			return st
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return n.Status(context.Background(), false)
		}
	}
}

// WaitRunning blocks until the node reaches "running" or fails/requires login.
func (n *TailscaleNode) WaitRunning(ctx context.Context) error {
	for {
		n.mu.Lock()
		st := n.state
		ch := n.notifyCh
		n.mu.Unlock()

		switch st.State {
		case "running":
			return nil
		case "needs_login":
			return errors.New("tailscale login required")
		case "error":
			if st.Error != "" {
				return errors.New(st.Error)
			}
			return errors.New("tailscale node error")
		case "disabled":
			return errors.New("tailscale node disabled")
		}

		select {
		case <-ch:
		case <-ctx.Done():
			return fmt.Errorf("tailscale not ready: %w", ctx.Err())
		}
	}
}

// Listen listens on the given TCP port inside the tailnet.
func (n *TailscaleNode) Listen(port uint16) (net.Listener, error) {
	n.mu.Lock()
	srv := n.srv
	n.mu.Unlock()
	if srv == nil {
		return nil, errors.New("tailscale node not started")
	}
	return srv.Listen("tcp", fmt.Sprintf(":%d", port))
}

// Dial connects to addr (host:port) inside the tailnet via tsnet.
func (n *TailscaleNode) Dial(ctx context.Context, addr string) (net.Conn, error) {
	n.mu.Lock()
	srv := n.srv
	n.mu.Unlock()
	if srv == nil {
		return nil, errors.New("tailscale node not started")
	}
	return srv.Dial(ctx, "tcp", addr)
}

// Status returns the current TailscaleState, optionally including sorted tailnet peers.
func (n *TailscaleNode) Status(ctx context.Context, includePeers bool) TailscaleState {
	n.mu.Lock()
	st := n.state
	lc := n.lc
	n.mu.Unlock()

	if st.State == "running" && lc != nil {
		sctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		if raw, err := lc.Status(sctx); err == nil && raw != nil {
			n.mu.Lock()
			n.applyStatusLocked(raw)
			st = n.state
			n.mu.Unlock()
			if includePeers {
				st.Peers = buildTailscalePeers(raw)
			}
		}
	}
	return st
}

func buildTailscalePeers(raw *ipnstate.Status) []TailscalePeer {
	if raw == nil || len(raw.Peer) == 0 {
		return nil
	}
	peers := make([]TailscalePeer, 0, len(raw.Peer))
	for _, p := range raw.Peer {
		if p == nil || p.ShareeNode {
			continue
		}
		if raw.Self != nil && p.ID != "" && p.ID == raw.Self.ID {
			continue
		}
		dnsName := strings.TrimSuffix(strings.TrimSpace(p.DNSName), ".")
		ip := pickPreferredIP(p.TailscaleIPs)
		target := dnsName
		if target == "" {
			target = ip
		}
		if target == "" {
			continue
		}
		host := strings.TrimSpace(p.HostName)
		if host == "" {
			host = target
		}
		lowerHost := strings.ToLower(host)
		isBotHub := isPebbleHostname(lowerHost)
		peers = append(peers, TailscalePeer{
			ID:       string(p.ID),
			Hostname: host,
			DNSName:  dnsName,
			IP:       ip,
			OS:       p.OS,
			Address:  fmt.Sprintf("tailscale://%s:%d", target, Port),
			Online:   p.Online,
			IsBotHub: isBotHub,
		})
	}
	sortTailscalePeers(peers)
	return peers
}

func sortTailscalePeers(peers []TailscalePeer) {
	sort.Slice(peers, func(i, j int) bool {
		a, b := peers[i], peers[j]
		aRank := peerRank(a)
		bRank := peerRank(b)
		if aRank != bRank {
			return aRank < bRank
		}
		ah, bh := strings.ToLower(a.Hostname), strings.ToLower(b.Hostname)
		if ah != bh {
			return ah < bh
		}
		return a.Address < b.Address
	})
}

func peerRank(p TailscalePeer) int {
	switch {
	case p.Online && p.IsBotHub:
		return 0
	case p.Online:
		return 1
	case p.IsBotHub:
		return 2
	default:
		return 3
	}
}

// Logout logs out the current node from the tailnet, closes tsnet, and removes StateDir.
func (n *TailscaleNode) Logout(ctx context.Context) error {
	n.mu.Lock()
	lc := n.lc
	stateDir := n.opts.StateDir
	n.mu.Unlock()

	if lc != nil {
		lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = lc.Logout(lctx)
		cancel()
	}
	err := n.Close()
	if strings.TrimSpace(stateDir) != "" {
		if rerr := os.RemoveAll(stateDir); rerr != nil && err == nil {
			err = rerr
		}
	}
	return err
}

// Close shuts down the tsnet.Server without removing StateDir.
func (n *TailscaleNode) Close() error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return nil
	}
	n.closed = true
	cancel := n.cancel
	srv := n.srv
	done := n.done
	n.srv = nil
	n.lc = nil
	n.state.State = "disabled"
	n.state.AuthURL = ""
	n.broadcastLocked()
	n.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	var err error
	if srv != nil {
		err = srv.Close()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
	return err
}

func isPebbleHostname(lowerHost string) bool {
	return lowerHost == "pebble" || strings.HasPrefix(lowerHost, "pebble-") ||
		lowerHost == "bothub" || strings.HasPrefix(lowerHost, "bothub-")
}

func tsLogf(format string, args ...any) {
	if tunnelLookupEnv("PEBBLE_TAILSCALE_DEBUG", "BOT_HUB_TAILSCALE_DEBUG") == "1" {
		log.Printf("[tailscale] "+format, args...)
	}
}
