package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	DefaultRepo    = "jarvisluk/pebble"
	DefaultAPIBase = "https://api.github.com"

	ChannelStable     = "stable"
	ChannelPrerelease = "prerelease"

	ErrCodeRateLimited     = "rate_limited"
	ErrCodeNetwork         = "network_error"
	ErrCodeInvalidResponse = "invalid_response"
	ErrCodeInvalidProxy    = "invalid_proxy"

	maxNotesRunes = 8000
)

// ReleaseInfo describes a GitHub Release discovered by the update checker.
type ReleaseInfo struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Name        string `json:"name,omitempty"`
	URL         string `json:"url"`
	Notes       string `json:"notes,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	Prerelease  bool   `json:"prerelease,omitempty"`
}

// CheckError wraps a structured error code with an English diagnostic message.
type CheckError struct {
	Code    string
	Message string
	Err     error
}

func (e *CheckError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *CheckError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Checker queries GitHub Releases API for newer versions.
type Checker struct {
	Repo           string
	APIBase        string
	CurrentVersion string
	Token          string
	HTTPClient     *http.Client
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
}

func (c *Checker) effectiveRepo() string {
	if v := strings.TrimSpace(os.Getenv("PEBBLE_UPDATE_REPO")); v != "" {
		return strings.Trim(v, "/")
	}
	if v := strings.TrimSpace(c.Repo); v != "" {
		return strings.Trim(v, "/")
	}
	return DefaultRepo
}

func (c *Checker) effectiveAPIBase() string {
	if v := strings.TrimSpace(os.Getenv("PEBBLE_UPDATE_API_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(c.APIBase); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultAPIBase
}

func (c *Checker) effectiveToken() string {
	if v := strings.TrimSpace(c.Token); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("PEBBLE_GITHUB_TOKEN")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

// ValidateProxyURL validates an optional proxy URL (http, https, socks5, socks5h).
func ValidateProxyURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", &CheckError{Code: ErrCodeInvalidProxy, Message: fmt.Sprintf("invalid proxy URL %q", raw)}
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return s, nil
	default:
		return "", &CheckError{Code: ErrCodeInvalidProxy, Message: fmt.Sprintf("unsupported proxy scheme %q (use http, https, or socks5)", u.Scheme)}
	}
}

func (c *Checker) httpClient(proxyURL string) (*http.Client, error) {
	cleanProxy, err := ValidateProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	if cleanProxy == "" && c.HTTPClient != nil {
		return c.HTTPClient, nil
	}
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if cleanProxy != "" {
		u, _ := url.Parse(cleanProxy)
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: tr,
	}, nil
}

// Check queries GitHub Releases for the latest release in channel.
// When cachedETag is non-empty and the server returns 304 Not Modified, cached is returned.
// When no release exists yet (404 or empty list), (nil, "", nil) is returned.
func (c *Checker) Check(ctx context.Context, channel, proxyURL, cachedETag string, cached *ReleaseInfo) (*ReleaseInfo, string, error) {
	ch := strings.ToLower(strings.TrimSpace(channel))
	if ch == "" {
		ch = ChannelStable
	}
	if ch != ChannelStable && ch != ChannelPrerelease {
		return nil, "", fmt.Errorf("invalid update channel %q (expected stable or prerelease)", channel)
	}

	client, err := c.httpClient(proxyURL)
	if err != nil {
		return nil, "", err
	}

	repo := c.effectiveRepo()
	base := c.effectiveAPIBase()
	endpoint := fmt.Sprintf("%s/repos/%s/releases/latest", base, repo)
	if ch == ChannelPrerelease {
		endpoint = fmt.Sprintf("%s/repos/%s/releases?per_page=10", base, repo)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", &CheckError{Code: ErrCodeNetwork, Message: err.Error(), Err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	uaVer := strings.TrimSpace(c.CurrentVersion)
	if uaVer == "" {
		uaVer = "dev"
	}
	req.Header.Set("User-Agent", "Pebble/"+uaVer)
	if tok := c.effectiveToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if cachedETag != "" && cached != nil {
		req.Header.Set("If-None-Match", cachedETag)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", &CheckError{
			Code:    ErrCodeNetwork,
			Message: fmt.Sprintf("unable to reach GitHub: %v", err),
			Err:     err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && cached != nil {
		etag := strings.TrimSpace(resp.Header.Get("ETag"))
		if etag == "" {
			etag = cachedETag
		}
		cp := *cached
		return &cp, etag, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, "", nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, "", &CheckError{Code: ErrCodeNetwork, Message: err.Error(), Err: err}
	}

	if resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && isRateLimitResponse(resp, body)) {
		return nil, "", &CheckError{
			Code:    ErrCodeRateLimited,
			Message: "GitHub API rate limit reached; try again later",
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := extractGitHubError(body)
		if msg == "" {
			msg = fmt.Sprintf("GitHub API returned HTTP %d", resp.StatusCode)
		}
		return nil, "", &CheckError{Code: ErrCodeInvalidResponse, Message: msg}
	}

	etag := strings.TrimSpace(resp.Header.Get("ETag"))
	if ch == ChannelStable {
		var rel githubRelease
		if err := json.Unmarshal(body, &rel); err != nil {
			return nil, "", &CheckError{Code: ErrCodeInvalidResponse, Message: "invalid release JSON from GitHub", Err: err}
		}
		if rel.Draft {
			return nil, etag, nil
		}
		info, ok := toReleaseInfo(rel, repo)
		if !ok {
			return nil, "", &CheckError{Code: ErrCodeInvalidResponse, Message: fmt.Sprintf("invalid release tag %q", rel.TagName)}
		}
		return info, etag, nil
	}

	var list []githubRelease
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, "", &CheckError{Code: ErrCodeInvalidResponse, Message: "invalid releases JSON from GitHub", Err: err}
	}
	var best *ReleaseInfo
	var bestVer Version
	for _, rel := range list {
		if rel.Draft {
			continue
		}
		info, ok := toReleaseInfo(rel, repo)
		if !ok {
			continue
		}
		v, _ := ParseVersion(info.Version)
		if best == nil || Compare(v, bestVer) > 0 {
			best = info
			bestVer = v
		}
	}
	return best, etag, nil
}

func toReleaseInfo(rel githubRelease, repo string) (*ReleaseInfo, bool) {
	tag := strings.TrimSpace(rel.TagName)
	v, ok := ParseVersion(tag)
	if !ok {
		return nil, false
	}
	urlStr := strings.TrimSpace(rel.HTMLURL)
	if urlStr == "" {
		urlStr = fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, tag)
	}
	notes := strings.TrimSpace(rel.Body)
	if r := []rune(notes); len(r) > maxNotesRunes {
		notes = string(r[:maxNotesRunes-1]) + "…"
	}
	name := strings.TrimSpace(rel.Name)
	if name == "" {
		name = tag
	}
	return &ReleaseInfo{
		Version:     v.Normalized,
		Tag:         tag,
		Name:        name,
		URL:         urlStr,
		Notes:       notes,
		PublishedAt: strings.TrimSpace(rel.PublishedAt),
		Prerelease:  rel.Prerelease || len(v.Prerelease) > 0,
	}, true
}

func isRateLimitResponse(resp *http.Response, body []byte) bool {
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "rate limit")
}

func extractGitHubError(body []byte) string {
	var doc struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &doc) == nil {
		return strings.TrimSpace(doc.Message)
	}
	return ""
}

// ErrorCodeOf extracts the structured error code from err when available.
func ErrorCodeOf(err error) string {
	var ce *CheckError
	if errors.As(err, &ce) && ce != nil {
		return ce.Code
	}
	return ""
}
