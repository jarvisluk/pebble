package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// PortableBotConfig contains only cross-machine portable bot fields.
// Machine-local runtime settings (harness, model, workspace path, env) are excluded.
type PortableBotConfig struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Description      string           `json:"description,omitempty"`
	Avatar           string           `json:"avatar,omitempty"`
	Color            string           `json:"color,omitempty"`
	PrivateSessions  bool             `json:"privateSessions,omitempty"`
	Schedules        []ScheduleConfig `json:"schedules,omitempty"`
	Deleted          bool             `json:"deleted,omitempty"`
	DeletedSchedules []string         `json:"deletedSchedules,omitempty"`
	UpdatedAt        string           `json:"updatedAt,omitempty"`
	UpdatedBy        string           `json:"updatedBy,omitempty"`
}

// PortableMemoryConfig contains cross-machine portable memory settings.
// Secret fields such as embedding.apiKey are intentionally excluded.
type PortableMemoryConfig struct {
	Enabled                 *bool              `json:"enabled,omitempty"`
	Coexistence             string             `json:"coexistence,omitempty"`
	SnapshotMaxChars        int                `json:"snapshotMaxChars,omitempty"`
	DisableClaudeAutoMemory *bool              `json:"disableClaudeAutoMemory,omitempty"`
	Dream                   *MemoryDreamConfig `json:"dream,omitempty"`
	Embedding               *MemoryEmbedConfig `json:"embedding,omitempty"`
}

// PortableBundle is the versioned portable snapshot exchanged across hosts or
// committed to the checkpoint repository.
type PortableBundle struct {
	Version    int                   `json:"version"`
	UpdatedAt  string                `json:"updatedAt"`
	SourceHost string                `json:"sourceHost,omitempty"`
	Memory     *PortableMemoryConfig `json:"memory,omitempty"`
	Bots       []PortableBotConfig   `json:"bots"`
}

// ExportPortable extracts only portable bot definitions and non-secret memory config from cfg.
func ExportPortable(cfg *HubConfig, sourceHost string) PortableBundle {
	out := PortableBundle{
		Version:    1,
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		SourceHost: strings.TrimSpace(sourceHost),
		Memory:     exportPortableMemory(cfg.Memory),
		Bots:       make([]PortableBotConfig, 0, len(cfg.Bots)),
	}
	for _, b := range cfg.Bots {
		scheds := make([]ScheduleConfig, len(b.Schedules))
		copy(scheds, b.Schedules)
		out.Bots = append(out.Bots, PortableBotConfig{
			ID:              b.ID,
			Name:            b.Name,
			Description:     b.Description,
			Avatar:          b.Avatar,
			Color:           b.Color,
			PrivateSessions: b.PrivateSessions,
			Schedules:       scheds,
		})
	}
	return out
}

func exportPortableMemory(m *MemoryConfig) *PortableMemoryConfig {
	if m == nil {
		return nil
	}
	pm := &PortableMemoryConfig{
		Coexistence:      m.EffectiveCoexistence(),
		SnapshotMaxChars: m.SnapshotMaxChars,
	}
	if m.Enabled != nil {
		v := *m.Enabled
		pm.Enabled = &v
	}
	if m.DisableClaudeAutoMemory != nil {
		v := *m.DisableClaudeAutoMemory
		pm.DisableClaudeAutoMemory = &v
	}
	if m.Dream != nil {
		d := *m.Dream
		if m.Dream.Enabled != nil {
			v := *m.Dream.Enabled
			d.Enabled = &v
		}
		pm.Dream = &d
	}
	if m.Embedding != nil {
		// Never export APIKey in portable bundles.
		pm.Embedding = &MemoryEmbedConfig{
			Enabled: m.Embedding.Enabled,
			BaseURL: strings.TrimSpace(m.Embedding.BaseURL),
			Model:   strings.TrimSpace(m.Embedding.Model),
		}
	}
	return pm
}

