package cli

import (
	"bot-hub/internal/config"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// --- bot ---

func runBotCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pebble bot <list|get|create|update|delete|message|send> [flags]")
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
			return errors.New("bot ID is required (--id or PEBBLE_BOT_ID)")
		}
		body, err := c.DoJSON(ctx, http.MethodGet, "/api/bots/"+url.PathEscape(target), nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "create":
		if err := rejectComputerUseRole("bot create"); err != nil {
			return err
		}
		fs := newSubFlagSet("bot create", stderr, &co)
		id := fs.String("id", "", "unique bot ID")
		name := fs.String("name", "", "display name")
		desc := fs.String("description", "", "bot description")
		avatar := fs.String("avatar", "", "avatar shape (blob|pebble-1..pebble-7)")
		color := fs.String("color", "", "avatar hex color (#rrggbb)")
		instr := fs.String("instructions", "", "AGENTS.md markdown content")
		instrFile := fs.String("instructions-file", "", "path to markdown file for AGENTS.md")
		model := fs.String("model", "", "model override")
		harnessName := fs.String("harness", "", "agent runtime (opencode|codebuddy|qoder|codex|claude|grok|trae|pi|dsh|agy)")
		priv := fs.Bool("private-sessions", false, "enable private sessions")
		deskTarget := fs.String("desktop-target", "", "bound desktop screen target (e.g. local, virtual:<slot>, <nodeId>, <nodeId>:virtual:<slot>)")
		browserMode := fs.String("browser-mode", "", "browser isolation mode (auto|isolated|attach)")
		permMode := fs.String("permission-mode", "", "permission mode (auto|edit|ask|plan)")
		force := fs.Bool("force", false, "skip strict harness/model validation")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
		if strings.TrimSpace(*id) == "" || strings.TrimSpace(*name) == "" {
			return errors.New("--id and --name are required")
		}
		instructions, hasInstr, err := readTextInput(*instr, *instrFile, cwd, visited["instructions"], visited["instructions-file"], "instructions")
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
		if visited["avatar"] {
			payload["avatar"] = *avatar
		}
		if visited["color"] {
			payload["color"] = *color
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
		if visited["desktop-target"] {
			payload["desktopTarget"] = *deskTarget
		}
		if visited["browser-mode"] {
			payload["browserMode"] = *browserMode
		}
		if visited["permission-mode"] {
			payload["permissionMode"] = *permMode
		}
		c := resolveClient(co, cwd)
		path := appendValidateDryRunQuery("/api/bots", !*force, false)
		body, err := c.DoJSON(ctx, http.MethodPost, path, payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "update":
		if err := rejectComputerUseRole("bot update"); err != nil {
			return err
		}
		fs := newSubFlagSet("bot update", stderr, &co)
		id := fs.String("id", "", "bot ID (defaults to current bot)")
		name := fs.String("name", "", "new display name")
		desc := fs.String("description", "", "new description")
		avatar := fs.String("avatar", "", "avatar shape (blob|pebble-1..pebble-7)")
		color := fs.String("color", "", "avatar hex color (#rrggbb; empty string restores default)")
		instr := fs.String("instructions", "", "new AGENTS.md markdown content")
		instrFile := fs.String("instructions-file", "", "path to markdown file for AGENTS.md")
		model := fs.String("model", "", "model override (empty string inherits hub default)")
		inheritModel := fs.Bool("inherit-model", false, "inherit hub default model (clears explicit model override)")
		harnessName := fs.String("harness", "", "agent runtime (opencode|codebuddy|qoder|codex|claude|grok|trae|pi|dsh|agy)")
		priv := fs.Bool("private-sessions", false, "enable private sessions")
		deskTarget := fs.String("desktop-target", "", "bound desktop screen target (empty string follows default)")
		browserMode := fs.String("browser-mode", "", "browser isolation mode (auto|isolated|attach)")
		permMode := fs.String("permission-mode", "", "permission mode (auto|edit|ask|plan)")
		dryRun := fs.Bool("dry-run", false, "validate and preview update without applying changes")
		force := fs.Bool("force", false, "skip strict harness/model validation")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
		if *inheritModel && visited["model"] && strings.TrimSpace(*model) != "" {
			return errors.New("cannot combine --inherit-model with a non-empty --model")
		}
		instructions, hasInstr, err := readTextInput(*instr, *instrFile, cwd, visited["instructions"], visited["instructions-file"], "instructions")
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
		if visited["avatar"] {
			payload["avatar"] = *avatar
		}
		if visited["color"] {
			payload["color"] = *color
		}
		if hasInstr {
			payload["instructions"] = instructions
		}
		if *inheritModel {
			payload["model"] = ""
		} else if visited["model"] {
			payload["model"] = *model
		}
		if visited["harness"] {
			payload["harness"] = *harnessName
		}
		if visited["private-sessions"] {
			payload["privateSessions"] = *priv
		}
		if visited["desktop-target"] {
			payload["desktopTarget"] = *deskTarget
		}
		if visited["browser-mode"] {
			payload["browserMode"] = *browserMode
		}
		if visited["permission-mode"] {
			payload["permissionMode"] = *permMode
		}
		if len(payload) == 0 {
			return errors.New("specify at least one field to update (--name, --description, --avatar, --color, --instructions, --instructions-file, --model, --inherit-model, --harness, --private-sessions, --desktop-target, --browser-mode, --permission-mode)")
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*id)
		if target == "" {
			return errors.New("bot ID is required (--id or PEBBLE_BOT_ID)")
		}
		path := appendValidateDryRunQuery("/api/bots/"+url.PathEscape(target), !*force, *dryRun)
		body, err := c.DoJSON(ctx, http.MethodPatch, path, payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "delete":
		if err := rejectComputerUseRole("bot delete"); err != nil {
			return err
		}
		fs := newSubFlagSet("bot delete", stderr, &co)
		id := fs.String("id", "", "bot ID to delete")
		confirmSelf := fs.Bool("confirm-self", false, "confirm deleting the currently active bot")
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
		if c.SelfBotID() != "" && target == c.SelfBotID() && !*confirmSelf {
			return fmt.Errorf("refusing to delete current bot %q without --confirm-self", target)
		}
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
		hop, _ := strconv.Atoi(config.LookupEnv("PEBBLE_HOP", "BOT_HUB_HOP"))
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(strings.TrimSpace(*id))+"/message", map[string]any{
			"message":   *msg,
			"fromBotId": c.SelfBotID(),
			"hop":       hop,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "send":
		fs := newSubFlagSet("bot send", stderr, &co)
		id := fs.String("id", "", "bot ID (defaults to current bot)")
		text := fs.String("text", "", "message text to send to user")
		var images multiStringFlag
		var files multiStringFlag
		fs.Var(&images, "image", "image file path to attach (repeatable)")
		fs.Var(&files, "file", "file path to attach (repeatable)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *text == "" && fs.NArg() > 0 {
			*text = strings.Join(fs.Args(), " ")
		}
		c := resolveClient(co, cwd)
		target := c.ResolveBotID(*id)
		if target == "" {
			return errors.New("bot ID is required (--id or PEBBLE_BOT_ID)")
		}
		if strings.TrimSpace(*text) == "" && len(images) == 0 && len(files) == 0 {
			return errors.New("--text (or positional text), --image, or --file is required")
		}
		type attPayload struct {
			Path string `json:"path"`
		}
		resolvePaths := func(paths []string) []attPayload {
			var out []attPayload
			for _, p := range paths {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if !filepath.IsAbs(p) && cwd != "" {
					p = filepath.Join(cwd, p)
				}
				out = append(out, attPayload{Path: p})
			}
			return out
		}
		imgList := resolvePaths(images)
		fileList := resolvePaths(files)
		payload := map[string]any{
			"text": *text,
		}
		if len(imgList) > 0 {
			payload["images"] = imgList
		}
		if len(fileList) > 0 {
			payload["files"] = fileList
		}
		body, err := c.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(target)+"/outbound", payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	default:
		return fmt.Errorf("unknown bot action %q (expected list|get|create|update|delete|message|send)", action)
	}
}
