package desktop

import (
	"context"
	"sync"
	"testing"
	"time"
)

type mockVirtualBackend struct {
	mu          sync.Mutex
	probe       ProbeInfo
	started     int
	frozen      int
	resumed     int
	stopped     int
	hasWindows  bool
	installHits int
	env         []string
}

func (m *mockVirtualBackend) Probe() ProbeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.probe
}

func (m *mockVirtualBackend) Start(context.Context, string) error {
	m.mu.Lock()
	m.started++
	m.env = []string{"DISPLAY=:99"}
	m.mu.Unlock()
	return nil
}

func (m *mockVirtualBackend) Freeze() error {
	m.mu.Lock()
	m.frozen++
	m.mu.Unlock()
	return nil
}

func (m *mockVirtualBackend) Resume() error {
	m.mu.Lock()
	m.resumed++
	m.mu.Unlock()
	return nil
}

func (m *mockVirtualBackend) Stop() error {
	m.mu.Lock()
	m.stopped++
	m.env = nil
	m.mu.Unlock()
	return nil
}

func (m *mockVirtualBackend) Env() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.env...)
}

func (m *mockVirtualBackend) HasWindows(context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hasWindows, nil
}

func (m *mockVirtualBackend) Install(context.Context) (*SetupResult, error) {
	m.mu.Lock()
	m.installHits++
	m.probe.VirtualReady = true
	m.probe.MissingDeps = nil
	m.mu.Unlock()
	return &SetupResult{OK: true, Installed: true}, nil
}

func TestVirtualManagerAutoNativeNeverSleeps(t *testing.T) {
	mb := &mockVirtualBackend{
		probe: ProbeInfo{
			HasNativeDisplay: true,
			DisplayServer:    "x11",
			VirtualSupported: true,
			VirtualReady:     true,
		},
	}
	vm := newVirtualManager(VirtualConfig{Mode: "auto"}, mb)
	defer vm.Close()

	st := vm.Status()
	if st.Virtual || !st.UsingNative {
		t.Fatalf("expected native mode, got %+v", st)
	}
	used, err := vm.EnsureAwake(context.Background())
	if err != nil || used {
		t.Fatalf("expected EnsureAwake no-op for native display, got used=%v err=%v", used, err)
	}
	if mb.started != 0 {
		t.Fatalf("native mode should not start virtual display")
	}
	if err := vm.Power(context.Background(), "freeze"); err == nil {
		t.Fatalf("expected freeze to fail on native display")
	}
}

