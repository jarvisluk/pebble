package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	apnsProdBaseURL    = "https://api.push.apple.com"
	apnsSandboxBaseURL = "https://api.sandbox.push.apple.com"
	jwtCacheTTL        = 50 * time.Minute
)

type APNsSenderOptions struct {
	KeyPath        string
	KeyPEM         string
	KeyID          string
	TeamID         string
	DefaultBundle  string
	DefaultEnv     string
	BaseURLProd    string
	BaseURLSandbox string
	HTTPClient     *http.Client
}

type APNsSender struct {
	key            *ecdsa.PrivateKey
	keyID          string
	teamID         string
	defaultBundle  string
	defaultEnv     string
	baseURLProd    string
	baseURLSandbox string
	client         *http.Client

	jwtMu    sync.Mutex
	jwtToken string
	jwtAt    time.Time
}

func NewAPNsSender(opts APNsSenderOptions) (*APNsSender, error) {
	pemBytes := []byte(strings.TrimSpace(opts.KeyPEM))
	if len(pemBytes) == 0 && strings.TrimSpace(opts.KeyPath) != "" {
		b, err := os.ReadFile(strings.TrimSpace(opts.KeyPath))
		if err != nil {
			return nil, fmt.Errorf("read apns key %s: %w", opts.KeyPath, err)
		}
		pemBytes = b
	}
	if len(pemBytes) == 0 || strings.TrimSpace(opts.KeyID) == "" || strings.TrimSpace(opts.TeamID) == "" {
		return nil, ErrNotConfigured
	}
	key, err := parseP8PrivateKey(pemBytes)
	if err != nil {
		return nil, err
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	prodURL := strings.TrimRight(strings.TrimSpace(opts.BaseURLProd), "/")
	if prodURL == "" {
		prodURL = apnsProdBaseURL
	}
	sandboxURL := strings.TrimRight(strings.TrimSpace(opts.BaseURLSandbox), "/")
	if sandboxURL == "" {
		sandboxURL = apnsSandboxBaseURL
	}
	defaultEnv := strings.ToLower(strings.TrimSpace(opts.DefaultEnv))
	if defaultEnv != "sandbox" {
		defaultEnv = "production"
	}
	return &APNsSender{
		key:            key,
		keyID:          strings.TrimSpace(opts.KeyID),
		teamID:         strings.TrimSpace(opts.TeamID),
		defaultBundle:  strings.TrimSpace(opts.DefaultBundle),
		defaultEnv:     defaultEnv,
		baseURLProd:    prodURL,
		baseURLSandbox: sandboxURL,
		client:         client,
	}, nil
}

func (s *APNsSender) Mode() string { return "direct" }

func (s *APNsSender) Send(ctx context.Context, req SendRequest) error {
	if strings.TrimSpace(req.Token) == "" {
		return errors.New("device token is required")
	}
	bundleID := strings.TrimSpace(req.BundleID)
	if bundleID == "" {
		bundleID = s.defaultBundle
	}
	topic := strings.TrimSpace(req.Topic)
	if topic == "" {
		if bundleID == "" {
			return errors.New("bundleId or topic is required")
		}
		if req.PushType == PushTypeLiveActivity {
			topic = bundleID + ".push-type.liveactivity"
		} else {
			topic = bundleID
		}
	}

	env := strings.ToLower(strings.TrimSpace(req.Env))
	if env == "" {
		env = s.defaultEnv
	}
	baseURL := s.baseURLProd
	if env == "sandbox" {
		baseURL = s.baseURLSandbox
	}

	body, err := json.Marshal(req.Payload)
	if err != nil {
		return err
	}
	jwtStr, err := s.bearerJWT()
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/3/device/%s", baseURL, strings.TrimSpace(req.Token))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "bearer "+jwtStr)
	httpReq.Header.Set("Content-Type", "application/json")
	pushType := req.PushType
	if pushType == "" {
		pushType = PushTypeAlert
	}
	httpReq.Header.Set("apns-push-type", string(pushType))
	httpReq.Header.Set("apns-topic", topic)
	priority := req.Priority
	if priority == 0 {
		priority = 10
	}
	httpReq.Header.Set("apns-priority", fmt.Sprintf("%d", priority))
	if cid := strings.TrimSpace(req.CollapseID); cid != "" {
		httpReq.Header.Set("apns-collapse-id", cid)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var apnsErr struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(respBody, &apnsErr)
	if resp.StatusCode == http.StatusGone ||
		apnsErr.Reason == "BadDeviceToken" ||
		apnsErr.Reason == "Unregistered" ||
		apnsErr.Reason == "DeviceTokenNotForTopic" {
		return fmt.Errorf("%w: status=%d reason=%s", ErrInvalidToken, resp.StatusCode, apnsErr.Reason)
	}
	return fmt.Errorf("apns error status=%d reason=%s", resp.StatusCode, apnsErr.Reason)
}

func (s *APNsSender) bearerJWT() (string, error) {
	s.jwtMu.Lock()
	defer s.jwtMu.Unlock()
	now := time.Now()
	if s.jwtToken != "" && now.Sub(s.jwtAt) < jwtCacheTTL {
		return s.jwtToken, nil
	}
	tok, err := signES256JWT(s.key, s.keyID, s.teamID, now.Unix())
	if err != nil {
		return "", err
	}
	s.jwtToken = tok
	s.jwtAt = now
	return tok, nil
}

func signES256JWT(key *ecdsa.PrivateKey, keyID, teamID string, iat int64) (string, error) {
	headerJSON, _ := json.Marshal(map[string]string{
		"alg": "ES256",
		"kid": keyID,
	})
	claimsJSON, _ := json.Marshal(map[string]any{
		"iss": teamID,
		"iat": iat,
	})
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimsJSON)

	sum := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	sig := encodeP1363(r, s, 32)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func encodeP1363(r, s *big.Int, coordLen int) []byte {
	out := make([]byte, coordLen*2)
	rb := r.Bytes()
	sb := s.Bytes()
	copy(out[coordLen-len(rb):coordLen], rb)
	copy(out[coordLen*2-len(sb):], sb)
	return out
}

func parseP8PrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("invalid APNs .p8 PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		ec, ecErr := x509.ParseECPrivateKey(block.Bytes)
		if ecErr != nil {
			return nil, fmt.Errorf("parse APNs private key: %w", err)
		}
		return ec, nil
	}
	ec, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("APNs key is not ECDSA")
	}
	return ec, nil
}
