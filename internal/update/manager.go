package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	StateIdle      = "idle"
	StateChecking  = "checking"
	StateUpToDate  = "up_to_date"
	StateAvailable = "available"
	StateError     = "error"

	DefaultInterval = "6h"
	defaultInitWait = 15 * time.Second
)

// Config holds persisted update-check preferences.
type Config struct {
	AutoCheck bool   `json:"autoCheck"`
	Interval  string `json:"interval"`
	Channel   string `json:"channel"`
	Proxy     string `json:"proxy,omitempty"`
}

// ConfigPatch represents a partial update to Config.
type ConfigPatch struct {
	AutoCheck *bool   `json:"autoCheck,omitempty"`
	Interval  *string `json:"interval,omitempty"`
	Channel   *string `json:"channel,omitempty"`
	Proxy     *string `json:"proxy,omitempty"`
}

// Status describes the current update check state returned by HTTP API and SSE events.
type Status struct {
	CurrentVersion string       `json:"currentVersion"`
	State          string       `json:"state"`
	HasUpdate      bool         `json:"hasUpdate"`
	Latest         *ReleaseInfo `json:"latest,omitempty"`
	CheckedAt      string       `json:"checkedAt,omitempty"`
	Error          string       `json:"error,omitempty"`
	ErrorCode      string       `json:"errorCode,omitempty"`
	Config         Config       `json:"config"`
}

// Options configures a Manager instance.
type Options struct {
	StateDir       string
	CurrentVersion string
	Repo           string
	APIBase        string
	InitialDelay   time.Duration
	OnChange       func(Status)
}

type persistedState struct {
	Config      Config       `json:"config"`
	ETag        string       `json:"etag,omitempty"`
	ETagChannel string       `json:"etagChannel,omitempty"`
	Latest      *ReleaseInfo `json:"latest,omitempty"`
	CheckedAt   string       `json:"checkedAt,omitempty"`
}

// DefaultConfig returns the default update-check configuration.
func DefaultConfig() Config {
	return Config{
		AutoCheck: true,
		Interval:  DefaultInterval,
		Channel:   ChannelStable,
	}
}

// IntervalDuration maps supported interval identifiers to time.Duration.
func IntervalDuration(interval string) (time.Duration, bool) {
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "30m":
		return 30 * time.Minute, true
	case "1h":
		return time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "24h":
		return 24 * time.Hour, true
	default:
		return 0, false
	}
}

// ValidateConfig normalizes and validates cfg.
func ValidateConfig(cfg Config) (Config, error) {
	iv := strings.ToLower(strings.TrimSpace(cfg.Interval))
	if iv == "" {
		iv = DefaultInterval
	}
	if _, ok := IntervalDuration(iv); !ok {
		return Config{}, fmt.Errorf("invalid update interval %q (expected 30m, 1h, 6h, or 24h)", cfg.Interval)
	}
	ch := strings.ToLower(strings.TrimSpace(cfg.Channel))
	if ch == "" {
		ch = ChannelStable
	}
	if ch != ChannelStable && ch != ChannelPrerelease {
		return Config{}, fmt.Errorf("invalid update channel %q (expected stable or prerelease)", cfg.Channel)
	}
	proxy, err := ValidateProxyURL(cfg.Proxy)
	if err != nil {
		return Config{}, err
	}
	return Config{
		AutoCheck: cfg.AutoCheck,
		Interval:  iv,
		Channel:   ch,
		Proxy:     proxy,
	}, nil
}

// Manager coordinates manual & periodic GitHub Releases update checks and persists state.
type Manager struct {
	statePath      string
	currentVersion string
	checker        *Checker
	initialDelay   time.Duration
	onChange       func(Status)

	checkMu sync.Mutex
	mu      sync.Mutex
	cfg     Config
	etag    string
	etagCh  string
	latest  *ReleaseInfo
	checked string
	state   string
	errMsg  string
	errCode string

	wakeCh chan struct{}
	wg     sync.WaitGroup
}