func TestVirtualManagerHeadlessLifecycleAndLeases(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	mb := &mockVirtualBackend{
		probe: ProbeInfo{
			HasNativeDisplay: false,
			DisplayServer:    "none",
			VirtualSupported: true,
			VirtualReady:     true,
		},
		hasWindows: true,
	}
	vm := newVirtualManager(VirtualConfig{
		Mode:                  "auto",
		IdleFreeze:            5 * time.Minute,
		IdleStop:              30 * time.Minute,
		StopOnlyWhenNoWindows: true,
	}, mb)
	vm.now = func() time.Time { return now }
	defer vm.Close()

	// Read-only Status() must NOT wake a stopped virtual desktop.
	st := vm.Status()
	if !st.Virtual || st.PowerState != PowerStopped || mb.started != 0 {
		t.Fatalf("expected stopped virtual desktop on Status(), got %+v started=%d", st, mb.started)
	}

	// EnsureAwake() starts the virtual desktop.
	used, err := vm.EnsureAwake(context.Background())
	if err != nil || !used {
		t.Fatalf("EnsureAwake failed: used=%v err=%v", used, err)
	}
	if vm.Status().PowerState != PowerRunning || mb.started != 1 {
		t.Fatalf("expected running after EnsureAwake, state=%s started=%d", vm.Status().PowerState, mb.started)
	}
	if len(vm.Env()) == 0 || vm.Env()[0] != "DISPLAY=:99" {
		t.Fatalf("expected DISPLAY=:99 env, got %v", vm.Env())
	}

	// Lease prevents freeze even after idle threshold.
	release := vm.AcquireLease()
	now = now.Add(6 * time.Minute)
	vm.CheckIdle(context.Background())
	if vm.Status().PowerState != PowerRunning {
		t.Fatalf("expected lease to prevent freeze, got %s", vm.Status().PowerState)
	}
	release()

	// Takeover also prevents freeze.
	takeoverActive := true
	vm.SetHasTakeover(func() bool { return takeoverActive })
	now = now.Add(6 * time.Minute)
	vm.CheckIdle(context.Background())
	if vm.Status().PowerState != PowerRunning {
		t.Fatalf("expected takeover to prevent freeze, got %s", vm.Status().PowerState)
	}
	takeoverActive = false

	// KeepAwake prevents freeze.
	if err := vm.Power(context.Background(), "keep_awake"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	vm.CheckIdle(context.Background())
	if vm.Status().PowerState != PowerRunning {
		t.Fatalf("expected keep_awake to prevent freeze, got %s", vm.Status().PowerState)
	}
	_ = vm.Power(context.Background(), "allow_sleep")

	// After idleFreeze, transitions to frozen.
	now = now.Add(6 * time.Minute)
	vm.CheckIdle(context.Background())
	if vm.Status().PowerState != PowerFrozen || mb.frozen != 1 {
		t.Fatalf("expected frozen state, got %s frozen=%d", vm.Status().PowerState, mb.frozen)
	}

	// After idleStop with open windows, remains frozen (does not kill windows).
	now = now.Add(31 * time.Minute)
	vm.CheckIdle(context.Background())
	if vm.Status().PowerState != PowerFrozen || mb.stopped != 0 {
		t.Fatalf("expected to stay frozen while windows exist, got %s stopped=%d", vm.Status().PowerState, mb.stopped)
	}

	// Once windows are gone, next idle check stops the virtual display.
	mb.mu.Lock()
	mb.hasWindows = false
	mb.mu.Unlock()
	vm.CheckIdle(context.Background())
	if vm.Status().PowerState != PowerStopped || mb.stopped != 1 {
		t.Fatalf("expected stopped state when no windows remain, got %s stopped=%d", vm.Status().PowerState, mb.stopped)
	}

	// EnsureAwake restarts from stopped.
	if _, err := vm.EnsureAwake(context.Background()); err != nil {
		t.Fatal(err)
	}
	if vm.Status().PowerState != PowerRunning || mb.started != 2 {
		t.Fatalf("expected restart from stopped, state=%s started=%d", vm.Status().PowerState, mb.started)
	}

	// Manual freeze + EnsureAwake resumes via SIGCONT without restarting.
	if err := vm.Power(context.Background(), "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.EnsureAwake(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mb.resumed == 0 || mb.started != 2 {
		t.Fatalf("expected resume from frozen without restart, resumed=%d started=%d", mb.resumed, mb.started)
	}
}

func TestVirtualManagerForcedVirtualOverNativeAndSetup(t *testing.T) {
	mb := &mockVirtualBackend{
		probe: ProbeInfo{
			HasNativeDisplay: true,
			DisplayServer:    "x11",
			VirtualSupported: true,
			VirtualReady:     false,
			Installable:      true,
			MissingDeps:      []string{"Xvfb", "openbox"},
			InstallCommand:   "sudo apt-get install -y xvfb openbox",
		},
	}
	vm := newVirtualManager(VirtualConfig{Mode: "virtual"}, mb)
	defer vm.Close()

	st := vm.Status()
	if !st.Virtual || !st.Installable || len(st.MissingDeps) != 2 {
		t.Fatalf("expected virtual mode reporting missing deps, got %+v", st)
	}
	if _, err := vm.EnsureAwake(context.Background()); err == nil {
		t.Fatalf("expected EnsureAwake error when deps are missing")
	}

	res, err := vm.Setup(context.Background())
	if err != nil || !res.OK || !res.Installed {
		t.Fatalf("expected Setup to succeed, got res=%+v err=%v", res, err)
	}
	if _, err := vm.EnsureAwake(context.Background()); err != nil {
		t.Fatalf("expected EnsureAwake to succeed after Setup, got %v", err)
	}
	if mb.started != 1 {
		t.Fatalf("expected virtual display started once, got %d", mb.started)
	}
}
