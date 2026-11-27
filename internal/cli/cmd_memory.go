package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func runMemoryCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pebble memory <search|list|get|save|forget|status|dream|reindex|import-magpie> [flags]")
	}
	action := args[0]
	rest := args[1:]
	var co connOpts

	switch action {
	case "search", "list":
		fs := newSubFlagSet("memory "+action, stderr, &co)
		query := fs.String("query", "", "search query")
		scope := fs.String("scope", "", "scope: global, bot, or all")
		botID := fs.String("bot", "", "target bot ID")
		kind := fs.String("kind", "", "kind filter")
		var tags multiStringFlag
		fs.Var(&tags, "tags", "tag filter (comma-separated or repeatable)")
		status := fs.String("status", "", "status filter: active|candidate|superseded|archived|all")
		includeSessions := fs.Bool("include-sessions", false, "include historical chat session matches")
		includeDaily := fs.Bool("include-daily", false, "include daily log entries")
		compact := fs.Bool("compact", false, "return compact L1 snippets")
		limit := fs.Int("limit", 0, "max items to return")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		qStr := strings.TrimSpace(*query)
		if action == "search" && qStr == "" && fs.NArg() > 0 {
			qStr = strings.TrimSpace(strings.Join(fs.Args(), " "))
		}
		c := resolveClient(co, cwd)
		q := url.Values{}
		sc := strings.TrimSpace(*scope)
		targetBot := strings.TrimSpace(*botID)
		if sc == "" && targetBot == "" {
			sc = "all"
		}
		if sc == "bot" && targetBot == "" {
			targetBot = c.SelfBotID()
		}
		if sc != "" {
			q.Set("scope", sc)
		}
		if targetBot != "" {
			q.Set("botId", targetBot)
		}
		if c.SelfBotID() != "" {
			q.Set("callerBotId", c.SelfBotID())
		}
		if qStr != "" {
			q.Set("query", qStr)
		}
		if strings.TrimSpace(*kind) != "" {
			q.Set("kind", strings.TrimSpace(*kind))
		}
		if flat := flattenTagFlags(tags); len(flat) > 0 {
			q.Set("tags", strings.Join(flat, ","))
		}
		if strings.TrimSpace(*status) != "" {
			q.Set("status", strings.TrimSpace(*status))
		}
		if *includeSessions {
			q.Set("includeSessions", "1")
		}
		if *includeDaily {
			q.Set("includeDaily", "1")
		}
		if *compact {
			q.Set("compact", "1")
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		path := "/api/memory"
		if enc := q.Encode(); enc != "" {
			path += "?" + enc
		}
		body, err := c.DoJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "get":
		fs := newSubFlagSet("memory get", stderr, &co)
		id := fs.String("id", "", "memory item ID")
		rawPath := fs.String("path", "", "workspace-relative memory file path")
		scope := fs.String("scope", "", "scope for relative paths")
		botID := fs.String("bot", "", "target bot ID")
		startLine := fs.Int("start-line", 0, "1-based start line")
		endLine := fs.Int("end-line", 0, "1-based end line")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		itemID := strings.TrimSpace(*id)
		filePath := strings.TrimSpace(*rawPath)
		if itemID == "" && filePath == "" && fs.NArg() > 0 {
			itemID = strings.TrimSpace(fs.Arg(0))
		}
		if itemID == "" && filePath == "" {
			return errors.New("--id or --path is required")
		}
		c := resolveClient(co, cwd)
		seg := itemID
		if seg == "" {
			seg = "_"
		}
		q := url.Values{}
		if filePath != "" {
			q.Set("path", filePath)
		}
		if strings.TrimSpace(*scope) != "" {
			q.Set("scope", strings.TrimSpace(*scope))
		}
		targetBot := strings.TrimSpace(*botID)
		if targetBot == "" && itemID == "" && c.SelfBotID() != "" {
			targetBot = c.SelfBotID()
		}
		if targetBot != "" {
			q.Set("botId", targetBot)
		}
		if c.SelfBotID() != "" {
			q.Set("callerBotId", c.SelfBotID())
		}
		if *startLine > 0 {
			q.Set("startLine", strconv.Itoa(*startLine))
		}
		if *endLine > 0 {
			q.Set("endLine", strconv.Itoa(*endLine))
		}
		apiPath := "/api/memory/" + url.PathEscape(seg)
		if enc := q.Encode(); enc != "" {
			apiPath += "?" + enc
		}
		body, err := c.DoJSON(ctx, http.MethodGet, apiPath, nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "save":
		if err := rejectComputerUseRole("memory save"); err != nil {
			return err
		}
		fs := newSubFlagSet("memory save", stderr, &co)
		id := fs.String("id", "", "existing memory ID to update")
		scope := fs.String("scope", "", "scope: bot (default when bot known) or global")
		botID := fs.String("bot", "", "target bot ID")
		kind := fs.String("kind", "", "kind: profile|preference|rule|fact|decision|procedure|reference|episode")
		title := fs.String("title", "", "short title")
		text := fs.String("text", "", "memory body text")
		textFile := fs.String("text-file", "", "read memory body text from file")
		why := fs.String("why", "", "rationale or source context")
		var tags multiStringFlag
		fs.Var(&tags, "tags", "tags (comma-separated or repeatable)")
		pin := fs.Bool("pin", false, "pin memory item")
		status := fs.String("status", "", "status: active|candidate")
		validFrom := fs.String("valid-from", "", "optional RFC3339 validFrom timestamp")
		validUntil := fs.String("valid-until", "", "optional RFC3339 validUntil timestamp")
		supersedes := fs.String("supersedes", "", "existing memory ID superseded by this item")
		daily := fs.Bool("daily", false, "append to daily log")
		date := fs.String("date", "", "optional YYYY-MM-DD date for --daily")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		setFlags := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

		bodyText, hasText, err := readTextInput(*text, *textFile, cwd, setFlags["text"], setFlags["text-file"], "text")
		if err != nil {
			return err
		}
		if !hasText && fs.NArg() > 0 {
			bodyText = strings.Join(fs.Args(), " ")
			hasText = true
		}
		if !hasText || strings.TrimSpace(bodyText) == "" {
			return errors.New("--text or --text-file is required")
		}

		c := resolveClient(co, cwd)
		sc := strings.TrimSpace(*scope)
		targetBot := strings.TrimSpace(*botID)
		if sc == "" {
			if targetBot != "" || c.SelfBotID() != "" {
				sc = "bot"
			} else {
				sc = "global"
			}
		}
		if !strings.EqualFold(sc, "global") && targetBot == "" {
			targetBot = c.SelfBotID()
		}

		payload := map[string]any{
			"scope":        sc,
			"text":         bodyText,
			"sourceAuthor": "agent",
		}
		if c.SelfBotID() != "" {
			payload["callerBotId"] = c.SelfBotID()
		}
		if targetBot != "" && !strings.EqualFold(sc, "global") {
			payload["botId"] = targetBot
		}
		if strings.TrimSpace(*id) != "" {
			payload["id"] = strings.TrimSpace(*id)
		}
		if strings.TrimSpace(*kind) != "" {
			payload["kind"] = strings.TrimSpace(*kind)
		}
		if strings.TrimSpace(*title) != "" {
			payload["title"] = strings.TrimSpace(*title)
		}
		if strings.TrimSpace(*why) != "" {
			payload["why"] = strings.TrimSpace(*why)
		}
		if flat := flattenTagFlags(tags); len(flat) > 0 {
			payload["tags"] = flat
		}
		if setFlags["pin"] {
			payload["pin"] = *pin
		}
		if strings.TrimSpace(*status) != "" {
			payload["status"] = strings.TrimSpace(*status)
		}
		if strings.TrimSpace(*validFrom) != "" {
			payload["validFrom"] = strings.TrimSpace(*validFrom)
		}
		if strings.TrimSpace(*validUntil) != "" {
			payload["validUntil"] = strings.TrimSpace(*validUntil)
		}
		if strings.TrimSpace(*supersedes) != "" {
			payload["supersedes"] = strings.TrimSpace(*supersedes)
		}
		if *daily {
			payload["daily"] = true
		}
		if strings.TrimSpace(*date) != "" {
			payload["date"] = strings.TrimSpace(*date)
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/memory", payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "forget":
		if err := rejectComputerUseRole("memory forget"); err != nil {
			return err
		}
		fs := newSubFlagSet("memory forget", stderr, &co)
		id := fs.String("id", "", "memory item ID")
		reason := fs.String("reason", "", "optional retirement reason")
		hard := fs.Bool("hard", false, "permanently delete instead of soft-archiving")
		dismiss := fs.Bool("dismiss", false, "record dismissal fingerprint so Dream will not re-add")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		itemID := strings.TrimSpace(*id)
		if itemID == "" && fs.NArg() > 0 {
			itemID = strings.TrimSpace(fs.Arg(0))
		}
		if itemID == "" {
			return errors.New("--id is required")
		}
		c := resolveClient(co, cwd)
		q := url.Values{}
		if c.SelfBotID() != "" {
			q.Set("callerBotId", c.SelfBotID())
		}
		if strings.TrimSpace(*reason) != "" {
			q.Set("reason", strings.TrimSpace(*reason))
		}
		if *hard {
			q.Set("hard", "1")
		}
		if *dismiss {
			q.Set("dismiss", "1")
		}
		apiPath := "/api/memory/" + url.PathEscape(itemID)
		if enc := q.Encode(); enc != "" {
			apiPath += "?" + enc
		}
		body, err := c.DoJSON(ctx, http.MethodDelete, apiPath, nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "status":
		fs := newSubFlagSet("memory status", stderr, &co)
		botID := fs.String("bot", "", "optional bot ID filter")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		path := "/api/memory/status"
		if b := strings.TrimSpace(*botID); b != "" {
			path += "?botId=" + url.QueryEscape(b)
		}
		body, err := c.DoJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "dream":
		if err := rejectComputerUseRole("memory dream"); err != nil {
			return err
		}
		fs := newSubFlagSet("memory dream", stderr, &co)
		botID := fs.String("bot", "", "optional single bot ID (omit for all bots)")
		force := fs.Bool("force", true, "run even if below minNewMessages threshold")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		payload := map[string]any{
			"force": *force,
		}
		if b := strings.TrimSpace(*botID); b != "" {
			payload["botId"] = b
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/memory/dream", payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "reindex":
		if err := rejectComputerUseRole("memory reindex"); err != nil {
			return err
		}
		fs := newSubFlagSet("memory reindex", stderr, &co)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/memory/reindex", map[string]any{})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "import-magpie":
		if err := rejectComputerUseRole("memory import-magpie"); err != nil {
			return err
		}
		fs := newSubFlagSet("memory import-magpie", stderr, &co)
		mode := fs.String("mode", "bots_only", "import mode: bots_only or all")
		botID := fs.String("bot", "", "optional single bot ID filter")
		dryRun := fs.Bool("dry-run", false, "preview import without writing files")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		payload := map[string]any{
			"mode":   strings.TrimSpace(*mode),
			"dryRun": *dryRun,
		}
		if b := strings.TrimSpace(*botID); b != "" {
			payload["botId"] = b
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/memory/import/magpie", payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	default:
		return fmt.Errorf("unknown memory action %q (expected search|list|get|save|forget|status|dream|reindex|import-magpie)", action)
	}
}

func flattenTagFlags(tags multiStringFlag) []string {
	var out []string
	for _, entry := range tags {
		for _, part := range strings.Split(entry, ",") {
			if t := strings.TrimSpace(part); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}
