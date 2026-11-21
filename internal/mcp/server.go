package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/node"
)

type Server struct {
	hubURL     string
	selfBotID  string
	configPath string
	httpClient *http.Client

	mu          sync.Mutex
	cachedToken string
}

func NewServer(hubURL, selfBotID string) *Server {
	return NewServerWithConfig(hubURL, selfBotID, "")
}

func NewServerWithConfig(hubURL, selfBotID, configPath string) *Server {
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if hubURL == "" {
		hubURL = "http://127.0.0.1:3789"
	}
	if selfBotID == "" {
		selfBotID = os.Getenv("BOT_HUB_BOT_ID")
	}
	return &Server{
		hubURL:      hubURL,
		selfBotID:   selfBotID,
		configPath:  strings.TrimSpace(configPath),
		httpClient:  &http.Client{Timeout: 10 * time.Minute},
		cachedToken: strings.TrimSpace(os.Getenv("BOT_HUB_API_TOKEN")),
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(w)

	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			continue
		}

		// Notifications have no ID and expect no response.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue
		}

		resp := s.handle(ctx, req)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(ctx context.Context, req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "bot-hub",
					"version": "0.1.0",
				},
			},
		}

	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	case "tools/list":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": toolDefinitions()},
		}

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32602, Message: "invalid tools/call params"},
			}
		}
		text, isErr := s.callTool(ctx, params.Name, params.Arguments)
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": text},
				},
				"isError": isErr,
			},
		}

	default:
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method %q not found", req.Method)},
		}
	}
}

func toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "list_bots",
			"description": "List all configured bots in bot-hub, including their ID, name, description, workspace, model, busy state, and session stats.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_bot",
			"description": "Get detailed information about a specific bot, including its status and full AGENTS.md instructions.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"botId": map[string]any{
						"type":        "string",
						"description": "The ID of the bot to inspect.",
					},
				},
				"required": []string{"botId"},
			},
		},
		{
			"name":        "create_bot",
			"description": "Create and immediately activate a new persistent bot with its own workspace, AGENTS.md instructions, and dedicated long-lived session.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Unique bot ID (letters, digits, '-' or '_'), e.g. 'ops' or 'writer'.",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Display name of the bot.",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Short summary of what this bot is responsible for.",
					},
					"instructions": map[string]any{
						"type":        "string",
						"description": "Optional Markdown instructions written to the bot's AGENTS.md.",
					},
					"model": map[string]any{
						"type":        "string",
						"description": "Optional model override (defaults to the hub default for the harness).",
					},
					"harness": map[string]any{
						"type":        "string",
						"enum":        []string{"opencode", "pi", "dsh", "claude"},
						"description": "Optional agent runtime (default opencode).",
					},
				},
				"required": []string{"id", "name"},
			},
		},
		{
			"name":        "message_bot",
			"description": "Send a message to another bot in bot-hub and return its reply. Both bots' chat timelines will record the collaboration.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"botId": map[string]any{
						"type":        "string",
						"description": "Target bot ID to send the message to.",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "The message or task to send to the target bot.",
					},
				},
				"required": []string{"botId", "message"},
			},
		},
		{
			"name":        "list_nodes",
			"description": "List remote computer nodes connected to bot-hub over Tailcat, including their ID, name, OS, connected status, and granted permission scopes (files, exec, desktop).",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "node_exec",
			"description": "Execute a shell command on a connected remote computer node (requires the node's 'exec' permission).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nodeId": map[string]any{
						"type":        "string",
						"description": "Optional target node ID. Omit when only one remote node is connected.",
					},
					"command": map[string]any{
						"type":        "string",
						"description": "Shell command to run on the remote computer.",
					},
					"cwd": map[string]any{
						"type":        "string",
						"description": "Optional working directory on the remote computer (supports ~/).",
					},
					"timeoutSec": map[string]any{
						"type":        "integer",
						"description": "Optional timeout in seconds (default 60, max 300).",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			"name":        "node_file",
			"description": "Read, write, or list files on a connected remote computer node (requires the node's 'files' permission).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nodeId": map[string]any{
						"type":        "string",
						"description": "Optional target node ID. Omit when only one remote node is connected.",
					},
					"op": map[string]any{
						"type":        "string",
						"enum":        []string{"read", "write", "list"},
						"description": "Operation: read, write, or list.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "File or directory path on the remote computer (supports ~/).",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "File content to write (for op='write').",
					},
					"append": map[string]any{
						"type":        "boolean",
						"description": "Append instead of overwriting (for op='write').",
					},
				},
				"required": []string{"op", "path"},
			},
		},
		{
			"name":        "node_desktop",
			"description": "Interact with the GUI desktop on the local hub computer (nodeId='local') or a connected remote node inside the logged-in session: screenshot, osascript (macOS AppleScript/JXA), open app/URL/file, click, double_click, right_click, type text, press key combo, or scroll.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nodeId": map[string]any{
						"type":        "string",
						"description": "Target node ID, or 'local' for the hub host desktop. Omit to auto-select the connected remote node (or 'local' when none are connected).",
					},
					"op": map[string]any{
						"type":        "string",
						"enum":        []string{"screenshot", "osascript", "open", "click", "double_click", "right_click", "type", "key", "scroll"},
						"description": "Desktop action.",
					},
					"script": map[string]any{
						"type":        "string",
						"description": "AppleScript or JavaScript source (for op='osascript').",
					},
					"language": map[string]any{
						"type":        "string",
						"enum":        []string{"AppleScript", "JavaScript"},
						"description": "Script language for op='osascript' (default AppleScript).",
					},
					"target": map[string]any{
						"type":        "string",
						"description": "File path or URL to open (for op='open').",
					},
					"app": map[string]any{
						"type":        "string",
						"description": "Application name to focus or open.",
					},
					"x": map[string]any{
						"type":        "number",
						"description": "Logical X coordinate on screen (for click/double_click/right_click/scroll).",
					},
					"y": map[string]any{
						"type":        "number",
						"description": "Logical Y coordinate on screen (for click/double_click/right_click/scroll).",
					},
					"deltaY": map[string]any{
						"type":        "number",
						"description": "Scroll amount (positive = up, negative = down, for op='scroll').",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Text to type (for op='type').",
					},
					"key": map[string]any{
						"type":        "string",
						"description": "Key or modifier combo like 'Return', 'Escape', 'cmd+c' (for op='key').",
					},
				},
				"required": []string{"op"},
			},
		},
		{
			"name":        "request_takeover",
			"description": "Pause automation and request a human to handle an interactive step (such as QR login, 2FA/OTP code, CAPTCHA, or payment confirmation) via the live Remote Desktop view. Blocks until the user clicks '完成并恢复 AI'.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reason": map[string]any{
						"type":        "string",
						"description": "Clear explanation shown to the user describing what to do on the screen.",
					},
					"nodeId": map[string]any{
						"type":        "string",
						"description": "Optional target desktop ('local' by default, or remote node ID).",
					},
					"app": map[string]any{
						"type":        "string",
						"description": "Optional application name involved in the takeover.",
					},
				},
				"required": []string{"reason"},
			},
		},
		{
			"name":        "list_sessions",
			"description": "List archived and active sessions in bot-hub, optionally filtering by botId ('*' for all accessible bots) or keyword query. Includes magpieSession references when available.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"botId": map[string]any{
						"type":        "string",
						"description": "Optional target bot ID (defaults to this bot; pass '*' to search all accessible bots).",
					},
					"query": map[string]any{
						"type":        "string",
						"description": "Optional keyword to match session titles, errors, or message contents.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of sessions to return (default 20, max 100).",
					},
				},
			},
		},
		{
			"name":        "recall_session",
			"description": "Read sliced messages from a prior or active session (yours or another accessible bot's). Defaults to the most recent archived session ('previous') and last 20 messages with length protection.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId": map[string]any{
						"type":        "string",
						"description": "Target session ID (Hub ses_... ID or runtimeSessionId). Omit or pass 'previous' for the latest archived session.",
					},
					"botId": map[string]any{
						"type":        "string",
						"description": "Optional target bot ID (defaults to this bot when sessionId is 'previous' or omitted).",
					},
					"tail": map[string]any{
						"type":        "integer",
						"description": "Return the last N messages (default 20, max 100).",
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based start offset for forward pagination.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Page size when paginating with offset (default 20, max 100).",
					},
					"query": map[string]any{
						"type":        "string",
						"description": "Optional keyword filter inside the session (returns matched messages with surrounding context).",
					},
					"maxChars": map[string]any{
						"type":        "integer",
						"description": "Per-message character limit before truncation (default 2000).",
					},
					"messageId": map[string]any{
						"type":        "string",
						"description": "Fetch the complete untruncated text of a single clipped message ID.",
					},
				},
			},
		},
		{
			"name":        "rotate_session",
			"description": "Schedule archiving the current session after this turn completes (with an optional handoff note) so the next message starts a clean session.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"handoffNote": map[string]any{
						"type":        "string",
						"description": "Optional concise handoff summary carried into the next session's breadcrumb.",
					},
				},
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, name string, rawArgs json.RawMessage) (string, bool) {
	switch name {
	case "list_bots":
		body, err := s.doJSON(ctx, http.MethodGet, "/api/bots", nil)
		if err != nil {
			return err.Error(), true
		}
		return prettyJSON(body), false

	case "get_bot":
		var args struct {
			BotID string `json:"botId"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.BotID) == "" {
			return "botId is required", true
		}
		body, err := s.doJSON(ctx, http.MethodGet, "/api/bots/"+args.BotID, nil)
		if err != nil {
			return err.Error(), true
		}
		return prettyJSON(body), false

	case "create_bot":
		var args map[string]any
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "invalid arguments", true
		}
		body, err := s.doJSON(ctx, http.MethodPost, "/api/bots", args)
		if err != nil {
			return err.Error(), true
		}
		return prettyJSON(body), false

	case "message_bot":
		var args struct {
			BotID   string `json:"botId"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.BotID) == "" || strings.TrimSpace(args.Message) == "" {
			return "botId and message are required", true
		}
		hop, _ := strconv.Atoi(os.Getenv("BOT_HUB_HOP"))
		payload := map[string]any{
			"message":   args.Message,
			"fromBotId": s.selfBotID,
			"hop":       hop,
		}
		body, err := s.doJSON(ctx, http.MethodPost, "/api/bots/"+args.BotID+"/message", payload)
		if err != nil {
			return err.Error(), true
		}
		var resp struct {
			Text      string `json:"text"`
			SessionID string `json:"sessionId"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && resp.Text != "" {
			return fmt.Sprintf("[Reply from bot:%s (session=%s)]\n%s", args.BotID, resp.SessionID, resp.Text), false
		}
		return prettyJSON(body), false

	case "list_nodes":
		body, err := s.doJSON(ctx, http.MethodGet, "/api/nodes", nil)
		if err != nil {
			return err.Error(), true
		}
		return prettyJSON(body), false

	case "node_exec":
		var args struct {
			NodeID     string `json:"nodeId"`
			Command    string `json:"command"`
			Cwd        string `json:"cwd"`
			TimeoutSec int    `json:"timeoutSec"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Command) == "" {
			return "command is required", true
		}
		return s.callNode(ctx, args.NodeID, "exec", map[string]any{
			"command": args.Command,
			"cwd":     args.Cwd,
		}, args.TimeoutSec)

	case "node_file":
		var args struct {
			NodeID  string `json:"nodeId"`
			Op      string `json:"op"`
			Path    string `json:"path"`
			Content string `json:"content"`
			Append  bool   `json:"append"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Path) == "" {
			return "op and path are required", true
		}
		actionMap := map[string]string{
			"read":  "file_read",
			"write": "file_write",
			"list":  "file_list",
		}
		action := actionMap[args.Op]
		if action == "" {
			return "op must be read, write, or list", true
		}
		return s.callNode(ctx, args.NodeID, action, map[string]any{
			"path":    args.Path,
			"content": args.Content,
			"append":  args.Append,
		}, 0)

	case "node_desktop":
		var args struct {
			NodeID   string   `json:"nodeId"`
			Op       string   `json:"op"`
			Script   string   `json:"script"`
			Language string   `json:"language"`
			Target   string   `json:"target"`
			App      string   `json:"app"`
			X        *float64 `json:"x"`
			Y        *float64 `json:"y"`
			DeltaY   float64  `json:"deltaY"`
			Text     string   `json:"text"`
			Key      string   `json:"key"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Op) == "" {
			return "op is required", true
		}
		switch args.Op {
		case "screenshot", "open":
		case "osascript":
			if strings.TrimSpace(args.Script) == "" {
				return "script is required for osascript", true
			}
		case "click", "double_click", "right_click":
			if args.X == nil || args.Y == nil {
				return "x and y are required for " + args.Op, true
			}
		case "type":
			if args.Text == "" {
				return "text is required for type", true
			}
		case "key":
			if strings.TrimSpace(args.Key) == "" {
				return "key is required for key", true
			}
		case "scroll":
		default:
			return "unsupported op " + args.Op, true
		}
		payload := map[string]any{
			"nodeId":   args.NodeID,
			"botId":    s.selfBotID,
			"op":       args.Op,
			"script":   args.Script,
			"language": args.Language,
			"target":   args.Target,
			"app":      args.App,
			"x":        args.X,
			"y":        args.Y,
			"deltaY":   args.DeltaY,
			"text":     args.Text,
			"key":      args.Key,
		}
		body, err := s.doJSON(ctx, http.MethodPost, "/api/desktop/mcp", payload)
		if err != nil {
			return err.Error(), true
		}
		var res node.Result
		if err := json.Unmarshal(body, &res); err != nil {
			return prettyJSON(body), false
		}
		return formatNodeResult(args.Op, &res)

	case "request_takeover":
		var args struct {
			Reason string `json:"reason"`
			NodeID string `json:"nodeId"`
			App    string `json:"app"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Reason) == "" {
			return "reason is required", true
		}
		body, err := s.doJSON(ctx, http.MethodPost, "/api/desktop/takeover/request", map[string]any{
			"target": args.NodeID,
			"botId":  s.selfBotID,
			"reason": args.Reason,
			"app":    args.App,
		})
		if err != nil {
			return err.Error(), true
		}
		var resp struct {
			Note string `json:"note"`
		}
		if json.Unmarshal(body, &resp) == nil && resp.Note != "" {
			return resp.Note, false
		}
		return "人工接管已完成，可继续执行。", false

	case "list_sessions":
		var args struct {
			BotID string `json:"botId"`
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return "invalid arguments", true
			}
		}
		botID := strings.TrimSpace(args.BotID)
		if botID == "" {
			botID = s.selfBotID
		}
		q := url.Values{}
		if botID != "" {
			q.Set("botId", botID)
		}
		if s.selfBotID != "" {
			q.Set("callerBotId", s.selfBotID)
		}
		if strings.TrimSpace(args.Query) != "" {
			q.Set("query", strings.TrimSpace(args.Query))
		}
		if args.Limit > 0 {
			q.Set("limit", strconv.Itoa(args.Limit))
		}
		path := "/api/sessions"
		if enc := q.Encode(); enc != "" {
			path += "?" + enc
		}
		body, err := s.doJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err.Error(), true
		}
		return prettyJSON(body), false

	case "recall_session":
		var args struct {
			SessionID string `json:"sessionId"`
			BotID     string `json:"botId"`
			Tail      int    `json:"tail"`
			Offset    int    `json:"offset"`
			Limit     int    `json:"limit"`
			Query     string `json:"query"`
			MaxChars  int    `json:"maxChars"`
			MessageID string `json:"messageId"`
		}
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return "invalid arguments", true
			}
		}
		sid := strings.TrimSpace(args.SessionID)
		if sid == "" {
			sid = "previous"
		}
		botID := strings.TrimSpace(args.BotID)
		if botID == "" && sid == "previous" {
			botID = s.selfBotID
		}
		q := url.Values{}
		if botID != "" {
			q.Set("botId", botID)
		}
		if s.selfBotID != "" {
			q.Set("callerBotId", s.selfBotID)
		}
		if args.Tail > 0 {
			q.Set("tail", strconv.Itoa(args.Tail))
		}
		if args.Offset > 0 {
			q.Set("offset", strconv.Itoa(args.Offset))
		}
		if args.Limit > 0 {
			q.Set("limit", strconv.Itoa(args.Limit))
		}
		if strings.TrimSpace(args.Query) != "" {
			q.Set("query", strings.TrimSpace(args.Query))
		}
		if args.MaxChars > 0 {
			q.Set("maxChars", strconv.Itoa(args.MaxChars))
		}
		if strings.TrimSpace(args.MessageID) != "" {
			q.Set("messageId", strings.TrimSpace(args.MessageID))
		}
		path := "/api/sessions/" + url.PathEscape(sid)
		if enc := q.Encode(); enc != "" {
			path += "?" + enc
		}
		body, err := s.doJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err.Error(), true
		}
		return prettyJSON(body), false

	case "rotate_session":
		if s.selfBotID == "" {
			return "caller bot ID is unknown", true
		}
		var args struct {
			HandoffNote string `json:"handoffNote"`
		}
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return "invalid arguments", true
			}
		}
		body, err := s.doJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(s.selfBotID)+"/rotate", map[string]any{
			"handoffNote": args.HandoffNote,
			"defer":       true,
		})
		if err != nil {
			return err.Error(), true
		}
		return prettyJSON(body), false

	default:
		return fmt.Sprintf("unknown tool %q", name), true
	}
}

func (s *Server) callNode(ctx context.Context, nodeID, action string, args any, timeoutSec int) (string, bool) {
	targetID := strings.TrimSpace(nodeID)
	if targetID == "" {
		targetID = "_default"
	}
	payload := map[string]any{
		"action":     action,
		"args":       args,
		"timeoutSec": timeoutSec,
		"botId":      s.selfBotID,
	}
	body, err := s.doJSON(ctx, http.MethodPost, "/api/nodes/"+url.PathEscape(targetID)+"/call", payload)
	if err != nil {
		return err.Error(), true
	}
	var res node.Result
	if err := json.Unmarshal(body, &res); err != nil {
		return prettyJSON(body), false
	}
	return formatNodeResult(action, &res)
}

func formatNodeResult(action string, res *node.Result) (string, bool) {
	if !res.OK {
		msg := res.Error
		if msg == "" {
			msg = res.Output
		}
		if msg == "" {
			msg = "node command failed"
		}
		return msg, true
	}
	if res.SavedPath != "" {
		if res.Output != "" {
			return fmt.Sprintf("[node:%s saved=%s]\n%s", res.NodeID, res.SavedPath, res.Output), false
		}
		return fmt.Sprintf("[node:%s saved=%s]", res.NodeID, res.SavedPath), false
	}
	if action == "exec" {
		return fmt.Sprintf("[node:%s exit=%d]\n%s", res.NodeID, res.ExitCode, res.Output), false
	}
	if res.Output != "" {
		return res.Output, false
	}
	return "ok", false
}

func (s *Server) token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cachedToken == "" && s.configPath != "" {
		s.cachedToken = config.ReadAPIToken(s.configPath)
	}
	return s.cachedToken
}

func (s *Server) doJSON(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var rawBody []byte
	if payload != nil {
		var err error
		rawBody, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

	doOnce := func(tok string) (int, []byte, error) {
		var bodyReader io.Reader
		if rawBody != nil {
			bodyReader = bytes.NewReader(rawBody)
		}
		req, err := http.NewRequestWithContext(ctx, method, s.hubURL+path, bodyReader)
		if err != nil {
			return 0, nil, err
		}
		if rawBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return 0, nil, fmt.Errorf("bot-hub API unreachable at %s (is `bot-hub serve` running?): %w", s.hubURL, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return resp.StatusCode, b, err
	}

	tok := s.token()
	code, b, err := doOnce(tok)
	if err != nil {
		return nil, err
	}
	if code == http.StatusUnauthorized && s.configPath != "" {
		if fresh := config.ReadAPIToken(s.configPath); fresh != "" && fresh != tok {
			s.mu.Lock()
			s.cachedToken = fresh
			s.mu.Unlock()
			code, b, err = doOnce(fresh)
			if err != nil {
				return nil, err
			}
		}
	}
	if code >= 300 {
		var errObj struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &errObj) == nil && errObj.Error != "" {
			return nil, fmt.Errorf("%s", errObj.Error)
		}
		return nil, fmt.Errorf("HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func prettyJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}
