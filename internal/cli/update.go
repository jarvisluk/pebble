package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/desktophost"
	"bot-hub/internal/protocol"
	"bot-hub/internal/update"
)

func runUpdateCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	action := "check"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = strings.ToLower(strings.TrimSpace(args[0]))
		rest = args[1:]
	}

	switch action {
	case "check":
		return runUpdateCheckCmd(ctx, rest, cwd, stdout, stderr)
	case "auto":
		return runUpdateAutoCmd(ctx, rest, cwd, stdout, stderr)
	case "channel":
		return runUpdateChannelCmd(ctx, rest, cwd, stdout, stderr)
	case "proxy":
		return runUpdateProxyCmd(ctx, rest, cwd, stdout, stderr)
	default:
		return fmt.Errorf("unknown update action %q (expected check|auto|channel|proxy)", action)
	}
}

func resolveUpdateStateDir(co connOpts, cwd string) string {
	if envDir := strings.TrimSpace(os.Getenv("PEBBLE_UPDATE_STATE_DIR")); envDir != "" {
		return envDir
	}
	cfgPath := strings.TrimSpace(co.cfg)
	if cfgPath == "" {
		cfgPath = config.LookupEnv("PEBBLE_CONFIG", "BOT_HUB_CONFIG")
	}
	if cfgPath == "" && strings.TrimSpace(cwd) != "" {
		if ws := discoverWorkspaceContext(cwd); ws.cfg != "" {
			cfgPath = ws.cfg
		}
	}
	if cfgPath != "" {
		absCfg := cfgPath
		if !filepath.IsAbs(absCfg) && cwd != "" {
			absCfg = filepath.Join(cwd, absCfg)
		}
		if raw, err := os.ReadFile(absCfg); err == nil {
			var doc struct {
				StateFile string `json:"stateFile"`
			}
			if json.Unmarshal(raw, &doc) == nil && strings.TrimSpace(doc.StateFile) != "" {
				root := filepath.Dir(filepath.Dir(absCfg))
				sf := doc.StateFile
				if !filepath.IsAbs(sf) {
					sf = filepath.Join(root, sf)
				}
				return filepath.Dir(sf)
			}
		}
		return filepath.Join(filepath.Dir(filepath.Dir(absCfg)), "state")
	}
	if cwd != "" {
		if _, err := os.Stat(filepath.Join(cwd, "config", "bots.json")); err == nil {
			return filepath.Join(cwd, "state")
		}
	}
	if dir, err := desktophost.DataDir(); err == nil && dir != "" {
		return filepath.Join(dir, "state")
	}
	return filepath.Join(cwd, "state")
}

func newCLIUpdateManager(co connOpts, cwd string) *update.Manager {
	return update.NewManager(update.Options{
		StateDir:       resolveUpdateStateDir(co, cwd),
		CurrentVersion: protocol.AppVersion,
	})
}

func syncUpdateConfigToRunningHub(ctx context.Context, co connOpts, cwd string, patch update.ConfigPatch) (*update.Status, bool) {
	if strings.TrimSpace(os.Getenv("PEBBLE_UPDATE_STATE_DIR")) != "" {
		return nil, false
	}
	c := resolveClient(co, cwd)
	sctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	body, err := c.DoJSON(sctx, http.MethodPatch, "/api/update/config", patch)
	if err != nil {
		return nil, false
	}
	var st update.Status
	if json.Unmarshal(body, &st) != nil {
		return nil, false
	}
	return &st, true
}

func runUpdateCheckCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	var co connOpts
	fs := newSubFlagSet("update check", stderr, &co)
	ch := fs.String("channel", "", "release channel: stable or prerelease")
	proxy := fs.String("proxy", "", "http(s):// or socks5:// proxy URL")
	jsonOut := fs.Bool("json", false, "output result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if c := strings.ToLower(strings.TrimSpace(*ch)); c != "" && c != update.ChannelStable && c != update.ChannelPrerelease {
		return fmt.Errorf("invalid --channel %q (expected stable or prerelease)", *ch)
	}
	if _, err := update.ValidateProxyURL(*proxy); err != nil {
		return err
	}

	mgr := newCLIUpdateManager(co, cwd)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	st := mgr.CheckNow(cctx, *ch, *proxy)
	if *jsonOut {
		b, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(b))
		if err != nil {
			return err
		}
		if st.State == update.StateError {
			return errors.New(st.Error)
		}
		return nil
	}

	if st.State == update.StateError {
		return errors.New(st.Error)
	}
	if st.HasUpdate && st.Latest != nil {
		fmt.Fprintf(stdout, "Update available: %s -> %s\n", st.CurrentVersion, st.Latest.Tag)
		fmt.Fprintf(stdout, "Release: %s\n", st.Latest.URL)
		if st.Latest.Notes != "" {
			fmt.Fprintf(stdout, "\n%s\n", st.Latest.Notes)
		}
		return nil
	}
	if st.Latest != nil {
		fmt.Fprintf(stdout, "Pebble %s is up to date (latest: %s)\n", st.CurrentVersion, st.Latest.Tag)
	} else {
		fmt.Fprintf(stdout, "Pebble %s is up to date (no published releases found)\n", st.CurrentVersion)
	}
	return nil
}

func runUpdateAutoCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	var co connOpts
	fs := newSubFlagSet("update auto", stderr, &co)
	jsonOut := fs.Bool("json", false, "output result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pos := fs.Args()

	var patch update.ConfigPatch
	hasPatch := false
	for _, raw := range pos {
		arg := strings.ToLower(strings.TrimSpace(raw))
		switch arg {
		case "on", "true":
			v := true
			patch.AutoCheck = &v
			hasPatch = true
		case "off", "false":
			v := false
			patch.AutoCheck = &v
			hasPatch = true
		case "30m", "1h", "6h", "24h":
			v := arg
			patch.Interval = &v
			hasPatch = true
		default:
			return fmt.Errorf("invalid argument %q (expected on|off or 30m|1h|6h|24h)", raw)
		}
	}

	mgr := newCLIUpdateManager(co, cwd)
	var st update.Status
	if hasPatch {
		var err error
		st, err = mgr.UpdateConfig(patch)
		if err != nil {
			return err
		}
		if synced, ok := syncUpdateConfigToRunningHub(ctx, co, cwd, patch); ok {
			st = *synced
		}
	} else {
		st = mgr.Status()
	}

	if *jsonOut {
		b, _ := json.MarshalIndent(st.Config, "", "  ")
		_, err := fmt.Fprintln(stdout, string(b))
		return err
	}
	stateWord := "off"
	if st.Config.AutoCheck {
		stateWord = "on"
	}
	_, err := fmt.Fprintf(stdout, "autoCheck: %s (interval: %s)\n", stateWord, st.Config.Interval)
	return err
}

func runUpdateChannelCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	var co connOpts
	fs := newSubFlagSet("update channel", stderr, &co)
	jsonOut := fs.Bool("json", false, "output result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pos := fs.Args()
	if len(pos) > 1 {
		return errors.New("usage: pebble update channel [stable|prerelease]")
	}

	mgr := newCLIUpdateManager(co, cwd)
	var st update.Status
	if len(pos) == 1 {
		ch := strings.ToLower(strings.TrimSpace(pos[0]))
		patch := update.ConfigPatch{Channel: &ch}
		var err error
		st, err = mgr.UpdateConfig(patch)
		if err != nil {
			return err
		}
		if synced, ok := syncUpdateConfigToRunningHub(ctx, co, cwd, patch); ok {
			st = *synced
		}
	} else {
		st = mgr.Status()
	}

	if *jsonOut {
		b, _ := json.MarshalIndent(st.Config, "", "  ")
		_, err := fmt.Fprintln(stdout, string(b))
		return err
	}
	_, err := fmt.Fprintf(stdout, "channel: %s\n", st.Config.Channel)
	return err
}

func runUpdateProxyCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	var co connOpts
	fs := newSubFlagSet("update proxy", stderr, &co)
	jsonOut := fs.Bool("json", false, "output result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pos := fs.Args()
	if len(pos) > 1 {
		return errors.New("usage: pebble update proxy [<url>|off]")
	}

	mgr := newCLIUpdateManager(co, cwd)
	var st update.Status
	if len(pos) == 1 {
		val := strings.TrimSpace(pos[0])
		if strings.EqualFold(val, "off") || strings.EqualFold(val, "none") {
			val = ""
		}
		patch := update.ConfigPatch{Proxy: &val}
		var err error
		st, err = mgr.UpdateConfig(patch)
		if err != nil {
			return err
		}
		if synced, ok := syncUpdateConfigToRunningHub(ctx, co, cwd, patch); ok {
			st = *synced
		}
	} else {
		st = mgr.Status()
	}

	if *jsonOut {
		b, _ := json.MarshalIndent(st.Config, "", "  ")
		_, err := fmt.Fprintln(stdout, string(b))
		return err
	}
	if st.Config.Proxy == "" {
		_, err := fmt.Fprintln(stdout, "proxy: off")
		return err
	}
	_, err := fmt.Fprintf(stdout, "proxy: %s\n", st.Config.Proxy)
	return err
}
