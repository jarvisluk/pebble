package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStreamlineOutputAndMedia(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.HubConfig{
		SharedDir: filepath.Join(tmp, "_shared"),
		Bots: []config.BotConfig{
			{ID: "alpha", Name: "Alpha", Harness: "opencode", Workspace: filepath.Join(tmp, "ws-alpha")},
			{ID: "beta", Name: "Beta", Harness: "opencode", Workspace: filepath.Join(tmp, "ws-beta")},
		},
	}
	for _, b := range cfg.Bots {
		_ = os.MkdirAll(b.Workspace, 0o755)
	}

	// Minimal valid 1x1 PNG
	pngBytes := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(filepath.Join(cfg.Bots[0].Workspace, "chart.png"), pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	var h *Hub
	reg := harness.NewRegistry()
	fh := &questionTestHarness{
		run: func(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			if strings.Contains(opts.Prompt, "USE_TOOL") {
				if _, err := h.SendOutboundMessage(opts.Bot.ID, "formal reply", []OutboundImage{{Path: "chart.png", Alt: "chart"}}); err != nil {
					return nil, err
				}
				return &harness.TurnResult{Text: "raw thinking trace"}, nil
			}
			if opts.Bot.ID == "beta" {
				return &harness.TurnResult{Text: "beta answer"}, nil
			}
			return &harness.TurnResult{Text: "fallback or silent"}, nil
		},
	}
	reg.Register(fh)
	h = New(cfg, store, nil, reg)
	defer h.Close()

	// 1. Explicit send_message hides raw res.Text as internal.
	if _, err := h.Send(context.Background(), "alpha", "USE_TOOL", SendOptions{Source: "web"}); err != nil {
		t.Fatal(err)
	}
	msgs, _ := h.Messages("alpha", 50)
	if len(msgs) != 2 || msgs[1].Text != "formal reply" || len(msgs[1].Media) != 1 {
		t.Fatalf("expected only user + formal outbound message with media, got %+v", msgs)
	}
	if _, _, err := h.ResolveMediaFile(msgs[1].Media[0].ID); err != nil {
		t.Fatalf("expected media file resolvable: %v", err)
	}
	if _, _, err := h.ResolveMediaFile("../etc/passwd"); err == nil {
		t.Fatalf("expected path traversal media id rejected")
	}

	// 2. Background webhook without send_message stays silent.
	if _, err := h.Send(context.Background(), "alpha", "BG_CHECK", SendOptions{Source: "webhook"}); err != nil {
		t.Fatal(err)
	}
	msgs, _ = h.Messages("alpha", 50)
	if len(msgs) != 3 {
		t.Fatalf("expected silent webhook turn (only user/webhook input added), got %d msgs", len(msgs))
	}

	// 3. Direct user turn without send_message falls back to showing res.Text.
	if _, err := h.Send(context.Background(), "alpha", "DIRECT_CHAT", SendOptions{Source: "web"}); err != nil {
		t.Fatal(err)
	}
	msgs, _ = h.Messages("alpha", 50)
	if len(msgs) != 5 || msgs[4].Role != "assistant" || msgs[4].Text != "fallback or silent" {
		t.Fatalf("expected user direct fallback assistant message, got %+v", msgs)
	}

	// 4. MessageBot records bidirectional bot_out/bot_in on both bots without assistant bubble on beta.
	if _, err := h.MessageBot(context.Background(), "alpha", "beta", "hello beta", 0); err != nil {
		t.Fatal(err)
	}
	alphaCollab, err := h.CollabMessages("alpha", "beta", 20)
	if err != nil || len(alphaCollab) != 2 {
		t.Fatalf("expected 2 collab messages on alpha, got %+v err=%v", alphaCollab, err)
	}
	betaCollab, err := h.CollabMessages("beta", "alpha", 20)
	if err != nil || len(betaCollab) != 2 || betaCollab[0].Role != "bot_in" || betaCollab[1].Role != "bot_out" {
		t.Fatalf("expected bot_in + bot_out on beta, got %+v err=%v", betaCollab, err)
	}

	// 5. Outbound file attachment.
	if err := os.WriteFile(filepath.Join(cfg.Bots[0].Workspace, "report.pdf"), []byte("%PDF-1.4\nhello"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileMsg, err := h.SendOutboundMessage("alpha", "attached report", nil, OutboundFile{Path: "report.pdf"})
	if err != nil {
		t.Fatalf("SendOutboundMessage with file failed: %v", err)
	}
	if len(fileMsg.Media) != 1 || fileMsg.Media[0].Kind != "file" || fileMsg.Media[0].Name != "report.pdf" || fileMsg.Media[0].Size <= 0 {
		t.Fatalf("unexpected file attachment metadata: %+v", fileMsg.Media)
	}
	if _, mimeType, err := h.ResolveMediaFile(fileMsg.Media[0].ID); err != nil || !strings.Contains(mimeType, "pdf") {
		t.Fatalf("ResolveMediaFile failed: mime=%q err=%v", mimeType, err)
	}
}

func TestSendOutboundMessageBackfillsMeta(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.HubConfig{
		SharedDir: filepath.Join(tmp, "_shared"),
		Bots: []config.BotConfig{
			{ID: "alpha", Name: "Alpha", Harness: "opencode", Workspace: filepath.Join(tmp, "ws-alpha")},
		},
	}
	_ = os.MkdirAll(cfg.Bots[0].Workspace, 0o755)

	var h *Hub
	reg := harness.NewRegistry()
	reg.Register(&questionTestHarness{
		run: func(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			opts.OnToolUse("bash")
			opts.OnToolUse("bash")
			opts.OnToolUse("send_message")
			if _, err := h.SendOutboundMessage(opts.Bot.ID, "final answer", nil); err != nil {
				return nil, err
			}
			// Empty res.Text still triggers metadata backfill on the outbound message.
			return &harness.TurnResult{
				DurationMs: 3450,
				Usage:      harness.Usage{CostUSD: 0.0042, InputTokens: 120, OutputTokens: 30},
			}, nil
		},
	})
	h = New(cfg, store, nil, reg)
	defer h.Close()

	subID, evCh := h.Subscribe()
	defer h.Unsubscribe(subID)

	if _, err := h.Send(context.Background(), "alpha", "hello", SendOptions{Source: "web"}); err != nil {
		t.Fatal(err)
	}

	msgs, err := h.Messages("alpha", 50)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %+v err=%v", msgs, err)
	}
	reply := msgs[1]
	if reply.DurationMs != 3450 || len(reply.Tools) != 2 || reply.Tools[0] != "bash×2" || reply.Tools[1] != "send_message" || reply.CostUSD != 0.0042 {
		t.Fatalf("expected backfilled metadata on outbound reply, got %+v", reply)
	}

	var sawUpdated bool
	timeout := time.After(2 * time.Second)
	for !sawUpdated {
		select {
		case ev := <-evCh:
			if ev.Type == "message_updated" {
				if m, ok := ev.Payload.(state.ChatMessage); ok && m.ID == reply.ID && m.DurationMs == 3450 && len(m.Tools) == 2 {
					sawUpdated = true
				}
			}
		case <-timeout:
			t.Fatal("timed out waiting for message_updated event")
		}
	}
}

type fakeCmdHarness struct {
	cmds  []harness.SlashCommand
	onRun func(context.Context, harness.RunOptions) (*harness.TurnResult, error)
}

func (f *fakeCmdHarness) Kind() string { return "fake" }

func (f *fakeCmdHarness) Label() string { return "Fake" }

func (f *fakeCmdHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (f *fakeCmdHarness) Close() {}

func (f *fakeCmdHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	return f.onRun(ctx, opts)
}

func (f *fakeCmdHarness) ListCommands(_ context.Context, _ config.BotConfig) ([]harness.SlashCommand, error) {
	return f.cmds, nil
}

func TestSlashPassthroughCommandsMentionsAndAttachments(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws-alpha")
	shared := filepath.Join(tmp, "_shared")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "USER.md"), []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(ws, "_shared")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("# readme"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))
	defer store.Close()

	var gotPrompt string
	var gotAtts []harness.Attachment
	reg := harness.NewRegistry()
	reg.Register(&fakeCmdHarness{
		onRun: func(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			gotPrompt = opts.Prompt
			gotAtts = opts.Attachments
			return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "s1", Text: "ok"}, nil
		},
		cmds: []harness.SlashCommand{
			{Name: "compact", Description: "compact ctx", Source: "harness"},
			{Name: "status", Description: "harness status", Source: "harness"},
		},
	})

	h := New(&config.HubConfig{
		SharedDir:   shared,
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "alpha", Name: "Alpha", Workspace: ws, Harness: "fake"},
			{ID: "beta", Name: "Beta", Workspace: filepath.Join(tmp, "ws-beta"), Harness: "fake"},
		},
	}, store, msgStore, reg)
	defer h.Close()

	// Seed an archived session so buildTurnPrompt would normally prepend [System Note: ...] on next turn.
	if _, err := h.HandleInput(context.Background(), "alpha", "warmup", SendOptions{Source: "web"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.ResetSession("alpha"); err != nil {
		t.Fatal(err)
	}

	// 1. Slash command prompt must not be prefixed with [System Note: ...]
	if _, err := h.HandleInput(context.Background(), "alpha", "/compact now", SendOptions{Source: "web"}); err != nil {
		t.Fatal(err)
	}
	if gotPrompt != "/compact now" {
		t.Fatalf("expected slash prompt to pass through verbatim, got %q", gotPrompt)
	}

	// 2. ListBotCommands merges harness + hub and keeps hub definition for /status
	cmds, err := h.ListBotCommands(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	foundCompact := false
	statusSource := ""
	for _, c := range cmds {
		if c.Name == "compact" && c.Source == "harness" {
			foundCompact = true
		}
		if c.Name == "status" {
			statusSource = c.Source
		}
	}
	if !foundCompact || statusSource != "hub" {
		t.Fatalf("unexpected commands: %+v", cmds)
	}

	// 3. ListBotMentions includes beta bot, README.md, _shared/USER.md, skips .git
	mentions, err := h.ListBotMentions(context.Background(), "alpha", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	var hasBeta, hasReadme, hasSharedUser, hasGit bool
	for _, m := range mentions {
		if m.Kind == "bot" && m.Value == "beta" {
			hasBeta = true
		}
		if m.Kind == "file" && m.Value == "README.md" {
			hasReadme = true
		}
		if m.Kind == "file" && m.Value == "_shared/USER.md" {
			hasSharedUser = true
		}
		if strings.Contains(m.Value, ".git") {
			hasGit = true
		}
	}
	if !hasBeta || !hasReadme || !hasSharedUser || hasGit {
		t.Fatalf("unexpected mentions: beta=%v readme=%v shared=%v git=%v items=%+v", hasBeta, hasReadme, hasSharedUser, hasGit, mentions)
	}

	// 4. SaveInboundAttachment + empty text send with MediaIDs
	att, err := h.SaveInboundAttachment("alpha", "note.txt", []byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(att.WorkspacePath, ".uploads/") {
		t.Fatalf("unexpected workspacePath: %q", att.WorkspacePath)
	}
	if _, err := h.HandleInput(context.Background(), "alpha", "", SendOptions{Source: "web", MediaIDs: []string{att.Media.ID}}); err != nil {
		t.Fatal(err)
	}
	if len(gotAtts) != 1 || gotAtts[0].ID != att.Media.ID {
		t.Fatalf("expected attachment forwarded to harness, got %+v", gotAtts)
	}
	msgs, _ := h.Messages("alpha", 10)
	var sawUserMedia bool
	for _, m := range msgs {
		if m.Role == "user" && len(m.Media) == 1 && m.Media[0].ID == att.Media.ID {
			sawUserMedia = true
		}
	}
	if !sawUserMedia {
		t.Fatalf("expected user message to persist attachment media, got %+v", msgs)
	}

	// 5. @bot shortcut delegates to target bot, while @unknown or @file goes to current bot.
	resMentionBot, err := h.HandleInput(context.Background(), "alpha", "@beta please check", SendOptions{Source: "web"})
	if err != nil {
		t.Fatalf("@beta HandleInput failed: %v", err)
	}
	if !strings.HasPrefix(resMentionBot.Text, "[beta]\n") {
		t.Fatalf("expected delegated [beta] response, got %q", resMentionBot.Text)
	}
	resMentionFile, err := h.HandleInput(context.Background(), "alpha", "@README.md summarize", SendOptions{Source: "web"})
	if err != nil {
		t.Fatalf("@README.md HandleInput failed: %v", err)
	}
	if resMentionFile.BotID != "alpha" || strings.HasPrefix(resMentionFile.Text, "[beta]") {
		t.Fatalf("expected @README.md to stay on alpha, got %+v", resMentionFile)
	}
}
