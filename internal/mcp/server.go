package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	hubURL     string
	selfBotID  string
	httpClient *http.Client
}

func NewServer(hubURL, selfBotID string) *Server {
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if hubURL == "" {
		hubURL = "http://127.0.0.1:3789"
	}
	if selfBotID == "" {
		selfBotID = os.Getenv("BOT_HUB_BOT_ID")
	}
	return &Server{
		hubURL:     hubURL,
		selfBotID:  selfBotID,
		httpClient: &http.Client{Timeout: 10 * time.Minute},
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

	default:
		return fmt.Sprintf("unknown tool %q", name), true
	}
}

func (s *Server) doJSON(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.hubURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := os.Getenv("BOT_HUB_API_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bot-hub API unreachable at %s (is `bot-hub serve` running?): %w", s.hubURL, err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		var errObj struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &errObj) == nil && errObj.Error != "" {
			return nil, fmt.Errorf("%s", errObj.Error)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
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
