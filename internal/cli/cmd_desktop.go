package cli

import (
	"bot-hub/internal/desktop"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// --- desktop ---

func runDesktopCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pebble desktop <task|takeover|setup|power|targets> [flags]")
	}
	action := args[0]
	rest := args[1:]
	var co connOpts

	switch action {
	case "targets":
		fs := newSubFlagSet("desktop targets", stderr, &co)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodGet, "/api/desktop/targets", nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "task":
		if err := rejectComputerUseRole("desktop task"); err != nil {
			return err
		}
		fs := newSubFlagSet("desktop task", stderr, &co)
		goal := fs.String("goal", "", "GUI goal description")
		nodeID := fs.String("node", "", "target desktop ('local' or node ID)")
		app := fs.String("app", "", "optional application name")
		timeout := fs.Int("timeout", 0, "optional timeout in seconds")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *goal == "" && fs.NArg() > 0 {
			*goal = strings.Join(fs.Args(), " ")
		}
		if strings.TrimSpace(*goal) == "" {
			return errors.New("--goal is required")
		}
		c := resolveClient(co, cwd)
		if c.SelfBotID() == "" {
			return errors.New("caller bot ID is unknown (--bot-id or PEBBLE_BOT_ID)")
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/desktop/task", map[string]any{
			"botId":      c.SelfBotID(),
			"nodeId":     *nodeID,
			"goal":       *goal,
			"app":        *app,
			"timeoutSec": *timeout,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "takeover":
		fs := newSubFlagSet("desktop takeover", stderr, &co)
		reason := fs.String("reason", "", "explanation shown to the user")
		nodeID := fs.String("node", "", "target desktop ('local' or node ID)")
		app := fs.String("app", "", "optional application name")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *reason == "" && fs.NArg() > 0 {
			*reason = strings.Join(fs.Args(), " ")
		}
		if strings.TrimSpace(*reason) == "" {
			return errors.New("--reason is required")
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/desktop/takeover/request", map[string]any{
			"target": *nodeID,
			"botId":  c.SelfBotID(),
			"reason": *reason,
			"app":    *app,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "setup":
		fs := newSubFlagSet("desktop setup", stderr, &co)
		nodeID := fs.String("node", "", "target desktop ('local' or node ID)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		target := strings.TrimSpace(*nodeID)
		if target == "" || strings.EqualFold(target, "local") {
			eng := desktop.NewEngine()
			defer eng.Close()
			res, err := eng.Setup(ctx)
			if err != nil {
				return err
			}
			b, err := json.Marshal(res)
			if err != nil {
				return err
			}
			if err := writeJSONOut(stdout, b); err != nil {
				return err
			}
			if res != nil && !res.OK {
				return errors.New(res.Error)
			}
			return nil
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/desktop/setup", map[string]any{
			"target": target,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "power":
		fs := newSubFlagSet("desktop power", stderr, &co)
		nodeID := fs.String("node", "", "target desktop ('local' or node ID)")
		action := fs.String("action", "", "power action: wake|freeze|stop|keep_awake|allow_sleep")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if strings.TrimSpace(*action) == "" && fs.NArg() > 0 {
			*action = fs.Arg(0)
		}
		if strings.TrimSpace(*action) == "" {
			return errors.New("--action is required (wake|freeze|stop|keep_awake|allow_sleep)")
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/desktop/power", map[string]any{
			"target": *nodeID,
			"action": *action,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	default:
		return fmt.Errorf("unknown desktop action %q (expected task|takeover|setup|power|targets)", action)
	}
}
