package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type PushDevice struct {
	DeviceID               string            `json:"deviceId"`
	Platform               string            `json:"platform"`
	Env                    string            `json:"env"`
	BundleID               string            `json:"bundleId"`
	PushToken              string            `json:"pushToken,omitempty"`
	LiveActivityStartToken string            `json:"liveActivityStartToken,omitempty"`
	ActivityTokens         map[string]string `json:"activityTokens,omitempty"`
	Enabled                bool              `json:"enabled"`
	UpdatedAt              string            `json:"updatedAt"`
}

func (s *Store) UpsertPushDevice(d PushDevice) error {
	if s == nil || s.db == nil {
		return nil
	}
	id := strings.TrimSpace(d.DeviceID)
	if id == "" {
		return errors.New("deviceId is required")
	}
	platform := strings.ToLower(strings.TrimSpace(d.Platform))
	if platform == "" {
		platform = "ios"
	}
	env := strings.ToLower(strings.TrimSpace(d.Env))
	if env != "sandbox" {
		env = "production"
	}
	bundleID := strings.TrimSpace(d.BundleID)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	existing, ok, err := s.GetPushDevice(id)
	if err != nil {
		return err
	}

	tokens := make(map[string]string)
	if ok && len(existing.ActivityTokens) > 0 {
		for k, v := range existing.ActivityTokens {
			tokens[k] = v
		}
	}
	for k, v := range d.ActivityTokens {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if v == "" {
			delete(tokens, k)
		} else {
			tokens[k] = v
		}
	}

	pushToken := strings.TrimSpace(d.PushToken)
	if pushToken == "" && ok {
		pushToken = existing.PushToken
	}
	startToken := strings.TrimSpace(d.LiveActivityStartToken)
	if startToken == "" && ok {
		startToken = existing.LiveActivityStartToken
	}
	if bundleID == "" && ok {
		bundleID = existing.BundleID
	}

	tokensJSON, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	enabledInt := 0
	if d.Enabled {
		enabledInt = 1
	}

	_, err = s.db.Exec(`
		INSERT INTO push_devices(
			device_id, platform, env, bundle_id, push_token,
			live_activity_start_token, activity_tokens_json, enabled, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET
			platform = excluded.platform,
			env = excluded.env,
			bundle_id = excluded.bundle_id,
			push_token = excluded.push_token,
			live_activity_start_token = excluded.live_activity_start_token,
			activity_tokens_json = excluded.activity_tokens_json,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at
	`, id, platform, env, bundleID, pushToken, startToken, string(tokensJSON), enabledInt, now)
	return err
}

func (s *Store) GetPushDevice(deviceID string) (PushDevice, bool, error) {
	if s == nil || s.db == nil {
		return PushDevice{}, false, nil
	}
	row := s.db.QueryRow(`
		SELECT device_id, platform, env, bundle_id, push_token,
		       live_activity_start_token, activity_tokens_json, enabled, updated_at
		FROM push_devices
		WHERE device_id = ?
	`, strings.TrimSpace(deviceID))
	d, err := scanPushDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PushDevice{}, false, nil
	}
	if err != nil {
		return PushDevice{}, false, err
	}
	return d, true, nil
}

func (s *Store) SetPushActivityToken(deviceID, activityKey, token string) error {
	if s == nil || s.db == nil {
		return nil
	}
	id := strings.TrimSpace(deviceID)
	key := strings.TrimSpace(activityKey)
	if id == "" || key == "" {
		return errors.New("deviceId and activityKey are required")
	}
	d, ok, err := s.GetPushDevice(id)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("push device not found")
	}
	if d.ActivityTokens == nil {
		d.ActivityTokens = make(map[string]string)
	}
	tok := strings.TrimSpace(token)
	if tok == "" {
		delete(d.ActivityTokens, key)
	} else {
		d.ActivityTokens[key] = tok
	}
	raw, err := json.Marshal(d.ActivityTokens)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(`
		UPDATE push_devices SET activity_tokens_json = ?, updated_at = ? WHERE device_id = ?
	`, string(raw), now, id)
	return err
}

func (s *Store) DeletePushDevice(deviceID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM push_devices WHERE device_id = ?`, strings.TrimSpace(deviceID))
	return err
}

func (s *Store) ListPushDevices() ([]PushDevice, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT device_id, platform, env, bundle_id, push_token,
		       live_activity_start_token, activity_tokens_json, enabled, updated_at
		FROM push_devices
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PushDevice
	for rows.Next() {
		d, err := scanPushDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ClearInvalidPushToken clears an expired/invalid token field ("push", "start", or "activity").
func (s *Store) ClearInvalidPushToken(deviceID, field, activityKey string) error {
	if s == nil || s.db == nil {
		return nil
	}
	id := strings.TrimSpace(deviceID)
	if id == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	switch field {
	case "push":
		_, err := s.db.Exec(`UPDATE push_devices SET push_token = '', updated_at = ? WHERE device_id = ?`, now, id)
		return err
	case "start":
		_, err := s.db.Exec(`UPDATE push_devices SET live_activity_start_token = '', updated_at = ? WHERE device_id = ?`, now, id)
		return err
	case "activity":
		if strings.TrimSpace(activityKey) == "" {
			return nil
		}
		return s.SetPushActivityToken(id, activityKey, "")
	default:
		return nil
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPushDevice(r rowScanner) (PushDevice, error) {
	var (
		d          PushDevice
		tokensJSON string
		enabledInt int
	)
	if err := r.Scan(
		&d.DeviceID, &d.Platform, &d.Env, &d.BundleID, &d.PushToken,
		&d.LiveActivityStartToken, &tokensJSON, &enabledInt, &d.UpdatedAt,
	); err != nil {
		return PushDevice{}, err
	}
	d.Enabled = enabledInt != 0
	d.ActivityTokens = make(map[string]string)
	if strings.TrimSpace(tokensJSON) != "" {
		_ = json.Unmarshal([]byte(tokensJSON), &d.ActivityTokens)
	}
	return d, nil
}
