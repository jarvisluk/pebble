package hub

import (
	"bot-hub/internal/config"
	"fmt"
	"strings"
)

// RedactedMemoryDreamConfig exposes safe dream settings to API clients.
type RedactedMemoryDreamConfig struct {
	Enabled        bool   `json:"enabled"`
	DailyAt        string `json:"dailyAt"`
	Model          string `json:"model,omitempty"`
	MinNewMessages int    `json:"minNewMessages"`
}

// RedactedMemoryEmbedConfig exposes safe embedding settings without leaking API keys.
type RedactedMemoryEmbedConfig struct {
	Enabled      bool   `json:"enabled"`
	BaseURL      string `json:"baseUrl,omitempty"`
	Model        string `json:"model,omitempty"`
	HasAPIKey    bool   `json:"hasApiKey,omitempty"`
	APIKeyEnvRef string `json:"apiKeyEnvRef,omitempty"`
}

// RedactedMemoryConfig is the secret-safe memory configuration returned by settings & status APIs.
type RedactedMemoryConfig struct {
	Enabled                 bool                      `json:"enabled"`
	Coexistence             string                    `json:"coexistence"`
	SnapshotMaxChars        int                       `json:"snapshotMaxChars"`
	DisableClaudeAutoMemory bool                      `json:"disableClaudeAutoMemory"`
	Dream                   RedactedMemoryDreamConfig `json:"dream"`
	Embedding               RedactedMemoryEmbedConfig `json:"embedding"`
}

// MemoryDreamConfigPatch applies partial updates to memory.dream.
type MemoryDreamConfigPatch struct {
	Enabled        *bool   `json:"enabled,omitempty"`
	DailyAt        *string `json:"dailyAt,omitempty"`
	Model          *string `json:"model,omitempty"`
	MinNewMessages *int    `json:"minNewMessages,omitempty"`
}

// MemoryEmbedConfigPatch applies partial updates to memory.embedding.
type MemoryEmbedConfigPatch struct {
	Enabled     *bool   `json:"enabled,omitempty"`
	BaseURL     *string `json:"baseUrl,omitempty"`
	APIKey      *string `json:"apiKey,omitempty"`
	ClearAPIKey *bool   `json:"clearApiKey,omitempty"`
	Model       *string `json:"model,omitempty"`
}

// MemoryConfigPatch applies partial updates to HubConfig.Memory.
type MemoryConfigPatch struct {
	Enabled                 *bool                   `json:"enabled,omitempty"`
	Coexistence             *string                 `json:"coexistence,omitempty"`
	SnapshotMaxChars        *int                    `json:"snapshotMaxChars,omitempty"`
	DisableClaudeAutoMemory *bool                   `json:"disableClaudeAutoMemory,omitempty"`
	Dream                   *MemoryDreamConfigPatch `json:"dream,omitempty"`
	Embedding               *MemoryEmbedConfigPatch `json:"embedding,omitempty"`
}

// MemoryConfig returns the current secret-safe memory configuration.
func (h *Hub) MemoryConfig() RedactedMemoryConfig {
	h.lanesMu.RLock()
	m := h.cfg.Memory
	cfgPath := h.cfg.ConfigPath
	h.lanesMu.RUnlock()
	return redactMemoryConfig(m, cfgPath)
}

func redactMemoryConfig(m *config.MemoryConfig, cfgPath string) RedactedMemoryConfig {
	emb := m.EffectiveEmbedding()
	envRef := config.RawMemoryEmbedAPIKeyEnvRef(cfgPath)
	return RedactedMemoryConfig{
		Enabled:                 m.IsEnabled(),
		Coexistence:             m.EffectiveCoexistence(),
		SnapshotMaxChars:        m.EffectiveSnapshotMaxChars(),
		DisableClaudeAutoMemory: m.ShouldDisableClaudeAutoMemory(),
		Dream: RedactedMemoryDreamConfig{
			Enabled:        m.DreamEnabled(),
			DailyAt:        m.EffectiveDreamDailyAt(),
			Model:          m.EffectiveDreamModel(),
			MinNewMessages: m.EffectiveDreamMinNewMessages(),
		},
		Embedding: RedactedMemoryEmbedConfig{
			Enabled:      emb.Enabled,
			BaseURL:      emb.BaseURL,
			Model:        emb.Model,
			HasAPIKey:    emb.APIKey != "" || envRef != "",
			APIKeyEnvRef: envRef,
		},
	}
}

