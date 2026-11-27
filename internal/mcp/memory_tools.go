package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func memoryToolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "recall_memory",
			"description": "Search Pebble's native persistent memory (compact L1 hits). Supports filtering by scope, botId, kind, tags, status, and optional session history fusion (includeSessions=true).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search keywords or natural-language query. Omit to browse recent items.",
					},
					"scope": map[string]any{
						"type":        "string",
						"enum":        []string{"global", "bot", "all"},
						"description": "Memory scope to search ('all' by default).",
					},
					"botId": map[string]any{
						"type":        "string",
						"description": "Optional target bot ID when scope is 'bot'. Defaults to this bot when scope='bot'.",
					},
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"profile", "preference", "rule", "fact", "decision", "procedure", "reference", "episode"},
						"description": "Optional memory kind filter.",
					},
					"tags": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional tag filters (all listed tags must match).",
					},
					"status": map[string]any{
						"type":        "string",
						"enum":        []string{"active", "candidate", "superseded", "archived", "all"},
						"description": "Optional item status filter (default 'active').",
					},
					"includeSessions": map[string]any{
						"type":        "boolean",
						"description": "When true, also fuse matching snippets from past chat sessions.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max items to return (default 20, max 100).",
					},
				},
			},
		},
		{
			"name":        "read_memory",
			"description": "Read the full L2 content of a Pebble memory item by id, or read a line-sliced workspace memory file by path (jailed to _shared/USER.md and memory/ directories).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Memory item ID (e.g. mem_abcd1234).",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Workspace-relative memory file path (e.g. '_shared/USER.md', 'MEMORY.md', 'daily/2026-11-27.md').",
					},
					"botId": map[string]any{
						"type":        "string",
						"description": "Optional bot ID when reading a bot-relative path (defaults to this bot).",
					},
					"startLine": map[string]any{
						"type":        "integer",
						"description": "Optional 1-based start line for line-sliced reads.",
					},
					"endLine": map[string]any{
						"type":        "integer",
						"description": "Optional 1-based inclusive end line.",
					},
				},
			},
		},
		{
			"name":        "save_memory",
			"description": "Create or update a durable memory item in Pebble's Markdown memory store, or append a note to today's daily log (daily=true). Rejects plaintext secrets/API keys.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "Durable memory content to store (Markdown).",
					},
					"title": map[string]any{
						"type":        "string",
						"description": "Short concise title (derived from text if omitted).",
					},
					"scope": map[string]any{
						"type":        "string",
						"enum":        []string{"bot", "global"},
						"description": "Target scope ('bot' by default for bot-local notes, 'global' for hub-wide user preferences).",
					},
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"profile", "preference", "rule", "fact", "decision", "procedure", "reference", "episode"},
						"description": "Memory category (default 'fact').",
					},
					"tags": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional tags for retrieval and grouping.",
					},
					"why": map[string]any{
						"type":        "string",
						"description": "Optional rationale or source context.",
					},
					"id": map[string]any{
						"type":        "string",
						"description": "Existing memory ID to update in place.",
					},
					"pin": map[string]any{
						"type":        "boolean",
						"description": "Pin this memory so it stays prioritized and protected from Dream mutations.",
					},
					"validFrom": map[string]any{
						"type":        "string",
						"description": "Optional RFC3339 timestamp when this fact became valid.",
					},
					"supersedes": map[string]any{
						"type":        "string",
						"description": "Optional existing memory ID that this new memory replaces.",
					},
					"daily": map[string]any{
						"type":        "boolean",
						"description": "When true, append as a working log entry under memory/daily/YYYY-MM-DD.md.",
					},
				},
				"required": []string{"text"},
			},
		},
		{
			"name":        "forget_memory",
			"description": "Archive (default) or hard-delete an obsolete or incorrect Pebble memory item by ID.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Memory item ID to forget.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Optional reason why this memory is being retired.",
					},
					"hardDelete": map[string]any{
						"type":        "boolean",
						"description": "When true, permanently delete the Markdown file instead of soft-archiving.",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

func (s *Server) callMemoryTool(ctx context.Context, name string, rawArgs json.RawMessage) (string, bool, bool) {
	switch name {
	case "recall_memory", "read_memory", "save_memory", "forget_memory":
	default:
		return "", false, false
	}
	if s.role == "computer_use" {
		return name + " cannot be called from inside a computer-use worker", true, true
	}
	selfBotID := s.client.SelfBotID()

	switch name {
	case "recall_memory":
		var args struct {
			Query           string   `json:"query"`
			Scope           string   `json:"scope"`
			BotID           string   `json:"botId"`
			Kind            string   `json:"kind"`
			Tags            []string `json:"tags"`
			Status          string   `json:"status"`
			IncludeSessions bool     `json:"includeSessions"`
			Limit           int      `json:"limit"`
		}
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return "invalid arguments", true, true
			}
		}
		scope := strings.TrimSpace(args.Scope)
		botID := strings.TrimSpace(args.BotID)
		if scope == "" && botID == "" {
			scope = "all"
		}
		if scope == "bot" && botID == "" {
			botID = selfBotID
		}
		q := url.Values{}
		q.Set("compact", "1")
		if scope != "" {
			q.Set("scope", scope)
		}
		if botID != "" {
			q.Set("botId", botID)
		}
		if selfBotID != "" {
			q.Set("callerBotId", selfBotID)
		}
		if strings.TrimSpace(args.Query) != "" {
			q.Set("query", strings.TrimSpace(args.Query))
		}
		if strings.TrimSpace(args.Kind) != "" {
			q.Set("kind", strings.TrimSpace(args.Kind))
		}
		if len(args.Tags) > 0 {
			q.Set("tags", strings.Join(args.Tags, ","))
		}
		if strings.TrimSpace(args.Status) != "" {
			q.Set("status", strings.TrimSpace(args.Status))
		}
		if args.IncludeSessions {
			q.Set("includeSessions", "1")
		}
		if args.Limit > 0 {
			q.Set("limit", strconv.Itoa(args.Limit))
		}
		body, err := s.client.DoJSON(ctx, http.MethodGet, "/api/memory?"+q.Encode(), nil)
		if err != nil {
			return err.Error(), true, true
		}
		return PrettyJSON(body), false, true

	case "read_memory":
		var args struct {
			ID        string `json:"id"`
			Path      string `json:"path"`
			BotID     string `json:"botId"`
			StartLine int    `json:"startLine"`
			EndLine   int    `json:"endLine"`
		}
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return "invalid arguments", true, true
			}
		}
		id := strings.TrimSpace(args.ID)
		rawPath := strings.TrimSpace(args.Path)
		if id == "" && rawPath == "" {
			return "id or path is required", true, true
		}
		pathSeg := id
		if pathSeg == "" {
			pathSeg = "_"
		}
		q := url.Values{}
		if rawPath != "" {
			q.Set("path", rawPath)
		}
		botID := strings.TrimSpace(args.BotID)
		if botID == "" && id == "" && selfBotID != "" {
			botID = selfBotID
		}
		if botID != "" {
			q.Set("botId", botID)
		}
		if selfBotID != "" {
			q.Set("callerBotId", selfBotID)
		}
		if args.StartLine > 0 {
			q.Set("startLine", strconv.Itoa(args.StartLine))
		}
		if args.EndLine > 0 {
			q.Set("endLine", strconv.Itoa(args.EndLine))
		}
		apiPath := "/api/memory/" + url.PathEscape(pathSeg)
		if enc := q.Encode(); enc != "" {
			apiPath += "?" + enc
		}
		body, err := s.client.DoJSON(ctx, http.MethodGet, apiPath, nil)
		if err != nil {
			return err.Error(), true, true
		}
		return PrettyJSON(body), false, true

	case "save_memory":
		var args struct {
			Text       string   `json:"text"`
			Title      string   `json:"title"`
			Scope      string   `json:"scope"`
			Kind       string   `json:"kind"`
			Tags       []string `json:"tags"`
			Why        string   `json:"why"`
			ID         string   `json:"id"`
			Pin        *bool    `json:"pin"`
			ValidFrom  string   `json:"validFrom"`
			Supersedes string   `json:"supersedes"`
			Daily      bool     `json:"daily"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Text) == "" {
			return "text is required", true, true
		}
		scope := strings.TrimSpace(args.Scope)
		if scope == "" {
			if selfBotID != "" {
				scope = "bot"
			} else {
				scope = "global"
			}
		}
		payload := map[string]any{
			"text":         args.Text,
			"scope":        scope,
			"sourceAuthor": "agent",
		}
		if selfBotID != "" {
			payload["callerBotId"] = selfBotID
			if !strings.EqualFold(scope, "global") {
				payload["botId"] = selfBotID
			}
		}
		if strings.TrimSpace(args.Title) != "" {
			payload["title"] = strings.TrimSpace(args.Title)
		}
		if strings.TrimSpace(args.Kind) != "" {
			payload["kind"] = strings.TrimSpace(args.Kind)
		}
		if len(args.Tags) > 0 {
			payload["tags"] = args.Tags
		}
		if strings.TrimSpace(args.Why) != "" {
			payload["why"] = strings.TrimSpace(args.Why)
		}
		if strings.TrimSpace(args.ID) != "" {
			payload["id"] = strings.TrimSpace(args.ID)
		}
		if args.Pin != nil {
			payload["pin"] = *args.Pin
		}
		if strings.TrimSpace(args.ValidFrom) != "" {
			payload["validFrom"] = strings.TrimSpace(args.ValidFrom)
		}
		if strings.TrimSpace(args.Supersedes) != "" {
			payload["supersedes"] = strings.TrimSpace(args.Supersedes)
		}
		if args.Daily {
			payload["daily"] = true
		}
		body, err := s.client.DoJSON(ctx, http.MethodPost, "/api/memory", payload)
		if err != nil {
			return err.Error(), true, true
		}
		return PrettyJSON(body), false, true

	case "forget_memory":
		var args struct {
			ID         string `json:"id"`
			Reason     string `json:"reason"`
			HardDelete bool   `json:"hardDelete"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.ID) == "" {
			return "id is required", true, true
		}
		q := url.Values{}
		if selfBotID != "" {
			q.Set("callerBotId", selfBotID)
		}
		if strings.TrimSpace(args.Reason) != "" {
			q.Set("reason", strings.TrimSpace(args.Reason))
		}
		if args.HardDelete {
			q.Set("hard", "1")
		}
		apiPath := "/api/memory/" + url.PathEscape(strings.TrimSpace(args.ID))
		if enc := q.Encode(); enc != "" {
			apiPath += "?" + enc
		}
		body, err := s.client.DoJSON(ctx, http.MethodDelete, apiPath, nil)
		if err != nil {
			return err.Error(), true, true
		}
		return PrettyJSON(body), false, true
	}
	return "", false, false
}
