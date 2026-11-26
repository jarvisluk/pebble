package cli

import (
	"bot-hub/internal/config"
	"bot-hub/internal/mcp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type connOpts struct {
	hubURL string
	botID  string
	cfg    string
	token  string
}

func registerConnFlags(fs *flag.FlagSet, o *connOpts) {
	fs.StringVar(&o.hubURL, "hub-url", "", "running Pebble HTTP base URL")
	fs.StringVar(&o.botID, "bot-id", "", "caller bot ID")
	fs.StringVar(&o.cfg, "config", "", "path to bots.json config file")
	fs.StringVar(&o.token, "token", "", "pebble API token")
}

// runCommand executes `pebble <bot|settings|session|routine|node|desktop> ...` and
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
	case "settings":
		err = runSettingsCmd(ctx, rest, cwd, stdout, stderr)
	case "session":
		err = runSessionCmd(ctx, rest, cwd, stdout, stderr)
	case "routine":
		err = runRoutineCmd(ctx, rest, cwd, stdout, stderr)
	case "node":
		err = runNodeCmd(ctx, rest, cwd, stdout, stderr)
	case "desktop":
		err = runDesktopCmd(ctx, rest, cwd, stdout, stderr)
	case "update":
		err = runUpdateCmd(ctx, rest, cwd, stdout, stderr)
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

func rejectComputerUseRole(cmdName string) error {
	if config.LookupEnv("PEBBLE_MCP_ROLE", "BOT_HUB_MCP_ROLE") == "computer_use" {
		return fmt.Errorf("%s cannot be called from inside a computer-use worker", cmdName)
	}
	return nil
}

func resolveClient(o connOpts, cwd string) *mcp.Client {
	hubURL := strings.TrimSpace(o.hubURL)
	if hubURL == "" {
		hubURL = config.LookupEnv("PEBBLE_URL", "BOT_HUB_URL")
	}
	botID := strings.TrimSpace(o.botID)
	if botID == "" {
		botID = config.LookupEnv("PEBBLE_BOT_ID", "BOT_HUB_BOT_ID")
	}
	cfgPath := strings.TrimSpace(o.cfg)
	if cfgPath == "" {
		cfgPath = config.LookupEnv("PEBBLE_CONFIG", "BOT_HUB_CONFIG")
	}
	tok := strings.TrimSpace(o.token)
	if tok == "" {
		tok = config.LookupEnv("PEBBLE_API_TOKEN", "BOT_HUB_API_TOKEN")
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

func lookupTokenEnvMap(m map[string]string) string {
	if tok := strings.TrimSpace(m["PEBBLE_API_TOKEN"]); tok != "" {
		return tok
	}
	return strings.TrimSpace(m["BOT_HUB_API_TOKEN"])
}

func discoverWorkspaceContext(cwd string) connOpts {
	var out connOpts
	if strings.TrimSpace(cwd) == "" {
		return out
	}

	// 1. opencode.json -> mcp.servers.pebble (or legacy bot-hub)
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
			for _, key := range []string{config.MCPServerName, config.LegacyMCPServerName} {
				if srv, ok := doc.MCP.Servers[key]; ok {
					parseMCPArgs(srv.Command, &out)
					if out.token == "" {
						out.token = lookupTokenEnvMap(srv.Environment)
					}
					return out
				}
			}
		}
	}

	// 2. .agents/mcp_config.json (agy) or .pi/mcp.json (pi) -> mcpServers.pebble (or legacy bot-hub)
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
			for _, key := range []string{config.MCPServerName, config.LegacyMCPServerName} {
				if srv, ok := doc.MCPServers[key]; ok {
					parseMCPArgs(append([]string{srv.Command}, srv.Args...), &out)
					if out.token == "" {
						out.token = lookupTokenEnvMap(srv.Env)
					}
					return out
				}
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

func readTextInput(inline, filePath, cwd string, inlineSet, fileSet bool, flagBase string) (string, bool, error) {
	if inlineSet && fileSet {
		return "", false, fmt.Errorf("cannot specify both --%s and --%s-file", flagBase, flagBase)
	}
	if fileSet {
		p := strings.TrimSpace(filePath)
		if p == "" {
			return "", false, fmt.Errorf("--%s-file cannot be empty", flagBase)
		}
		if !filepath.IsAbs(p) && cwd != "" {
			p = filepath.Join(cwd, p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", false, fmt.Errorf("read %s file: %w", flagBase, err)
		}
		return string(b), true, nil
	}
	if inlineSet {
		return inline, true, nil
	}
	return "", false, nil
}

type multiStringFlag []string

func (m *multiStringFlag) String() string { return strings.Join(*m, ",") }

func (m *multiStringFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func appendValidateDryRunQuery(path string, validate, dryRun bool) string {
	q := url.Values{}
	if validate {
		q.Set("validate", "1")
	}
	if dryRun {
		q.Set("dryRun", "1")
	}
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}
