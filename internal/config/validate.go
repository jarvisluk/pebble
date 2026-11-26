package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(
	cron.SecondOptional |
		cron.Minute |
		cron.Hour |
		cron.Dom |
		cron.Month |
		cron.Dow |
		cron.Descriptor,
)

// ParseCron parses a 5-field standard cron expression, 6-field second-level cron expression, or @every/@daily descriptor.
func ParseCron(expr string) (cron.Schedule, error) {
	return cronParser.Parse(strings.TrimSpace(expr))
}

// Validate checks that the routine has a valid ID/name, trigger, prompt, and delivery mode.
func (s ScheduleConfig) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("routine name is required")
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return errors.New("routine prompt is required")
	}
	if id := strings.TrimSpace(s.ID); id != "" && !botIDRe.MatchString(id) {
		return fmt.Errorf("invalid routine id %q (use letters, digits, - or _)", id)
	}
	if s.EveryMinutes < 0 {
		return errors.New("everyMinutes must be >= 0")
	}
	daily := strings.TrimSpace(s.DailyAt)
	if daily != "" {
		if !isValidDailyAt(daily) {
			return fmt.Errorf("invalid dailyAt %q (expected HH:MM)", s.DailyAt)
		}
	}
	cronExpr := strings.TrimSpace(s.Cron)
	if cronExpr != "" {
		if _, err := ParseCron(cronExpr); err != nil {
			return fmt.Errorf("invalid cron %q: %w", s.Cron, err)
		}
	}
	if wurl := strings.TrimSpace(s.WebhookURL); wurl != "" && !ContainsEnvRef(wurl) {
		u, err := url.ParseRequestURI(wurl)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid webhookUrl %q (expected http:// or https:// URL)", s.WebhookURL)
		}
	}
	if s.EveryMinutes == 0 && daily == "" && cronExpr == "" && !s.WebhookTrigger {
		return errors.New("at least one trigger (everyMinutes, dailyAt, cron, or webhookTrigger) is required")
	}
	switch strings.ToLower(strings.TrimSpace(s.MainDelivery)) {
	case "", RoutineDeliveryReceipt, RoutineDeliveryNotify, RoutineDeliveryNone:
	default:
		return fmt.Errorf("invalid mainDelivery %q (expected receipt, notify, or none)", s.MainDelivery)
	}
	switch strings.ToLower(strings.TrimSpace(s.FailoverPolicy)) {
	case "", RoutineFailoverAuto, RoutineFailoverPrimaryOnly, RoutineFailoverManual:
	default:
		return fmt.Errorf("invalid failoverPolicy %q (expected auto, primary_only, or manual)", s.FailoverPolicy)
	}
	if _, err := NormalizeRoutinePermissionMode(s.PermissionMode); err != nil {
		return err
	}
	return nil
}

func isValidDailyAt(v string) bool {
	if len(v) != 5 || v[2] != ':' {
		return false
	}
	hh, err1 := strconv.Atoi(v[:2])
	mm, err2 := strconv.Atoi(v[3:])
	return err1 == nil && err2 == nil && hh >= 0 && hh <= 23 && mm >= 0 && mm <= 59
}

func slugifyRoutineID(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
		if isAlnum {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "routine"
	}
	return slug
}

var (
	envRefRe   = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)
	botIDRe    = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	botColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	nodeIDRe   = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	deskSlotRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
)

// ValidBotAvatars lists the supported built-in bot avatar shapes.
var ValidBotAvatars = []string{"blob", "pebble-1", "pebble-2", "pebble-3", "pebble-4", "pebble-5", "pebble-6", "pebble-7"}

var legacyBotAvatarAliases = map[string]string{
	"squircle": "pebble-1",
	"cat":      "pebble-2",
	"bear":     "pebble-3",
	"cloud":    "pebble-4",
	"ghost":    "pebble-5",
	"star":     "pebble-6",
	"sprout":   "pebble-7",
}

