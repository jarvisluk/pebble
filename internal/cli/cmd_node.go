package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// --- node ---

func runNodeCmd(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pebble node <list|exec|file> [flags]")
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
