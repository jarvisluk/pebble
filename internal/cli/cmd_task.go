package cli

import (
	"bot-hub/internal/config"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// --- task ---

func runTaskCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pebble task <list|get|spawn|read|send|cancel|approve|diff|rollback> [flags]")
	}
	action := args[0]
	rest := args[1:]
	var co connOpts

	switch action {
	case "list":
		fs := newSubFlagSet("task list", stderr, &co)
		botID := fs.String("bot", "", "target bot ID (defaults to current bot)")
		kind := fs.String("kind", "", "filter by kind (worker|routine|desktop)")
		status := fs.String("status", "", "filter by status")
		limit := fs.Int("limit", 0, "max tasks to return")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*botID)
		if target == "" {
			return errors.New("bot ID is required (--bot or PEBBLE_BOT_ID)")
		}
		q := url.Values{}
		if strings.TrimSpace(*kind) != "" {
			q.Set("kind", strings.TrimSpace(*kind))
		}
		if strings.TrimSpace(*status) != "" {
			q.Set("status", strings.TrimSpace(*status))
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		path := "/api/bots/" + url.PathEscape(target) + "/tasks"
		if enc := q.Encode(); enc != "" {
			path += "?" + enc
		}
		body, err := c.DoJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "get":
		fs := newSubFlagSet("task get", stderr, &co)
		id := fs.String("id", "", "task ID")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		tid := strings.TrimSpace(*id)
		if tid == "" {
			return errors.New("--id is required")
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodGet, "/api/tasks/"+url.PathEscape(tid), nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "spawn":
		fs := newSubFlagSet("task spawn", stderr, &co)
		botID := fs.String("bot", "", "target bot ID (defaults to current bot)")
		title := fs.String("title", "", "optional task title")
		prompt := fs.String("prompt", "", "task prompt")
		promptFile := fs.String("prompt-file", "", "path to file containing prompt")
		kind := fs.String("kind", "", "task kind (worker|desktop)")
		device := fs.String("device", "", "target device ID (default local)")
		workspaceID := fs.String("workspace-id", "", "optional authorized workspace ID on a remote node")
		isolation := fs.String("isolation", "", "optional code isolation mode (current|worktree|temp_dir)")
		harnessName := fs.String("harness", "", "optional harness override")
		model := fs.String("model", "", "optional model override")
		permMode := fs.String("permission-mode", "", "optional permission mode (auto|edit|ask|plan)")
		workdir := fs.String("workdir", "", "optional working directory")
		delivery := fs.String("delivery", "", "delivery mode (wake|receipt|silent)")
		var dependsOn multiStringFlag
		fs.Var(&dependsOn, "depends-on", "parent task ID dependency (repeatable or comma-separated)")
		parentTask := fs.String("parent-task", "", "optional parent task ID")
		timeout := fs.Int("timeout", 0, "optional timeout in seconds")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		pText, hasPrompt, err := readTextInput(*prompt, *promptFile, cwd, *prompt != "", *promptFile != "", "prompt")
		if err != nil {
			return err
		}
		if !hasPrompt && fs.NArg() > 0 {
			pText = strings.Join(fs.Args(), " ")
			hasPrompt = strings.TrimSpace(pText) != ""
		}
		if !hasPrompt || strings.TrimSpace(pText) == "" {
			return errors.New("--prompt (or --prompt-file / positional text) is required")
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*botID)
		if target == "" {
			return errors.New("bot ID is required (--bot or PEBBLE_BOT_ID)")
		}
		hop, _ := strconv.Atoi(config.LookupEnv("PEBBLE_HOP", "BOT_HUB_HOP"))
		payload := map[string]any{
			"prompt": pText,
			"hop":    hop,
		}
		if strings.TrimSpace(*title) != "" {
			payload["title"] = strings.TrimSpace(*title)
		}
		if strings.TrimSpace(*kind) != "" {
			payload["kind"] = strings.TrimSpace(*kind)
		}
		if strings.TrimSpace(*device) != "" {
			payload["deviceId"] = strings.TrimSpace(*device)
		}
		if strings.TrimSpace(*workspaceID) != "" {
			payload["workspaceId"] = strings.TrimSpace(*workspaceID)
		}
		if strings.TrimSpace(*isolation) != "" {
			payload["isolation"] = strings.TrimSpace(*isolation)
		}
		if strings.TrimSpace(*harnessName) != "" {
			payload["harness"] = strings.TrimSpace(*harnessName)
		}
		if strings.TrimSpace(*model) != "" {
			payload["model"] = strings.TrimSpace(*model)
		}
		if strings.TrimSpace(*permMode) != "" {
			payload["permissionMode"] = strings.TrimSpace(*permMode)
		}
		if strings.TrimSpace(*workdir) != "" {
			payload["workdir"] = strings.TrimSpace(*workdir)
		}
		if strings.TrimSpace(*delivery) != "" {
			payload["delivery"] = strings.TrimSpace(*delivery)
		}
		var deps []string
		for _, item := range dependsOn {
			for _, part := range strings.Split(item, ",") {
				if p := strings.TrimSpace(part); p != "" {
					deps = append(deps, p)
				}
			}
		}
		if len(deps) > 0 {
			payload["dependsOn"] = deps
		}
		pt := strings.TrimSpace(*parentTask)
		if pt == "" {
			pt = config.LookupEnv("PEBBLE_TASK_ID", "")
		}
		if pt != "" {
			payload["parentTaskId"] = pt
		}
		if *timeout > 0 {
			payload["timeoutSec"] = *timeout
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(target)+"/tasks", payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "read":
		fs := newSubFlagSet("task read", stderr, &co)
		id := fs.String("id", "", "task ID")
		afterSeq := fs.Int64("after-seq", 0, "return events with seq > after-seq")
		limit := fs.Int("limit", 200, "max events to return")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		tid := strings.TrimSpace(*id)
		if tid == "" {
			return errors.New("--id is required")
		}
		c := resolveClient(co, cwd)
		taskBody, err := c.DoJSON(ctx, http.MethodGet, "/api/tasks/"+url.PathEscape(tid), nil)
		if err != nil {
			return err
		}
		q := url.Values{}
		if *afterSeq > 0 {
			q.Set("afterSeq", strconv.FormatInt(*afterSeq, 10))
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		evPath := "/api/tasks/" + url.PathEscape(tid) + "/events"
		if enc := q.Encode(); enc != "" {
			evPath += "?" + enc
		}
		evBody, err := c.DoJSON(ctx, http.MethodGet, evPath, nil)
		if err != nil {
			return err
		}
		var evDoc struct {
			Events json.RawMessage `json:"events"`
		}
		_ = json.Unmarshal(evBody, &evDoc)
		combined, err := json.Marshal(map[string]any{
			"task":   json.RawMessage(taskBody),
			"events": evDoc.Events,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, combined)

	case "send":
		fs := newSubFlagSet("task send", stderr, &co)
		id := fs.String("id", "", "task ID")
		msg := fs.String("message", "", "follow-up message")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
			if *msg == "" && fs.NArg() > 1 {
				*msg = strings.Join(fs.Args()[1:], " ")
			}
		} else if *msg == "" && fs.NArg() > 0 {
			*msg = strings.Join(fs.Args(), " ")
		}
		tid := strings.TrimSpace(*id)
		if tid == "" || strings.TrimSpace(*msg) == "" {
			return errors.New("--id and --message are required")
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/tasks/"+url.PathEscape(tid)+"/send", map[string]any{
			"message": *msg,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "cancel":
		fs := newSubFlagSet("task cancel", stderr, &co)
		id := fs.String("id", "", "task ID")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		tid := strings.TrimSpace(*id)
		if tid == "" {
			return errors.New("--id is required")
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/tasks/"+url.PathEscape(tid)+"/cancel", nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "approve":
		fs := newSubFlagSet("task approve", stderr, &co)
		id := fs.String("id", "", "approval ID")
		decision := fs.String("decision", "", "decision (allow_once|allow_always|reject)")
		optionID := fs.String("option-id", "", "optional option ID")
		reason := fs.String("reason", "", "optional reason")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		aid := strings.TrimSpace(*id)
		if aid == "" || strings.TrimSpace(*decision) == "" {
			return errors.New("--id and --decision are required")
		}
		c := resolveClient(co, cwd)
		payload := map[string]any{
			"decision": strings.TrimSpace(*decision),
		}
		if strings.TrimSpace(*optionID) != "" {
			payload["optionId"] = strings.TrimSpace(*optionID)
		}
		if strings.TrimSpace(*reason) != "" {
			payload["reason"] = strings.TrimSpace(*reason)
		}
		if c.SelfBotID() != "" {
			payload["callerBotId"] = c.SelfBotID()
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/approvals/"+url.PathEscape(aid)+"/reply", payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "diff":
		fs := newSubFlagSet("task diff", stderr, &co)
		id := fs.String("id", "", "task ID")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		tid := strings.TrimSpace(*id)
		if tid == "" {
			return errors.New("--id is required")
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodGet, "/api/tasks/"+url.PathEscape(tid)+"/diff", nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "rollback":
		fs := newSubFlagSet("task rollback", stderr, &co)
		id := fs.String("id", "", "task ID")
		removeWorktree := fs.Bool("remove-worktree", false, "remove the isolated worktree and branch")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" && fs.NArg() > 0 {
			*id = fs.Arg(0)
		}
		tid := strings.TrimSpace(*id)
		if tid == "" {
			return errors.New("--id is required")
		}
		c := resolveClient(co, cwd)
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/tasks/"+url.PathEscape(tid)+"/rollback", map[string]any{
			"removeWorktree": *removeWorktree,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	default:
		return fmt.Errorf("unknown task action %q (expected list|get|spawn|read|send|cancel|approve|diff|rollback)", action)
	}
}
