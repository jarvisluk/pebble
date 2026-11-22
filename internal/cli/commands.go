package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"bot-hub/internal/desktop"
	"bot-hub/internal/mcp"
)

type connOpts struct {
	hubURL string
	botID  string
	cfg    string
	token  string
}

func registerConnFlags(fs *flag.FlagSet, o *connOpts) {
	fs.StringVar(&o.hubURL, "hub-url", "", "running bot-hub HTTP base URL")
	fs.StringVar(&o.botID, "bot-id", "", "caller bot ID")
	fs.StringVar(&o.cfg, "config", "", "path to bots.json config file")
	fs.StringVar(&o.token, "token", "", "bot-hub API token")
}

// runCommand executes `bot-hub <bot|session|routine|node|desktop> ...` and
// returns a process exit code. Separated from os.Exit for unit testing.
func runCommand(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing command")
		return 1
	}
	sub := args[0]
	rest := args[1:]

	var err error
	switch sub {
	case "bot":
		err = runBotCmd(ctx, rest, cwd, stdout, stderr)
	case "session":
		err = runSessionCmd(ctx, rest, cwd, stdout, stderr)
	case "routine":
		err = runRoutineCmd(ctx, rest, cwd, stdout, stderr)
	case "node":
		err = runNodeCmd(ctx, rest, cwd, stdout, stderr)
	case "desktop":
		err = runDesktopCmd(ctx, rest, cwd, stdout, stderr)
	default:
		err = fmt.Errorf("unknown command %q", sub)
	}
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(stderr, "error: %v\n", err)
		}
		return 1
	}
	return 0
}

func resolveClient(o connOpts, cwd string) *mcp.Client {
	hubURL := strings.TrimSpace(o.hubURL)
	if hubURL == "" {
		hubURL = strings.TrimSpace(os.Getenv("BOT_HUB_URL"))
	}
	botID := strings.TrimSpace(o.botID)
	if botID == "" {
		botID = strings.TrimSpace(os.Getenv("BOT_HUB_BOT_ID"))
	}
	cfgPath := strings.TrimSpace(o.cfg)
	if cfgPath == "" {
		cfgPath = strings.TrimSpace(os.Getenv("BOT_HUB_CONFIG"))
	}
	tok := strings.TrimSpace(o.token)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("BOT_HUB_API_TOKEN"))
	}

	if hubURL == "" || botID == "" || cfgPath == "" || tok == "" {
		ws := discoverWorkspaceContext(cwd)
		if hubURL == "" {
			hubURL = ws.hubURL
		}
		if botID == "" {
			botID = ws.botID
		}
		if cfgPath == "" {
			cfgPath = ws.cfg
		}
		if tok == "" {
			tok = ws.token
		}
	}
	return mcp.NewClient(hubURL, botID, cfgPath, tok)
}

func discoverWorkspaceContext(cwd string) connOpts {
	var out connOpts
	if strings.TrimSpace(cwd) == "" {
		return out
	}

	// 1. opencode.json -> mcp.servers.bot-hub
	if raw, err := os.ReadFile(filepath.Join(cwd, "opencode.json")); err == nil {
		var doc struct {
			MCP struct {
				Servers map[string]struct {
					Command     []string          `json:"command"`
					Environment map[string]string `json:"environment"`
				} `json:"servers"`
			} `json:"mcp"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			if srv, ok := doc.MCP.Servers["bot-hub"]; ok {
				parseMCPArgs(srv.Command, &out)
				if out.token == "" {
					out.token = strings.TrimSpace(srv.Environment["BOT_HUB_API_TOKEN"])
				}
				return out
			}
		}
	}

	// 2. .agents/mcp_config.json (agy) or .pi/mcp.json (pi) -> mcpServers.bot-hub
	for _, rel := range []string{filepath.Join(".agents", "mcp_config.json"), filepath.Join(".pi", "mcp.json")} {
		raw, err := os.ReadFile(filepath.Join(cwd, rel))
		if err != nil {
			continue
		}
		var doc struct {
			MCPServers map[string]struct {
				Command string            `json:"command"`
				Args    []string          `json:"args"`
				Env     map[string]string `json:"env"`
			} `json:"mcpServers"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			if srv, ok := doc.MCPServers["bot-hub"]; ok {
				parseMCPArgs(append([]string{srv.Command}, srv.Args...), &out)
				if out.token == "" {
					out.token = strings.TrimSpace(srv.Env["BOT_HUB_API_TOKEN"])
				}
				return out
			}
		}
	}
	return out
}

func parseMCPArgs(argv []string, out *connOpts) {
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		take := func(flagName string) (string, bool) {
			if arg == flagName && i+1 < len(argv) {
				i++
				return argv[i], true
			}
			if strings.HasPrefix(arg, flagName+"=") {
				return strings.TrimPrefix(arg, flagName+"="), true
			}
			return "", false
		}
		if v, ok := take("--hub-url"); ok && out.hubURL == "" {
			out.hubURL = strings.TrimSpace(v)
		} else if v, ok := take("--bot-id"); ok && out.botID == "" {
			out.botID = strings.TrimSpace(v)
		} else if v, ok := take("--config"); ok && out.cfg == "" {
			out.cfg = strings.TrimSpace(v)
		}
	}
}