func isRedactedPlaceholder(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	if strings.EqualFold(t, "REDACTED") {
		return true
	}
	for _, r := range t {
		if r != '*' && r != '\u2022' {
			return false
		}
	}
	return true
}

// ValidateMemoryConfigPatch validates a partial memory config update without mutating state.
func (h *Hub) ValidateMemoryConfigPatch(patch MemoryConfigPatch) error {
	h.lanesMu.RLock()
	merged := mergeMemoryConfigExpanded(h.cfg.Memory, patch)
	h.lanesMu.RUnlock()
	return config.ValidateMemoryConfig(merged)
}

// SetMemoryConfig updates, persists (preserving ${ENV_VAR} references on disk),
// reloads the runtime memory engine config, and re-arms the Dream scheduler.
func (h *Hub) SetMemoryConfig(patch MemoryConfigPatch) error {
	h.lanesMu.Lock()
	prevExpanded := cloneMemoryConfig(h.cfg.Memory)
	nextExpanded := mergeMemoryConfigExpanded(prevExpanded, patch)
	if err := config.ValidateMemoryConfig(nextExpanded); err != nil {
		h.lanesMu.Unlock()
		return err
	}

	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.Memory = mergeMemoryConfigOnDisk(raw.Memory, prevExpanded, nextExpanded, patch)
			return nil
		})
		if err != nil {
			h.lanesMu.Unlock()
			return fmt.Errorf("save config: %w", err)
		}
	}

	h.cfg.Memory = nextExpanded
	memStore := h.memory
	engineCfg := buildMemoryEngineConfig(nextExpanded)
	h.lanesMu.Unlock()

	if memStore != nil {
		memStore.UpdateConfig(engineCfg)
	}
	h.notifySchedulesChanged()
	return nil
}

func cloneMemoryConfig(m *config.MemoryConfig) *config.MemoryConfig {
	if m == nil {
		return nil
	}
	cp := *m
	if m.Enabled != nil {
		v := *m.Enabled
		cp.Enabled = &v
	}
	if m.DisableClaudeAutoMemory != nil {
		v := *m.DisableClaudeAutoMemory
		cp.DisableClaudeAutoMemory = &v
	}
	if m.Dream != nil {
		d := *m.Dream
		if m.Dream.Enabled != nil {
			v := *m.Dream.Enabled
			d.Enabled = &v
		}
		cp.Dream = &d
	}
	if m.Embedding != nil {
		e := *m.Embedding
		cp.Embedding = &e
	}
	return &cp
}

func mergeMemoryConfigExpanded(cur *config.MemoryConfig, patch MemoryConfigPatch) *config.MemoryConfig {
	next := cloneMemoryConfig(cur)
	if next == nil {
		next = &config.MemoryConfig{}
	}
	if patch.Enabled != nil {
		v := *patch.Enabled
		next.Enabled = &v
	}
	if patch.Coexistence != nil {
		next.Coexistence = strings.ToLower(strings.TrimSpace(*patch.Coexistence))
	}
	if patch.SnapshotMaxChars != nil {
		next.SnapshotMaxChars = *patch.SnapshotMaxChars
	}
	if patch.DisableClaudeAutoMemory != nil {
		v := *patch.DisableClaudeAutoMemory
		next.DisableClaudeAutoMemory = &v
	}
	if patch.Dream != nil {
		if next.Dream == nil {
			next.Dream = &config.MemoryDreamConfig{}
		}
		if patch.Dream.Enabled != nil {
			v := *patch.Dream.Enabled
			next.Dream.Enabled = &v
		}
		if patch.Dream.DailyAt != nil {
			next.Dream.DailyAt = strings.TrimSpace(*patch.Dream.DailyAt)
		}
		if patch.Dream.Model != nil {
			next.Dream.Model = strings.TrimSpace(config.ExpandEnv(*patch.Dream.Model))
		}
		if patch.Dream.MinNewMessages != nil {
			next.Dream.MinNewMessages = *patch.Dream.MinNewMessages
		}
	}
	if patch.Embedding != nil {
		if next.Embedding == nil {
			next.Embedding = &config.MemoryEmbedConfig{}
		}
		if patch.Embedding.Enabled != nil {
			next.Embedding.Enabled = *patch.Embedding.Enabled
		}
		if patch.Embedding.BaseURL != nil {
			next.Embedding.BaseURL = strings.TrimSpace(config.ExpandEnv(*patch.Embedding.BaseURL))
		}
		if patch.Embedding.Model != nil {
			next.Embedding.Model = strings.TrimSpace(config.ExpandEnv(*patch.Embedding.Model))
		}
		if patch.Embedding.ClearAPIKey != nil && *patch.Embedding.ClearAPIKey {
			next.Embedding.APIKey = ""
		} else if patch.Embedding.APIKey != nil {
			rawKey := strings.TrimSpace(*patch.Embedding.APIKey)
			if !isRedactedPlaceholder(rawKey) {
				next.Embedding.APIKey = strings.TrimSpace(config.ExpandEnv(rawKey))
			}
		}
	}
	return next
}

