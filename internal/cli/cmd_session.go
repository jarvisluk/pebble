package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// --- session ---

func runSessionCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pebble session <list|recall|rotate> [flags]")
	}
	action := args[0]
	rest := args[1:]
	var co connOpts

	switch action {
	case "list":
		fs := newSubFlagSet("session list", stderr, &co)
		botID := fs.String("bot", "", "target bot ID (defaults to current bot; '*' for all)")
		query := fs.String("query", "", "keyword filter")
		limit := fs.Int("limit", 0, "max sessions to return")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*botID)
		q := url.Values{}
		if target != "" {
			q.Set("botId", target)
		}
		if c.SelfBotID() != "" {
			q.Set("callerBotId", c.SelfBotID())
		}
		if strings.TrimSpace(*query) != "" {
			q.Set("query", strings.TrimSpace(*query))
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		path := "/api/sessions"
		if enc := q.Encode(); enc != "" {
			path += "?" + enc
		}
		body, err := c.DoJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "recall":
		fs := newSubFlagSet("session recall", stderr, &co)
		sid := fs.String("id", "previous", "session ID, 'previous', or 'active'")
		botID := fs.String("bot", "", "target bot ID")
		tail := fs.Int("tail", 0, "return last N messages")
		offset := fs.Int("offset", 0, "zero-based offset")
		limit := fs.Int("limit", 0, "page size")
		query := fs.String("query", "", "keyword filter")
		maxChars := fs.Int("max-chars", 0, "per-message char limit")
		msgID := fs.String("message-id", "", "single message ID to fetch untruncated")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		sessionID := strings.TrimSpace(*sid)
		if sessionID == "" {
			sessionID = "previous"
		}
		targetBot := strings.TrimSpace(*botID)
		if targetBot == "" && sessionID == "previous" {
			targetBot = c.SelfBotID()
		}
		q := url.Values{}
		if targetBot != "" {
			q.Set("botId", targetBot)
		}
		if c.SelfBotID() != "" {
			q.Set("callerBotId", c.SelfBotID())
		}
		if *tail > 0 {
			q.Set("tail", strconv.Itoa(*tail))
		}
		if *offset > 0 {
			q.Set("offset", strconv.Itoa(*offset))
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		if strings.TrimSpace(*query) != "" {
			q.Set("query", strings.TrimSpace(*query))
		}
		if *maxChars > 0 {
			q.Set("maxChars", strconv.Itoa(*maxChars))
		}
		if strings.TrimSpace(*msgID) != "" {
			q.Set("messageId", strings.TrimSpace(*msgID))
		}
		path := "/api/sessions/" + url.PathEscape(sessionID)
		if enc := q.Encode(); enc != "" {
			path += "?" + enc
		}
		body, err := c.DoJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "rotate":
		fs := newSubFlagSet("session rotate", stderr, &co)
		botID := fs.String("bot", "", "target bot ID (defaults to current bot)")
		handoff := fs.String("handoff", "", "optional handoff note for next session")
		deferRotate := fs.Bool("defer", true, "archive after current turn completes")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*botID)
		if target == "" {
			return errors.New("bot ID is required (--bot or PEBBLE_BOT_ID)")
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(target)+"/rotate", map[string]any{
			"handoffNote": *handoff,
			"defer":       *deferRotate,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	default:
		return fmt.Errorf("unknown session action %q (expected list|recall|rotate)", action)
	}
}