func newSubFlagSet(name string, stderr io.Writer, co *connOpts) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	registerConnFlags(fs, co)
	return fs
}

func writeJSONOut(w io.Writer, raw []byte) error {
	_, err := fmt.Fprintln(w, mcp.PrettyJSON(raw))
	return err
}

func readInstructionsInput(inline, filePath, cwd string, inlineSet, fileSet bool) (string, bool, error) {
	if inlineSet && fileSet {
		return "", false, errors.New("cannot specify both --instructions and --instructions-file")
	}
	if fileSet {
		p := strings.TrimSpace(filePath)
		if p == "" {
			return "", false, errors.New("--instructions-file cannot be empty")
		}
		if !filepath.IsAbs(p) && cwd != "" {
			p = filepath.Join(cwd, p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", false, fmt.Errorf("read instructions file: %w", err)
		}
		return string(b), true, nil
	}
	if inlineSet {
		return inline, true, nil
	}
	return "", false, nil
}

// --- bot ---

func runBotCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: bot-hub bot <list|get|create|update|delete|message> [flags]")
	}
	action := args[0]
	rest := args[1:]
	var co connOpts

	switch action {
	case "list":
		fs := newSubFlagSet("bot list", stderr, &co)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodGet, "/api/bots", nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "get":
		fs := newSubFlagSet("bot get", stderr, &co)
		id := fs.String("id", "", "bot ID (defaults to current bot)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*id)
		if target == "" {
			return errors.New("bot ID is required (--id or BOT_HUB_BOT_ID)")
		}
		body, err := c.DoJSON(ctx, http.MethodGet, "/api/bots/"+url.PathEscape(target), nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "create":
		fs := newSubFlagSet("bot create", stderr, &co)
		id := fs.String("id", "", "unique bot ID")
		name := fs.String("name", "", "display name")
		desc := fs.String("description", "", "bot description")
		instr := fs.String("instructions", "", "AGENTS.md markdown content")
		instrFile := fs.String("instructions-file", "", "path to markdown file for AGENTS.md")
		model := fs.String("model", "", "model override")
		harnessName := fs.String("harness", "", "agent runtime (opencode|pi|dsh|claude|agy)")
		priv := fs.Bool("private-sessions", false, "enable private sessions")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
		if strings.TrimSpace(*id) == "" || strings.TrimSpace(*name) == "" {
			return errors.New("--id and --name are required")
		}
		instructions, hasInstr, err := readInstructionsInput(*instr, *instrFile, cwd, visited["instructions"], visited["instructions-file"])
		if err != nil {
			return err
		}
		payload := map[string]any{
			"id":   strings.TrimSpace(*id),
			"name": strings.TrimSpace(*name),
		}
		if visited["description"] {
			payload["description"] = *desc
		}
		if hasInstr {
			payload["instructions"] = instructions
		}
		if visited["model"] {
			payload["model"] = *model
		}
		if visited["harness"] {
			payload["harness"] = *harnessName
		}
		if visited["private-sessions"] {
			payload["privateSessions"] = *priv
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/bots", payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "update":
		fs := newSubFlagSet("bot update", stderr, &co)
		id := fs.String("id", "", "bot ID (defaults to current bot)")
		name := fs.String("name", "", "new display name")
		desc := fs.String("description", "", "new description")
		instr := fs.String("instructions", "", "new AGENTS.md markdown content")
		instrFile := fs.String("instructions-file", "", "path to markdown file for AGENTS.md")
		model := fs.String("model", "", "model override (empty string inherits hub default)")
		harnessName := fs.String("harness", "", "agent runtime (opencode|pi|dsh|claude|agy)")
		priv := fs.Bool("private-sessions", false, "enable private sessions")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
		instructions, hasInstr, err := readInstructionsInput(*instr, *instrFile, cwd, visited["instructions"], visited["instructions-file"])
		if err != nil {
			return err
		}
		payload := map[string]any{}
		if visited["name"] {
			payload["name"] = *name
		}
		if visited["description"] {
			payload["description"] = *desc
		}
		if hasInstr {
			payload["instructions"] = instructions
		}
		if visited["model"] {
			payload["model"] = *model
		}
		if visited["harness"] {
			payload["harness"] = *harnessName
		}
		if visited["private-sessions"] {
			payload["privateSessions"] = *priv
		}
		if len(payload) == 0 {
			return errors.New("specify at least one field to update (--name, --description, --instructions, --instructions-file, --model, --harness, --private-sessions)")
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*id)
		if target == "" {
			return errors.New("bot ID is required (--id or BOT_HUB_BOT_ID)")
		}
		body, err := c.DoJSON(ctx, http.MethodPatch, "/api/bots/"+url.PathEscape(target), payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "delete":
		fs := newSubFlagSet("bot delete", stderr, &co)
		id := fs.String("id", "", "bot ID to delete")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		target := strings.TrimSpace(*id)
		if target == "" {
			return errors.New("--id is required")
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodDelete, "/api/bots/"+url.PathEscape(target), nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "message":
		fs := newSubFlagSet("bot message", stderr, &co)
		id := fs.String("id", "", "target bot ID")
		msg := fs.String("message", "", "message text")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
			if *msg == "" && fs.NArg() > 1 {
				*msg = strings.Join(fs.Args()[1:], " ")
			}
		}
		if strings.TrimSpace(*id) == "" || strings.TrimSpace(*msg) == "" {
			return errors.New("--id and --message are required")
		}
		c := resolveClient(co, cwd)
		hop, _ := strconv.Atoi(os.Getenv("BOT_HUB_HOP"))
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(strings.TrimSpace(*id))+"/message", map[string]any{
			"message":   *msg,
			"fromBotId": c.SelfBotID(),
			"hop":       hop,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	default:
		return fmt.Errorf("unknown bot action %q (expected list|get|create|update|delete|message)", action)
	}
}

// --- session ---

func runSessionCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: bot-hub session <list|recall|rotate> [flags]")
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
			return errors.New("bot ID is required (--bot or BOT_HUB_BOT_ID)")
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

// --- routine ---

func runRoutineCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: bot-hub routine <list|upsert|delete|run> [flags]")
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
			return errors.New("bot ID is required (--bot or BOT_HUB_BOT_ID)")
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
			return errors.New("bot ID is required (--bot or BOT_HUB_BOT_ID)")
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
			return errors.New("bot ID is required (--bot or BOT_HUB_BOT_ID)")
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
			return errors.New("bot ID is required (--bot or BOT_HUB_BOT_ID)")
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

// --- node ---

func runNodeCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: bot-hub node <list|exec|file> [flags]")
	}
	action := args[0]
	rest := args[1:]
	var co connOpts

	switch action {
	case "list":
		fs := newSubFlagSet("node list", stderr, &co)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodGet, "/api/nodes", nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "exec":
		fs := newSubFlagSet("node exec", stderr, &co)
		nodeID := fs.String("node", "", "target node ID (optional if only one connected)")
		cmdStr := fs.String("command", "", "shell command to execute")
		cmdCwd := fs.String("cwd", "", "remote working directory")
		timeout := fs.Int("timeout", 0, "timeout in seconds")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *cmdStr == "" && fs.NArg() > 0 {
			*cmdStr = strings.Join(fs.Args(), " ")
		}
		if strings.TrimSpace(*cmdStr) == "" {
			return errors.New("--command is required")
		}
		c := resolveClient(co, cwd)
		text, isErr := c.CallNode(ctx, *nodeID, "exec", map[string]any{
			"command": *cmdStr,
			"cwd":     *cmdCwd,
		}, *timeout)
		if isErr {
			return errors.New(text)
		}
		_, err := fmt.Fprintln(stdout, text)
		return err

	case "file":
		fs := newSubFlagSet("node file", stderr, &co)
		nodeID := fs.String("node", "", "target node ID")
		op := fs.String("op", "", "read|write|list")
		path := fs.String("path", "", "remote file or directory path")
		content := fs.String("content", "", "content for write")
		contentFile := fs.String("content-file", "", "local file whose content to write")
		appendMode := fs.Bool("append", false, "append instead of overwrite")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
		actionMap := map[string]string{
			"read":  "file_read",
			"write": "file_write",
			"list":  "file_list",
		}
		rpcAction := actionMap[strings.TrimSpace(*op)]
		if rpcAction == "" || strings.TrimSpace(*path) == "" {
			return errors.New("--op (read|write|list) and --path are required")
		}
		writeContent := *content
		if visited["content-file"] {
			p := strings.TrimSpace(*contentFile)
			if !filepath.IsAbs(p) && cwd != "" {
				p = filepath.Join(cwd, p)
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read content file: %w", err)
			}
			writeContent = string(b)
		}
		c := resolveClient(co, cwd)
		text, isErr := c.CallNode(ctx, *nodeID, rpcAction, map[string]any{
			"path":    *path,
			"content": writeContent,
			"append":  *appendMode,
		}, 0)
		if isErr {
			return errors.New(text)
		}
		_, err := fmt.Fprintln(stdout, text)
		return err

	default:
		return fmt.Errorf("unknown node action %q (expected list|exec|file)", action)
	}
}

// --- desktop ---

func runDesktopCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: bot-hub desktop <task|takeover|setup|power> [flags]")
	}
	action := args[0]
	rest := args[1:]
	var co connOpts

	switch action {
	case "task":
		if strings.TrimSpace(os.Getenv("BOT_HUB_MCP_ROLE")) == "computer_use" {
			return errors.New("desktop task cannot be called from inside a computer-use worker")
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
			return errors.New("caller bot ID is unknown (--bot-id or BOT_HUB_BOT_ID)")
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
		return fmt.Errorf("unknown desktop action %q (expected task|takeover|setup|power)", action)
	}
}
