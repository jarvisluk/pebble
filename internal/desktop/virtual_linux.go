//go:build linux

package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type linuxVirtualBackend struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	pgid    int
	display string
	dbus    string
}

func newPlatformVirtualBackend() virtualBackend {
	return &linuxVirtualBackend{}
}

func (b *linuxVirtualBackend) Probe() ProbeInfo {
	hasNative, srv := detectLinuxNativeDisplay()
	missing := detectMissingVirtualDeps()
	pkgMgr, installCmd := detectLinuxPackageInstall(missing)

	return ProbeInfo{
		HasNativeDisplay: hasNative,
		DisplayServer:    srv,
		VirtualSupported: true,
		VirtualReady:     len(missing) == 0,
		Installable:      pkgMgr != "",
		MissingDeps:      missing,
		InstallCommand:   installCmd,
	}
}

func detectLinuxNativeDisplay() (bool, string) {
	if wd := strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")); wd != "" {
		if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
			sock := wd
			if !filepath.IsAbs(sock) {
				sock = filepath.Join(runtimeDir, wd)
			}
			if _, err := os.Stat(sock); err == nil {
				return true, "wayland"
			}
		}
	}
	if disp := strings.TrimSpace(os.Getenv("DISPLAY")); disp != "" {
		if x11SocketExists(disp) {
			return true, "x11"
		}
	}
	return false, "none"
}

func x11SocketExists(disp string) bool {
	// Formats: ":0", ":0.0", "localhost:10.0"
	idx := strings.LastIndex(disp, ":")
	if idx < 0 {
		return false
	}
	numPart := disp[idx+1:]
	if dot := strings.IndexByte(numPart, '.'); dot >= 0 {
		numPart = numPart[:dot]
	}
	n, err := strconv.Atoi(numPart)
	if err != nil || n < 0 {
		return false
	}
	sock := fmt.Sprintf("/tmp/.X11-unix/X%d", n)
	if info, err := os.Stat(sock); err == nil && (info.Mode()&os.ModeSocket != 0) {
		return true
	}
	// Fallback: try abstract socket or tcp dial if DISPLAY is remote.
	conn, err := net.DialTimeout("unix", sock, 150*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

func detectMissingVirtualDeps() []string {
	var missing []string
	if _, err := exec.LookPath("Xvfb"); err != nil {
		missing = append(missing, "Xvfb")
	}
	if _, err := exec.LookPath("openbox"); err != nil {
		missing = append(missing, "openbox")
	}
	if _, err := exec.LookPath("dbus-run-session"); err != nil {
		if _, err2 := exec.LookPath("dbus-daemon"); err2 != nil {
			missing = append(missing, "dbus-x11")
		}
	}
	return missing
}

func detectLinuxPackageInstall(missing []string) (pkgMgr string, cmd string) {
	if _, err := exec.LookPath("apt-get"); err == nil {
		return "apt-get", "sudo apt-get update && sudo apt-get install -y xvfb openbox dbus-x11 at-spi2-core fonts-noto-cjk"
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		return "dnf", "sudo dnf install -y xorg-x11-server-Xvfb openbox dbus-x11 at-spi2-core google-noto-sans-cjk-ttc-fonts"
	}
	if _, err := exec.LookPath("pacman"); err == nil {
		return "pacman", "sudo pacman -S --noconfirm xorg-server-xvfb openbox dbus at-spi2-core noto-fonts-cjk"
	}
	if _, err := exec.LookPath("apk"); err == nil {
		return "apk", "sudo apk add --no-cache xvfb openbox dbus at-spi2-core font-noto-cjk"
	}
	_ = missing
	return "", ""
}

var (
	reservedDisplayMu sync.Mutex
	reservedDisplays  = map[string]bool{}
)

func reserveFreeDisplay() string {
	reservedDisplayMu.Lock()
	defer reservedDisplayMu.Unlock()
	for n := 99; n < 199; n++ {
		disp := fmt.Sprintf(":%d", n)
		if reservedDisplays[disp] {
			continue
		}
		sock := fmt.Sprintf("/tmp/.X11-unix/X%d", n)
		lock := fmt.Sprintf("/tmp/.X%d-lock", n)
		if _, err := os.Stat(sock); err == nil {
			continue
		}
		if _, err := os.Stat(lock); err == nil {
			continue
		}
		reservedDisplays[disp] = true
		return disp
	}
	return ":99"
}

func releaseDisplay(disp string) {
	if disp == "" {
		return
	}
	reservedDisplayMu.Lock()
	delete(reservedDisplays, disp)
	reservedDisplayMu.Unlock()
}

func parseResolution(res string) (int, int) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(res)), "x")
	if len(parts) == 2 {
		w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 == nil && err2 == nil && w >= 640 && h >= 480 && w <= 7680 && h <= 4320 {
			return w, h
		}
	}
	return 1440, 900
}

