package desktop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// PowerState represents the lifecycle state of a managed virtual desktop session.
type PowerState string

const (
	PowerRunning PowerState = "running"
	PowerFrozen  PowerState = "frozen"
	PowerStopped PowerState = "stopped"
)

// VirtualConfig configures virtual desktop mode, resolution, and idle sleep timers.
type VirtualConfig struct {
	Mode                  string
	AutoInstall           bool
	Resolution            string
	IdleFreeze            time.Duration
	IdleStop              time.Duration
	StopOnlyWhenNoWindows bool
}

func normalizeVirtualConfig(cfg VirtualConfig) VirtualConfig {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "native", "virtual":
		cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	default:
		cfg.Mode = "auto"
	}
	if strings.TrimSpace(cfg.Resolution) == "" {
		cfg.Resolution = "1440x900"
	}
	if cfg.IdleFreeze == 0 {
		cfg.IdleFreeze = 5 * time.Minute
	} else if cfg.IdleFreeze < 0 {
		cfg.IdleFreeze = 0
	}
	if cfg.IdleStop == 0 {
		cfg.IdleStop = 30 * time.Minute
	} else if cfg.IdleStop < 0 {
		cfg.IdleStop = 0
	}
	return cfg
}

// ProbeInfo describes host display capabilities and missing virtual desktop dependencies.
type ProbeInfo struct {
	HasNativeDisplay bool
	DisplayServer    string // "x11" | "wayland" | "none"
	VirtualSupported bool   // true on platforms that support managed virtual displays (Linux)
	VirtualReady     bool
	Installable      bool
	MissingDeps      []string
	InstallCommand   string
}