// NormalizeBotAvatar trims, lowercases, and validates a bot avatar shape.
// An empty string is allowed and represents the default "blob" avatar.
// Legacy static avatar names are transparently mapped to their Pebble C variants.
func NormalizeBotAvatar(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "" || s == "blob" {
		return s, nil
	}
	if mapped, ok := legacyBotAvatarAliases[s]; ok {
		return mapped, nil
	}
	for _, valid := range ValidBotAvatars {
		if s == valid {
			return s, nil
		}
	}
	return "", fmt.Errorf("invalid bot avatar %q (expected one of %s)", v, strings.Join(ValidBotAvatars, ", "))
}

// NormalizeBotColor trims, lowercases, and validates a bot hex color (#rrggbb).
// An empty string is allowed and represents the automatic palette color.
func NormalizeBotColor(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "" {
		return "", nil
	}
	if !botColorRe.MatchString(s) {
		return "", fmt.Errorf("invalid bot color %q (expected #rrggbb)", v)
	}
	return s, nil
}

func ValidateBotID(id string) error {
	if !botIDRe.MatchString(id) {
		return fmt.Errorf("invalid bot id %q (use letters, digits, - or _)", id)
	}
	if id == "_shared" {
		return fmt.Errorf("bot id %q is reserved", id)
	}
	return nil
}

// ValidateModelID rejects excessively long or control/whitespace-containing model IDs.
// Empty string is valid and represents inheriting the hub/harness default.
func ValidateModelID(model string) error {
	if model == "" {
		return nil
	}
	if len(model) > 256 {
		return fmt.Errorf("model ID too long (max 256 characters)")
	}
	for _, r := range model {
		if r <= 0x20 || r == 0x7f {
			return fmt.Errorf("invalid model ID %q (cannot contain whitespace or control characters)", model)
		}
	}
	return nil
}

// NormalizeDesktopTarget validates and canonicalizes a bot's desktopTarget binding.
// Accepted values:
//   - "" (follow global active target)
//   - "local" (local host primary screen)
//   - "virtual:<slot>" or "local:virtual:<slot>" (local Linux virtual screen slot)
//   - "<nodeId>" (remote node default screen)
//   - "<nodeId>:virtual:<slot>" (remote Linux node virtual screen slot)
func NormalizeDesktopTarget(v string) (string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", nil
	}
	if strings.EqualFold(s, "local") {
		return "local", nil
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "local:virtual:") {
		slot := strings.TrimSpace(s[len("local:virtual:"):])
		if !deskSlotRe.MatchString(slot) {
			return "", fmt.Errorf("invalid virtual screen slot %q (use 1-64 letters, digits, - or _)", slot)
		}
		return "virtual:" + slot, nil
	}
	if strings.HasPrefix(lower, "virtual:") {
		slot := strings.TrimSpace(s[len("virtual:"):])
		if !deskSlotRe.MatchString(slot) {
			return "", fmt.Errorf("invalid virtual screen slot %q (use 1-64 letters, digits, - or _)", slot)
		}
		return "virtual:" + slot, nil
	}
	if idx := strings.Index(lower, ":virtual:"); idx >= 0 {
		nodeID := strings.TrimSpace(s[:idx])
		slot := strings.TrimSpace(s[idx+len(":virtual:"):])
		if nodeID == "" || strings.EqualFold(nodeID, "local") || !nodeIDRe.MatchString(nodeID) {
			return "", fmt.Errorf("invalid desktop node id %q", nodeID)
		}
		if !deskSlotRe.MatchString(slot) {
			return "", fmt.Errorf("invalid virtual screen slot %q (use 1-64 letters, digits, - or _)", slot)
		}
		return nodeID + ":virtual:" + slot, nil
	}
	if strings.Contains(s, ":") || !nodeIDRe.MatchString(s) {
		return "", fmt.Errorf("invalid desktopTarget %q", v)
	}
	return s, nil
}

// NormalizeBrowserMode validates and normalizes a bot's browser isolation mode.
// "" and "auto" normalize to ""; "isolated" and "attach" are preserved.
func NormalizeBrowserMode(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "", "auto":
		return "", nil
	case "isolated", "attach":
		return s, nil
	default:
		return "", fmt.Errorf("invalid browserMode %q (expected auto, isolated, or attach)", v)
	}
}

