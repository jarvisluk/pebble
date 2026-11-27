package harness

import (
	"context"
	"errors"
	"strings"
)

// ErrorKind categorizes runtime errors returned by harnesses.
type ErrorKind string

const (
	ErrorKindNone         ErrorKind = ""
	ErrorKindUsageLimit   ErrorKind = "usage_limit"
	ErrorKindAuthRequired ErrorKind = "auth_required"
	ErrorKindTimeout      ErrorKind = "timeout"
	ErrorKindCancelled    ErrorKind = "cancelled"
	ErrorKindRuntime      ErrorKind = "runtime_error"
)

// ClassifiedError wraps an error with a normalized ErrorKind and retryability hint.
type ClassifiedError struct {
	Kind      ErrorKind `json:"kind"`
	Message   string    `json:"message,omitempty"`
	Retryable bool      `json:"retryable,omitempty"`
	Err       error     `json:"-"`
}

func (e ClassifiedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Kind)
}

func (e ClassifiedError) Unwrap() error {
	return e.Err
}

// ClassifyError inspects err and stderr output to categorize rate-limit/usage-limit,
// authentication, timeout, cancellation, or general runtime failures.
func ClassifyError(err error, stderr string) ClassifiedError {
	errMsg := ""
	if err != nil {
		errMsg = strings.TrimSpace(err.Error())
	}
	stderrTrim := strings.TrimSpace(stderr)
	if err == nil && stderrTrim == "" {
		return ClassifiedError{Kind: ErrorKindNone}
	}

	msg := errMsg
	if msg == "" {
		msg = stderrTrim
	} else if stderrTrim != "" && !strings.Contains(msg, stderrTrim) {
		msg = msg + ": " + stderrTrim
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return ClassifiedError{
			Kind:      ErrorKindTimeout,
			Message:   msg,
			Retryable: true,
			Err:       err,
		}
	}
	if errors.Is(err, context.Canceled) {
		return ClassifiedError{
			Kind:      ErrorKindCancelled,
			Message:   msg,
			Retryable: false,
			Err:       err,
		}
	}

	combined := strings.ToLower(errMsg + "\n" + stderrTrim)

	if containsAny(combined,
		"rate limit",
		"rate_limit",
		"ratelimit",
		"too many requests",
		"http 429",
		"status 429",
		"status: 429",
		"code 429",
		"429 ",
		" 429",
		"usage limit",
		"usage_limit",
		"hit your limit",
		"quota exceeded",
		"exceeded your current quota",
		"insufficient_quota",
		"credit balance is too low",
		"resource_exhausted",
		"overloaded_error",
		"http 529",
	) {
		return ClassifiedError{
			Kind:      ErrorKindUsageLimit,
			Message:   msg,
			Retryable: true,
			Err:       err,
		}
	}

	if containsAny(combined,
		"unauthorized",
		"authentication failed",
		"authentication_error",
		"invalid api key",
		"invalid_api_key",
		"missing api key",
		"api key is required",
		"not logged in",
		"login required",
		"please login",
		"please run login",
		"auth required",
		"reauthenticate",
		"http 401",
		"status 401",
		"status: 401",
		"401 unauthorized",
		"token expired",
		"expired token",
	) {
		return ClassifiedError{
			Kind:      ErrorKindAuthRequired,
			Message:   msg,
			Retryable: false,
			Err:       err,
		}
	}

	if containsAny(combined,
		"timed out",
		"timeout",
		"deadline exceeded",
	) {
		return ClassifiedError{
			Kind:      ErrorKindTimeout,
			Message:   msg,
			Retryable: true,
			Err:       err,
		}
	}

	if containsAny(combined,
		"interrupted",
		"context canceled",
		"cancelled",
		"canceled",
	) {
		return ClassifiedError{
			Kind:      ErrorKindCancelled,
			Message:   msg,
			Retryable: false,
			Err:       err,
		}
	}

	return ClassifiedError{
		Kind:      ErrorKindRuntime,
		Message:   msg,
		Retryable: false,
		Err:       err,
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

// HarnessCapabilities describes high-level runtime capabilities supported by a Harness.
type HarnessCapabilities struct {
	Streaming       bool     `json:"streaming"`
	Interrupt       bool     `json:"interrupt"`
	SessionResume   bool     `json:"sessionResume"`
	PermissionModes []string `json:"permissionModes,omitempty"`
	UsageLimitAware bool     `json:"usageLimitAware"`
}

// HarnessCapabilityProvider is optionally implemented by harnesses that expose
// explicit HarnessCapabilities metadata without changing the core Harness interface.
type HarnessCapabilityProvider interface {
	HarnessCapabilities() HarnessCapabilities
}

// HarnessCapabilitiesOf returns the runtime capabilities of h without requiring
// changes to existing Harness implementations.
func HarnessCapabilitiesOf(h Harness) HarnessCapabilities {
	if h == nil {
		return HarnessCapabilities{}
	}
	if cp, ok := h.(HarnessCapabilityProvider); ok {
		caps := cp.HarnessCapabilities()
		if len(caps.PermissionModes) == 0 {
			caps.PermissionModes = CapabilitiesOf(h).Modes
		}
		return caps
	}
	permCaps := CapabilitiesOf(h)
	caps := HarnessCapabilities{
		Streaming:       true,
		Interrupt:       true,
		SessionResume:   true,
		PermissionModes: permCaps.Modes,
		UsageLimitAware: true,
	}
	switch h.(type) {
	case *ClaudeHarness, *OpenCodeHarness, *CodexHarness, *ACPHarness, *AgyHarness, *DshHarness, *PiHarness:
		return caps
	default:
		caps.UsageLimitAware = false
		return caps
	}
}