func (b *linuxVirtualBackend) Start(ctx context.Context, resolution string) error {
	b.mu.Lock()
	if b.cmd != nil && b.cmd.Process != nil && b.cmd.ProcessState == nil {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	disp := reserveFreeDisplay()
	w, h := parseResolution(resolution)
	screenArg := fmt.Sprintf("%dx%dx24", w, h)

	// Start Xvfb + openbox (+ optional at-spi-bus-launcher) inside a single isolated process group.
	// Using a shell supervisor ensures SIGSTOP/SIGCONT/SIGTERM affects the entire virtual display session.
	script := fmt.Sprintf(
		`Xvfb %s -screen 0 %s -nolisten tcp -ac &
XVFB_PID=$!
for i in $(seq 1 50); do
  if [ -S "/tmp/.X11-unix/X%s" ]; then break; fi
  sleep 0.1
done
export DISPLAY=%s
if command -v at-spi-bus-launcher >/dev/null 2>&1; then
  at-spi-bus-launcher --launch-immediately >/dev/null 2>&1 &
fi
openbox >/dev/null 2>&1 &
OB_PID=$!
trap 'kill -TERM $OB_PID $XVFB_PID 2>/dev/null || true; wait' TERM INT
wait $XVFB_PID`,
		disp, screenArg, strings.TrimPrefix(disp, ":"), disp,
	)

	var cmd *exec.Cmd
	if _, err := exec.LookPath("dbus-run-session"); err == nil {
		cmd = exec.Command("dbus-run-session", "--", "/bin/sh", "-c", script)
	} else {
		cmd = exec.Command("/bin/sh", "-c", script)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start virtual desktop: %w", err)
	}

	pgid := cmd.Process.Pid
	sock := fmt.Sprintf("/tmp/.X11-unix/X%s", strings.TrimPrefix(disp, ":"))
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			break
		}
		if _, err := os.Stat(sock); err == nil {
			ready = true
			break
		}
		time.Sleep(80 * time.Millisecond)
	}

	if !ready {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait()
		releaseDisplay(disp)
		return errors.New("Xvfb timed out; virtual display did not become ready")
	}

	b.mu.Lock()
	b.cmd = cmd
	b.pgid = pgid
	b.display = disp
	b.mu.Unlock()

	go func(c *exec.Cmd, d string) {
		_ = c.Wait()
		b.mu.Lock()
		if b.cmd == c {
			b.cmd = nil
			b.pgid = 0
			b.display = ""
		}
		b.mu.Unlock()
		releaseDisplay(d)
	}(cmd, disp)

	return nil
}

func (b *linuxVirtualBackend) Freeze() error {
	b.mu.Lock()
	pgid := b.pgid
	b.mu.Unlock()
	if pgid <= 0 {
		return nil
	}
	return syscall.Kill(-pgid, syscall.SIGSTOP)
}

func (b *linuxVirtualBackend) Resume() error {
	b.mu.Lock()
	pgid := b.pgid
	b.mu.Unlock()
	if pgid <= 0 {
		return nil
	}
	return syscall.Kill(-pgid, syscall.SIGCONT)
}

