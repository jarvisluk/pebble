package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// --- routine ---

func runRoutineCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pebble routine <list|upsert|delete|run> [flags]")
	}
	action := args[0]
	rest := args[1:]
	var co connOpts

	switch action {
	case "list":
		fs := newSubFlagSet("routine list", stderr, &co)
		botID := fs.String("bot", "", "target bot ID (defaults to current bot)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*botID)
		if target == "" {
			return errors.New("bot ID is required (--bot or PEBBLE_BOT_ID)")
		}
		body, err := c.DoJSON(ctx, http.MethodGet, "/api/bots/"+url.PathEscape(target)+"/routines", nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "upsert":
		fs := newSubFlagSet("routine upsert", stderr, &co)
		botID := fs.String("bot", "", "target bot ID (defaults to current bot)")
		id := fs.String("id", "", "optional routine ID")
		name := fs.String("name", "", "routine name")
		everyMin := fs.Int("every-minutes", 0, "run every N minutes")
		dailyAt := fs.String("daily-at", "", "run daily at HH:MM")
		cronExpr := fs.String("cron", "", "cron expression (5-field, 6-field with seconds, or @every)")
		webhookTrigger := fs.Bool("webhook-trigger", false, "allow inbound webhook trigger")
		webhookSecret := fs.String("webhook-secret", "", "optional webhook secret")
		webhookURL := fs.String("webhook-url", "", "optional outbound webhook URL")
		prompt := fs.String("prompt", "", "task prompt")
		model := fs.String("model", "", "optional model override")
		delivery := fs.String("main-delivery", "", "receipt|notify|none")
		enabled := fs.Bool("enabled", true, "whether routine is enabled")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
		if strings.TrimSpace(*name) == "" || strings.TrimSpace(*prompt) == "" {
			return errors.New("--name and --prompt are required")
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*botID)
		if target == "" {
			return errors.New("bot ID is required (--bot or PEBBLE_BOT_ID)")
		}
		payload := map[string]any{
			"name":   strings.TrimSpace(*name),
			"prompt": *prompt,
		}
		if visited["id"] {
			payload["id"] = strings.TrimSpace(*id)
		}
		if visited["every-minutes"] {
			payload["everyMinutes"] = *everyMin
		}
		if visited["daily-at"] {
			payload["dailyAt"] = strings.TrimSpace(*dailyAt)
		}
		if visited["cron"] {
			payload["cron"] = strings.TrimSpace(*cronExpr)
		}
		if visited["webhook-trigger"] {
			payload["webhookTrigger"] = *webhookTrigger
		}
		if visited["webhook-secret"] {
			payload["webhookSecret"] = strings.TrimSpace(*webhookSecret)
		}
		if visited["webhook-url"] {
			payload["webhookUrl"] = strings.TrimSpace(*webhookURL)
		}
		if visited["model"] {
			payload["model"] = strings.TrimSpace(*model)
		}
		if visited["main-delivery"] {
			payload["mainDelivery"] = strings.TrimSpace(*delivery)
		}
		if visited["enabled"] {
			payload["enabled"] = *enabled
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(target)+"/routines?upsert=1", payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "delete":
		fs := newSubFlagSet("routine delete", stderr, &co)
		botID := fs.String("bot", "", "target bot ID (defaults to current bot)")
		id := fs.String("id", "", "routine ID")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		if strings.TrimSpace(*id) == "" {
			return errors.New("--id is required")
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*botID)
		if target == "" {
			return errors.New("bot ID is required (--bot or PEBBLE_BOT_ID)")
		}
		body, err := c.DoJSON(ctx, http.MethodDelete, "/api/bots/"+url.PathEscape(target)+"/routines/"+url.PathEscape(strings.TrimSpace(*id)), nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "run":
		fs := newSubFlagSet("routine run", stderr, &co)
		botID := fs.String("bot", "", "target bot ID (defaults to current bot)")
		id := fs.String("id", "", "routine ID")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		if strings.TrimSpace(*id) == "" {
			return errors.New("--id is required")
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*botID)
		if target == "" {
			return errors.New("bot ID is required (--bot or PEBBLE_BOT_ID)")
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(target)+"/routines/"+url.PathEscape(strings.TrimSpace(*id))+"/run", nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	default:
		return fmt.Errorf("unknown routine action %q (expected list|upsert|delete|run)", action)
	}
}
