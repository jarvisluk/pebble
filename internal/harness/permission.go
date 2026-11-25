package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bot-hub/internal/config"
)

const approvalTurnMinTimeoutSec = 900

// EffectivePermissionMode resolves the permission mode for a single turn,
// preferring RunOptions.PermissionMode over BotConfig.EffectivePermissionMode().
func EffectivePermissionMode(opts RunOptions) string {
	if pm, err := config.NormalizePermissionMode(opts.PermissionMode); err == nil && pm != "" {
		return pm
	}
	return opts.Bot.EffectivePermissionMode()
}

// SupportsMode reports whether caps includes mode.
func (c PermissionCapabilities) SupportsMode(mode string) bool {
	for _, m := range c.Modes {
		if m == mode {
			return true
		}
	}
	return false
}

// StandardPermissionOptions builds the default option set when a harness does
// not supply protocol-specific options.
func StandardPermissionOptions(allowAlways bool) []PermissionOption {
	opts := []PermissionOption{
		{ID: "allow_once", Kind: "allow_once", Label: "Allow once"},
	}
	if allowAlways {
		opts = append(opts, PermissionOption{ID: "allow_always", Kind: "allow_always", Label: "Allow for session"})
	}
	opts = append(opts, PermissionOption{ID: "reject", Kind: "reject", Label: "Deny"})
	return opts
}

// withTurnTimeout returns a turn context that gives interactive approval turns
// enough room so waiting for user confirmation does not trip the default 300s timeout.
func withTurnTimeout(ctx context.Context, timeoutSec int, opts RunOptions) (context.Context, context.CancelFunc) {
	if timeoutSec <= 0 {
		return ctx, func() {}
	}
	if opts.OnPermission != nil && EffectivePermissionMode(opts) != config.PermissionModeAuto && timeoutSec < approvalTurnMinTimeoutSec {
		timeoutSec = approvalTurnMinTimeoutSec
	}
	return context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
}

// ResolveApprovalDecision evaluates mode policies first and only blocks on
// opts.OnPermission when interactive user approval is actually required.
func ResolveApprovalDecision(ctx context.Context, opts RunOptions, req PermissionRequest) PermissionResponse {
	mode := EffectivePermissionMode(opts)
	if req.Kind == "" {
		req.Kind = ClassifyToolKind(req.ToolName)
	}
	switch mode {
	case config.PermissionModeAuto:
		return PermissionResponse{Decision: "allow_once"}
	case config.PermissionModePlan:
		if IsReadOnlyTool(req.Kind, req.ToolName) {
			return PermissionResponse{Decision: "allow_once"}
		}
		if !strings.EqualFold(req.ToolName, "ExitPlanMode") {
			return PermissionResponse{
				Decision: "reject",
				Reason:   "Plan mode only allows read-only analysis",
			}
		}
	case config.PermissionModeEdit:
		if IsReadOnlyTool(req.Kind, req.ToolName) || IsWorkspaceEditTool(req.Kind, req.ToolName, opts.Bot.Workspace, req.RawInput) {
			return PermissionResponse{Decision: "allow_once"}
		}
	}

	if opts.OnPermission == nil {
		return PermissionResponse{
			Decision: "reject",
			Reason:   "Unattended run with non-auto permission mode; action auto-rejected",
		}
	}

	if req.Title == "" || req.Summary == "" {
		t, s := SummarizeToolCall(req.ToolName, req.RawInput)
		if req.Title == "" {
			req.Title = t
		}
		if req.Summary == "" {
			req.Summary = s
		}
	}
	req.RawInput = SanitizePermissionInput(req.RawInput)
	if len(req.Options) == 0 {
		req.Options = StandardPermissionOptions(true)
	}

	resp, err := opts.OnPermission(ctx, req)
	if err != nil || ctx.Err() != nil {
		return PermissionResponse{
			Decision: "cancelled",
			Reason:   "approval interrupted",
		}
	}
	switch resp.Decision {
	case "allow_once", "allow_always", "reject", "cancelled":
		return resp
	default:
		return PermissionResponse{Decision: "reject", Reason: resp.Reason}
	}
}

// NormalizeToolKind maps protocol-level kind/action strings and tool names into
// canonical UI categories ("shell", "write", "read", "mcp", "network", "other").
func NormalizeToolKind(rawKind, toolName string) string {
	k := strings.ToLower(strings.TrimSpace(rawKind))
	switch k {
	case "shell", "execute", "command", "bash", "terminal", "command_execution":
		return "shell"
	case "write", "edit", "delete", "move", "patch", "file_change", "filechange", "external_directory":
		return "write"
	case "read", "search", "think", "list", "glob", "grep":
		return "read"
	case "network", "fetch", "web":
		return "network"
	case "mcp":
		return "mcp"
	}
	return ClassifyToolKind(toolName)
}