// NewManager creates an update Manager and loads persisted state from opts.StateDir if present.
func NewManager(opts Options) *Manager {
	initDelay := opts.InitialDelay
	if initDelay <= 0 {
		initDelay = defaultInitWait
	}
	statePath := ""
	if strings.TrimSpace(opts.StateDir) != "" {
		statePath = filepath.Join(opts.StateDir, "update.json")
	}
	ver := strings.TrimSpace(opts.CurrentVersion)
	if ver == "" {
		ver = "0.1.0"
	}
	m := &Manager{
		statePath:      statePath,
		currentVersion: ver,
		checker: &Checker{
			Repo:           opts.Repo,
			APIBase:        opts.APIBase,
			CurrentVersion: ver,
		},
		initialDelay: initDelay,
		onChange:     opts.OnChange,
		cfg:          DefaultConfig(),
		state:        StateIdle,
		wakeCh:       make(chan struct{}, 1),
	}
	m.load()
	return m
}

func (m *Manager) load() {
	if m.statePath == "" {
		return
	}
	raw, err := os.ReadFile(m.statePath)
	if err != nil {
		return
	}
	var ps persistedState
	if json.Unmarshal(raw, &ps) != nil {
		return
	}
	if norm, err := ValidateConfig(ps.Config); err == nil {
		m.cfg = norm
	}
	m.etag = strings.TrimSpace(ps.ETag)
	m.etagCh = strings.TrimSpace(ps.ETagChannel)
	m.latest = ps.Latest
	m.checked = strings.TrimSpace(ps.CheckedAt)
	if m.latest != nil {
		if IsNewer(m.latest.Version, m.currentVersion) {
			m.state = StateAvailable
		} else {
			m.state = StateUpToDate
		}
	} else if m.checked != "" {
		m.state = StateUpToDate
	}
}

func (m *Manager) saveLocked() error {
	if m.statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.statePath), 0o755); err != nil {
		return err
	}
	var latestCopy *ReleaseInfo
	if m.latest != nil {
		cp := *m.latest
		latestCopy = &cp
	}
	ps := persistedState{
		Config:      m.cfg,
		ETag:        m.etag,
		ETagChannel: m.etagCh,
		Latest:      latestCopy,
		CheckedAt:   m.checked,
	}
	raw, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := m.statePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, m.statePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (m *Manager) snapshotLocked() Status {
	var latestCopy *ReleaseInfo
	hasUpdate := false
	if m.latest != nil {
		cp := *m.latest
		latestCopy = &cp
		hasUpdate = IsNewer(cp.Version, m.currentVersion)
	}
	st := m.state
	if st == "" {
		st = StateIdle
	}
	if st == StateUpToDate && hasUpdate {
		st = StateAvailable
	} else if st == StateAvailable && !hasUpdate {
		st = StateUpToDate
	}
	return Status{
		CurrentVersion: m.currentVersion,
		State:          st,
		HasUpdate:      hasUpdate,
		Latest:         latestCopy,
		CheckedAt:      m.checked,
		Error:          m.errMsg,
		ErrorCode:      m.errCode,
		Config:         m.cfg,
	}
}

func (m *Manager) emit(st Status) {
	if m.onChange != nil {
		m.onChange(st)
	}
}

func (m *Manager) signalWake() {
	select {
	case m.wakeCh <- struct{}{}:
	default:
	}
}

// Status returns a thread-safe snapshot of the current update check state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

// UpdateConfig validates and applies partial configuration updates, persisting to disk.
func (m *Manager) UpdateConfig(patch ConfigPatch) (Status, error) {
	m.mu.Lock()
	next := m.cfg
	if patch.AutoCheck != nil {
		next.AutoCheck = *patch.AutoCheck
	}
	if patch.Interval != nil {
		next.Interval = *patch.Interval
	}
	if patch.Channel != nil {
		next.Channel = *patch.Channel
	}
	if patch.Proxy != nil {
		next.Proxy = *patch.Proxy
	}
	validated, err := ValidateConfig(next)
	if err != nil {
		m.mu.Unlock()
		return Status{}, err
	}

	channelChanged := validated.Channel != m.cfg.Channel
	scheduleChanged := validated.AutoCheck != m.cfg.AutoCheck || validated.Interval != m.cfg.Interval || channelChanged
	m.cfg = validated
	if channelChanged {
		m.etag = ""
		m.etagCh = ""
		m.latest = nil
		m.checked = ""
		m.errMsg = ""
		m.errCode = ""
		m.state = StateIdle
	}
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		return Status{}, err
	}
	snap := m.snapshotLocked()
	m.mu.Unlock()

	if scheduleChanged {
		m.signalWake()
	}
	m.emit(snap)
	return snap, nil
}