func applyPortableMemory(dst *MemoryConfig, src *PortableMemoryConfig) (*MemoryConfig, bool) {
	if src == nil {
		return dst, false
	}
	next := &MemoryConfig{}
	if dst != nil {
		*next = *dst
		if dst.Enabled != nil {
			v := *dst.Enabled
			next.Enabled = &v
		}
		if dst.DisableClaudeAutoMemory != nil {
			v := *dst.DisableClaudeAutoMemory
			next.DisableClaudeAutoMemory = &v
		}
		if dst.Dream != nil {
			d := *dst.Dream
			if dst.Dream.Enabled != nil {
				v := *dst.Dream.Enabled
				d.Enabled = &v
			}
			next.Dream = &d
		}
		if dst.Embedding != nil {
			e := *dst.Embedding
			next.Embedding = &e
		}
	}
	if src.Enabled != nil {
		v := *src.Enabled
		next.Enabled = &v
	}
	if c, err := NormalizeMemoryCoexistence(src.Coexistence); err == nil && src.Coexistence != "" {
		next.Coexistence = c
	}
	if src.SnapshotMaxChars > 0 {
		next.SnapshotMaxChars = src.SnapshotMaxChars
	}
	if src.DisableClaudeAutoMemory != nil {
		v := *src.DisableClaudeAutoMemory
		next.DisableClaudeAutoMemory = &v
	}
	if src.Dream != nil {
		d := *src.Dream
		if src.Dream.Enabled != nil {
			v := *src.Dream.Enabled
			d.Enabled = &v
		}
		next.Dream = &d
	}
	if src.Embedding != nil {
		existingKey := ""
		if next.Embedding != nil {
			existingKey = next.Embedding.APIKey
		}
		next.Embedding = &MemoryEmbedConfig{
			Enabled: src.Embedding.Enabled,
			BaseURL: strings.TrimSpace(src.Embedding.BaseURL),
			APIKey:  existingKey, // Strictly preserve local API key / ${ENV_VAR}.
			Model:   strings.TrimSpace(src.Embedding.Model),
		}
	}
	if reflect.DeepEqual(dst, next) {
		return dst, false
	}
	return next, true
}

