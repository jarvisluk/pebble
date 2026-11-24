package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/node"
)

// Client talks to a running Pebble HTTP server on behalf of a bot (used by
// both the stdio MCP server and `pebble` CLI subcommands).
type Client struct {
	hubURL     string
	selfBotID  string
	configPath string
	httpClient *http.Client

	mu          sync.Mutex
	cachedToken string
}

// NewClient constructs a Hub API client, falling back to environment variables
// when explicit arguments are empty.
func NewClient(hubURL, selfBotID, configPath, explicitToken string) *Client {
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if hubURL == "" {
		hubURL = strings.TrimRight(config.LookupEnv("PEBBLE_URL", "BOT_HUB_URL"), "/")
	}
	if hubURL == "" {
		hubURL = "http://127.0.0.1:3789"
	}
	selfBotID = strings.TrimSpace(selfBotID)
	if selfBotID == "" {
		selfBotID = config.LookupEnv("PEBBLE_BOT_ID", "BOT_HUB_BOT_ID")
	}
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = config.LookupEnv("PEBBLE_CONFIG", "BOT_HUB_CONFIG")
	}
	tok := strings.TrimSpace(explicitToken)
	if tok == "" {
		tok = config.LookupEnv("PEBBLE_API_TOKEN", "BOT_HUB_API_TOKEN")
	}
	return &Client{
		hubURL:      hubURL,
		selfBotID:   selfBotID,
		configPath:  configPath,
		httpClient:  &http.Client{Timeout: 10 * time.Minute},
		cachedToken: tok,
	}
}

func (c *Client) HubURL() string     { return c.hubURL }
func (c *Client) SelfBotID() string  { return c.selfBotID }
func (c *Client) ConfigPath() string { return c.configPath }

func (c *Client) ResolveBotID(explicit string) string {
	if id := strings.TrimSpace(explicit); id != "" {
		return id
	}
	return c.selfBotID
}

func (c *Client) token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedToken == "" && c.configPath != "" {
		c.cachedToken = config.ReadAPIToken(c.configPath)
	}
	return c.cachedToken
}

func (c *Client) DoJSON(ctx context.Context, method, path string, payload any) ([]byte, error) {
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
		req, err := http.NewRequestWithContext(ctx, method, c.hubURL+path, bodyReader)
		if err != nil {
			return 0, nil, err
		}
		if rawBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return 0, nil, fmt.Errorf("pebble API unreachable at %s (is `pebble serve` running?): %w", c.hubURL, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return resp.StatusCode, b, err
	}

	tok := c.token()
	code, b, err := doOnce(tok)
	if err != nil {
		return nil, err
	}
	if code == http.StatusUnauthorized && c.configPath != "" {
		if fresh := config.ReadAPIToken(c.configPath); fresh != "" && fresh != tok {
			c.mu.Lock()
			c.cachedToken = fresh
			c.mu.Unlock()
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

func (c *Client) CallNode(ctx context.Context, nodeID, action string, args any, timeoutSec int) (string, bool) {
	targetID := strings.TrimSpace(nodeID)
	if targetID == "" {
		targetID = "_default"
	}
	payload := map[string]any{
		"action":     action,
		"args":       args,
		"timeoutSec": timeoutSec,
		"botId":      c.selfBotID,
	}
	body, err := c.DoJSON(ctx, http.MethodPost, "/api/nodes/"+url.PathEscape(targetID)+"/call", payload)
	if err != nil {
		return err.Error(), true
	}
	var res node.Result
	if err := json.Unmarshal(body, &res); err != nil {
		return PrettyJSON(body), false
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

func PrettyJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}