// CheckNow immediately queries GitHub Releases, updating cached state and notifying listeners.
func (m *Manager) CheckNow(ctx context.Context, overrideChannel, overrideProxy string) Status {
	m.checkMu.Lock()
	defer m.checkMu.Unlock()

	m.mu.Lock()
	ch := m.cfg.Channel
	if s := strings.ToLower(strings.TrimSpace(overrideChannel)); s != "" {
		ch = s
	}
	proxy := m.cfg.Proxy
	if s := strings.TrimSpace(overrideProxy); s != "" {
		proxy = s
	}
	var cachedETag string
	var cachedLatest *ReleaseInfo
	if m.etagCh == ch && m.latest != nil {
		cachedETag = m.etag
		cp := *m.latest
		cachedLatest = &cp
	}
	m.state = StateChecking
	m.errMsg = ""
	m.errCode = ""
	checkingSnap := m.snapshotLocked()
	m.mu.Unlock()
	m.emit(checkingSnap)

	info, etag, err := m.checker.Check(ctx, ch, proxy, cachedETag, cachedLatest)

	m.mu.Lock()
	now := time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		m.state = StateError
		m.errMsg = err.Error()
		m.errCode = ErrorCodeOf(err)
		if m.errCode == "" {
			m.errCode = ErrCodeNetwork
		}
		m.checked = now
	} else {
		m.errMsg = ""
		m.errCode = ""
		m.checked = now
		m.latest = info
		// Persist ETag only for the configured channel so manual channel overrides don't poison cache.
		if ch == m.cfg.Channel {
			m.etag = etag
			m.etagCh = ch
		}
		if info != nil && IsNewer(info.Version, m.currentVersion) {
			m.state = StateAvailable
		} else {
			m.state = StateUpToDate
		}
		_ = m.saveLocked()
	}
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emit(snap)
	return snap
}

// Start launches the periodic background update-check loop until ctx is canceled.
func (m *Manager) Start(ctx context.Context) {
	if os.Getenv("PEBBLE_UPDATE_DISABLE_AUTO") == "1" {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runLoop(ctx)
	}()
}

// Wait blocks until the background loop started by Start exits.
func (m *Manager) Wait() {
	m.wg.Wait()
}

func (m *Manager) runLoop(ctx context.Context) {
	delayTimer := time.NewTimer(m.initialDelay)
	defer delayTimer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-delayTimer.C:
	case <-m.wakeCh:
	}

	for {
		if ctx.Err() != nil {
			return
		}
		waitFor := m.nextWaitDuration()
		if waitFor == 0 {
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			_ = m.CheckNow(cctx, "", "")
			cancel()
			waitFor = m.nextWaitDuration()
			if waitFor == 0 {
				waitFor = 6 * time.Hour
			}
		}

		timer := time.NewTimer(waitFor)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-m.wakeCh:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (m *Manager) nextWaitDuration() time.Duration {
	m.mu.Lock()
	cfg := m.cfg
	checkedStr := m.checked
	m.mu.Unlock()

	if !cfg.AutoCheck {
		return 24 * time.Hour
	}
	iv, ok := IntervalDuration(cfg.Interval)
	if !ok {
		iv = 6 * time.Hour
	}
	if checkedStr == "" {
		return 0
	}
	last, err := time.Parse(time.RFC3339, checkedStr)
	if err != nil {
		return 0
	}
	elapsed := time.Since(last)
	if elapsed >= iv {
		return 0
	}
	return iv - elapsed
}
