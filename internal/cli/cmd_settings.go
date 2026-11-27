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
	"strings"
)

// --- settings ---

func runSettingsCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pebble settings <get|models|set> [flags]")
	}
	action := args[0]
	rest := args[1:]
	var co connOpts

	switch action {
	case "get":
		fs := newSubFlagSet("settings get", stderr, &co)
		fresh := fs.Bool("fresh", false, "bypass cache and probe harnesses live")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		path := "/api/settings"
		if *fresh {
			path += "?fresh=1"
		}
		body, err := c.DoJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	case "models":
		fs := newSubFlagSet("settings models", stderr, &co)
		harnessName := fs.String("harness", "", "target harness ID (defaults to current bot's harness)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		c := resolveClient(co, cwd)
		targetHarness := strings.TrimSpace(*harnessName)
		if targetHarness == "" && c.SelfBotID() != "" {
			if botBody, err := c.DoJSON(ctx, http.MethodGet, "/api/bots/"+url.PathEscape(c.SelfBotID()), nil); err == nil {
				var botDoc struct {
					Harness string `json:"harness"`
				}
				if json.Unmarshal(botBody, &botDoc) == nil && strings.TrimSpace(botDoc.Harness) != "" {
					targetHarness = strings.TrimSpace(botDoc.Harness)
				}
			}
		}
		if targetHarness == "" {
			if sBody, err := c.DoJSON(ctx, http.MethodGet, "/api/settings", nil); err == nil {
				var sDoc struct {
					DefaultHarness string `json:"defaultHarness"`
				}
				if json.Unmarshal(sBody, &sDoc) == nil && strings.TrimSpace(sDoc.DefaultHarness) != "" {
					targetHarness = strings.TrimSpace(sDoc.DefaultHarness)
				}
			}
		}
		if targetHarness == "" {
			targetHarness = "opencode"
		}

		q := url.Values{}
		q.Set("harness", targetHarness)
		body, err := c.DoJSON(ctx, http.MethodGet, "/api/models?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		var mDoc struct {
			Models []string `json:"models"`
		}
		if err := json.Unmarshal(body, &mDoc); err != nil {
			return writeJSONOut(stdout, body)
		}
		if mDoc.Models == nil {
			mDoc.Models = []string{}
		}
		outBytes, err := json.Marshal(struct {
			Harness string   `json:"harness"`
			Models  []string `json:"models"`
		}{
			Harness: targetHarness,
			Models:  mDoc.Models,
		})
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, outBytes)

	case "set":
		if err := rejectComputerUseRole("settings set"); err != nil {
			return err
		}
		fs := newSubFlagSet("settings set", stderr, &co)
		defHarness := fs.String("default-harness", "", "hub-wide default harness")
		defModel := fs.String("default-model", "", "hub-wide default model")
		clearDefModel := fs.Bool("clear-default-model", false, "clear hub-wide default model")
		var harnessModels multiStringFlag
		var clearHarnessModels multiStringFlag
		fs.Var(&harnessModels, "harness-model", "per-harness default model override in <harness>=<model> form (repeatable)")
		fs.Var(&clearHarnessModels, "clear-harness-model", "clear per-harness default model override for <harness> (repeatable)")
		deskHarness := fs.String("desktop-harness", "", "desktop worker harness")
		clearDeskHarness := fs.Bool("clear-desktop-harness", false, "clear desktop worker harness")
		deskModel := fs.String("desktop-model", "", "desktop worker model")
		clearDeskModel := fs.Bool("clear-desktop-model", false, "clear desktop worker model")
		sharedUserMD := fs.String("shared-user-md", "", "shared _shared/USER.md markdown content")
		sharedUserMDFile := fs.String("shared-user-md-file", "", "path to markdown file for _shared/USER.md")
		dryRun := fs.Bool("dry-run", false, "validate and preview impact without applying changes")
		force := fs.Bool("force", false, "skip strict harness/model validation")
		if err := fs.Parse(rest); err != nil {
			return err
		}

		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

		payload := map[string]any{}
		if visited["default-harness"] {
			v := strings.TrimSpace(*defHarness)
			if v == "" {
				return errors.New("--default-harness cannot be empty")
			}
			payload["defaultHarness"] = v
		}

		if visited["default-model"] && *clearDefModel {
			return errors.New("cannot specify both --default-model and --clear-default-model")
		}
		if visited["default-model"] {
			v := strings.TrimSpace(*defModel)
			if v == "" {
				return errors.New("--default-model cannot be empty (use --clear-default-model to clear)")
			}
			payload["defaultModel"] = v
		} else if *clearDefModel {
			payload["defaultModel"] = ""
		}

		patch := map[string]string{}
		for _, entry := range harnessModels {
			h, m, ok := strings.Cut(entry, "=")
			h = strings.TrimSpace(h)
			m = strings.TrimSpace(m)
			if !ok || h == "" || m == "" {
				return fmt.Errorf("invalid --harness-model %q (expected <harness>=<model>; use --clear-harness-model <harness> to clear)", entry)
			}
			patch[h] = m
		}
		for _, rawH := range clearHarnessModels {
			h := strings.TrimSpace(rawH)
			if h == "" {
				return errors.New("--clear-harness-model cannot be empty")
			}
			if _, exists := patch[h]; exists {
				return fmt.Errorf("cannot both set and clear harness model for %q", h)
			}
			patch[h] = ""
		}
		if len(patch) > 0 {
			payload["defaultModelsPatch"] = patch
		}

		if visited["desktop-harness"] && *clearDeskHarness {
			return errors.New("cannot specify both --desktop-harness and --clear-desktop-harness")
		}
		if visited["desktop-harness"] {
			v := strings.TrimSpace(*deskHarness)
			if v == "" {
				return errors.New("--desktop-harness cannot be empty (use --clear-desktop-harness to clear)")
			}
			payload["desktopHarness"] = v
		} else if *clearDeskHarness {
			payload["desktopHarness"] = ""
		}

		if visited["desktop-model"] && *clearDeskModel {
			return errors.New("cannot specify both --desktop-model and --clear-desktop-model")
		}
		if visited["desktop-model"] {
			v := strings.TrimSpace(*deskModel)
			if v == "" {
				return errors.New("--desktop-model cannot be empty (use --clear-desktop-model to clear)")
			}
			payload["desktopModel"] = v
		} else if *clearDeskModel {
			payload["desktopModel"] = ""
		}

		userMD, hasUserMD, err := readTextInput(*sharedUserMD, *sharedUserMDFile, cwd, visited["shared-user-md"], visited["shared-user-md-file"], "shared-user-md")
		if err != nil {
			return err
		}
		if hasUserMD {
			payload["sharedUserMd"] = userMD
		}

		if len(payload) == 0 {
			return errors.New("specify at least one setting to update")
		}

		c := resolveClient(co, cwd)
		path := appendValidateDryRunQuery("/api/settings", !*force, *dryRun)
		body, err := c.DoJSON(ctx, http.MethodPatch, path, payload)
		if err != nil {
			return err
		}
		return writeJSONOut(stdout, body)

	default:
		return fmt.Errorf("unknown settings action %q (expected get|models|set)", action)
	}
}