func mergeMemoryConfigOnDisk(curDisk, prevExpanded, nextExpanded *config.MemoryConfig, patch MemoryConfigPatch) *config.MemoryConfig {
	out := cloneMemoryConfig(curDisk)
	if out == nil {
		out = &config.MemoryConfig{}
	}
	if nextExpanded.Enabled != nil {
		v := *nextExpanded.Enabled
		out.Enabled = &v
	}
	if nextExpanded.Coexistence != "" {
		out.Coexistence = nextExpanded.Coexistence
	}
	if nextExpanded.SnapshotMaxChars > 0 || patch.SnapshotMaxChars != nil {
		out.SnapshotMaxChars = nextExpanded.SnapshotMaxChars
	}
	if nextExpanded.DisableClaudeAutoMemory != nil {
		v := *nextExpanded.DisableClaudeAutoMemory
		out.DisableClaudeAutoMemory = &v
	}
	if nextExpanded.Dream != nil {
		if out.Dream == nil {
			out.Dream = &config.MemoryDreamConfig{}
		}
		if nextExpanded.Dream.Enabled != nil {
			v := *nextExpanded.Dream.Enabled
			out.Dream.Enabled = &v
		}
		out.Dream.DailyAt = nextExpanded.Dream.DailyAt
		out.Dream.MinNewMessages = nextExpanded.Dream.MinNewMessages
		if patch.Dream != nil && patch.Dream.Model != nil {
			submitted := strings.TrimSpace(*patch.Dream.Model)
			prevModel := prevExpanded.EffectiveDreamModel()
			if !(submitted == prevModel && config.ContainsEnvRef(out.Dream.Model)) {
				out.Dream.Model = submitted
			}
		}
	}
	if nextExpanded.Embedding != nil {
		if out.Embedding == nil {
			out.Embedding = &config.MemoryEmbedConfig{}
		}
		out.Embedding.Enabled = nextExpanded.Embedding.Enabled
		prevEmb := prevExpanded.EffectiveEmbedding()
		if patch.Embedding != nil && patch.Embedding.BaseURL != nil {
			submitted := strings.TrimSpace(*patch.Embedding.BaseURL)
			if !(submitted == prevEmb.BaseURL && config.ContainsEnvRef(out.Embedding.BaseURL)) {
				out.Embedding.BaseURL = submitted
			}
		}
		if patch.Embedding != nil && patch.Embedding.Model != nil {
			submitted := strings.TrimSpace(*patch.Embedding.Model)
			if !(submitted == prevEmb.Model && config.ContainsEnvRef(out.Embedding.Model)) {
				out.Embedding.Model = submitted
			}
		}
		if patch.Embedding != nil {
			if patch.Embedding.ClearAPIKey != nil && *patch.Embedding.ClearAPIKey {
				out.Embedding.APIKey = ""
			} else if patch.Embedding.APIKey != nil {
				submitted := strings.TrimSpace(*patch.Embedding.APIKey)
				if !isRedactedPlaceholder(submitted) {
					if !(submitted == prevEmb.APIKey && config.ContainsEnvRef(out.Embedding.APIKey)) {
						out.Embedding.APIKey = submitted
					}
				}
			}
		}
	}
	return out
}