// NormalizePermissionMode validates and normalizes a permission mode.
// Empty string is preserved (meaning inherit / default).
func NormalizePermissionMode(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "", PermissionModeAuto, PermissionModeEdit, PermissionModeAsk, PermissionModePlan:
		return s, nil
	default:
		return "", fmt.Errorf("invalid permissionMode %q (expected auto, edit, ask, or plan)", v)
	}
}

// NormalizeRoutinePermissionMode validates and normalizes a routine's unattended permission override.
func NormalizeRoutinePermissionMode(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "", PermissionModeAuto, PermissionModePlan:
		return s, nil
	default:
		return "", fmt.Errorf("invalid routine permissionMode %q (expected empty, auto, or plan)", v)
	}
}

func NormalizeBot(b *BotConfig, cfg *HubConfig) {
	if b.Name == "" {
		b.Name = b.ID
	}
	if avatar, err := NormalizeBotAvatar(b.Avatar); err == nil {
		b.Avatar = avatar
	}
	if color, err := NormalizeBotColor(b.Color); err == nil {
		b.Color = color
	}
	if dt, err := NormalizeDesktopTarget(b.DesktopTarget); err == nil {
		b.DesktopTarget = dt
	}
	if bm, err := NormalizeBrowserMode(b.BrowserMode); err == nil {
		b.BrowserMode = bm
	}
	if pm, err := NormalizePermissionMode(b.PermissionMode); err == nil {
		b.PermissionMode = pm
	}
	if b.PermissionMode == "" && b.AutoApprove == nil && cfg != nil {
		if defPM := cfg.EffectiveDefaultPermissionMode(); defPM != PermissionModeAuto {
			b.PermissionMode = defPM
		}
	}
	if b.Harness == "" {
		b.Harness = cfg.EffectiveDefaultHarness()
	}
	b.Model = expandEnv(b.Model)
	b.ModelInherited = b.Model == ""
	if b.ModelInherited {
		b.Model = cfg.DefaultModelFor(b.Harness)
	}
	if b.TimeoutSec <= 0 {
		b.TimeoutSec = 300
	}
	if b.Workspace == "" {
		b.Workspace = filepath.Join("./workspaces", b.ID)
	}
	b.Workspace = absFrom(cfg.ProjectRoot, b.Workspace)

	for k, v := range b.Env {
		b.Env[k] = expandEnv(v)
	}
	b.Schedules = NormalizeSchedules(b.Schedules)
}

// NormalizeSchedules expands environment references and normalizes IDs and
// delivery modes across a bot's schedules, preserving unique IDs.
func NormalizeSchedules(in []ScheduleConfig) []ScheduleConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]ScheduleConfig, len(in))
	usedIDs := make(map[string]int, len(in))
	for j, sc := range in {
		sc.Name = strings.TrimSpace(sc.Name)
		sc.DailyAt = strings.TrimSpace(sc.DailyAt)
		sc.Cron = strings.TrimSpace(sc.Cron)
		sc.WebhookSecret = strings.TrimSpace(expandEnv(sc.WebhookSecret))
		sc.WebhookURL = strings.TrimSpace(expandEnv(sc.WebhookURL))
		sc.Model = strings.TrimSpace(expandEnv(sc.Model))
		sc.MainDelivery = sc.EffectiveMainDelivery()
		if rpm, err := NormalizeRoutinePermissionMode(sc.PermissionMode); err == nil {
			sc.PermissionMode = rpm
		}
		baseID := sc.EffectiveID()
		if n, exists := usedIDs[baseID]; exists {
			n++
			usedIDs[baseID] = n
			baseID = fmt.Sprintf("%s-%d", sc.EffectiveID(), n)
			for usedIDs[baseID] > 0 {
				n++
				baseID = fmt.Sprintf("%s-%d", sc.EffectiveID(), n)
			}
		}
		usedIDs[baseID] = 1
		sc.ID = baseID
		out[j] = sc
	}
	return out
}
