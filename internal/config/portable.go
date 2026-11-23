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
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Description     string           `json:"description,omitempty"`
	Avatar          string           `json:"avatar,omitempty"`
	Color           string           `json:"color,omitempty"`
	PrivateSessions bool             `json:"privateSessions,omitempty"`
	Schedules       []ScheduleConfig `json:"schedules,omitempty"`
}

// PortableBundle is the versioned portable snapshot exchanged across hosts or
// committed to the checkpoint repository.
type PortableBundle struct {
	Version    int                 `json:"version"`
	UpdatedAt  string              `json:"updatedAt"`
	SourceHost string              `json:"sourceHost,omitempty"`
	Bots       []PortableBotConfig `json:"bots"`
}

// ExportPortable extracts only portable bot definitions from cfg.
func ExportPortable(cfg *HubConfig, sourceHost string) PortableBundle {
	out := PortableBundle{
		Version:    1,
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		SourceHost: strings.TrimSpace(sourceHost),
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
		name := strings.TrimSpace(pb.Name)
		if name == "" {
			name = id
		}
		scheds := make([]ScheduleConfig, len(pb.Schedules))
		copy(scheds, pb.Schedules)

		if idx, exists := byID[id]; exists {
			cur := cfg.Bots[idx]
			if cur.Name != name ||
				cur.Description != pb.Description ||
				cur.Avatar != pb.Avatar ||
				cur.Color != pb.Color ||
				cur.PrivateSessions != pb.PrivateSessions ||
				!reflect.DeepEqual(cur.Schedules, scheds) {
				cur.Name = name
				cur.Description = pb.Description
				cur.Avatar = pb.Avatar
				cur.Color = pb.Color
				cur.PrivateSessions = pb.PrivateSessions
				cur.Schedules = scheds
				cfg.Bots[idx] = cur
				changed = true
				updated = append(updated, id)
			}
			continue
		}

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

	if !changed || strings.TrimSpace(cfg.ConfigPath) == "" {
		return changed, added, updated, nil
	}

	err = UpdateRaw(cfg, func(raw *HubConfig) error {
		rawIdx := make(map[string]int, len(raw.Bots))
		for i, b := range raw.Bots {
			rawIdx[b.ID] = i
		}
		for _, pb := range bundle.Bots {
			id := strings.TrimSpace(pb.ID)
			if id == "" || !botIDRe.MatchString(id) {
				continue
			}
			name := strings.TrimSpace(pb.Name)
			if name == "" {
				name = id
			}
			scheds := make([]ScheduleConfig, len(pb.Schedules))
			copy(scheds, pb.Schedules)
			if idx, ok := rawIdx[id]; ok {
				cur := raw.Bots[idx]
				cur.Name = name
				cur.Description = pb.Description
				cur.Avatar = pb.Avatar
				cur.Color = pb.Color
				cur.PrivateSessions = pb.PrivateSessions
				cur.Schedules = scheds
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
					Schedules:       scheds,
				})
			}
		}
		return nil
	})
	return changed, added, updated, err
}
