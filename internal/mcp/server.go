package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"bot-hub/internal/config"
)

type Server struct {
	client *Client
	role   string
}

func NewServer(hubURL, selfBotID string) *Server {
	return NewServerWithConfig(hubURL, selfBotID, "")
}

func NewServerWithConfig(hubURL, selfBotID, configPath string) *Server {
	return &Server{
		client: NewClient(hubURL, selfBotID, configPath, ""),
		role:   config.LookupEnv("PEBBLE_MCP_ROLE", "BOT_HUB_MCP_ROLE"),
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
					"name":    config.MCPServerName,
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
			Result:  map[string]any{"tools": s.visibleTools()},
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
			"name":        "send_message",
			"description": "Send a visible message (text, images, and/or files) to the user in Pebble. Your ordinary text output is internal and hidden from the user; call this tool whenever you want the user to see a reply, progress update, image, or file.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "Markdown text to show to the user (supports headings, tables, code blocks, lists, task lists, blockquotes, inline formatting, math, footnotes, and links; send local images via the images parameter rather than Markdown image syntax). Optional if images or files are provided.",
					},
					"images": map[string]any{
						"type":        "array",
						"description": "Optional list of images to attach (either local file paths relative to workspace or absolute, or objects with path and alt).",
						"items": map[string]any{
							"anyOf": []map[string]any{
								{"type": "string"},
								{
									"type": "object",
									"properties": map[string]any{
										"path": map[string]any{
											"type":        "string",
											"description": "Local image file path (png/jpg/gif/webp).",
										},
										"alt": map[string]any{
											"type":        "string",
											"description": "Optional alt description.",
										},
									},
									"required": []string{"path"},
								},
							},
						},
					},
					"files": map[string]any{
						"type":        "array",
						"description": "Optional list of files to attach (either local file paths relative to workspace or absolute, or objects with path and optional name).",
						"items": map[string]any{
							"anyOf": []map[string]any{
								{"type": "string"},
								{
									"type": "object",
									"properties": map[string]any{
										"path": map[string]any{
											"type":        "string",
											"description": "Local file path.",
										},
										"name": map[string]any{
											"type":        "string",
											"description": "Optional display filename.",
										},
									},
									"required": []string{"path"},
								},
							},
						},
					},
				},
			},
		},
		{
			"name":        "message_bot",
			"description": "Send a message to another bot in Pebble and return its reply. Both bots' chat timelines will record the collaboration.",
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
			"name":        "desktop_task",
			"description": "Delegate a multi-step desktop GUI or visible-browser task to an isolated computer-use worker running on the visible desktop (observable and take-over-ready via Remote Desktop). Use this instead of running headless browsers or multi-step screenshot/click loops in the main session.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal": map[string]any{
						"type":        "string",
						"description": "Clear description of the GUI goal to accomplish on the visible desktop.",
					},
					"nodeId": map[string]any{
						"type":        "string",
						"description": "Target desktop ('local', 'virtual:<slot>', remote node ID, or '<nodeId>:virtual:<slot>'). Omit to use this bot's bound screen or the active desktop target.",
					},
					"app": map[string]any{
						"type":        "string",
						"description": "Optional application name involved.",
					},
					"timeoutSec": map[string]any{
						"type":        "integer",
						"description": "Optional timeout in seconds.",
					},
				},
				"required": []string{"goal"},
			},
		},
		{
			"name":        "request_takeover",
			"description": "Pause automation and request a human to handle an interactive step (such as QR login, 2FA/OTP code, CAPTCHA, or payment confirmation) via the live Remote Desktop view. Blocks until the user clicks 'Done & Resume AI'.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reason": map[string]any{
						"type":        "string",
						"description": "Clear explanation shown to the user describing what to do on the screen.",
					},
					"nodeId": map[string]any{
						"type":        "string",
						"description": "Optional target desktop ('local', 'virtual:<slot>', or remote node ID). Omit to use this bot's bound screen.",
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
			"name":        "recall_session",
			"description": "Read sliced messages from a prior or active session (yours or another accessible bot's). Defaults to the most recent archived session ('previous') and last 20 messages with length protection.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId": map[string]any{
						"type":        "string",
						"description": "Target session ID (Hub ses_... ID or runtimeSessionId). Omit or pass 'previous' for the latest archived session, or 'active' for the current main session.",
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
			"name":        "open_stage",
			"description": "Submit a side-tab item to the user's Single-Bot Stage UI and post a clickable jump card in chat. By default autoOpen is false (the user clicks the jump card or topbar Stage button to jump to fullscreen; set autoOpen=true only when explicitly asked to jump immediately). Use kind='file' (or 'files' with path) to submit a file preview tab, kind='files' without path for the workspace browser, or kind='desktop' for Remote Desktop.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"file", "files", "desktop", "browser", "artifact", "mcp-app"},
						"description": "Stage tab kind to submit.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Workspace-relative file path (for kind='file' or 'files').",
					},
					"title": map[string]any{
						"type":        "string",
						"description": "Optional custom tab title.",
					},
					"subtitle": map[string]any{
						"type":        "string",
						"description": "Optional short subtitle shown on the chat jump card.",
					},
					"url": map[string]any{
						"type":        "string",
						"description": "Optional URL (for browser/artifact).",
					},
					"target": map[string]any{
						"type":        "string",
						"description": "Optional desktop target ID (for kind='desktop', defaults to 'local').",
					},
					"autoOpen": map[string]any{
						"type":        "boolean",
						"description": "If true, immediately switch the client into fullscreen Stage if not already open. Defaults to false (posts jump card and updates latest tab indicator).",
					},
				},
				"required": []string{"kind"},
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, name string, rawArgs json.RawMessage) (string, bool) {
	selfBotID := s.client.SelfBotID()
	switch name {
	case "send_message":
		if selfBotID == "" {
			return "caller bot ID is unknown", true
		}
		var args struct {
			Text   string            `json:"text"`
			Images []json.RawMessage `json:"images"`
			Files  []json.RawMessage `json:"files"`
		}
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return "invalid arguments", true
			}
		}
		type imgArg struct {
			Path string `json:"path"`
			Alt  string `json:"alt,omitempty"`
		}
		type fileArg struct {
			Path string `json:"path"`
			Name string `json:"name,omitempty"`
		}
		var images []imgArg
		for _, rawImg := range args.Images {
			var strPath string
			if err := json.Unmarshal(rawImg, &strPath); err == nil {
				if strings.TrimSpace(strPath) != "" {
					images = append(images, imgArg{Path: strings.TrimSpace(strPath)})
				}
				continue
			}
			var obj imgArg
			if err := json.Unmarshal(rawImg, &obj); err == nil && strings.TrimSpace(obj.Path) != "" {
				obj.Path = strings.TrimSpace(obj.Path)
				images = append(images, obj)
				continue
			}
			return "each image must be a file path string or {path, alt} object", true
		}
		var files []fileArg
		for _, rawFile := range args.Files {
			var strPath string
			if err := json.Unmarshal(rawFile, &strPath); err == nil {
				if strings.TrimSpace(strPath) != "" {
					files = append(files, fileArg{Path: strings.TrimSpace(strPath)})
				}
				continue
			}
			var obj fileArg
			if err := json.Unmarshal(rawFile, &obj); err == nil && strings.TrimSpace(obj.Path) != "" {
				obj.Path = strings.TrimSpace(obj.Path)
				files = append(files, obj)
				continue
			}
			return "each file must be a file path string or {path, name} object", true
		}
		if strings.TrimSpace(args.Text) == "" && len(images) == 0 && len(files) == 0 {
			return "text, images, or files is required", true
		}
		payload := map[string]any{
			"text": args.Text,
		}
		if len(images) > 0 {
			payload["images"] = images
		}
		if len(files) > 0 {
			payload["files"] = files
		}
		body, err := s.client.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(selfBotID)+"/outbound", payload)
		if err != nil {
			return err.Error(), true
		}
		var resp struct {
			ID    string `json:"id"`
			Media []any  `json:"media"`
		}
		if json.Unmarshal(body, &resp) == nil && resp.ID != "" {
			return fmt.Sprintf("Message sent to user (id=%s, attachments=%d).", resp.ID, len(resp.Media)), false
		}
		return PrettyJSON(body), false

	case "message_bot":
		var args struct {
			BotID   string `json:"botId"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.BotID) == "" || strings.TrimSpace(args.Message) == "" {
			return "botId and message are required", true
		}
		hop, _ := strconv.Atoi(config.LookupEnv("PEBBLE_HOP", "BOT_HUB_HOP"))
		payload := map[string]any{
			"message":   args.Message,
			"fromBotId": selfBotID,
			"hop":       hop,
		}
		body, err := s.client.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(args.BotID)+"/message", payload)
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
		return PrettyJSON(body), false

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
		return s.client.CallNode(ctx, args.NodeID, "exec", map[string]any{
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
		return s.client.CallNode(ctx, args.NodeID, action, map[string]any{
			"path":    args.Path,
			"content": args.Content,
			"append":  args.Append,
		}, 0)

	case "desktop_task":
		if s.role == "computer_use" {
			return "desktop_task cannot be called from inside a computer-use worker", true
		}
		if selfBotID == "" {
			return "caller bot ID is unknown", true
		}
		var args struct {
			Goal       string `json:"goal"`
			NodeID     string `json:"nodeId"`
			App        string `json:"app"`
			TimeoutSec int    `json:"timeoutSec"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Goal) == "" {
			return "goal is required", true
		}
		body, err := s.client.DoJSON(ctx, http.MethodPost, "/api/desktop/task", map[string]any{
			"botId":      selfBotID,
			"nodeId":     args.NodeID,
			"goal":       args.Goal,
			"app":        args.App,
			"timeoutSec": args.TimeoutSec,
		})
		if err != nil {
			return err.Error(), true
		}
		var resp struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(body, &resp) == nil && resp.Text != "" {
			return resp.Text, false
		}
		return PrettyJSON(body), false

	case "request_takeover":
		var args struct {
			Reason string `json:"reason"`
			NodeID string `json:"nodeId"`
			App    string `json:"app"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Reason) == "" {
			return "reason is required", true
		}
		body, err := s.client.DoJSON(ctx, http.MethodPost, "/api/desktop/takeover/request", map[string]any{
			"target": args.NodeID,
			"botId":  selfBotID,
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
		return "Manual takeover completed; you may continue.", false

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
			botID = selfBotID
		}
		q := url.Values{}
		if botID != "" {
			q.Set("botId", botID)
		}
		if selfBotID != "" {
			q.Set("callerBotId", selfBotID)
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
		body, err := s.client.DoJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err.Error(), true
		}
		return PrettyJSON(body), false

	case "open_stage":
		if selfBotID == "" {
			return "caller bot ID is unknown", true
		}
		var args struct {
			Kind     string `json:"kind"`
			Path     string `json:"path"`
			Title    string `json:"title"`
			Subtitle string `json:"subtitle"`
			URL      string `json:"url"`
			Target   string `json:"target"`
			AutoOpen bool   `json:"autoOpen"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Kind) == "" {
			return "kind is required", true
		}
		body, err := s.client.DoJSON(ctx, http.MethodPost, "/api/bots/"+url.PathEscape(selfBotID)+"/stage", map[string]any{
			"kind":     strings.TrimSpace(args.Kind),
			"path":     strings.TrimSpace(args.Path),
			"title":    strings.TrimSpace(args.Title),
			"subtitle": strings.TrimSpace(args.Subtitle),
			"url":      strings.TrimSpace(args.URL),
			"target":   strings.TrimSpace(args.Target),
			"autoOpen": args.AutoOpen,
		})
		if err != nil {
			return err.Error(), true
		}
		return PrettyJSON(body), false

	default:
		return fmt.Sprintf("unknown tool %q", name), true
	}
}

func (s *Server) visibleTools() []map[string]any {
	all := toolDefinitions()
	if s.role == "computer_use" {
		out := make([]map[string]any, 0, 3)
		for _, t := range all {
			switch t["name"] {
			case "node_exec", "node_file", "request_takeover":
				out = append(out, t)
			}
		}
		return out
	}
	return all
}