// ApplyPortable merges portable bot definitions into cfg while strictly preserving
// each machine's local harness, model, workspace path, env, and HTTP/token settings.
func ApplyPortable(cfg *HubConfig, bundle PortableBundle) (changed bool, added []string, updated []string, err error) {
	if cfg == nil {
		return false, nil, nil, nil
	}
	byID := make(map[string]int, len(cfg.Bots))
	for i, b := range cfg.Bots {
		byID[b.ID] = i
	}

	defHarness := strings.TrimSpace(cfg.DefaultHarness)
	if defHarness == "" {
		defHarness = "opencode"
	}

	for _, pb := range bundle.Bots {
		id := strings.TrimSpace(pb.ID)
		if id == "" || !botIDRe.MatchString(id) {
			continue
		}
		if pb.Deleted {
			if idx, exists := byID[id]; exists {
				cfg.Bots = append(cfg.Bots[:idx], cfg.Bots[idx+1:]...)
				delete(byID, id)
				for i, b := range cfg.Bots {
					byID[b.ID] = i
				}
				changed = true
				updated = append(updated, id)
			}
			continue
		}
		name := strings.TrimSpace(pb.Name)
		if name == "" {
			name = id
		}

		if idx, exists := byID[id]; exists {
			cur := cfg.Bots[idx]
			mergedScheds := mergeSchedules(cur.Schedules, pb.Schedules, pb.DeletedSchedules)
			if cur.Name != name ||
				cur.Description != pb.Description ||
				cur.Avatar != pb.Avatar ||
				cur.Color != pb.Color ||
				cur.PrivateSessions != pb.PrivateSessions ||
				!reflect.DeepEqual(cur.Schedules, mergedScheds) {
				cur.Name = name
				cur.Description = pb.Description
				cur.Avatar = pb.Avatar
				cur.Color = pb.Color
				cur.PrivateSessions = pb.PrivateSessions
				cur.Schedules = mergedScheds
				cfg.Bots[idx] = cur
				changed = true
				updated = append(updated, id)
			}
			continue
		}

		scheds := mergeSchedules(nil, pb.Schedules, pb.DeletedSchedules)
		ws := "./workspaces/" + id
		if cfg.ProjectRoot != "" {
			ws = filepath.Join(cfg.ProjectRoot, "workspaces", id)
		}
		defModel := cfg.DefaultModelFor(defHarness)
		nb := BotConfig{
			ID:              id,
			Name:            name,
			Description:     pb.Description,
			Avatar:          pb.Avatar,
			Color:           pb.Color,
			Workspace:       ws,
			Harness:         defHarness,
			Model:           defModel,
			ModelInherited:  defModel != "",
			PrivateSessions: pb.PrivateSessions,
			Schedules:       scheds,
		}
		byID[id] = len(cfg.Bots)
		cfg.Bots = append(cfg.Bots, nb)
		changed = true
		added = append(added, id)
	}

	if bundle.Memory != nil {
		if nextMem, memChanged := applyPortableMemory(cfg.Memory, bundle.Memory); memChanged {
			cfg.Memory = nextMem
			changed = true
		}
	}

	if !changed || strings.TrimSpace(cfg.ConfigPath) == "" {
		return changed, added, updated, nil
	}

	err = UpdateRaw(cfg, func(raw *HubConfig) error {
		if bundle.Memory != nil {
			if nextMem, _ := applyPortableMemory(raw.Memory, bundle.Memory); nextMem != nil {
				raw.Memory = nextMem
			}
		}
		rawIdx := make(map[string]int, len(raw.Bots))
		for i, b := range raw.Bots {
			rawIdx[b.ID] = i
		}
		for _, pb := range bundle.Bots {
			id := strings.TrimSpace(pb.ID)
			if id == "" || !botIDRe.MatchString(id) {
				continue
			}
			if pb.Deleted {
				if idx, ok := rawIdx[id]; ok {
					raw.Bots = append(raw.Bots[:idx], raw.Bots[idx+1:]...)
					delete(rawIdx, id)
					for i, b := range raw.Bots {
						rawIdx[b.ID] = i
					}
				}
				continue
			}
			name := strings.TrimSpace(pb.Name)
			if name == "" {
				name = id
			}
			if idx, ok := rawIdx[id]; ok {
				cur := raw.Bots[idx]
				cur.Name = name
				cur.Description = pb.Description
				cur.Avatar = pb.Avatar
				cur.Color = pb.Color
				cur.PrivateSessions = pb.PrivateSessions
				cur.Schedules = mergeSchedules(cur.Schedules, pb.Schedules, pb.DeletedSchedules)
				raw.Bots[idx] = cur
			} else {
				rawIdx[id] = len(raw.Bots)
				raw.Bots = append(raw.Bots, BotConfig{
					ID:              id,
					Name:            name,
					Description:     pb.Description,
					Avatar:          pb.Avatar,
					Color:           pb.Color,
					Workspace:       "./workspaces/" + id,
					PrivateSessions: pb.PrivateSessions,
					Schedules:       mergeSchedules(nil, pb.Schedules, pb.DeletedSchedules),
				})
			}
		}
		return nil
	})
	return changed, added, updated, err
}

func mergeSchedules(local, remote []ScheduleConfig, deletedIDs []string) []ScheduleConfig {
	deleted := make(map[string]bool, len(deletedIDs))
	for _, id := range deletedIDs {
		if id = strings.TrimSpace(id); id != "" {
			deleted[id] = true
		}
	}
	var out []ScheduleConfig
	idxByID := make(map[string]int)
	for _, s := range local {
		id := s.EffectiveID()
		if deleted[id] {
			continue
		}
		idxByID[id] = len(out)
		out = append(out, s)
	}
	for _, s := range remote {
		id := s.EffectiveID()
		if deleted[id] {
			continue
		}
		if idx, exists := idxByID[id]; exists {
			out[idx] = s
		} else {
			idxByID[id] = len(out)
			out = append(out, s)
		}
	}
	return out
}
