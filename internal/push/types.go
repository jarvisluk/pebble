package push

import (
	"context"
	"errors"
)

var (
	// ErrInvalidToken indicates APNs/Relay rejected the device or activity token permanently (410 / BadDeviceToken / Unregistered).
	ErrInvalidToken = errors.New("push token invalid or unregistered")
	// ErrNotConfigured indicates push credentials or relay URL are missing.
	ErrNotConfigured = errors.New("push sender not configured")
)

type PushType string

const (
	PushTypeAlert        PushType = "alert"
	PushTypeLiveActivity PushType = "liveactivity"
)

// SendRequest represents a single outbound push notification or Live Activity update.
type SendRequest struct {
	Platform   string
	Env        string // "production" | "sandbox"
	BundleID   string
	PushType   PushType
	Topic      string
	Token      string
	Priority   int    // 10 (high/immediate) or 5 (power-saving)
	CollapseID string // optional apns-collapse-id
	Payload    any
}

type Sender interface {
	Send(ctx context.Context, req SendRequest) error
	Mode() string
}

type Status struct {
	Configured  bool   `json:"configured"`
	Mode        string `json:"mode"` // "direct" | "relay" | "disabled"
	DeviceCount int    `json:"deviceCount"`
}

// LiveActivityContentState mirrors PebbleActivityAttributes.ContentState on iOS.
type LiveActivityContentState struct {
	Phase          string  `json:"phase"`                    // "running" | "takeover" | "completed" | "error"
	StatusText     string  `json:"statusText"`               // e.g. "正在运行 bash"
	StepCount      int     `json:"stepCount"`                // tool call count
	StartedAt      float64 `json:"startedAt"`                // Unix timestamp seconds
	Summary        string  `json:"summary,omitempty"`        // final result or error summary
	TakeoverReason string  `json:"takeoverReason,omitempty"` // reason when phase == "takeover"
}

// LiveActivityAttributes mirrors static fields of PebbleActivityAttributes on iOS.
type LiveActivityAttributes struct {
	Kind    string `json:"kind"` // "bot" | "desktop"
	BotID   string `json:"botId,omitempty"`
	BotName string `json:"botName,omitempty"`
	Target  string `json:"target,omitempty"`
}

type APSAlert struct {
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body,omitempty"`
}

type APSBody struct {
	Alert             *APSAlert                 `json:"alert,omitempty"`
	Sound             string                    `json:"sound,omitempty"`
	InterruptionLevel string                    `json:"interruption-level,omitempty"` // "passive" | "active" | "time-sensitive"
	Event             string                    `json:"event,omitempty"`              // "start" | "update" | "end"
	AttributesType    string                    `json:"attributes-type,omitempty"`    // "PebbleActivityAttributes"
	Attributes        *LiveActivityAttributes   `json:"attributes,omitempty"`
	ContentState      *LiveActivityContentState `json:"content-state,omitempty"`
	Timestamp         int64                     `json:"timestamp,omitempty"`
	DismissalDate     int64                     `json:"dismissal-date,omitempty"`
}

type APNSPayload struct {
	APS      APSBody `json:"aps"`
	DeepLink string  `json:"deepLink,omitempty"`
	BotID    string  `json:"botId,omitempty"`
	Target   string  `json:"target,omitempty"`
	Kind     string  `json:"kind,omitempty"`
}
