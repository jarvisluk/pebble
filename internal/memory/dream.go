package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	DreamActionAdd       = "ADD"
	DreamActionUpdate    = "UPDATE"
	DreamActionSupersede = "SUPERSEDE"
	DreamActionArchive   = "ARCHIVE"
	DreamActionNoop      = "NOOP"
)

// DreamPlan is the structured consolidation output returned by a Dream LLM run.
type DreamPlan struct {
	Summary    string    `json:"summary"`
	Operations []DreamOp `json:"operations"`
}

// DreamOp describes one atomic consolidation operation.
type DreamOp struct {
	Action     string   `json:"action"` // ADD | UPDATE | SUPERSEDE | ARCHIVE | NOOP
	ID         string   `json:"id,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	BotID      string   `json:"botId,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Title      string   `json:"title,omitempty"`
	Text       string   `json:"text,omitempty"`
	Why        string   `json:"why,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	ValidFrom  string   `json:"validFrom,omitempty"`
	ValidUntil string   `json:"validUntil,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// DreamPromptInput provides context to BuildDreamPrompt.
type DreamPromptInput struct {
	Scope           string
	BotID           string
	ExistingItems   []Item
	DailyLogs       []string
	SessionExcerpts []string
	DismissedHints  []string
	MaxIndexLines   int
}

// DreamApplyResult summarizes the applied operations and generated artifacts of a Dream run.
type DreamApplyResult struct {
	Added      int      `json:"added"`
	Updated    int      `json:"updated"`
	Superseded int      `json:"superseded"`
	Archived   int      `json:"archived"`
	Skipped    int      `json:"skipped"`
	Blocked    int      `json:"blocked"`
	DiaryPath  string   `json:"diaryPath"`
	IndexPath  string   `json:"indexPath"`
	IndexLines int      `json:"indexLines"`
	Warnings   []string `json:"warnings,omitempty"`
}

// DreamCursor tracks incremental Dream progress in state.Store meta.
type DreamCursor struct {
	LastRunAt        string `json:"lastRunAt,omitempty"`
	LastMessageRowID int64  `json:"lastMessageRowId,omitempty"`
	LastSessionID    string `json:"lastSessionId,omitempty"`
	LastDailyDate    string `json:"lastDailyDate,omitempty"`
	RunCount         int    `json:"runCount,omitempty"`
}

func dreamCursorKey(scope, botID string) string {
	normScope, err := NormalizeScope(scope, botID)
	if err != nil || normScope == ScopeGlobal {
		return "memory_dream_cursor:global"
	}
	return "memory_dream_cursor:bot:" + strings.TrimSpace(botID)
}

// GetDreamCursor reads the persisted DreamCursor from state.Store meta.
func (s *Store) GetDreamCursor(scope, botID string) DreamCursor {
	if s == nil || s.stateStore == nil {
		return DreamCursor{}
	}
	raw := strings.TrimSpace(s.stateStore.GetMeta(dreamCursorKey(scope, botID)))
	if raw == "" {
		return DreamCursor{}
	}
	var cur DreamCursor
	if json.Unmarshal([]byte(raw), &cur) != nil {
		return DreamCursor{}
	}
	return cur
}

// SetDreamCursor persists a DreamCursor in state.Store meta.
func (s *Store) SetDreamCursor(scope, botID string, cur DreamCursor) error {
	if s == nil || s.stateStore == nil {
		return nil
	}
	raw, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	return s.stateStore.SetMeta(dreamCursorKey(scope, botID), string(raw))
}

// BuildDreamPrompt creates a structured consolidation prompt instructing an LLM to emit a DreamPlan JSON.
func BuildDreamPrompt(input DreamPromptInput) string {
	scope := input.Scope
	if scope == "" {
		if strings.TrimSpace(input.BotID) != "" {
			scope = ScopeBot
		} else {
			scope = ScopeGlobal
		}
	}
	maxLines := input.MaxIndexLines
	if maxLines <= 0 {
		if scope == ScopeGlobal {
			maxLines = GlobalIndexMaxLines
		} else {
			maxLines = BotIndexMaxLines
		}
	}

	var b strings.Builder
	b.WriteString("You are Pebble's Dream memory consolidation engine.\n")
	b.WriteString("Consolidate recent daily working logs and session excerpts into clean, durable memory operations.\n")
	b.WriteString("Return ONLY a valid JSON object matching this exact schema (no markdown prose outside JSON):\n")
	b.WriteString(`{"summary":"short audit summary","operations":[{"action":"ADD|UPDATE|SUPERSEDE|ARCHIVE|NOOP","id":"existing-id-if-updating","scope":"bot|global","kind":"profile|preference|rule|fact|decision|procedure|reference|episode","title":"concise title","text":"durable memory statement","why":"source or rationale","tags":["tag"],"confidence":0.8,"reason":"why this op"}]}` + "\n\n")

	b.WriteString("Rules:\n")
	b.WriteString("- Target scope: " + scope)
	if input.BotID != "" {
		b.WriteString(" (botId=" + input.BotID + ")")
	}
	b.WriteString(fmt.Sprintf("; keep curated index within %d lines.\n", maxLines))
	b.WriteString("- Never store plaintext secrets, tokens, API keys, or private keys.\n")
	b.WriteString("- Never UPDATE, SUPERSEDE, or ARCHIVE items marked pin=true.\n")
	b.WriteString("- Never re-add facts listed under Dismissed Hints.\n")
	b.WriteString("- Prefer UPDATE or SUPERSEDE over creating duplicate items.\n")
	b.WriteString("- Global inferred memories will be queued as status=candidate for user review.\n")
	b.WriteString("- Emit {\"summary\":\"no durable changes\",\"operations\":[{\"action\":\"NOOP\"}]} when nothing durable is new.\n\n")

	if len(input.ExistingItems) > 0 {
		b.WriteString("## Existing Active Memories\n")
		for _, it := range input.ExistingItems {
			pinTag := ""
			if it.Pin {
				pinTag = " [PINNED]"
			}
			b.WriteString(fmt.Sprintf("- %s [%s/%s]%s %s: %s\n",
				it.ID, it.Scope, it.Kind, pinTag, it.Title, truncateRunes(strings.Join(strings.Fields(it.Text), " "), 200)))
		}
		b.WriteByte('\n')
	}

	if len(input.DismissedHints) > 0 {
		b.WriteString("## Dismissed Hints (DO NOT re-add)\n")
		for _, h := range input.DismissedHints {
			if strings.TrimSpace(h) != "" {
				b.WriteString("- " + strings.TrimSpace(h) + "\n")
			}
		}
		b.WriteByte('\n')
	}

	if len(input.DailyLogs) > 0 {
		b.WriteString("## Recent Daily Logs\n")
		for _, log := range input.DailyLogs {
			if strings.TrimSpace(log) != "" {
				b.WriteString(strings.TrimSpace(log) + "\n\n")
			}
		}
	}

	if len(input.SessionExcerpts) > 0 {
		b.WriteString("## Recent Session Excerpts\n")
		for _, ex := range input.SessionExcerpts {
			if strings.TrimSpace(ex) != "" {
				b.WriteString(strings.TrimSpace(ex) + "\n\n")
			}
		}
	}
	return b.String()
}

// ApplyDreamPlan safely applies a parsed DreamPlan, enforcing SecretGuard, pin protection,
// memory_dismissed protection, global candidate status, MEMORY.md rebuild, and audit diary append.
func (s *Store) ApplyDreamPlan(ctx context.Context, scope, botID string, plan *DreamPlan, now time.Time) (*DreamApplyResult, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: store is nil", ErrInvalidRequest)
	}
	if plan == nil {
		return nil, fmt.Errorf("%w: plan is nil", ErrInvalidRequest)
	}
	normScope, err := NormalizeScope(scope, botID)
	if err != nil {
		return nil, err
	}
	if normScope == ScopeAll {
		return nil, fmt.Errorf("%w: dream scope must be global or bot", ErrInvalidScope)
	}
	botID = strings.TrimSpace(botID)
	if normScope == ScopeGlobal {
		botID = ""
	} else if err := validateBotID(botID); err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()

	res := &DreamApplyResult{}
	var diaryOps []string

	for _, op := range plan.Operations {
		act := strings.ToUpper(strings.TrimSpace(op.Action))
		if act == DreamActionNoop || act == "" {
			res.Skipped++
			continue
		}

		// Enforce scope & bot ownership boundary for ADD operations.
		opScope := normScope
		opBotID := botID
		if act == DreamActionAdd {
			reqScope := strings.ToLower(strings.TrimSpace(op.Scope))
			reqBotID := strings.TrimSpace(op.BotID)
			if normScope == ScopeGlobal {
				if (reqScope != "" && reqScope != ScopeGlobal) || reqBotID != "" {
					res.Blocked++
					res.Warnings = append(res.Warnings, fmt.Sprintf("blocked ADD: global dream cannot write into bot %q", reqBotID))
					continue
				}
				opScope = ScopeGlobal
				opBotID = ""
			} else {
				// Bot-scoped dream may write into its own bot or propose a global candidate (never another bot).
				if reqBotID != "" && reqBotID != botID {
					res.Blocked++
					res.Warnings = append(res.Warnings, fmt.Sprintf("blocked ADD: dream for bot %q cannot write into bot %q", botID, reqBotID))
					continue
				}
				if reqScope == ScopeGlobal {
					opScope = ScopeGlobal
					opBotID = ""
				} else if reqScope != "" && reqScope != ScopeBot {
					res.Blocked++
					res.Warnings = append(res.Warnings, fmt.Sprintf("blocked ADD: invalid scope %q", op.Scope))
					continue
				}
			}
		}

		// SecretGuard check on all textual fields.
		if secErr := CheckFields(map[string]string{
			"title": op.Title,
			"text":  op.Text,
			"why":   op.Why,
			"tags":  strings.Join(op.Tags, " "),
		}); secErr != nil {
			res.Blocked++
			res.Warnings = append(res.Warnings, fmt.Sprintf("blocked %s by secret guard: %v", act, secErr))
			continue
		}

		switch act {
		case DreamActionAdd:
			if strings.TrimSpace(op.Text) == "" {
				res.Skipped++
				continue
			}
			title := DeriveTitle(op.Title, op.Text)
			if s.IsDismissed(ctx, opScope, opBotID, title, op.Text) {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("blocked ADD %q: matches dismissed memory", title))
				continue
			}
			status := dreamNewItemStatus(opScope, op.Confidence)
			conf := op.Confidence
			if conf <= 0 {
				conf = DefaultConfidence
			}
			saved, saveErr := s.Save(ctx, SaveReq{
				Scope:        opScope,
				BotID:        opBotID,
				Kind:         NormalizeKind(op.Kind),
				Title:        title,
				Text:         strings.TrimSpace(op.Text),
				Why:          strings.TrimSpace(firstNonEmpty(op.Why, op.Reason)),
				Tags:         op.Tags,
				Status:       status,
				Confidence:   &conf,
				ValidFrom:    strings.TrimSpace(op.ValidFrom),
				ValidUntil:   strings.TrimSpace(op.ValidUntil),
				SourceAuthor: AuthorDream,
			})
			if saveErr != nil {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("ADD failed: %v", saveErr))
				continue
			}
			res.Added++
			diaryOps = append(diaryOps, fmt.Sprintf("- ADD `%s` (%s/%s, status=%s): %s", saved.Item.ID, opScope, saved.Item.Kind, saved.Item.Status, saved.Item.Title))

		case DreamActionUpdate:
			targetID := strings.TrimSpace(op.ID)
			if targetID == "" {
				res.Skipped++
				continue
			}
			_, _, existingFM, _, loadErr := s.loadItemByID(ctx, targetID)
			if loadErr != nil {
				res.Skipped++
				res.Warnings = append(res.Warnings, fmt.Sprintf("UPDATE %s skipped: %v", targetID, loadErr))
				continue
			}
			if !dreamOwnsItem(normScope, botID, existingFM) {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("blocked UPDATE %s: item outside dream scope (%s/%s)", targetID, normScope, botID))
				continue
			}
			if existingFM.Pin {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("blocked UPDATE %s: item is pinned", targetID))
				continue
			}
			patch := PatchReq{}
			if strings.TrimSpace(op.Title) != "" {
				v := strings.TrimSpace(op.Title)
				patch.Title = &v
			}
			if strings.TrimSpace(op.Text) != "" {
				v := strings.TrimSpace(op.Text)
				patch.Text = &v
			}
			if strings.TrimSpace(op.Kind) != "" {
				v := NormalizeKind(op.Kind)
				patch.Kind = &v
			}
			if strings.TrimSpace(op.Why) != "" {
				v := strings.TrimSpace(op.Why)
				patch.Why = &v
			}
			if len(op.Tags) > 0 {
				t := NormalizeTags(op.Tags)
				patch.Tags = &t
			}
			if op.Confidence > 0 {
				c := op.Confidence
				patch.Confidence = &c
			}
			if strings.TrimSpace(op.ValidUntil) != "" {
				v := strings.TrimSpace(op.ValidUntil)
				patch.ValidUntil = &v
			}
			updated, patchErr := s.Patch(ctx, targetID, patch)
			if patchErr != nil {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("UPDATE %s failed: %v", targetID, patchErr))
				continue
			}
			res.Updated++
			diaryOps = append(diaryOps, fmt.Sprintf("- UPDATE `%s`: %s", updated.ID, updated.Title))

		case DreamActionSupersede:
			targetID := strings.TrimSpace(op.ID)
			if targetID == "" || strings.TrimSpace(op.Text) == "" {
				res.Skipped++
				continue
			}
			_, _, existingFM, _, loadErr := s.loadItemByID(ctx, targetID)
			if loadErr != nil {
				res.Skipped++
				res.Warnings = append(res.Warnings, fmt.Sprintf("SUPERSEDE %s skipped: %v", targetID, loadErr))
				continue
			}
			if !dreamOwnsItem(normScope, botID, existingFM) {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("blocked SUPERSEDE %s: item outside dream scope (%s/%s)", targetID, normScope, botID))
				continue
			}
			if existingFM.Pin {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("blocked SUPERSEDE %s: item is pinned", targetID))
				continue
			}
			title := DeriveTitle(firstNonEmpty(op.Title, existingFM.Title), op.Text)
			if s.IsDismissed(ctx, existingFM.Scope, existingFM.BotID, title, op.Text) {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("blocked SUPERSEDE %s: replacement matches dismissed memory", targetID))
				continue
			}
			status := dreamNewItemStatus(existingFM.Scope, op.Confidence)
			conf := op.Confidence
			if conf <= 0 {
				conf = DefaultConfidence
			}
			created, saveErr := s.Save(ctx, SaveReq{
				Scope:        existingFM.Scope,
				BotID:        existingFM.BotID,
				Kind:         NormalizeKind(firstNonEmpty(op.Kind, existingFM.Kind)),
				Title:        title,
				Text:         strings.TrimSpace(op.Text),
				Why:          strings.TrimSpace(firstNonEmpty(op.Why, op.Reason)),
				Tags:         mergeTags(existingFM.Tags, op.Tags),
				Status:       status,
				Confidence:   &conf,
				ValidFrom:    strings.TrimSpace(firstNonEmpty(op.ValidFrom, now.Format(time.RFC3339))),
				SourceAuthor: AuthorDream,
			})
			if saveErr != nil {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("SUPERSEDE %s create failed: %v", targetID, saveErr))
				continue
			}
			supStatus := StatusSuperseded
			supBy := created.Item.ID
			validUntil := now.Format(time.RFC3339)
			if _, pErr := s.Patch(ctx, targetID, PatchReq{
				Status:       &supStatus,
				SupersededBy: &supBy,
				ValidUntil:   &validUntil,
			}); pErr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("SUPERSEDE %s mark old failed: %v", targetID, pErr))
			}
			res.Superseded++
			diaryOps = append(diaryOps, fmt.Sprintf("- SUPERSEDE `%s` -> `%s`: %s", targetID, created.Item.ID, created.Item.Title))

		case DreamActionArchive:
			targetID := strings.TrimSpace(op.ID)
			if targetID == "" {
				res.Skipped++
				continue
			}
			_, _, existingFM, _, loadErr := s.loadItemByID(ctx, targetID)
			if loadErr != nil {
				res.Skipped++
				res.Warnings = append(res.Warnings, fmt.Sprintf("ARCHIVE %s skipped: %v", targetID, loadErr))
				continue
			}
			if !dreamOwnsItem(normScope, botID, existingFM) {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("blocked ARCHIVE %s: item outside dream scope (%s/%s)", targetID, normScope, botID))
				continue
			}
			if existingFM.Pin {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("blocked ARCHIVE %s: item is pinned", targetID))
				continue
			}
			if _, fErr := s.Forget(ctx, targetID, ForgetOpts{Reason: op.Reason}); fErr != nil {
				res.Blocked++
				res.Warnings = append(res.Warnings, fmt.Sprintf("ARCHIVE %s failed: %v", targetID, fErr))
				continue
			}
			res.Archived++
			diaryOps = append(diaryOps, fmt.Sprintf("- ARCHIVE `%s`: %s", targetID, existingFM.Title))
		}
	}

	// Rebuild curated MEMORY.md index within line & byte caps.
	idxDoc, err := s.RebuildIndex(ctx, normScope, botID)
	if err != nil {
		return res, err
	}
	res.IndexPath = idxDoc.Path
	res.IndexLines = idxDoc.Lines

	// Append audit diary entry under dreams/YYYY-MM-DD.md.
	diaryPath, err := s.appendDreamDiary(ctx, normScope, botID, plan.Summary, diaryOps, res, now)
	if err != nil {
		return res, err
	}
	res.DiaryPath = diaryPath
	return res, nil
}