func (b *linuxVirtualBackend) Stop() error {
	b.mu.Lock()
	pgid := b.pgid
	cmd := b.cmd
	disp := b.display
	b.mu.Unlock()

	if pgid <= 0 {
		releaseDisplay(disp)
		return nil
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		if cmd != nil {
			_, _ = cmd.Process.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}

	b.mu.Lock()
	if b.cmd == cmd {
		b.cmd = nil
		b.pgid = 0
		b.display = ""
	}
	b.mu.Unlock()
	releaseDisplay(disp)
	return nil
}

func (b *linuxVirtualBackend) Env() []string {
	b.mu.Lock()
	disp := b.display
	dbus := b.dbus
	b.mu.Unlock()
	var env []string
	if disp != "" {
		env = append(env, "DISPLAY="+disp)
	}
	if dbus != "" {
		env = append(env, "DBUS_SESSION_BUS_ADDRESS="+dbus)
	}
	return env
}

func (b *linuxVirtualBackend) HasWindows(ctx context.Context) (bool, error) {
	bin, err := exec.LookPath("agent-desktop")
	if err != nil {
		return false, err
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "list-windows")
	cmd.Env = mergeEnv(os.Environ(), b.Env())
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	wins, err := parseAgentDesktopWindows(out)
	if err != nil {
		return false, err
	}
	return len(wins) > 0, nil
}

func (b *linuxVirtualBackend) Install(ctx context.Context) (*SetupResult, error) {
	missing := detectMissingVirtualDeps()
	if len(missing) == 0 {
		return &SetupResult{OK: true, AlreadyReady: true, Output: "virtual desktop dependencies are ready"}, nil
	}
	pkgMgr, manualCmd := detectLinuxPackageInstall(missing)
	if pkgMgr == "" {
		return &SetupResult{
			OK:          false,
			MissingDeps: missing,
			Error:       "no supported package manager detected (apt-get/dnf/pacman/apk); install manually: " + strings.Join(missing, ", "),
		}, nil
	}

	var installArgs []string
	switch pkgMgr {
	case "apt-get":
		installArgs = []string{"sh", "-c", "DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y xvfb openbox dbus-x11 at-spi2-core fonts-noto-cjk"}
	case "dnf":
		installArgs = []string{"dnf", "install", "-y", "xorg-x11-server-Xvfb", "openbox", "dbus-x11", "at-spi2-core", "google-noto-sans-cjk-ttc-fonts"}
	case "pacman":
		installArgs = []string{"pacman", "-S", "--noconfirm", "xorg-server-xvfb", "openbox", "dbus", "at-spi2-core", "noto-fonts-cjk"}
	case "apk":
		installArgs = []string{"apk", "add", "--no-cache", "xvfb", "openbox", "dbus", "at-spi2-core", "font-noto-cjk"}
	}

	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		cmd = exec.CommandContext(ctx, installArgs[0], installArgs[1:]...)
	} else {
		cmd = exec.CommandContext(ctx, "sudo", append([]string{"-n"}, installArgs...)...)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	outStr := strings.TrimSpace(buf.String())

	if err != nil {
		errMsg := outStr
		if errMsg == "" {
			errMsg = err.Error()
		}
		if os.Geteuid() != 0 {
			errMsg = fmt.Sprintf("automatic installation requires root or passwordless sudo (%s); run manually in terminal: %s", errMsg, manualCmd)
		}
		return &SetupResult{
			OK:             false,
			MissingDeps:    detectMissingVirtualDeps(),
			InstallCommand: manualCmd,
			Output:         outStr,
			Error:          errMsg,
		}, nil
	}

	rem := detectMissingVirtualDeps()
	return &SetupResult{
		OK:             len(rem) == 0,
		Installed:      true,
		MissingDeps:    rem,
		InstallCommand: manualCmd,
		Output:         outStr,
	}, nil
}

// parseAgentDesktopWindows extracts window entries from either a JSON array or {data:{windows:[...]}}.
func parseAgentDesktopWindows(raw []byte) ([]WindowInfo, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var direct []WindowInfo
	if err := json.Unmarshal(trimmed, &direct); err == nil {
		return direct, nil
	}
	var env struct {
		Data struct {
			Windows []WindowInfo `json:"windows"`
		} `json:"data"`
		Windows []WindowInfo `json:"windows"`
	}
	if err := json.Unmarshal(trimmed, &env); err == nil {
		if len(env.Data.Windows) > 0 {
			return env.Data.Windows, nil
		}
		return env.Windows, nil
	}
	return nil, errors.New("failed to parse agent-desktop window list")
}

var (
	linuxWakeMu   sync.Mutex
	linuxLastWake time.Time
)

// wakeLinuxNativeDisplay gently wakes an X11 display via xset dpms force on (rate-limited).
func wakeLinuxNativeDisplay() {
	if strings.TrimSpace(os.Getenv("DISPLAY")) == "" {
		return
	}
	xset, err := exec.LookPath("xset")
	if err != nil {
		return
	}
	linuxWakeMu.Lock()
	shouldRun := linuxLastWake.IsZero() || time.Since(linuxLastWake) > 5*time.Second
	if shouldRun {
		linuxLastWake = time.Now()
	}
	linuxWakeMu.Unlock()
	if !shouldRun {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, xset, "dpms", "force", "on").Run()
}