// SetupResult reports the outcome of installing virtual desktop dependencies.
type SetupResult struct {
	OK             bool     `json:"ok"`
	Installed      bool     `json:"installed"`
	AlreadyReady   bool     `json:"alreadyReady,omitempty"`
	MissingDeps    []string `json:"missingDeps,omitempty"`
	InstallCommand string   `json:"installCommand,omitempty"`
	Output         string   `json:"output,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// VirtualStatus is a read-only snapshot merged into HostStatus.
type VirtualStatus struct {
	Virtual        bool
	PowerState     PowerState
	KeepAwake      bool
	Installable    bool
	MissingDeps    []string
	InstallCommand string
	DisplayServer  string
	VirtualReady   bool
	UsingNative    bool
}

type virtualBackend interface {
	Probe() ProbeInfo
	Start(ctx context.Context, resolution string) error
	Freeze() error
	Resume() error
	Stop() error
	Env() []string
	HasWindows(ctx context.Context) (bool, error)
	Install(ctx context.Context) (*SetupResult, error)
}

// virtualManager manages on-demand virtual desktop startup, idle freeze/stop, and transparent wakeup.
type virtualManager struct {
	cfg         VirtualConfig
	backend     virtualBackend
	mu          sync.Mutex
	state       PowerState
	lastActive  time.Time
	frozenAt    time.Time
	leases      int
	keepAwake   bool
	hasTakeover func() bool
	now         func() time.Time
	stopCh      chan struct{}
	closeOnce   sync.Once
}

func newVirtualManager(cfg VirtualConfig, backend virtualBackend) *virtualManager {
	if backend == nil {
		backend = newPlatformVirtualBackend()
	}
	cfg = normalizeVirtualConfig(cfg)
	m := &virtualManager{
		cfg:     cfg,
		backend: backend,
		state:   PowerStopped,
		now:     time.Now,
		stopCh:  make(chan struct{}),
	}
	go m.idleLoop()
	return m
}

func (m *virtualManager) idleLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.CheckIdle(context.Background())
		}
	}
}

func (m *virtualManager) SetHasTakeover(fn func() bool) {
	m.mu.Lock()
	m.hasTakeover = fn
	m.mu.Unlock()
}

func (m *virtualManager) isVirtualModeLocked(probe ProbeInfo) bool {
	if !probe.VirtualSupported {
		return false
	}
	switch m.cfg.Mode {
	case "native":
		return false
	case "virtual":
		return true
	default: // "auto"
		return !probe.HasNativeDisplay
	}
}

// Status returns a read-only snapshot without waking a sleeping virtual desktop.
func (m *virtualManager) Status() VirtualStatus {
	probe := m.backend.Probe()
	m.mu.Lock()
	defer m.mu.Unlock()

	useVirtual := m.isVirtualModeLocked(probe)
	st := VirtualStatus{
		Virtual:        useVirtual,
		KeepAwake:      m.keepAwake,
		DisplayServer:  probe.DisplayServer,
		VirtualReady:   probe.VirtualReady,
		UsingNative:    probe.HasNativeDisplay && !useVirtual,
		InstallCommand: probe.InstallCommand,
	}
	if useVirtual {
		st.PowerState = m.state
		if !probe.VirtualReady {
			st.Installable = probe.Installable
			st.MissingDeps = append([]string(nil), probe.MissingDeps...)
		}
	}
	return st
}

// Env returns extra environment variables (DISPLAY, DBUS_SESSION_BUS_ADDRESS) for the active virtual session.
func (m *virtualManager) Env() []string {
	probe := m.backend.Probe()
	m.mu.Lock()
	useVirtual := m.isVirtualModeLocked(probe)
	state := m.state
	m.mu.Unlock()
	if !useVirtual || state == PowerStopped {
		return nil
	}
	return m.backend.Env()
}

// Touch refreshes the idle timer without forcing a stopped desktop to start.
func (m *virtualManager) Touch() {
	m.mu.Lock()
	m.lastActive = m.now()
	m.mu.Unlock()
}

// AcquireLease prevents the virtual desktop from sleeping until the returned release func is called.
func (m *virtualManager) AcquireLease() func() {
	m.mu.Lock()
	m.leases++
	m.lastActive = m.now()
	m.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			if m.leases > 0 {
				m.leases--
			}
			m.lastActive = m.now()
			m.mu.Unlock()
		})
	}
}

// EnsureAwake wakes a frozen or stopped virtual desktop if virtual mode is active.
// Returns (usedVirtual, error).
func (m *virtualManager) EnsureAwake(ctx context.Context) (bool, error) {
	probe := m.backend.Probe()
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isVirtualModeLocked(probe) {
		return false, nil
	}
	if !probe.VirtualReady {
		if len(probe.MissingDeps) > 0 {
			return true, errors.New("virtual desktop is missing dependencies (" + strings.Join(probe.MissingDeps, ", ") + "); install them first")
		}
		return true, errors.New("virtual desktop environment is not ready")
	}

	switch m.state {
	case PowerRunning:
		m.lastActive = m.now()
		return true, nil
	case PowerFrozen:
		if err := m.backend.Resume(); err != nil {
			return true, err
		}
		m.state = PowerRunning
		m.lastActive = m.now()
		m.frozenAt = time.Time{}
		return true, nil
	default: // PowerStopped
		if err := m.backend.Start(ctx, m.cfg.Resolution); err != nil {
			return true, err
		}
		m.state = PowerRunning
		m.lastActive = m.now()
		m.frozenAt = time.Time{}
		return true, nil
	}
}

// Power manually changes virtual desktop power state or keep-awake policy.
func (m *virtualManager) Power(ctx context.Context, action string) error {
	act := strings.ToLower(strings.TrimSpace(action))
	probe := m.backend.Probe()

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isVirtualModeLocked(probe) {
		return errors.New("virtual desktop power control is not available in native desktop mode")
	}

	switch act {
	case "wake", "start", "resume":
		if !probe.VirtualReady {
			return errors.New("virtual desktop is missing dependencies; install them first")
		}
		switch m.state {
		case PowerRunning:
			m.lastActive = m.now()
			return nil
		case PowerFrozen:
			if err := m.backend.Resume(); err != nil {
				return err
			}
			m.state = PowerRunning
			m.lastActive = m.now()
			m.frozenAt = time.Time{}
			return nil
		default:
			if err := m.backend.Start(ctx, m.cfg.Resolution); err != nil {
				return err
			}
			m.state = PowerRunning
			m.lastActive = m.now()
			m.frozenAt = time.Time{}
			return nil
		}

	case "freeze":
		if m.state == PowerStopped {
			return errors.New("virtual desktop is already stopped")
		}
		if m.state == PowerFrozen {
			return nil
		}
		if err := m.backend.Freeze(); err != nil {
			return err
		}
		m.state = PowerFrozen
		m.frozenAt = m.now()
		return nil

	case "stop":
		if m.state == PowerStopped {
			return nil
		}
		if m.state == PowerFrozen {
			_ = m.backend.Resume()
		}
		if err := m.backend.Stop(); err != nil {
			return err
		}
		m.state = PowerStopped
		m.frozenAt = time.Time{}
		return nil

	case "keep_awake":
		m.keepAwake = true
		m.lastActive = m.now()
		return nil

	case "allow_sleep":
		m.keepAwake = false
		m.lastActive = m.now()
		return nil

	default:
		return errors.New("unsupported power action (supported: wake / freeze / stop / keep_awake / allow_sleep)")
	}
}

// CheckIdle evaluates idle thresholds and transitions running -> frozen -> stopped when eligible.
func (m *virtualManager) CheckIdle(ctx context.Context) {
	probe := m.backend.Probe()

	m.mu.Lock()
	if !m.isVirtualModeLocked(probe) || m.state == PowerStopped {
		m.mu.Unlock()
		return
	}
	if m.leases > 0 || m.keepAwake {
		m.mu.Unlock()
		return
	}
	takeoverFn := m.hasTakeover
	m.mu.Unlock()

	if takeoverFn != nil && takeoverFn() {
		return
	}

	now := m.now()

	m.mu.Lock()
	if m.leases > 0 || m.keepAwake {
		m.mu.Unlock()
		return
	}
	state := m.state
	lastActive := m.lastActive
	frozenAt := m.frozenAt
	idleFreeze := m.cfg.IdleFreeze
	idleStop := m.cfg.IdleStop
	stopOnlyNoWin := m.cfg.StopOnlyWhenNoWindows

	if state == PowerRunning {
		if idleFreeze > 0 && !lastActive.IsZero() && now.Sub(lastActive) >= idleFreeze {
			if err := m.backend.Freeze(); err == nil {
				m.state = PowerFrozen
				m.frozenAt = now
			}
			m.mu.Unlock()
			return
		}
		// If auto-freeze is disabled but auto-stop is enabled, stop directly after idleStop.
		if idleFreeze == 0 && idleStop > 0 && !lastActive.IsZero() && now.Sub(lastActive) >= idleStop {
			m.mu.Unlock()
			m.tryStopIfNoWindows(ctx, stopOnlyNoWin)
			return
		}
		m.mu.Unlock()
		return
	}

	if state == PowerFrozen {
		if idleStop > 0 && !frozenAt.IsZero() && now.Sub(frozenAt) >= idleStop {
			m.mu.Unlock()
			m.tryStopIfNoWindows(ctx, stopOnlyNoWin)
			return
		}
	}
	m.mu.Unlock()
}

func (m *virtualManager) tryStopIfNoWindows(ctx context.Context, stopOnlyNoWin bool) {
	if stopOnlyNoWin {
		m.mu.Lock()
		wasFrozen := m.state == PowerFrozen
		if wasFrozen {
			_ = m.backend.Resume()
		}
		m.mu.Unlock()

		hasWin, err := m.backend.HasWindows(ctx)

		m.mu.Lock()
		defer m.mu.Unlock()
		if m.leases > 0 || m.keepAwake {
			if wasFrozen && m.state == PowerFrozen {
				m.state = PowerRunning
				m.frozenAt = time.Time{}
				m.lastActive = m.now()
			}
			return
		}
		if err != nil || hasWin {
			// Windows still open (or unable to verify); keep frozen instead of killing user windows.
			if wasFrozen {
				_ = m.backend.Freeze()
			}
			return
		}
		_ = m.backend.Stop()
		m.state = PowerStopped
		m.frozenAt = time.Time{}
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.leases > 0 || m.keepAwake || m.state == PowerStopped {
		return
	}
	if m.state == PowerFrozen {
		_ = m.backend.Resume()
	}
	_ = m.backend.Stop()
	m.state = PowerStopped
	m.frozenAt = time.Time{}
}

// Setup installs missing virtual desktop dependencies on supported headless platforms.
func (m *virtualManager) Setup(ctx context.Context) (*SetupResult, error) {
	probe := m.backend.Probe()
	if !probe.VirtualSupported {
		return &SetupResult{
			OK:    false,
			Error: "virtual desktop installation is not required on this OS",
		}, nil
	}
	if probe.VirtualReady {
		return &SetupResult{
			OK:           true,
			AlreadyReady: true,
			Output:       "virtual desktop dependencies are ready",
		}, nil
	}
	return m.backend.Install(ctx)
}

// Close stops the idle loop and terminates any active virtual desktop process group.
func (m *virtualManager) Close() {
	m.closeOnce.Do(func() {
		close(m.stopCh)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.state == PowerFrozen {
			_ = m.backend.Resume()
		}
		if m.state != PowerStopped {
			_ = m.backend.Stop()
			m.state = PowerStopped
		}
	})
}

func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	m := make(map[string]string, len(extra))
	for _, kv := range extra {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	out := make([]string, 0, len(base)+len(extra))
	seen := make(map[string]bool, len(extra))
	for _, kv := range base {
		k, _, ok := strings.Cut(kv, "=")
		if ok {
			if v, override := m[k]; override {
				out = append(out, k+"="+v)
				seen[k] = true
				continue
			}
		}
		out = append(out, kv)
	}
	for _, kv := range extra {
		if k, _, ok := strings.Cut(kv, "="); ok && !seen[k] {
			out = append(out, kv)
		}
	}
	return out
}