// ClassifyToolKind infers a canonical category from a tool name.
func ClassifyToolKind(toolName string) string {
	n := strings.ToLower(strings.TrimSpace(toolName))
	if n == "" {
		return "other"
	}
	if strings.HasPrefix(n, "mcp") || strings.Contains(n, "__") {
		return "mcp"
	}
	switch {
	case n == "bash" || n == "shell" || n == "exec" || n == "execute" ||
		n == "run_command" || n == "command_execution" || n == "terminal" ||
		strings.Contains(n, "command") && !strings.Contains(n, "status"):
		return "shell"
	case n == "fetch" || n == "webfetch" || n == "web_fetch" || n == "websearch" || n == "web_search" ||
		n == "read_url_content" || n == "search_web" || strings.HasPrefix(n, "browser"):
		return "network"
	case n == "read" || n == "read_file" || n == "view_file" || n == "view_file_outline" || n == "view_code_item" ||
		n == "glob" || n == "grep" || n == "grep_search" || n == "find_by_name" || n == "list_dir" ||
		n == "ls" || n == "command_status" || n == "read_terminal" || n == "recall_session" ||
		n == "todoread" || n == "todo_read":
		return "read"
	case n == "write" || n == "write_file" || n == "write_to_file" || n == "edit" || n == "edit_file" ||
		n == "multi_edit" || n == "replace_file_content" || n == "multi_replace_file_content" ||
		n == "apply_patch" || n == "notebook_edit" || n == "notebookedit" || n == "create_file" ||
		n == "delete_file" || n == "remove_file" || n == "file_change" || n == "filechange" ||
		n == "mkdir" || n == "patch":
		return "write"
	default:
		return "other"
	}
}

// IsReadOnlyTool reports whether the tool/kind performs only read-only inspection.
func IsReadOnlyTool(kind, toolName string) bool {
	if kind == "" {
		kind = ClassifyToolKind(toolName)
	}
	return kind == "read"
}

// IsWorkspaceEditTool reports whether the tool edits files inside workspace.
func IsWorkspaceEditTool(kind, toolName, workspace string, input map[string]any) bool {
	if kind == "" {
		kind = ClassifyToolKind(toolName)
	}
	if kind != "write" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(toolName), "external_directory") {
		return false
	}
	paths := extractInputPaths(input)
	if len(paths) == 0 || strings.TrimSpace(workspace) == "" {
		return true
	}
	wsAbs, err := filepath.Abs(workspace)
	if err != nil {
		return false
	}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		target := p
		if !filepath.IsAbs(target) {
			target = filepath.Join(wsAbs, target)
		}
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			return false
		}
		rel, err := filepath.Rel(wsAbs, targetAbs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
	}
	return true
}

func extractInputPaths(input map[string]any) []string {
	if len(input) == 0 {
		return nil
	}
	var out []string
	for _, key := range []string{
		"file_path", "filePath", "TargetFile", "target_file",
		"path", "Path", "directory", "DirectoryPath", "SearchPath", "grantRoot",
	} {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	}
	for _, arrKey := range []string{"resources", "paths", "files"} {
		if raw, ok := input[arrKey]; ok {
			switch items := raw.(type) {
			case []string:
				out = append(out, items...)
			case []any:
				for _, item := range items {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						out = append(out, s)
					} else if m, ok := item.(map[string]any); ok {
						if p, _ := m["path"].(string); strings.TrimSpace(p) != "" {
							out = append(out, p)
						}
					}
				}
			}
		}
	}
	return out
}

// SummarizeToolCall builds a concise human-readable title and primary target
// preview (command, path, URL, or JSON snippet) from tool inputs.
func SummarizeToolCall(toolName string, input map[string]any) (title, summary string) {
	title = strings.TrimSpace(toolName)
	if title == "" {
		title = "tool"
	}
	if len(input) == 0 {
		return title, ""
	}

	for _, key := range []string{
		"command", "CommandLine", "cmd", "script",
		"file_path", "filePath", "TargetFile", "target_file", "path", "Path",
		"url", "Url", "uri", "directory", "DirectoryPath",
		"goal", "reason", "query", "Query", "pattern",
	} {
		if v, ok := input[key]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && v != nil {
				return title, truncatePermString(s, 1000)
			}
		}
	}
	if paths := extractInputPaths(input); len(paths) > 0 {
		return title, truncatePermString(strings.Join(paths, ", "), 1000)
	}
	if b, err := json.Marshal(SanitizePermissionInput(input)); err == nil {
		s := strings.TrimSpace(string(b))
		if s != "{}" {
			return title, truncatePermString(s, 600)
		}
	}
	return title, ""
}

// SanitizePermissionInput redacts sensitive fields and bounds large values so
// approval payloads remain safe and lightweight for SSE and push delivery.
func SanitizePermissionInput(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	return sanitizeMap(raw, 0)
}

func sanitizeMap(m map[string]any, depth int) map[string]any {
	if len(m) == 0 {
		return nil
	}
	if depth >= 4 {
		return map[string]any{"_truncated": true}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]any, min(len(keys), 32))
	for i, k := range keys {
		if i >= 32 {
			out["_moreKeys"] = len(keys) - 32
			break
		}
		if isSensitiveKey(k) {
			out[k] = "[REDACTED]"
			continue
		}
		out[k] = sanitizeValue(m[k], depth+1)
	}
	return out
}

func sanitizeValue(v any, depth int) any {
	switch val := v.(type) {
	case nil, bool, int, int64, float64:
		return val
	case string:
		return truncatePermString(val, 4096)
	case map[string]any:
		return sanitizeMap(val, depth)
	case []string:
		limit := min(len(val), 32)
		out := make([]string, 0, limit)
		for i := 0; i < limit; i++ {
			out = append(out, truncatePermString(val[i], 1024))
		}
		return out
	case []any:
		limit := min(len(val), 32)
		out := make([]any, 0, limit)
		for i := 0; i < limit; i++ {
			out = append(out, sanitizeValue(val[i], depth+1))
		}
		return out
	default:
		return truncatePermString(fmt.Sprint(val), 1024)
	}
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, pat := range []string{
		"token", "secret", "password", "passwd", "apikey", "api_key",
		"authorization", "cookie", "credential", "private_key", "privatekey",
	} {
		if strings.Contains(k, pat) {
			return true
		}
	}
	return false
}

func truncatePermString(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 16 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-12]) + "…(truncated)"
}
