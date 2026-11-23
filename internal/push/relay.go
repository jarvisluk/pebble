package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RelaySender struct {
	endpoint string
	client   *http.Client
}

func NewRelaySender(relayURL string, client *http.Client) (*RelaySender, error) {
	u := strings.TrimSpace(relayURL)
	if u == "" {
		return nil, ErrNotConfigured
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &RelaySender{
		endpoint: u,
		client:   client,
	}, nil
}

func (s *RelaySender) Mode() string { return "relay" }

func (s *RelaySender) Send(ctx context.Context, req SendRequest) error {
	if strings.TrimSpace(req.Token) == "" {
		return errors.New("device token is required")
	}
	body, err := json.Marshal(map[string]any{
		"platform":   req.Platform,
		"env":        req.Env,
		"bundleId":   req.BundleID,
		"pushType":   req.PushType,
		"topic":      req.Topic,
		"token":      req.Token,
		"priority":   req.Priority,
		"collapseId": req.CollapseID,
		"payload":    req.Payload,
	})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusGone || strings.Contains(string(respBody), "BadDeviceToken") || strings.Contains(string(respBody), "Unregistered") {
		return fmt.Errorf("%w: relay status=%d", ErrInvalidToken, resp.StatusCode)
	}
	return fmt.Errorf("push relay status=%d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}
