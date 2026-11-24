package hub

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/desktop"
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
	"bot-hub/internal/protocol"
	"bot-hub/internal/push"
	"bot-hub/internal/state"
)

const MaxBotHopDepth = 4

// ErrBotNotFound indicates that the requested bot ID is not registered in the hub.
var ErrBotNotFound = errors.New("bot not found")

// ErrRoutineNotFound indicates that the requested routine ID does not exist on the bot.
var ErrRoutineNotFound = errors.New("routine not found")

// ErrDesktopTaskBusy indicates a computer-use worker is already running on the target desktop.
var ErrDesktopTaskBusy = errors.New("desktop task already running for target")

// ErrLocalDesktopDisabled indicates local host desktop automation is turned off in settings.
var ErrLocalDesktopDisabled = errors.New("本机桌面操控已在「设置 › 远程与设备」中关闭")

// ErrBoundDesktopOffline indicates a bot's explicitly bound desktop target is offline or unavailable.
var ErrBoundDesktopOffline = errors.New("绑定的桌面节点当前离线或未开启桌面权限")

// ErrNodeUpgradeRequired indicates the target remote node is running an older version that lacks a required capability.
var ErrNodeUpgradeRequired = errors.New("远端节点版本过低，不支持此功能，请升级该节点")

// ErrForbidden indicates the caller is not permitted to access another bot's private sessions.
var ErrForbidden = errors.New("forbidden")

// ErrQuestionNotFound indicates that the requested pending question does not exist or has already been settled.
var ErrQuestionNotFound = errors.New("question not found")

// PendingQuestion describes an interactive question card awaiting user input.
type PendingQuestion struct {
	ID        string                  `json:"id"`
	BotID     string                  `json:"botId"`
	BotName   string                  `json:"botName,omitempty"`
	Title     string                  `json:"title,omitempty"`
	Fields    []harness.QuestionField `json:"fields"`
	CreatedAt string                  `json:"createdAt"`
}

type pendingQuestionEntry struct {
	item PendingQuestion
	ch   chan harness.QuestionResponse
}

type SendOptions struct {
	Source    string
	FromBotID string
	Hop       int
	MediaIDs  []string
	OnText    func(chunk string)
	OnToolUse func(toolName string)
}

type InboundAttachment struct {
	Media         state.MessageMedia `json:"media"`
	WorkspacePath string             `json:"workspacePath"`
}

type inboundAttEntry struct {
	media state.MessageMedia
	att   harness.Attachment
}

type MentionCandidate struct {
	Kind        string `json:"kind"` // "file" | "bot" | "agent"
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type IsolatedOptions struct {
	Source     string
	Harness    string
	Model      string
	MCPRole    string
	TimeoutSec int
	ExtraEnv   map[string]string
	OnToolUse  func(toolName string)
}

type DesktopTaskRequest struct {
	BotID      string `json:"botId"`
	Target     string `json:"nodeId,omitempty"`
	Goal       string `json:"goal"`
	App        string `json:"app,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

type RoutinePatch struct {
	Name               *string `json:"name,omitempty"`
	EveryMinutes       *int    `json:"everyMinutes,omitempty"`
	DailyAt            *string `json:"dailyAt,omitempty"`
	Cron               *string `json:"cron,omitempty"`
	WebhookTrigger     *bool   `json:"webhookTrigger,omitempty"`
	WebhookSecret      *string `json:"webhookSecret,omitempty"`
	ClearWebhookSecret *bool   `json:"clearWebhookSecret,omitempty"`
	WebhookURL         *string `json:"webhookUrl,omitempty"`
	Prompt             *string `json:"prompt,omitempty"`
	Model              *string `json:"model,omitempty"`
	MainDelivery       *string `json:"mainDelivery,omitempty"`
	Enabled            *bool   `json:"enabled,omitempty"`
}

type BotStatus struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Color       string `json:"color,omitempty"`
	Workspace   string `json:"workspace"`
	Harness     string `json:"harness"`
	Model       string `json:"model,omitempty"`
	// ModelInherited: the model is the hub default for the bot's harness.
	ModelInherited   bool                 `json:"modelInherited"`
	PrivateSessions  bool                 `json:"privateSessions,omitempty"`
	DesktopTarget    string               `json:"desktopTarget,omitempty"`
	BrowserMode      string               `json:"browserMode,omitempty"`
	ArchivedSessions int                  `json:"archivedSessions,omitempty"`
	Busy             bool                 `json:"busy"`
	Queued           int64                `json:"queued"`
	Session          *state.SessionRecord `json:"session,omitempty"`
}

type BotDetail struct {
	BotStatus
	Instructions string `json:"instructions"`
}

type CreateBotRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	Avatar          string `json:"avatar,omitempty"`
	Color           string `json:"color,omitempty"`
	Harness         string `json:"harness,omitempty"`
	Model           string `json:"model,omitempty"`
	Instructions    string `json:"instructions,omitempty"`
	PrivateSessions bool   `json:"privateSessions,omitempty"`
	DesktopTarget   string `json:"desktopTarget,omitempty"`
	BrowserMode     string `json:"browserMode,omitempty"`
}

// BotUpdate changes a bot's settings; nil fields stay as they are. An empty
// Model means "inherit the hub default".
type BotUpdate struct {
	Name            *string `json:"name,omitempty"`
	Description     *string `json:"description,omitempty"`
	Avatar          *string `json:"avatar,omitempty"`
	Color           *string `json:"color,omitempty"`
	Harness         *string `json:"harness,omitempty"`
	Model           *string `json:"model,omitempty"`
	Instructions    *string `json:"instructions,omitempty"`
	PrivateSessions *bool   `json:"privateSessions,omitempty"`
	DesktopTarget   *string `json:"desktopTarget,omitempty"`
	BrowserMode     *string `json:"browserMode,omitempty"`
}

type RotateResult struct {
	Rotated   bool   `json:"rotated"`
	Deferred  bool   `json:"deferred"`
	SessionID string `json:"sessionId,omitempty"`
}

// HarnessInfo describes one available harness.
type HarnessInfo struct {
	ID             string                     `json:"id"`
	Label          string                     `json:"label"`
	DefaultModel   string                     `json:"defaultModel"`
	Installed      bool                       `json:"installed"`
	BinaryPath     string                     `json:"binaryPath,omitempty"`
	AuthStatus     string                     `json:"authStatus,omitempty"` // "ready" | "needs_login" | "unknown"
	AuthDetail     string                     `json:"authDetail,omitempty"`
	RequiresLogin  bool                       `json:"requiresLogin,omitempty"`
	CanAutoInstall bool                       `json:"canAutoInstall,omitempty"`
	Category       string                     `json:"category,omitempty"` // "recommended" | "advanced"
	Summary        string                     `json:"summary,omitempty"`
	Install        harness.HarnessInstallInfo `json:"install"`
	Magpie         harness.MagpieStatus       `json:"magpie"`
}

type Event struct {
	Type    string `json:"type"` // "message" | "tool_use" | "bot_status" | "bot_created" | "bot_deleted"
	BotID   string `json:"botId,omitempty"`
	Payload any    `json:"payload"`
}

type turnJob struct {
	ctx             context.Context
	prompt          string
	attachments     []harness.Attachment
	opts            SendOptions
	userMsgID       string
	queuedSessionID string
	respCh          chan turnResponse
}

type turnResponse struct {
	res *harness.TurnResult
	err error
}

type botLane struct {
	bot      config.BotConfig
	ch       chan turnJob
	busy     atomic.Bool
	queued   atomic.Int64
	stopCh   chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once

	sessionGen atomic.Uint64

	turnSentCount   atomic.Int64
	turnMu          sync.Mutex
	activeSessionID string
	activeTurn      int
	turnLastMsgID   string

	rotateMu       sync.Mutex
	pendingRotate  bool
	pendingHandoff string

	cancelMu sync.Mutex
	cancel   context.CancelFunc // interrupts the running turn
}

func (l *botLane) stop() {
	l.stopOnce.Do(func() { close(l.stopCh) })
	<-l.stopped
}

type Hub struct {
	cfg       *config.HubConfig
	store     *state.Store
	messages  *state.MessageStore
	mediaDir  string
	registry      *harness.Registry
	nodes         *node.Registry
	localDeskPool *desktop.EnginePool
	desktop       *desktop.Coordinator

	lanesMu sync.RWMutex
	lanes   map[string]*botLane
	order   []string

	reloadMu      sync.Mutex
	pendingReload map[string]bool

	activeEphemeralMu sync.Mutex
	activeEphemeral   map[string]int

	isolatedCancelsMu sync.Mutex
	isolatedCancels   map[string]map[uint64]context.CancelFunc
	isolatedSeq       atomic.Uint64

	desktopTaskMu      sync.Mutex
	activeDesktopTasks map[string]string // target -> botID

	desktopTargetMu      sync.Mutex
	activeDesktopTarget  string
	activeDesktopExpires time.Time
	activeDesktopTimer   *time.Timer

	schedMu            sync.RWMutex
	onSchedulesChanged func()

	subsMu sync.Mutex
	subs   map[uint64]chan Event
	subSeq atomic.Uint64

	push *push.Manager

	closeMu sync.Mutex
	closed  bool

	// hops records the bot-to-bot hop depth of each bot's in-flight turn. MCP
	// servers run inside opencode's shared service and can't see per-turn env,
	// so the hub derives the hop of outgoing bot messages from here.
	hopsMu sync.Mutex
	hops   map[string]int

	questionsMu sync.Mutex
	questions   map[string]*pendingQuestionEntry

	inboundAttsMu sync.Mutex
	inboundAtts   map[string]inboundAttEntry
}

var askCmdRe = regexp.MustCompile(`(?s)^/ask\s+([a-zA-Z0-9_-]+)\s+(.+)$`)
var mentionBotRe = regexp.MustCompile(`(?s)^@([A-Za-z0-9_.-]+)\s+(.+)$`)

func New(cfg *config.HubConfig, store *state.Store, messages *state.MessageStore, reg *harness.Registry) *Hub {
	if reg == nil {
		reg = harness.NewDefaultRegistry(cfg)
	}
	deskCfg := desktop.VirtualConfig{
		Mode:                  cfg.Desktop.EffectiveMode(),
		AutoInstall:           cfg.Desktop.AutoInstall,
		Resolution:            cfg.Desktop.EffectiveResolution(),
		IdleFreeze:            time.Duration(cfg.Desktop.EffectiveIdleFreezeMinutes()) * time.Minute,
		IdleStop:              time.Duration(cfg.Desktop.EffectiveIdleStopMinutes()) * time.Minute,
		StopOnlyWhenNoWindows: cfg.Desktop.ShouldStopOnlyWhenNoWindows(),
	}
	mediaDir := filepath.Join(os.TempDir(), "bot-hub-media")
	if store != nil && store.DBPath() != "" && store.DBPath() != ":memory:" {
		mediaDir = filepath.Join(filepath.Dir(store.DBPath()), "media")
	} else if cfg != nil && cfg.StateFile != "" {
		mediaDir = filepath.Join(filepath.Dir(cfg.StateFile), "media")
	} else if cfg != nil && cfg.MessagesDir != "" {
		mediaDir = filepath.Join(filepath.Dir(cfg.MessagesDir), "media")
	}
	h := &Hub{
		cfg:                cfg,
		store:              store,
		messages:           messages,
		mediaDir:           mediaDir,
		registry:           reg,
		localDeskPool:      desktop.NewEnginePool(deskCfg),
		lanes:              make(map[string]*botLane, len(cfg.Bots)),
		order:              make([]string, 0, len(cfg.Bots)),
		pendingReload:      make(map[string]bool),
		activeEphemeral:    make(map[string]int),
		isolatedCancels:    make(map[string]map[uint64]context.CancelFunc),
		activeDesktopTasks: make(map[string]string),
		subs:               make(map[uint64]chan Event),
		hops:               make(map[string]int),
		questions:          make(map[string]*pendingQuestionEntry),
		inboundAtts:        make(map[string]inboundAttEntry),
		push:               push.NewManager(cfg, store),
	}
	h.nodes = node.NewRegistry(func() {
		h.reconcileActiveDesktopTarget()
		h.broadcast(Event{
			Type:    "nodes_changed",
			Payload: map[string]any{"nodes": h.nodes.List()},
		})
	})
	h.desktop = desktop.NewCoordinator(
		func(st desktop.ScreenState) {
			h.broadcast(Event{Type: "desktop", Payload: st})
		},
		func(items []desktop.TakeoverRequest) {
			h.broadcast(Event{Type: "takeover", Payload: map[string]any{"items": items}})
		},
	)
	h.localDeskPool.SetHasTakeover(func(target string) bool {
		t := desktop.NormalizeTarget(target)
		for _, item := range h.desktop.ListTakeovers() {
			if desktop.NormalizeTarget(item.Target) == t {
				return true
			}
		}
		return false
	})
	for _, bot := range cfg.Bots {
		h.startLaneLocked(bot)
	}
	return h
}

func (h *Hub) startLaneLocked(bot config.BotConfig) {
	lane := &botLane{
		bot:     bot,
		ch:      make(chan turnJob, 64),
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
	h.lanes[bot.ID] = lane
	h.order = append(h.order, bot.ID)
	go h.runLane(lane)
}

func (h *Hub) Close() {
	h.closeMu.Lock()
	if h.closed {
		h.closeMu.Unlock()
		return
	}
	h.closed = true
	h.closeMu.Unlock()

	h.lanesMu.RLock()
	lanes := make([]*botLane, 0, len(h.order))
	for _, id := range h.order {
		lanes = append(lanes, h.lanes[id])
	}
	h.lanesMu.RUnlock()

	for _, lane := range lanes {
		lane.stop()
	}
	h.desktopTargetMu.Lock()
	if h.activeDesktopTimer != nil {
		h.activeDesktopTimer.Stop()
		h.activeDesktopTimer = nil
	}
	h.desktopTargetMu.Unlock()
	h.localDeskPool.Close()
	h.registry.Close()
	if h.push != nil {
		h.push.Close()
	}
	if h.store != nil {
		_ = h.store.Close()
	}
}

func (h *Hub) Config() *config.HubConfig {
	return h.cfg
}

func (h *Hub) Nodes() *node.Registry {
	return h.nodes
}

func (h *Hub) Store() *state.Store {
	return h.store
}

func (h *Hub) LocalDesktop() *desktop.Engine {
	if h.localDeskPool == nil {
		return nil
	}
	return h.localDeskPool.Default()
}

func (h *Hub) LocalDesktopPool() *desktop.EnginePool {
	return h.localDeskPool
}

func (h *Hub) Desktop() *desktop.Coordinator {
	return h.desktop
}

func (h *Hub) BotName(botID string) string {
	if b, ok := h.GetBot(botID); ok {
		return b.Name
	}
	return botID
}

func (h *Hub) BotWorkspace(botID string) string {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	if lane := h.lanes[botID]; lane != nil {
		return lane.bot.Workspace
	}
	return ""
}

// SetAPIToken updates the in-memory API token and re-syncs workspace MCP
// configs so bots receive the updated token on disk and on subsequent turns.
func (h *Hub) SetAPIToken(token string) {
	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	h.cfg.HTTP.APIToken = token
	for i := range h.cfg.Bots {
		_ = config.EnsureWorkspace(&h.cfg.Bots[i], h.cfg, "")
	}
}

func (h *Hub) Subscribe() (uint64, <-chan Event) {
	id := h.subSeq.Add(1)
	ch := make(chan Event, 64)
	h.subsMu.Lock()
	h.subs[id] = ch
	h.subsMu.Unlock()
	return id, ch
}

func (h *Hub) Unsubscribe(id uint64) {
	h.subsMu.Lock()
	if ch, ok := h.subs[id]; ok {
		delete(h.subs, id)
		close(ch)
	}
	h.subsMu.Unlock()
}

func (h *Hub) broadcast(evt Event) {
	if h.push != nil {
		h.push.EnqueueEvent(evt.Type, evt.BotID, evt.Payload)
	}
	h.subsMu.Lock()
	defer h.subsMu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- evt:
		default:
			// Drop if a slow web client lags behind.
		}
	}
}

func (h *Hub) PushStatus() push.Status {
	if h.push == nil {
		return push.Status{Mode: "disabled"}
	}
	return h.push.Status()
}

func (h *Hub) UpsertPushDevice(d state.PushDevice) error {
	if h.store == nil {
		return errors.New("state store unavailable")
	}
	return h.store.UpsertPushDevice(d)
}

func (h *Hub) SetPushActivityToken(deviceID, activityKey, token string) error {
	if h.store == nil {
		return errors.New("state store unavailable")
	}
	return h.store.SetPushActivityToken(deviceID, activityKey, token)
}

func (h *Hub) DeletePushDevice(deviceID string) error {
	if h.store == nil {
		return errors.New("state store unavailable")
	}
	return h.store.DeletePushDevice(deviceID)
}

func (h *Hub) appendMessage(msg state.ChatMessage) state.ChatMessage {
	var (
		saved state.ChatMessage
		err   error
	)
	if h.store != nil {
		saved, err = h.store.AppendMessage(msg)
	} else if h.messages != nil {
		saved, err = h.messages.Append(msg)
	} else {
		return msg
	}
	if err != nil {
		return msg
	}
	h.broadcast(Event{
		Type:    "message",
		BotID:   saved.BotID,
		Payload: saved,
	})
	return saved
}

func (h *Hub) updateMessageMeta(msgID string, tools []string, durationMs int64, costUSD float64, inTok, outTok int) {
	if msgID == "" || h.store == nil {
		return
	}
	updated, ok, err := h.store.UpdateMessageMeta(msgID, tools, durationMs, costUSD, inTok, outTok)
	if err != nil || !ok {
		return
	}
	h.broadcast(Event{
		Type:    "message_updated",
		BotID:   updated.BotID,
		Payload: updated,
	})
}

func (h *Hub) appendInternalMessage(msg state.ChatMessage) state.ChatMessage {
	msg.Role = "internal"
	if h.store != nil {
		if saved, err := h.store.AppendMessage(msg); err == nil {
			return saved
		}
		return msg
	}
	if h.messages != nil {
		if saved, err := h.messages.Append(msg); err == nil {
			return saved
		}
	}
	return msg
}

func (h *Hub) CollabMessages(botID, peerID string, limit int) ([]state.ChatMessage, error) {
	h.lanesMu.RLock()
	_, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found", botID)
	}
	if strings.TrimSpace(peerID) == "" {
		return nil, fmt.Errorf("peerId is required")
	}
	if h.store != nil {
		return h.store.ListCollabMessages(botID, peerID, limit)
	}
	if h.messages == nil {
		return []state.ChatMessage{}, nil
	}
	return h.messages.ListCollab(botID, peerID, limit)
}

type OutboundImage struct {
	Path       string `json:"path,omitempty"`
	Alt        string `json:"alt,omitempty"`
	DataBase64 string `json:"dataBase64,omitempty"`
	Filename   string `json:"filename,omitempty"`
}

type OutboundFile struct {
	Path       string `json:"path,omitempty"`
	Name       string `json:"name,omitempty"`
	DataBase64 string `json:"dataBase64,omitempty"`
	Filename   string `json:"filename,omitempty"`
}

type OutboundAttachment struct {
	Path       string `json:"path,omitempty"`
	Name       string `json:"name,omitempty"`
	Alt        string `json:"alt,omitempty"`
	DataBase64 string `json:"dataBase64,omitempty"`
	Filename   string `json:"filename,omitempty"`
}

const (
	maxOutboundImageBytes = 20 << 20 // 20 MiB
	maxOutboundFileBytes  = 50 << 20 // 50 MiB
)

var (
	validMediaIDRe = regexp.MustCompile(`^med_[0-9]+_[a-f0-9]+(\.[a-z0-9]{1,12})?$`)
	safeFileExtRe  = regexp.MustCompile(`^\.[a-z0-9]{1,12}$`)
)

func imageExtForMIME(mimeType string) (string, bool) {
	switch mimeType {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func (h *Hub) SaveOutboundImage(botWorkspace string, img OutboundImage) (state.MessageMedia, error) {
	return h.SaveOutboundAttachment(botWorkspace, OutboundAttachment{
		Path:       img.Path,
		Alt:        img.Alt,
		DataBase64: img.DataBase64,
		Filename:   img.Filename,
	}, true)
}

func (h *Hub) SaveOutboundFile(botWorkspace string, f OutboundFile) (state.MessageMedia, error) {
	return h.SaveOutboundAttachment(botWorkspace, OutboundAttachment{
		Path:       f.Path,
		Name:       f.Name,
		DataBase64: f.DataBase64,
		Filename:   f.Filename,
	}, false)
}

func (h *Hub) SaveOutboundAttachment(botWorkspace string, att OutboundAttachment, requireImage bool) (state.MessageMedia, error) {
	maxBytes := int64(maxOutboundFileBytes)
	kindLabel := "file"
	if requireImage {
		maxBytes = maxOutboundImageBytes
		kindLabel = "image"
	}

	var raw []byte
	var name string

	if strings.TrimSpace(att.DataBase64) != "" {
		b64 := strings.TrimSpace(att.DataBase64)
		if idx := strings.Index(b64, ";base64,"); idx >= 0 {
			b64 = b64[idx+len(";base64,"):]
		}
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return state.MessageMedia{}, fmt.Errorf("invalid base64 %s: %w", kindLabel, err)
		}
		if int64(len(decoded)) > maxBytes {
			return state.MessageMedia{}, fmt.Errorf("%s exceeds max size (%d bytes)", kindLabel, maxBytes)
		}
		raw = decoded
		name = filepath.Base(strings.TrimSpace(att.Filename))
	} else {
		p := strings.TrimSpace(att.Path)
		if p == "" {
			return state.MessageMedia{}, fmt.Errorf("%s path or dataBase64 is required", kindLabel)
		}
		if !filepath.IsAbs(p) && botWorkspace != "" {
			p = filepath.Join(botWorkspace, p)
		}
		info, err := os.Stat(p)
		if err != nil {
			return state.MessageMedia{}, fmt.Errorf("stat %s %s: %w", kindLabel, p, err)
		}
		if info.IsDir() {
			return state.MessageMedia{}, fmt.Errorf("%s path %s is a directory", kindLabel, p)
		}
		if info.Size() > maxBytes {
			return state.MessageMedia{}, fmt.Errorf("%s %s exceeds max size (%d bytes)", kindLabel, p, maxBytes)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return state.MessageMedia{}, fmt.Errorf("read %s %s: %w", kindLabel, p, err)
		}
		raw = b
		name = filepath.Base(p)
	}

	if len(raw) == 0 {
		return state.MessageMedia{}, fmt.Errorf("%s is empty", kindLabel)
	}
	if customName := strings.TrimSpace(att.Name); customName != "" {
		name = filepath.Base(customName)
	}

	var mimeType, ext string
	if requireImage {
		mimeType = http.DetectContentType(raw)
		var ok bool
		ext, ok = imageExtForMIME(mimeType)
		if !ok {
			return state.MessageMedia{}, fmt.Errorf("unsupported image type %q", mimeType)
		}
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "image" + ext
		}
	} else {
		rawExt := strings.ToLower(filepath.Ext(name))
		if safeFileExtRe.MatchString(rawExt) {
			ext = rawExt
		}
		if ext != "" {
			mimeType = mime.TypeByExtension(ext)
		}
		if mimeType == "" {
			mimeType = http.DetectContentType(raw)
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "file" + ext
		}
	}

	if err := os.MkdirAll(h.mediaDir, 0o755); err != nil {
		return state.MessageMedia{}, fmt.Errorf("create media dir: %w", err)
	}
	rb := make([]byte, 4)
	_, _ = rand.Read(rb)
	id := fmt.Sprintf("med_%d_%s%s", time.Now().UTC().UnixMilli(), hex.EncodeToString(rb), ext)
	dst := filepath.Join(h.mediaDir, id)
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return state.MessageMedia{}, fmt.Errorf("write media file: %w", err)
	}

	return state.MessageMedia{
		ID:       id,
		Kind:     kindLabel,
		URL:      "/api/media/" + id,
		MimeType: mimeType,
		Name:     name,
		Alt:      strings.TrimSpace(att.Alt),
		Size:     int64(len(raw)),
	}, nil
}

func (h *Hub) ResolveMediaFile(mediaID string) (string, string, error) {
	id := strings.TrimSpace(mediaID)
	if !validMediaIDRe.MatchString(id) || strings.Contains(id, "..") || strings.ContainsAny(id, "/\\") {
		return "", "", fmt.Errorf("invalid media id")
	}
	p := filepath.Join(h.mediaDir, id)
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return "", "", os.ErrNotExist
	}
	ext := strings.ToLower(filepath.Ext(id))
	mimeType := ""
	if ext != "" {
		mimeType = mime.TypeByExtension(ext)
	}
	if mimeType == "" {
		if f, err := os.Open(p); err == nil {
			var buf [512]byte
			n, _ := f.Read(buf[:])
			_ = f.Close()
			if n > 0 {
				mimeType = http.DetectContentType(buf[:n])
			}
		}
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return p, mimeType, nil
}

func (h *Hub) SendOutboundMessage(botID, text string, images []OutboundImage, files ...OutboundFile) (state.ChatMessage, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	laneBot := config.BotConfig{}
	if ok {
		laneBot = lane.bot
	}
	h.lanesMu.RUnlock()
	if !ok {
		return state.ChatMessage{}, fmt.Errorf("bot %q not found", botID)
	}

	if strings.TrimSpace(text) == "" && len(images) == 0 && len(files) == 0 {
		return state.ChatMessage{}, fmt.Errorf("text, images, or files is required")
	}

	var media []state.MessageMedia
	for _, img := range images {
		m, err := h.SaveOutboundImage(laneBot.Workspace, img)
		if err != nil {
			return state.ChatMessage{}, err
		}
		media = append(media, m)
	}
	for _, f := range files {
		m, err := h.SaveOutboundFile(laneBot.Workspace, f)
		if err != nil {
			return state.ChatMessage{}, err
		}
		media = append(media, m)
	}

	lane.turnMu.Lock()
	sessID := lane.activeSessionID
	turn := lane.activeTurn
	lane.turnMu.Unlock()

	if sessID == "" && h.store != nil {
		if sess, err := h.store.EnsureActiveSession(botID, laneBot.Harness, laneBot.Model); err == nil {
			sessID = sess.ID
			turn = sess.TurnCount
		}
	}

	saved := h.appendMessage(state.ChatMessage{
		BotID:     botID,
		SessionID: sessID,
		Turn:      turn,
		Role:      "assistant",
		From:      "bot:" + botID,
		Text:      text,
		Media:     media,
	})
	lane.turnMu.Lock()
	lane.turnLastMsgID = saved.ID
	lane.turnMu.Unlock()
	lane.turnSentCount.Add(1)
	return saved, nil
}

func (h *Hub) Messages(botID string, limit int) ([]state.ChatMessage, error) {
	h.lanesMu.RLock()
	_, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found", botID)
	}
	if h.store != nil {
		return h.store.ListMessages(botID, limit)
	}
	if h.messages == nil {
		return []state.ChatMessage{}, nil
	}
	return h.messages.List(botID, limit)
}

func (h *Hub) Bots() []config.BotConfig {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	out := make([]config.BotConfig, 0, len(h.order))
	for _, id := range h.order {
		out = append(out, h.lanes[id].bot)
	}
	return out
}

func (h *Hub) GetBot(botID string) (config.BotConfig, bool) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	lane, ok := h.lanes[botID]
	if !ok {
		return config.BotConfig{}, false
	}
	return lane.bot, true
}

func (h *Hub) Status(botID string) ([]BotStatus, error) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()

	ids := h.order
	if botID != "" {
		if _, ok := h.lanes[botID]; !ok {
			return nil, fmt.Errorf("bot %q not found", botID)
		}
		ids = []string{botID}
	}

	out := make([]BotStatus, 0, len(ids))
	for _, id := range ids {
		out = append(out, h.statusOfLocked(h.lanes[id]))
	}
	return out, nil
}

func (h *Hub) statusOfLocked(lane *botLane) BotStatus {
	var sessPtr *state.SessionRecord
	archivedCount := 0
	if h.store != nil {
		if rec, ok := h.store.Get(lane.bot.ID); ok {
			recCopy := rec
			sessPtr = &recCopy
		}
		archivedCount = h.store.ArchivedCount(lane.bot.ID)
	}
	return BotStatus{
		ID:               lane.bot.ID,
		Name:             lane.bot.Name,
		Description:      lane.bot.Description,
		Avatar:           lane.bot.Avatar,
		Color:            lane.bot.Color,
		Workspace:        lane.bot.Workspace,
		Harness:          lane.bot.Harness,
		Model:            lane.bot.Model,
		ModelInherited:   lane.bot.ModelInherited,
		PrivateSessions:  lane.bot.PrivateSessions,
		DesktopTarget:    lane.bot.DesktopTarget,
		BrowserMode:      lane.bot.BrowserMode,
		ArchivedSessions: archivedCount,
		Busy:             lane.busy.Load(),
		Queued:           lane.queued.Load(),
		Session:          sessPtr,
	}
}

func (h *Hub) GetBotDetail(botID string) (*BotDetail, error) {
	sts, err := h.Status(botID)
	if err != nil {
		return nil, err
	}
	st := sts[0]
	raw, _ := os.ReadFile(filepath.Join(st.Workspace, "AGENTS.md"))
	return &BotDetail{
		BotStatus:    st,
		Instructions: string(raw),
	}, nil
}

// CreateBot dynamically provisions a new bot workspace, persists config/bots.json,
// and starts its dedicated serialized worker goroutine immediately.
func (h *Hub) CreateBot(req CreateBotRequest) (*BotStatus, error) {
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	avatar, err := config.NormalizeBotAvatar(req.Avatar)
	if err != nil {
		return nil, err
	}
	req.Avatar = avatar
	color, err := config.NormalizeBotColor(req.Color)
	if err != nil {
		return nil, err
	}
	req.Color = color
	req.Model = strings.TrimSpace(req.Model)
	req.Harness = strings.TrimSpace(req.Harness)
	if req.Harness == "" {
		req.Harness = h.DefaultHarness()
	}
	if _, err := h.registry.Get(req.Harness); err != nil {
		return nil, err
	}
	dt, err := config.NormalizeDesktopTarget(req.DesktopTarget)
	if err != nil {
		return nil, err
	}
	req.DesktopTarget = dt
	bm, err := config.NormalizeBrowserMode(req.BrowserMode)
	if err != nil {
		return nil, err
	}
	req.BrowserMode = bm
	req.Instructions = strings.TrimSpace(req.Instructions)

	if err := config.ValidateBotID(req.ID); err != nil {
		return nil, err
	}

	h.lanesMu.Lock()

	if _, exists := h.lanes[req.ID]; exists {
		h.lanesMu.Unlock()
		return nil, fmt.Errorf("bot %q already exists", req.ID)
	}

	bot := config.BotConfig{
		ID:              req.ID,
		Name:            req.Name,
		Description:     req.Description,
		Avatar:          req.Avatar,
		Color:           req.Color,
		Model:           req.Model,
		Harness:         req.Harness,
		PrivateSessions: req.PrivateSessions,
		DesktopTarget:   req.DesktopTarget,
		BrowserMode:     req.BrowserMode,
	}
	config.NormalizeBot(&bot, h.cfg)
	if err := config.EnsureWorkspace(&bot, h.cfg, req.Instructions); err != nil {
		h.lanesMu.Unlock()
		return nil, fmt.Errorf("init workspace for %q: %w", req.ID, err)
	}

	h.cfg.Bots = append(h.cfg.Bots, bot)
	if h.cfg.ProjectRoot != "" {
		if _, err := config.SaveLocal(h.cfg); err != nil {
			// Roll back slice entry if persisting fails
			h.cfg.Bots = h.cfg.Bots[:len(h.cfg.Bots)-1]
			h.lanesMu.Unlock()
			return nil, fmt.Errorf("save config: %w", err)
		}
	}

	h.startLaneLocked(bot)
	st := h.statusOfLocked(h.lanes[bot.ID])
	h.lanesMu.Unlock()

	h.broadcast(Event{
		Type:    "bot_created",
		BotID:   bot.ID,
		Payload: st,
	})
	h.notifySchedulesChanged()
	return &st, nil
}

// SetOnSchedulesChanged registers a callback invoked whenever bots or routines change.
func (h *Hub) SetOnSchedulesChanged(fn func()) {
	h.schedMu.Lock()
	h.onSchedulesChanged = fn
	h.schedMu.Unlock()
}

func (h *Hub) notifySchedulesChanged() {
	h.schedMu.RLock()
	fn := h.onSchedulesChanged
	h.schedMu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (h *Hub) ResetSession(botID string) (state.SessionRecord, bool, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return state.SessionRecord{}, false, fmt.Errorf("bot %q not found", botID)
	}
	lane.sessionGen.Add(1)
	lane.rotateMu.Lock()
	lane.pendingRotate = false
	lane.pendingHandoff = ""
	lane.rotateMu.Unlock()

	lane.cancelMu.Lock()
	cancel := lane.cancel
	lane.cancelMu.Unlock()
	if cancel != nil {
		cancel()
		h.desktop.CancelTakeoversByBot(botID)
	}
	if h.cancelIsolatedByBot(botID) > 0 {
		h.desktop.CancelTakeoversByBot(botID)
	}

	prev, existed, err := h.store.ArchiveActive(botID, "reset", "", "")
	if err == nil {
		_ = h.store.DismissPendingReceipts(botID)
	}
	if err == nil && existed {
		h.appendMessage(state.ChatMessage{
			BotID:     botID,
			SessionID: prev.ID,
			Role:      "system",
			From:      "system",
			Text:      fmt.Sprintf("Session 已重置（旧 Session: %s），Workspace 和 memory/ 已保留。", prev.SessionID),
		})
		h.emitStatus(lane)
	}
	return prev, existed, err
}

// RotateSession archives the bot's active session immediately or after the in-flight turn completes.
func (h *Hub) RotateSession(botID, handoffNote string, deferUntilTurnEnd bool) (*RotateResult, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	note := strings.TrimSpace(handoffNote)
	if deferUntilTurnEnd && lane.busy.Load() {
		lane.rotateMu.Lock()
		lane.pendingRotate = true
		lane.pendingHandoff = note
		lane.rotateMu.Unlock()
		rec, _ := h.store.Get(botID)
		return &RotateResult{Deferred: true, SessionID: rec.ID}, nil
	}

	lane.sessionGen.Add(1)
	lane.rotateMu.Lock()
	lane.pendingRotate = false
	lane.pendingHandoff = ""
	lane.rotateMu.Unlock()

	prev, existed, err := h.store.ArchiveActive(botID, "reset", "", note)
	if err != nil {
		return nil, err
	}
	if h.store != nil {
		_ = h.store.DismissPendingReceipts(botID)
	}
	if existed {
		h.appendMessage(state.ChatMessage{
			BotID:     botID,
			SessionID: prev.ID,
			Role:      "system",
			From:      "system",
			Text:      fmt.Sprintf("Session 已归档（%s），下一轮将开启新会话。", prev.ID),
		})
		h.emitStatus(lane)
	}
	return &RotateResult{Rotated: existed, SessionID: prev.ID}, nil
}

func (h *Hub) HandleInput(ctx context.Context, botID, rawInput string, opts SendOptions) (*harness.TurnResult, error) {
	input := strings.TrimSpace(rawInput)
	if input == "" && len(opts.MediaIDs) == 0 {
		rec, _ := h.store.Get(botID)
		return &harness.TurnResult{BotID: botID, SessionID: rec.SessionID}, nil
	}

	switch input {
	case "/status":
		sts, err := h.Status(botID)
		if err != nil {
			return nil, err
		}
		st := sts[0]
		sessID := "(not started yet)"
		turns := 0
		cost := 0.0
		if st.Session != nil {
			sessID = st.Session.SessionID
			turns = st.Session.TurnCount
			cost = st.Session.TotalCostUSD
		}
		stateStr := "idle"
		if st.Busy {
			stateStr = "busy"
		}
		modelStr := st.Model
		if modelStr == "" {
			modelStr = "(opencode default)"
		}
		text := fmt.Sprintf(
			"Bot: %s (%s)\nWorkspace: %s\nModel: %s\nSession: %s\nTurns: %d | Cost: $%.4f\nQueue: %s (pending: %d)",
			st.Name, st.ID, st.Workspace, modelStr, sessID, turns, cost, stateStr, st.Queued,
		)
		return &harness.TurnResult{BotID: botID, SessionID: sessID, Text: text}, nil

	case "/reset":
		prev, existed, err := h.ResetSession(botID)
		if err != nil {
			return nil, err
		}
		text := "No active session to reset."
		if existed {
			text = fmt.Sprintf("Session reset (previous: %s). Workspace & memory/ are preserved.", prev.SessionID)
		}
		return &harness.TurnResult{BotID: botID, Text: text}, nil

	case "/bots":
		sts, _ := h.Status("")
		var lines []string
		for _, st := range sts {
			tag := "new"
			if st.Session != nil {
				tag = fmt.Sprintf("turns=%d", st.Session.TurnCount)
			}
			desc := st.Description
			if desc == "" {
				desc = "no description"
			}
			lines = append(lines, fmt.Sprintf("- %s (%s): %s [%s]", st.ID, st.Name, desc, tag))
		}
		rec, _ := h.store.Get(botID)
		return &harness.TurnResult{
			BotID:     botID,
			SessionID: rec.SessionID,
			Text:      strings.Join(lines, "\n"),
		}, nil
	}

	if m := askCmdRe.FindStringSubmatch(input); len(m) == 3 {
		targetID := m[1]
		forwarded := m[2]
		res, err := h.MessageBot(ctx, botID, targetID, forwarded, opts.Hop)
		if err != nil {
			return nil, err
		}
		res.Text = fmt.Sprintf("[%s]\n%s", targetID, res.Text)
		return res, nil
	}

	if m := mentionBotRe.FindStringSubmatch(input); len(m) == 3 {
		targetID := m[1]
		forwarded := m[2]
		if targetID != botID {
			if _, exists := h.GetBot(targetID); exists {
				res, err := h.MessageBot(ctx, botID, targetID, forwarded, opts.Hop)
				if err != nil {
					return nil, err
				}
				res.Text = fmt.Sprintf("[%s]\n%s", targetID, res.Text)
				return res, nil
			}
		}
	}

	return h.Send(ctx, botID, input, opts)
}

// MessageBot sends a message from one bot to another with loop-protection hop limits
// and logs the exchange in both bots' chat timelines so users can observe collaboration.
func (h *Hub) MessageBot(ctx context.Context, fromBotID, toBotID, message string, hop int) (*harness.TurnResult, error) {
	if fromBotID == toBotID {
		return nil, fmt.Errorf("bot %q cannot message itself", fromBotID)
	}
	h.hopsMu.Lock()
	if active, ok := h.hops[fromBotID]; ok && active > hop {
		hop = active
	}
	h.hopsMu.Unlock()
	if hop >= MaxBotHopDepth {
		return nil, fmt.Errorf("bot-to-bot hop limit reached (%d); refusing to avoid runaway loop", MaxBotHopDepth)
	}
	if _, ok := h.GetBot(toBotID); !ok {
		return nil, fmt.Errorf("target bot %q not found", toBotID)
	}

	// Record outgoing collaboration message on sender's timeline.
	var fromSessID string
	var fromTurn int
	if fromBotID != "" {
		if _, ok := h.GetBot(fromBotID); ok {
			if h.store != nil {
				if rec, ok := h.store.Get(fromBotID); ok {
					fromSessID = rec.ID
					fromTurn = rec.TurnCount + 1
				}
			}
			h.appendMessage(state.ChatMessage{
				BotID:     fromBotID,
				SessionID: fromSessID,
				Turn:      fromTurn,
				Role:      "bot_out",
				From:      "bot:" + fromBotID,
				To:        "bot:" + toBotID,
				Text:      message,
			})
		}
	}

	wrappedPrompt := fmt.Sprintf("[Message from bot:%s]\n%s", fromBotID, message)
	res, err := h.Send(ctx, toBotID, wrappedPrompt, SendOptions{
		Source:    "bot:" + fromBotID,
		FromBotID: fromBotID,
		Hop:       hop + 1,
	})
	if err != nil {
		return nil, err
	}

	// Record reply back on sender's timeline as well so sender's chat shows the full round-trip.
	if fromBotID != "" {
		if _, ok := h.GetBot(fromBotID); ok {
			h.appendMessage(state.ChatMessage{
				BotID:      fromBotID,
				SessionID:  fromSessID,
				Turn:       fromTurn,
				Role:       "bot_in",
				From:       "bot:" + toBotID,
				To:         "bot:" + fromBotID,
				Text:       res.Text,
				DurationMs: res.DurationMs,
				CostUSD:    res.Usage.CostUSD,
			})
		}
	}

	return res, nil
}

// Send enqueues a turn on the bot's single-lane serialized goroutine.
// Different bots execute concurrently without blocking each other.
func (h *Hub) Send(ctx context.Context, botID, prompt string, opts SendOptions) (*harness.TurnResult, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	laneBot := config.BotConfig{}
	if ok {
		laneBot = lane.bot
	}
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found", botID)
	}

	role := "user"
	from := opts.Source
	to := ""
	if from == "" {
		from = "user"
	}
	if opts.FromBotID != "" {
		role = "bot_in"
		from = "bot:" + opts.FromBotID
		to = "bot:" + botID
	}

	var queuedSessID string
	var queuedTurn int
	if h.store != nil {
		if sess, err := h.store.EnsureActiveSession(botID, laneBot.Harness, laneBot.Model); err == nil {
			queuedSessID = sess.ID
			queuedTurn = sess.TurnCount + 1
		}
	}
	var media []state.MessageMedia
	var atts []harness.Attachment
	if len(opts.MediaIDs) > 0 {
		h.inboundAttsMu.Lock()
		for _, id := range opts.MediaIDs {
			if entry, ok := h.inboundAtts[id]; ok {
				media = append(media, entry.media)
				atts = append(atts, entry.att)
			}
		}
		h.inboundAttsMu.Unlock()
	}

	userMsg := h.appendMessage(state.ChatMessage{
		BotID:     botID,
		SessionID: queuedSessID,
		Turn:      queuedTurn,
		Role:      role,
		From:      from,
		To:        to,
		Text:      prompt,
		Media:     media,
	})

	job := turnJob{
		ctx:             ctx,
		prompt:          prompt,
		attachments:     atts,
		opts:            opts,
		userMsgID:       userMsg.ID,
		queuedSessionID: queuedSessID,
		respCh:          make(chan turnResponse, 1),
	}

	lane.queued.Add(1)
	h.emitStatus(lane)
	select {
	case lane.ch <- job:
	case <-ctx.Done():
		lane.queued.Add(-1)
		h.emitStatus(lane)
		return nil, ctx.Err()
	case <-lane.stopCh:
		lane.queued.Add(-1)
		return nil, fmt.Errorf("hub is shutting down")
	}

	select {
	case resp := <-job.respCh:
		return resp.res, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *Hub) emitStatus(lane *botLane) {
	h.lanesMu.RLock()
	st := h.statusOfLocked(lane)
	h.lanesMu.RUnlock()
	h.broadcast(Event{
		Type:    "bot_status",
		BotID:   lane.bot.ID,
		Payload: st,
	})
}

func (h *Hub) runLane(lane *botLane) {
	defer close(lane.stopped)
	for {
		select {
		case <-lane.stopCh:
			return
		case job := <-lane.ch:
			lane.queued.Add(-1)
			lane.busy.Store(true)
			h.emitStatus(lane)
			ctx, cancel := context.WithCancel(job.ctx)
			lane.cancelMu.Lock()
			lane.cancel = cancel
			lane.cancelMu.Unlock()
			h.lanesMu.RLock()
			bot := lane.bot
			h.lanesMu.RUnlock()
			res, err := h.executeTurn(ctx, lane, bot, job.prompt, job.attachments, job.opts, job.userMsgID, job.queuedSessionID)
			lane.cancelMu.Lock()
			lane.cancel = nil
			lane.cancelMu.Unlock()
			cancel()
			lane.busy.Store(false)
			h.flushPendingReload(bot.Harness)
			h.emitStatus(lane)
			job.respCh <- turnResponse{res: res, err: err}
		}
	}
}

func (h *Hub) platformInstructionsPath() string {
	h.lanesMu.RLock()
	sharedDir := h.cfg.SharedDir
	h.lanesMu.RUnlock()
	if sharedDir == "" {
		return ""
	}
	return filepath.Join(sharedDir, config.PlatformInstructionsFilename)
}

func (h *Hub) buildTurnPrompt(bot config.BotConfig, prompt string, isFirstTurn bool, platPath string, pendingReceipts []state.RoutineRun) string {
	// Slash commands must stay at the start of the prompt so harnesses recognize them.
	if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
		return prompt
	}
	var prefixParts []string
	if isFirstTurn {
		if h.store != nil {
			if prev, ok := h.store.LatestArchivedSession(bot.ID); ok {
				details := []string{fmt.Sprintf("%d turns", prev.TurnCount), "status=" + prev.Status}
				if prev.LastError != "" {
					details = append(details, fmt.Sprintf("lastError=%q", firstLineSummary(prev.LastError, 120)))
				}
				if prev.HandoffNote != "" {
					details = append(details, fmt.Sprintf("handoff=%q", firstLineSummary(prev.HandoffNote, 120)))
				}
				prefixParts = append(prefixParts, fmt.Sprintf(
					"[System Note: New session started. Previous session %s (%s) is archived. Use recall_session if you need prior context.]",
					prev.ID, strings.Join(details, ", "),
				))
			} else if recent, err := h.store.RecentMessagesForHandoff(bot.ID, 8); err == nil && len(recent) > 0 {
				prefixParts = append(prefixParts, formatCrossHostHandoffPrefix(recent))
			}
		}
		if bot.Harness == "dsh" && platPath != "" {
			if raw, err := os.ReadFile(platPath); err == nil && len(raw) > 0 {
				prefixParts = append(prefixParts, strings.TrimSpace(string(raw)))
			}
		}
	}
	if len(pendingReceipts) > 0 {
		prefixParts = append(prefixParts, formatRoutineReceiptsPrefix(pendingReceipts))
	}
	if len(prefixParts) == 0 {
		return prompt
	}
	return strings.Join(prefixParts, "\n\n") + "\n\n" + prompt
}

func formatCrossHostHandoffPrefix(recent []state.SyncMessageRow) string {
	var lines []string
	lines = append(lines, "[System Note: Cross-host session handoff — recent conversation synced from another host:]")
	for _, m := range recent {
		lines = append(lines, fmt.Sprintf("- %s: %s", m.Role, firstLineSummary(m.Text, 240)))
	}
	return strings.Join(lines, "\n")
}

func formatRoutineReceiptsPrefix(receipts []state.RoutineRun) string {
	var lines []string
	lines = append(lines, "[后台定时任务回执 — 以下为此前后台巡检结果，仅供参考，无需重复汇报]")
	for _, r := range receipts {
		name := r.RoutineName
		if name == "" {
			name = r.RoutineID
		}
		ts := r.FinishedAt
		if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			ts = parsed.In(time.Local).Format("01-02 15:04")
		} else if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			ts = parsed.In(time.Local).Format("01-02 15:04")
		}
		excerpt := truncateRunes(strings.TrimSpace(r.Output), 1500)
		lines = append(lines, fmt.Sprintf("- [%s @ %s]: %s", name, ts, excerpt))
	}
	return strings.Join(lines, "\n")
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func formatToolCounts(order []string, counts map[string]int) []string {
	if len(order) == 0 {
		return nil
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		if n := counts[name]; n > 1 {
			out = append(out, fmt.Sprintf("%s×%d", name, n))
		} else {
			out = append(out, name)
		}
	}
	return out
}

func isFatalSessionError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "timed out") || strings.Contains(s, "interrupted") {
		return false
	}
	patterns := []string{
		"context_length_exceeded",
		"context length exceeded",
		"maximum context length",
		"context window",
		"prompt is too long",
		"input is too long",
		"too many tokens",
		"session not found",
		"no conversation found",
		"invalid session",
		"failed to resume",
		"unable to resume",
	}
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	if strings.Contains(s, "tool_use") && strings.Contains(s, "tool_result") {
		return true
	}
	return false
}

func firstLineSummary(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	r := []rune(s)
	if len(r) <= max || max <= 0 {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func (h *Hub) executeTurn(ctx context.Context, lane *botLane, bot config.BotConfig, prompt string, attachments []harness.Attachment, opts SendOptions, userMsgID, queuedSessionID string) (*harness.TurnResult, error) {
	runner, err := h.registry.Get(bot.Harness)
	if err != nil {
		return nil, err
	}

	h.lanesMu.RLock()
	apiToken := h.cfg.HTTP.APIToken
	hubURL := h.cfg.HTTP.BaseURL()
	cfgPath := h.cfg.ConfigPath
	mcpCmd := config.MCPCommand(h.cfg, bot.ID)
	h.lanesMu.RUnlock()

	platPath := h.platformInstructionsPath()
	extraEnv := map[string]string{
		"BOT_HUB_URL":    hubURL,
		"BOT_HUB_BOT_ID": bot.ID,
		"BOT_HUB_HOP":    strconv.Itoa(opts.Hop),
	}
	if cfgPath != "" {
		extraEnv["BOT_HUB_CONFIG"] = cfgPath
	}
	if apiToken != "" {
		extraEnv["BOT_HUB_API_TOKEN"] = apiToken
	}
	for k, v := range h.botBrowserEnv(bot) {
		if k != "" && v != "" {
			extraEnv[k] = v
		}
	}

	h.hopsMu.Lock()
	h.hops[bot.ID] = opts.Hop
	h.hopsMu.Unlock()
	defer func() {
		h.hopsMu.Lock()
		delete(h.hops, bot.ID)
		h.hopsMu.Unlock()
	}()

	var mcpEnv map[string]string
	if apiToken != "" {
		mcpEnv = map[string]string{"BOT_HUB_API_TOKEN": apiToken}
	}

	for attempt := 0; attempt < 2; attempt++ {
		gen := lane.sessionGen.Load()
		var sess state.SessionRecord
		if h.store != nil {
			var sErr error
			sess, sErr = h.store.EnsureActiveSession(bot.ID, bot.Harness, bot.Model)
			if sErr != nil {
				return nil, sErr
			}
			if userMsgID != "" && sess.ID != queuedSessionID {
				_ = h.store.ReassignMessageSession(userMsgID, sess.ID, sess.TurnCount+1)
				queuedSessionID = sess.ID
			}
		}
		turnNum := sess.TurnCount + 1
		var pendingReceipts []state.RoutineRun
		if h.store != nil {
			pendingReceipts, _ = h.store.ListPendingReceipts(bot.ID, 5)
		}
		effectivePrompt := h.buildTurnPrompt(bot, prompt, sess.TurnCount == 0, platPath, pendingReceipts)

		var toolMu sync.Mutex
		var toolOrder []string
		toolCounts := make(map[string]int)

		var onQuestion func(context.Context, harness.QuestionRequest) (harness.QuestionResponse, error)
		if isInteractiveQuestionSource(opts) {
			onQuestion = func(qCtx context.Context, req harness.QuestionRequest) (harness.QuestionResponse, error) {
				return h.AskQuestion(qCtx, bot, req)
			}
		}

		lane.turnSentCount.Store(0)
		lane.turnMu.Lock()
		lane.activeSessionID = sess.ID
		lane.activeTurn = turnNum
		lane.turnLastMsgID = ""
		lane.turnMu.Unlock()

		res, err := runner.RunTurn(ctx, harness.RunOptions{
			Bot:                      bot,
			Prompt:                   effectivePrompt,
			Attachments:              attachments,
			SessionID:                sess.RuntimeSessionID,
			PlatformInstructionsPath: platPath,
			MCPCommand:               mcpCmd,
			MCPEnv:                   mcpEnv,
			ExtraEnv:                 extraEnv,
			OnText:                   opts.OnText,
			OnQuestion:               onQuestion,
			OnToolUse: func(toolName string) {
				if toolName != "" {
					toolMu.Lock()
					if toolCounts[toolName] == 0 {
						toolOrder = append(toolOrder, toolName)
					}
					toolCounts[toolName]++
					toolMu.Unlock()
				}
				h.broadcast(Event{
					Type:  "tool_use",
					BotID: bot.ID,
					Payload: map[string]any{
						"botId": bot.ID,
						"tool":  toolName,
					},
				})
				if opts.OnToolUse != nil {
					opts.OnToolUse(toolName)
				}
			},
		})

		lane.turnMu.Lock()
		lane.activeSessionID = ""
		lane.activeTurn = 0
		lane.turnMu.Unlock()

		toolMu.Lock()
		tools := formatToolCounts(toolOrder, toolCounts)
		toolMu.Unlock()

		if err != nil {
			resetMidFlight := gen != lane.sessionGen.Load()
			if resetMidFlight {
				if res != nil && res.SessionID != "" && h.store != nil {
					_, _ = h.store.RecordTurnDetail(sess.ID, state.TurnRecordUpdate{
						RuntimeSessionID: res.SessionID,
						Harness:          bot.Harness,
						Model:            bot.Model,
						PromptTitle:      prompt,
					})
				}
				if errors.Is(ctx.Err(), context.Canceled) {
					h.appendMessage(state.ChatMessage{
						BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
						Role: "system", From: "system", Text: "已停止",
					})
				}
				return nil, err
			}

			if errors.Is(ctx.Err(), context.Canceled) {
				if res != nil && res.SessionID != "" && res.SessionID != sess.RuntimeSessionID && h.store != nil {
					_, _ = h.store.RecordTurnDetail(sess.ID, state.TurnRecordUpdate{
						RuntimeSessionID: res.SessionID,
						Harness:          bot.Harness,
						Model:            bot.Model,
						PromptTitle:      prompt,
					})
				}
				if res != nil && strings.TrimSpace(res.Text) != "" {
					h.appendInternalMessage(state.ChatMessage{
						BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
						From: "bot:" + bot.ID, Text: res.Text,
					})
				}
				h.appendMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
					Role: "system", From: "system", Text: "已停止",
				})
				return nil, err
			}

			// D2-A: Fatal session corruption/overflow on an existing session -> archive & retry once cleanly.
			if attempt == 0 && h.store != nil && sess.RuntimeSessionID != "" && isFatalSessionError(err) {
				if res != nil && strings.TrimSpace(res.Text) != "" {
					h.appendInternalMessage(state.ChatMessage{
						BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
						From: "bot:" + bot.ID, Text: res.Text,
					})
				}
				h.appendMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
					Role: "system", From: "error",
					Text: fmt.Sprintf("执行失败 (%s): %v", firstLineSummary(prompt, 80), err),
				})
				_, _, _ = h.store.ArchiveSessionByID(sess.ID, "error", err.Error(), "")
				h.appendMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum,
					Role: "system", From: "system",
					Text: fmt.Sprintf("检测到会话异常，已归档旧会话 (%s) 并开启新会话重试。", sess.ID),
				})
				h.emitStatus(lane)
				continue
			}

			if res != nil && res.SessionID != "" && res.SessionID != sess.RuntimeSessionID && h.store != nil {
				_, _ = h.store.RecordTurnDetail(sess.ID, state.TurnRecordUpdate{
					RuntimeSessionID: res.SessionID,
					Harness:          bot.Harness,
					Model:            bot.Model,
					PromptTitle:      prompt,
				})
			}
			if res != nil && strings.TrimSpace(res.Text) != "" {
				h.appendInternalMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
					From: "bot:" + bot.ID, Text: res.Text,
				})
			}
			h.appendMessage(state.ChatMessage{
				BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
				Role: "system", From: "error", Text: fmt.Sprintf("执行失败: %v", err),
			})
			return nil, err
		}

		if h.store != nil && sess.ID != "" {
			if _, saveErr := h.store.RecordTurnDetail(sess.ID, state.TurnRecordUpdate{
				RuntimeSessionID: res.SessionID,
				Harness:          bot.Harness,
				Model:            bot.Model,
				PromptTitle:      prompt,
				CostUSD:          res.Usage.CostUSD,
				InputTokens:      res.Usage.InputTokens,
				IncrementTurn:    true,
			}); saveErr != nil {
				return res, fmt.Errorf("turn succeeded but failed to persist session: %w", saveErr)
			}
		}
		if h.store != nil && len(pendingReceipts) > 0 {
			receiptIDs := make([]string, 0, len(pendingReceipts))
			for _, pr := range pendingReceipts {
				receiptIDs = append(receiptIDs, pr.ID)
			}
			_ = h.store.MarkReceiptsInjected(receiptIDs)
		}

		res.Tools = tools
		if opts.FromBotID != "" {
			if strings.TrimSpace(res.Text) != "" {
				h.appendMessage(state.ChatMessage{
					BotID:        bot.ID,
					SessionID:    sess.ID,
					Turn:         turnNum,
					Role:         "bot_out",
					From:         "bot:" + bot.ID,
					To:           "bot:" + opts.FromBotID,
					Text:         res.Text,
					Tools:        tools,
					DurationMs:   res.DurationMs,
					CostUSD:      res.Usage.CostUSD,
					InputTokens:  res.Usage.InputTokens,
					OutputTokens: res.Usage.OutputTokens,
				})
			}
		} else {
			if strings.TrimSpace(res.Text) != "" {
				h.appendInternalMessage(state.ChatMessage{
					BotID:        bot.ID,
					SessionID:    sess.ID,
					Turn:         turnNum,
					From:         "bot:" + bot.ID,
					Text:         res.Text,
					Tools:        tools,
					DurationMs:   res.DurationMs,
					CostUSD:      res.Usage.CostUSD,
					InputTokens:  res.Usage.InputTokens,
					OutputTokens: res.Usage.OutputTokens,
				})
			}
			if lane.turnSentCount.Load() > 0 {
				lane.turnMu.Lock()
				lastMsgID := lane.turnLastMsgID
				lane.turnMu.Unlock()
				h.updateMessageMeta(lastMsgID, tools, res.DurationMs, res.Usage.CostUSD, res.Usage.InputTokens, res.Usage.OutputTokens)
			} else if strings.TrimSpace(res.Text) != "" && isUserDirectSource(opts) {
				h.appendMessage(state.ChatMessage{
					BotID:        bot.ID,
					SessionID:    sess.ID,
					Turn:         turnNum,
					Role:         "assistant",
					From:         "bot:" + bot.ID,
					Text:         res.Text,
					Tools:        tools,
					DurationMs:   res.DurationMs,
					CostUSD:      res.Usage.CostUSD,
					InputTokens:  res.Usage.InputTokens,
					OutputTokens: res.Usage.OutputTokens,
				})
			}
		}

		lane.rotateMu.Lock()
		doRotate := lane.pendingRotate
		handoff := lane.pendingHandoff
		lane.pendingRotate = false
		lane.pendingHandoff = ""
		lane.rotateMu.Unlock()

		if doRotate && h.store != nil && gen == lane.sessionGen.Load() {
			if _, archived, _ := h.store.ArchiveSessionByID(sess.ID, "reset", "", handoff); archived {
				_ = h.store.DismissPendingReceipts(bot.ID)
				h.appendMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum,
					Role: "system", From: "system",
					Text: fmt.Sprintf("Session 已归档（%s），下一轮将开启新会话。", sess.ID),
				})
			}
		}
		return res, nil
	}
	return nil, fmt.Errorf("bot %q exhausted retries", bot.ID)
}

// Interrupt cancels the bot's running turn and any active isolated tasks, if any.
func (h *Hub) Interrupt(botID string) (bool, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return false, fmt.Errorf("bot %q not found", botID)
	}
	lane.cancelMu.Lock()
	cancel := lane.cancel
	lane.cancelMu.Unlock()
	interrupted := false
	if cancel != nil {
		cancel()
		interrupted = true
	}
	if h.cancelIsolatedByBot(botID) > 0 {
		interrupted = true
	}
	if interrupted {
		h.desktop.CancelTakeoversByBot(botID)
	}
	return interrupted, nil
}

func (h *Hub) reloadHarness(kind string) {
	r, err := h.registry.Get(kind)
	if err != nil {
		return
	}
	rl, ok := r.(harness.Reloader)
	if !ok {
		return
	}
	if h.isHarnessBusy(kind) {
		h.reloadMu.Lock()
		h.pendingReload[kind] = true
		h.reloadMu.Unlock()
		return
	}
	h.reloadMu.Lock()
	delete(h.pendingReload, kind)
	h.reloadMu.Unlock()
	rl.Reload()
}

func (h *Hub) flushPendingReload(kind string) {
	h.reloadMu.Lock()
	need := h.pendingReload[kind]
	h.reloadMu.Unlock()
	if need {
		h.reloadHarness(kind)
	}
}

func (h *Hub) isHarnessBusy(kind string) bool {
	h.activeEphemeralMu.Lock()
	ephBusy := h.activeEphemeral[kind] > 0
	h.activeEphemeralMu.Unlock()
	if ephBusy {
		return true
	}
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	for _, lane := range h.lanes {
		if lane.bot.Harness == kind && lane.busy.Load() {
			return true
		}
	}
	return false
}

// HarnessKinds returns the registered harness IDs without running external CLI checks.
func (h *Hub) HarnessKinds() []string {
	var kinds []string
	for _, r := range h.registry.All() {
		kinds = append(kinds, r.Kind())
	}
	return kinds
}

// Harnesses lists the registered harnesses with the model their bots
// inherit and how magpie sees them.
func (h *Hub) Harnesses(ctx context.Context) []HarnessInfo {
	h.lanesMu.RLock()
	var out []HarnessInfo
	for _, r := range h.registry.All() {
		installed, binPath := harness.DetectHarness(r.Kind())
		meta := harness.HarnessInstallMeta(r.Kind())
		authStatus := "unknown"
		if installed && !meta.RequiresLogin {
			authStatus = "ready"
		}
		out = append(out, HarnessInfo{
			ID:             r.Kind(),
			Label:          r.Label(),
			DefaultModel:   h.cfg.DefaultModelFor(r.Kind()),
			Installed:      installed,
			BinaryPath:     binPath,
			AuthStatus:     authStatus,
			RequiresLogin:  meta.RequiresLogin,
			CanAutoInstall: meta.CanAutoInstall,
			Category:       meta.Category,
			Summary:        meta.Summary,
			Install:        meta,
		})
	}
	h.lanesMu.RUnlock()
	magpieOK := harness.MagpieInstalled()
	var wg sync.WaitGroup
	for i := range out {
		if !out[i].Installed {
			out[i].Magpie = harness.MagpieStatus{Installed: magpieOK}
			continue
		}
		wg.Add(1)
		go func(hi *HarnessInfo) {
			defer wg.Done()
			if magpieOK {
				hi.Magpie = harness.GetMagpieStatus(ctx, hi.ID)
			}
			hi.AuthStatus, hi.AuthDetail = harness.DetectHarnessAuthCached(ctx, hi.ID)
		}(&out[i])
	}
	wg.Wait()
	return out
}

// Models lists the models a harness offers: magpie's catalog (when installed)
// merged with the harness's own models.
func (h *Hub) Models(ctx context.Context, kind string) ([]string, error) {
	return h.ModelsWithSource(ctx, kind, "")
}

// ModelsWithSource returns the available models for kind.
// When source == "magpie", it returns Magpie gateway models for wiring that harness.
// Otherwise it returns Magpie models only when kind is connected to Magpie, and
// native CLI models when kind is running in native mode.
func (h *Hub) ModelsWithSource(ctx context.Context, kind, source string) ([]string, error) {
	r, err := h.registry.Get(kind)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(source), "magpie") {
		if !harness.MagpieInstalled() {
			return []string{}, nil
		}
		return harness.MagpieModels(ctx, kind), nil
	}
	if harness.MagpieInstalled() && harness.GetMagpieStatus(ctx, kind).Connected {
		if models := harness.MagpieModels(ctx, kind); len(models) > 0 {
			return models, nil
		}
	}
	return r.Models(ctx)
}

// MagpieConnect asks magpie to point a harness's CLI at its gateway, using
// the given model or the harness's default, and reloads the harness when idle.
func (h *Hub) MagpieConnect(ctx context.Context, kind, model string) error {
	if _, err := h.registry.Get(kind); err != nil {
		return err
	}
	h.lanesMu.RLock()
	if strings.TrimSpace(model) == "" {
		model = h.cfg.DefaultModelFor(kind)
	}
	h.lanesMu.RUnlock()
	if err := harness.MagpieConnect(ctx, kind, model); err != nil {
		return err
	}
	h.reloadHarness(kind)
	return nil
}

// MagpieDisconnect asks magpie to restore a harness's CLI to its native config
// (`magpie <agent> default`), and reloads the harness when idle.
func (h *Hub) MagpieDisconnect(ctx context.Context, kind string) error {
	if _, err := h.registry.Get(kind); err != nil {
		return err
	}
	if err := harness.MagpieDisconnect(ctx, kind); err != nil {
		return err
	}
	h.reloadHarness(kind)
	return nil
}

// MagpieProviderAction invokes `magpie provider ...` and schedules harness reloads
// when provider definitions change.
func (h *Hub) MagpieProviderAction(ctx context.Context, req harness.MagpieProviderReq) (string, error) {
	out, err := harness.MagpieProviderAction(ctx, req)
	if err != nil {
		return "", err
	}
	if req.Action != "test" {
		for _, r := range h.registry.All() {
			h.reloadHarness(r.Kind())
		}
	}
	return out, nil
}

// UpdateBot changes a bot's name, description, harness, model or instructions
// (AGENTS.md), persists it, and applies it from the bot's next turn. Switching
// harness starts a new session, since sessions do not carry across harnesses.
func (h *Hub) UpdateBot(botID string, upd BotUpdate) (*BotStatus, error) {
	if upd.Harness != nil {
		*upd.Harness = strings.TrimSpace(*upd.Harness)
		if _, err := h.registry.Get(*upd.Harness); err != nil {
			return nil, err
		}
	}
	if upd.Name != nil && strings.TrimSpace(*upd.Name) == "" {
		return nil, errors.New("名称不能为空")
	}
	if upd.Avatar != nil {
		avatar, err := config.NormalizeBotAvatar(*upd.Avatar)
		if err != nil {
			return nil, err
		}
		*upd.Avatar = avatar
	}
	if upd.Color != nil {
		color, err := config.NormalizeBotColor(*upd.Color)
		if err != nil {
			return nil, err
		}
		*upd.Color = color
	}
	if upd.DesktopTarget != nil {
		dt, err := config.NormalizeDesktopTarget(*upd.DesktopTarget)
		if err != nil {
			return nil, err
		}
		*upd.DesktopTarget = dt
	}
	if upd.BrowserMode != nil {
		bm, err := config.NormalizeBrowserMode(*upd.BrowserMode)
		if err != nil {
			return nil, err
		}
		*upd.BrowserMode = bm
	}
	h.lanesMu.Lock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.Unlock()
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	bot := lane.bot
	harnessChanged := upd.Harness != nil && *upd.Harness != bot.Harness
	if upd.Name != nil {
		bot.Name = strings.TrimSpace(*upd.Name)
	}
	if upd.Description != nil {
		bot.Description = strings.TrimSpace(*upd.Description)
	}
	if upd.Avatar != nil {
		bot.Avatar = *upd.Avatar
	}
	if upd.Color != nil {
		bot.Color = *upd.Color
	}
	if upd.Harness != nil {
		bot.Harness = *upd.Harness
	}
	if upd.Model != nil {
		bot.Model = strings.TrimSpace(*upd.Model)
		bot.ModelInherited = bot.Model == ""
	}
	if upd.PrivateSessions != nil {
		bot.PrivateSessions = *upd.PrivateSessions
	}
	if upd.DesktopTarget != nil {
		bot.DesktopTarget = *upd.DesktopTarget
	}
	if upd.BrowserMode != nil {
		bot.BrowserMode = *upd.BrowserMode
	}
	if bot.ModelInherited {
		bot.Model = h.cfg.DefaultModelFor(bot.Harness)
	}

	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			for i := range raw.Bots {
				rb := &raw.Bots[i]
				if rb.ID != botID {
					continue
				}
				rb.Name, rb.Description, rb.Harness = bot.Name, bot.Description, bot.Harness
				rb.Avatar, rb.Color = bot.Avatar, bot.Color
				rb.PrivateSessions = bot.PrivateSessions
				rb.DesktopTarget = bot.DesktopTarget
				rb.BrowserMode = bot.BrowserMode
				if upd.Model != nil || harnessChanged {
					rb.Model = ""
					if !bot.ModelInherited {
						rb.Model = bot.Model
					}
				}
				return nil
			}
			return fmt.Errorf("bot %q is not in %s", botID, h.cfg.ConfigPath)
		})
		if err != nil {
			h.lanesMu.Unlock()
			return nil, fmt.Errorf("save config: %w", err)
		}
	}
	if upd.Instructions != nil {
		if err := os.MkdirAll(bot.Workspace, 0o755); err != nil {
			h.lanesMu.Unlock()
			return nil, fmt.Errorf("create workspace: %w", err)
		}
		content := strings.TrimSpace(*upd.Instructions)
		if content != "" {
			content += "\n"
		}
		if err := writeFileAtomic(filepath.Join(bot.Workspace, "AGENTS.md"), []byte(content), 0o644); err != nil {
			h.lanesMu.Unlock()
			return nil, fmt.Errorf("write AGENTS.md: %w", err)
		}
	}
	lane.bot = bot
	for i := range h.cfg.Bots {
		if h.cfg.Bots[i].ID == botID {
			h.cfg.Bots[i] = bot
		}
	}
	st := h.statusOfLocked(lane)
	h.lanesMu.Unlock()

	if harnessChanged {
		lane.sessionGen.Add(1)
		if prev, existed, _ := h.store.ArchiveActive(botID, "harness_changed", "", ""); existed {
			h.appendMessage(state.ChatMessage{
				BotID: botID, SessionID: prev.ID, Role: "system", From: "system",
				Text: fmt.Sprintf("已切换到 %s，开启新 Session（旧 Session: %s）。", bot.Harness, prev.SessionID),
			})
		}
		st, _ = h.statusOne(botID)
	}
	h.broadcast(Event{Type: "bot_status", BotID: botID, Payload: st})
	return &st, nil
}

// DeleteBot stops the bot's serial lane and removes it from config, keeping its
// workspace directory and chat history files intact on disk.
func (h *Hub) DeleteBot(botID string) error {
	h.lanesMu.Lock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.Unlock()
		return fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	if len(h.order) <= 1 {
		h.lanesMu.Unlock()
		return errors.New("至少需要保留一个 Bot")
	}
	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			filtered := make([]config.BotConfig, 0, len(raw.Bots))
			for _, rb := range raw.Bots {
				if rb.ID != botID {
					filtered = append(filtered, rb)
				}
			}
			if len(filtered) == len(raw.Bots) {
				return fmt.Errorf("bot %q is not in %s", botID, h.cfg.ConfigPath)
			}
			raw.Bots = filtered
			return nil
		})
		if err != nil {
			h.lanesMu.Unlock()
			return fmt.Errorf("save config: %w", err)
		}
	}

	delete(h.lanes, botID)
	nextOrder := make([]string, 0, len(h.order)-1)
	for _, id := range h.order {
		if id != botID {
			nextOrder = append(nextOrder, id)
		}
	}
	h.order = nextOrder
	nextBots := make([]config.BotConfig, 0, len(h.cfg.Bots))
	for _, b := range h.cfg.Bots {
		if b.ID != botID {
			nextBots = append(nextBots, b)
		}
	}
	h.cfg.Bots = nextBots
	h.lanesMu.Unlock()

	lane.cancelMu.Lock()
	cancel := lane.cancel
	lane.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	h.cancelIsolatedByBot(botID)
	if h.desktop != nil {
		h.desktop.CancelTakeoversByBot(botID)
	}
	if h.localDeskPool != nil {
		h.localDeskPool.RemoveSlot(botID)
	}
	go closeBotBrowserSession(botID)
	lane.stop()

	if h.store != nil {
		_, _, _ = h.store.Reset(botID)
	}
	h.broadcast(Event{
		Type:    "bot_deleted",
		BotID:   botID,
		Payload: map[string]any{"id": botID},
	})
	h.notifySchedulesChanged()
	return nil
}

// DefaultHarness returns the hub-wide default harness for new bots.
func (h *Hub) DefaultHarness() string {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	return h.cfg.EffectiveDefaultHarness()
}

// SetDefaultHarness updates and persists the hub-wide default harness.
func (h *Hub) SetDefaultHarness(harness string) error {
	harness = strings.TrimSpace(harness)
	if harness != "" {
		if _, err := h.registry.Get(harness); err != nil {
			return err
		}
	}
	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	if h.cfg.ConfigPath != "" {
		if err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.DefaultHarness = harness
			return nil
		}); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.DefaultHarness = harness
	return nil
}

// DefaultModelsSnapshot returns the hub-wide default model and a copy of the
// per-harness overrides safely under the hub lock.
func (h *Hub) DefaultModelsSnapshot() (string, map[string]string) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	m := make(map[string]string, len(h.cfg.DefaultModels))
	for k, v := range h.cfg.DefaultModels {
		m[k] = v
	}
	return h.cfg.DefaultModel, m
}

// SetDefaultModels sets the hub-wide default model and per-harness overrides
// (an empty override is removed), then re-resolves every inheriting bot.
func (h *Hub) SetDefaultModels(defaultModel string, perHarness map[string]string) error {
	clean := map[string]string{}
	for k, v := range perHarness {
		if v = strings.TrimSpace(v); v != "" {
			if _, err := h.registry.Get(k); err != nil {
				return err
			}
			clean[k] = v
		}
	}
	defaultModel = strings.TrimSpace(defaultModel)
	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.DefaultModel = defaultModel
			raw.DefaultModels = clean
			if len(clean) == 0 {
				raw.DefaultModels = nil
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.DefaultModel = defaultModel
	h.cfg.DefaultModels = clean
	for i := range h.cfg.Bots {
		b := &h.cfg.Bots[i]
		if b.ModelInherited {
			b.Model = h.cfg.DefaultModelFor(b.Harness)
			if lane := h.lanes[b.ID]; lane != nil {
				lane.bot.Model = b.Model
			}
		}
	}
	return nil
}

// OnboardingCompleted reports whether the first-run unbox wizard has been completed or skipped.
func (h *Hub) OnboardingCompleted() bool {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	return h.cfg.OnboardingCompleted
}

// SetOnboardingCompleted persists whether the first-run unbox wizard has been completed.
func (h *Hub) SetOnboardingCompleted(done bool) error {
	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	if h.cfg.ConfigPath != "" {
		if err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.OnboardingCompleted = done
			return nil
		}); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.OnboardingCompleted = done
	return nil
}

// HasAnyConversation reports whether any configured bot has persisted turns or messages.
func (h *Hub) HasAnyConversation() bool {
	h.lanesMu.RLock()
	ids := append([]string(nil), h.order...)
	h.lanesMu.RUnlock()
	for _, id := range ids {
		if h.store != nil {
			if rec, ok := h.store.Get(id); ok && rec.TurnCount > 0 {
				return true
			}
			if h.store.ArchivedCount(id) > 0 {
				return true
			}
			if msgs, err := h.store.ListMessages(id, 1); err == nil && len(msgs) > 0 {
				return true
			}
		} else if h.messages != nil {
			if msgs, err := h.messages.List(id, 1); err == nil && len(msgs) > 0 {
				return true
			}
		}
	}
	return false
}
// SharedUserMD reads the shared _shared/USER.md memory file across all bots.
func (h *Hub) SharedUserMD() string {
	h.lanesMu.RLock()
	sharedDir := h.cfg.SharedDir
	h.lanesMu.RUnlock()
	if sharedDir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(sharedDir, "USER.md"))
	if err != nil {
		return ""
	}
	return string(raw)
}

// SetSharedUserMD writes the shared _shared/USER.md file atomically.
func (h *Hub) SetSharedUserMD(content string) error {
	h.lanesMu.RLock()
	sharedDir := h.cfg.SharedDir
	h.lanesMu.RUnlock()
	if sharedDir == "" {
		return errors.New("sharedDir is not configured")
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return err
	}
	trimmed := strings.TrimRight(content, " \t\r\n")
	if trimmed != "" {
		trimmed += "\n"
	}
	return writeFileAtomic(filepath.Join(sharedDir, "USER.md"), []byte(trimmed), 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (h *Hub) statusOne(botID string) (BotStatus, error) {
	sts, err := h.Status(botID)
	if err != nil {
		return BotStatus{}, err
	}
	return sts[0], nil
}

func (h *Hub) privateBotIDsExcept(callerBotID string) []string {
	if callerBotID == "" {
		return nil
	}
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	var excluded []string
	for id, lane := range h.lanes {
		if id != callerBotID && lane.bot.PrivateSessions {
			excluded = append(excluded, id)
		}
	}
	return excluded
}

func (h *Hub) isPrivateFor(targetBotID, callerBotID string) bool {
	if callerBotID == "" || targetBotID == "" || targetBotID == callerBotID {
		return false
	}
	b, ok := h.GetBot(targetBotID)
	return ok && b.PrivateSessions
}

// ListSessions lists archived and active sessions visible to callerBotID.
func (h *Hub) ListSessions(callerBotID string, opts state.SessionListOptions) ([]state.SessionSummary, error) {
	if h.store == nil {
		return []state.SessionSummary{}, nil
	}
	callerBotID = strings.TrimSpace(callerBotID)
	botFilter := strings.TrimSpace(opts.BotID)
	if botFilter == "" && callerBotID != "" {
		botFilter = callerBotID
		opts.BotID = botFilter
	}
	if botFilter != "" && botFilter != "*" && h.isPrivateFor(botFilter, callerBotID) {
		return nil, fmt.Errorf("%w: bot %q sessions are private", ErrForbidden, botFilter)
	}
	opts.ExcludedBotIDs = append(opts.ExcludedBotIDs, h.privateBotIDsExcept(callerBotID)...)
	return h.store.ListSessions(opts)
}

// RecallSession fetches a sliced transcript for a historical or active session.
func (h *Hub) RecallSession(callerBotID string, opts state.RecallOptions) (*state.RecallResult, error) {
	if h.store == nil {
		return nil, state.ErrSessionNotFound
	}
	callerBotID = strings.TrimSpace(callerBotID)
	if opts.BotID == "" && callerBotID != "" && (opts.SessionID == "" || strings.EqualFold(opts.SessionID, "previous")) {
		opts.BotID = callerBotID
	}
	if opts.BotID != "" && opts.BotID != "*" && h.isPrivateFor(opts.BotID, callerBotID) {
		return nil, fmt.Errorf("%w: bot %q sessions are private", ErrForbidden, opts.BotID)
	}
	res, err := h.store.RecallSession(opts)
	if err != nil {
		return nil, err
	}
	if h.isPrivateFor(res.Session.BotID, callerBotID) {
		return nil, fmt.Errorf("%w: bot %q sessions are private", ErrForbidden, res.Session.BotID)
	}
	return res, nil
}

// MemorySearch queries Magpie Memory (global or scoped to a specific bot's workspace + tag).
func (h *Hub) MemorySearch(ctx context.Context, scope, botID, query, kind string, limit int) (harness.MagpieMemoryList, error) {
	projectDir := ""
	botID = strings.TrimSpace(botID)
	if botID != "" {
		bot, ok := h.GetBot(botID)
		if !ok {
			return harness.MagpieMemoryList{}, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
		}
		projectDir = bot.Workspace
		scope = "project"
	}
	return harness.MagpieMemorySearch(ctx, scope, projectDir, botID, query, kind, limit), nil
}

// MemorySave writes or updates a Magpie Memory entry.
func (h *Hub) MemorySave(ctx context.Context, req harness.MagpieMemorySaveReq) (string, error) {
	req.BotID = strings.TrimSpace(req.BotID)
	if req.BotID != "" {
		bot, ok := h.GetBot(req.BotID)
		if !ok {
			return "", fmt.Errorf("bot %q not found: %w", req.BotID, ErrBotNotFound)
		}
		req.Project = bot.Workspace
		req.Scope = "project"
	}
	return harness.MagpieMemorySave(ctx, req)
}

// MemoryForget forgets/deletes a Magpie Memory entry by ID.
func (h *Hub) MemoryForget(ctx context.Context, id string) (string, error) {
	return harness.MagpieMemoryForget(ctx, id)
}

// RunIsolated runs a single ephemeral turn for botID without touching or blocking
// the bot's active main session.
func (h *Hub) RunIsolated(ctx context.Context, botID, prompt string, opts IsolatedOptions) (*harness.TurnResult, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	var bot config.BotConfig
	var apiToken string
	var mcpCmd []string
	if ok {
		bot = lane.bot
		apiToken = h.cfg.HTTP.APIToken
		mcpCmd = config.MCPCommand(h.cfg, bot.ID)
	}
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}

	if hk := strings.TrimSpace(opts.Harness); hk != "" && hk != bot.Harness {
		bot.Harness = hk
		if strings.TrimSpace(opts.Model) == "" && bot.ModelInherited {
			h.lanesMu.RLock()
			bot.Model = h.cfg.DefaultModelFor(bot.Harness)
			h.lanesMu.RUnlock()
		}
	}
	if m := strings.TrimSpace(opts.Model); m != "" {
		bot.Model = m
		bot.ModelInherited = false
	}
	if opts.TimeoutSec > 0 {
		bot.TimeoutSec = opts.TimeoutSec
	}

	runner, err := h.registry.Get(bot.Harness)
	if err != nil {
		return nil, err
	}

	h.activeEphemeralMu.Lock()
	h.activeEphemeral[bot.Harness]++
	h.activeEphemeralMu.Unlock()
	defer func() {
		h.activeEphemeralMu.Lock()
		if h.activeEphemeral[bot.Harness] > 0 {
			h.activeEphemeral[bot.Harness]--
		}
		h.activeEphemeralMu.Unlock()
		h.flushPendingReload(bot.Harness)
	}()

	platPath := h.platformInstructionsPath()
	effectivePrompt := prompt
	if bot.Harness == "dsh" && platPath != "" {
		if raw, err := os.ReadFile(platPath); err == nil && len(raw) > 0 {
			effectivePrompt = strings.TrimSpace(string(raw)) + "\n\n" + prompt
		}
	}

	extraEnv := map[string]string{
		"BOT_HUB_URL":    h.cfg.HTTP.BaseURL(),
		"BOT_HUB_BOT_ID": bot.ID,
		"BOT_HUB_HOP":    "0",
	}
	if h.cfg.ConfigPath != "" {
		extraEnv["BOT_HUB_CONFIG"] = h.cfg.ConfigPath
	}
	var mcpEnv map[string]string
	if apiToken != "" {
		extraEnv["BOT_HUB_API_TOKEN"] = apiToken
		mcpEnv = map[string]string{"BOT_HUB_API_TOKEN": apiToken}
	}
	if role := strings.TrimSpace(opts.MCPRole); role != "" {
		extraEnv["BOT_HUB_MCP_ROLE"] = role
		if mcpEnv == nil {
			mcpEnv = make(map[string]string, 1)
		}
		mcpEnv["BOT_HUB_MCP_ROLE"] = role
	}
	for k, v := range h.botBrowserEnv(bot) {
		if k != "" && v != "" {
			extraEnv[k] = v
		}
	}
	for k, v := range opts.ExtraEnv {
		if k != "" && v != "" {
			extraEnv[k] = v
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	cancelID := h.registerIsolatedCancel(bot.ID, cancel)
	defer func() {
		cancel()
		h.unregisterIsolatedCancel(bot.ID, cancelID)
	}()

	var toolMu sync.Mutex
	var toolOrder []string
	toolCounts := make(map[string]int)

	res, err := runner.RunTurn(runCtx, harness.RunOptions{
		Bot:                      bot,
		Prompt:                   effectivePrompt,
		SessionID:                "",
		Ephemeral:                true,
		PlatformInstructionsPath: platPath,
		MCPCommand:               mcpCmd,
		MCPEnv:                   mcpEnv,
		ExtraEnv:                 extraEnv,
		OnToolUse: func(toolName string) {
			if toolName != "" {
				toolMu.Lock()
				if toolCounts[toolName] == 0 {
					toolOrder = append(toolOrder, toolName)
				}
				toolCounts[toolName]++
				toolMu.Unlock()
			}
			if opts.OnToolUse != nil {
				opts.OnToolUse(toolName)
			}
		},
	})
	if err != nil {
		return res, err
	}
	toolMu.Lock()
	res.Tools = formatToolCounts(toolOrder, toolCounts)
	toolMu.Unlock()
	return res, nil
}

// AppendRoutineReceipt writes a routine receipt onto the bot's main timeline
// without waking the main LLM session.
func (h *Hub) AppendRoutineReceipt(botID, routineName, text string, durMs int64, costUSD float64, inTok, outTok int) state.ChatMessage {
	var sessID string
	var turn int
	if h.store != nil {
		if rec, ok := h.store.Get(botID); ok {
			sessID = rec.ID
			turn = rec.TurnCount
		}
	}
	return h.appendMessage(state.ChatMessage{
		BotID:        botID,
		SessionID:    sessID,
		Turn:         turn,
		Role:         "assistant",
		From:         "routine:" + routineName,
		Text:         text,
		DurationMs:   durMs,
		CostUSD:      costUSD,
		InputTokens:  inTok,
		OutputTokens: outTok,
	})
}

func (h *Hub) ListRoutines(botID string) ([]config.ScheduleConfig, error) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	lane, ok := h.lanes[botID]
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	out := make([]config.ScheduleConfig, len(lane.bot.Schedules))
	copy(out, lane.bot.Schedules)
	return out, nil
}

// CreateRoutine adds a new routine on botID (auto-assigning a unique ID if omitted).
func (h *Hub) CreateRoutine(botID string, raw config.ScheduleConfig) (config.ScheduleConfig, error) {
	return h.saveRoutine(botID, raw, "", true)
}

// UpsertRoutine creates or updates a routine by ID (or by matching name when ID is omitted).
func (h *Hub) UpsertRoutine(botID string, raw config.ScheduleConfig) (config.ScheduleConfig, error) {
	return h.saveRoutine(botID, raw, strings.TrimSpace(raw.ID), false)
}

// UpdateRoutine applies a partial patch to an existing routine.
func (h *Hub) UpdateRoutine(botID, routineID string, patch RoutinePatch) (config.ScheduleConfig, error) {
	routineID = strings.TrimSpace(routineID)
	if routineID == "" {
		return config.ScheduleConfig{}, fmt.Errorf("routine id is required: %w", ErrRoutineNotFound)
	}

	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.RUnlock()
		return config.ScheduleConfig{}, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	var base config.ScheduleConfig
	found := false
	for _, sc := range lane.bot.Schedules {
		if sc.EffectiveID() == routineID {
			base = sc
			found = true
			break
		}
	}
	h.lanesMu.RUnlock()
	if !found {
		return config.ScheduleConfig{}, fmt.Errorf("routine %q not found: %w", routineID, ErrRoutineNotFound)
	}

	if patch.Name != nil {
		base.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.EveryMinutes != nil {
		base.EveryMinutes = *patch.EveryMinutes
	}
	if patch.DailyAt != nil {
		base.DailyAt = strings.TrimSpace(*patch.DailyAt)
	}
	if patch.Cron != nil {
		base.Cron = strings.TrimSpace(*patch.Cron)
	}
	if patch.WebhookTrigger != nil {
		base.WebhookTrigger = *patch.WebhookTrigger
	}
	if patch.ClearWebhookSecret != nil && *patch.ClearWebhookSecret {
		base.WebhookSecret = ""
	} else if patch.WebhookSecret != nil && strings.TrimSpace(*patch.WebhookSecret) != "" {
		base.WebhookSecret = strings.TrimSpace(*patch.WebhookSecret)
	}
	if patch.WebhookURL != nil {
		base.WebhookURL = strings.TrimSpace(*patch.WebhookURL)
	}
	if patch.Prompt != nil {
		base.Prompt = strings.TrimSpace(*patch.Prompt)
	}
	if patch.Model != nil {
		base.Model = strings.TrimSpace(*patch.Model)
	}
	if patch.MainDelivery != nil {
		base.MainDelivery = strings.TrimSpace(*patch.MainDelivery)
	}
	if patch.Enabled != nil {
		v := *patch.Enabled
		base.Enabled = &v
	}
	return h.saveRoutine(botID, base, routineID, false)
}

func (h *Hub) saveRoutine(botID string, raw config.ScheduleConfig, targetID string, createOnly bool) (config.ScheduleConfig, error) {
	clean := cleanScheduleInput(raw)
	if err := clean.Validate(); err != nil {
		return config.ScheduleConfig{}, err
	}

	h.lanesMu.Lock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.Unlock()
		return config.ScheduleConfig{}, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}

	curExpanded := make([]config.ScheduleConfig, len(lane.bot.Schedules))
	copy(curExpanded, lane.bot.Schedules)

	idx := -1
	if createOnly {
		if clean.ID != "" {
			for _, sc := range curExpanded {
				if sc.EffectiveID() == clean.ID {
					h.lanesMu.Unlock()
					return config.ScheduleConfig{}, fmt.Errorf("routine %q already exists", clean.ID)
				}
			}
		}
	} else {
		matchID := strings.TrimSpace(targetID)
		if matchID == "" {
			matchID = clean.ID
		}
		if matchID != "" {
			for i, sc := range curExpanded {
				if sc.EffectiveID() == matchID {
					idx = i
					break
				}
			}
			if targetID != "" && idx == -1 && clean.ID == "" {
				h.lanesMu.Unlock()
				return config.ScheduleConfig{}, fmt.Errorf("routine %q not found: %w", targetID, ErrRoutineNotFound)
			}
		} else {
			for i, sc := range curExpanded {
				if strings.EqualFold(strings.TrimSpace(sc.Name), clean.Name) {
					idx = i
					break
				}
			}
		}
		if idx >= 0 && clean.ID == "" {
			clean.ID = curExpanded[idx].EffectiveID()
		}
	}

	var prevExpanded config.ScheduleConfig
	if idx >= 0 {
		prevExpanded = curExpanded[idx]
	}

	nextRawList := make([]config.ScheduleConfig, len(curExpanded))
	copy(nextRawList, curExpanded)
	if idx >= 0 {
		nextRawList[idx] = clean
	} else {
		nextRawList = append(nextRawList, clean)
		idx = len(nextRawList) - 1
	}

	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(rawCfg *config.HubConfig) error {
			for i := range rawCfg.Bots {
				rb := &rawCfg.Bots[i]
				if rb.ID != botID {
					continue
				}
				normExisting := config.NormalizeSchedules(rb.Schedules)
				diskIdx := -1
				if idx < len(normExisting) && normExisting[idx].EffectiveID() == prevExpanded.EffectiveID() && prevExpanded.EffectiveID() != "" {
					diskIdx = idx
				} else if prevExpanded.EffectiveID() != "" {
					for j, ns := range normExisting {
						if ns.EffectiveID() == prevExpanded.EffectiveID() {
							diskIdx = j
							break
						}
					}
				}
				toDisk := clean
				if diskIdx >= 0 {
					toDisk = preserveRoutineEnvRefs(toDisk, rb.Schedules[diskIdx], prevExpanded)
					if toDisk.ID == "" {
						toDisk.ID = normExisting[diskIdx].EffectiveID()
					}
					rb.Schedules[diskIdx] = toDisk
				} else {
					normAll := config.NormalizeSchedules(append(append([]config.ScheduleConfig(nil), rb.Schedules...), toDisk))
					toDisk.ID = normAll[len(normAll)-1].ID
					rb.Schedules = append(rb.Schedules, toDisk)
				}
				return nil
			}
			return fmt.Errorf("bot %q is not in %s", botID, h.cfg.ConfigPath)
		})
		if err != nil {
			h.lanesMu.Unlock()
			return config.ScheduleConfig{}, fmt.Errorf("save config: %w", err)
		}
	}

	normalized := config.NormalizeSchedules(nextRawList)
	h.setBotSchedulesLocked(lane, normalized)
	saved := normalized[idx]
	h.lanesMu.Unlock()

	h.broadcast(Event{
		Type:    "routine_updated",
		BotID:   botID,
		Payload: map[string]any{"botId": botID, "routine": saved},
	})
	h.notifySchedulesChanged()
	return saved, nil
}

func (h *Hub) DeleteRoutine(botID, routineID string) error {
	routineID = strings.TrimSpace(routineID)
	if routineID == "" {
		return fmt.Errorf("routine %q not found: %w", routineID, ErrRoutineNotFound)
	}

	h.lanesMu.Lock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.Unlock()
		return fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	next := make([]config.ScheduleConfig, 0, len(lane.bot.Schedules))
	found := false
	for _, sc := range lane.bot.Schedules {
		if sc.EffectiveID() == routineID {
			found = true
			continue
		}
		next = append(next, sc)
	}
	if !found {
		h.lanesMu.Unlock()
		return fmt.Errorf("routine %q not found: %w", routineID, ErrRoutineNotFound)
	}

	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(rawCfg *config.HubConfig) error {
			for i := range rawCfg.Bots {
				rb := &rawCfg.Bots[i]
				if rb.ID != botID {
					continue
				}
				normExisting := config.NormalizeSchedules(rb.Schedules)
				filtered := make([]config.ScheduleConfig, 0, len(rb.Schedules))
				for j, sc := range rb.Schedules {
					if j < len(normExisting) && normExisting[j].EffectiveID() == routineID {
						continue
					}
					filtered = append(filtered, sc)
				}
				rb.Schedules = filtered
				return nil
			}
			return fmt.Errorf("bot %q is not in %s", botID, h.cfg.ConfigPath)
		})
		if err != nil {
			h.lanesMu.Unlock()
			return fmt.Errorf("save config: %w", err)
		}
	}

	h.setBotSchedulesLocked(lane, config.NormalizeSchedules(next))
	h.lanesMu.Unlock()

	h.broadcast(Event{
		Type:    "routine_updated",
		BotID:   botID,
		Payload: map[string]any{"botId": botID, "deletedId": routineID},
	})
	h.notifySchedulesChanged()
	return nil
}

func (h *Hub) setBotSchedulesLocked(lane *botLane, schedules []config.ScheduleConfig) {
	lane.bot.Schedules = schedules
	for i := range h.cfg.Bots {
		if h.cfg.Bots[i].ID == lane.bot.ID {
			h.cfg.Bots[i].Schedules = schedules
		}
	}
}

func cleanScheduleInput(raw config.ScheduleConfig) config.ScheduleConfig {
	raw.ID = strings.TrimSpace(raw.ID)
	raw.Name = strings.TrimSpace(raw.Name)
	raw.DailyAt = strings.TrimSpace(raw.DailyAt)
	raw.Cron = strings.TrimSpace(raw.Cron)
	raw.WebhookSecret = strings.TrimSpace(raw.WebhookSecret)
	raw.WebhookURL = strings.TrimSpace(raw.WebhookURL)
	raw.Prompt = strings.TrimSpace(raw.Prompt)
	raw.Model = strings.TrimSpace(raw.Model)
	raw.MainDelivery = raw.EffectiveMainDelivery()
	if raw.Enabled == nil {
		v := true
		raw.Enabled = &v
	}
	return raw
}

func preserveRoutineEnvRefs(next, curDisk, curExpanded config.ScheduleConfig) config.ScheduleConfig {
	if next.Model == curExpanded.Model && config.ContainsEnvRef(curDisk.Model) {
		next.Model = curDisk.Model
	}
	if next.WebhookSecret == curExpanded.WebhookSecret && config.ContainsEnvRef(curDisk.WebhookSecret) {
		next.WebhookSecret = curDisk.WebhookSecret
	}
	if next.WebhookURL == curExpanded.WebhookURL && config.ContainsEnvRef(curDisk.WebhookURL) {
		next.WebhookURL = curDisk.WebhookURL
	}
	return next
}

func (h *Hub) RecordRoutineRun(run state.RoutineRun) (state.RoutineRun, error) {
	if h.store == nil {
		return run, nil
	}
	return h.store.RecordRoutineRun(run)
}

func (h *Hub) ListRoutineRuns(botID, routineID string, limit int) ([]state.RoutineRun, error) {
	if _, ok := h.GetBot(botID); !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	if h.store == nil {
		return []state.RoutineRun{}, nil
	}
	return h.store.ListRoutineRuns(botID, routineID, limit)
}

func (h *Hub) LatestRoutineRun(botID, routineID string) (state.RoutineRun, bool) {
	if h.store == nil {
		return state.RoutineRun{}, false
	}
	return h.store.LatestRoutineRun(botID, routineID)
}

func (h *Hub) HasDailyRunOnDate(botID, routineID, localDate string) bool {
	if h.store == nil {
		return false
	}
	return h.store.HasDailyRunOnDate(botID, routineID, localDate)
}

func (h *Hub) MarkReceiptsInjected(ids []string) error {
	if h.store == nil {
		return nil
	}
	return h.store.MarkReceiptsInjected(ids)
}

func (h *Hub) BroadcastRoutineRun(run state.RoutineRun) {
	h.broadcast(Event{
		Type:    "routine_run",
		BotID:   run.BotID,
		Payload: run,
	})
}

func (h *Hub) registerIsolatedCancel(botID string, cancel context.CancelFunc) uint64 {
	id := h.isolatedSeq.Add(1)
	h.isolatedCancelsMu.Lock()
	m := h.isolatedCancels[botID]
	if m == nil {
		m = make(map[uint64]context.CancelFunc)
		h.isolatedCancels[botID] = m
	}
	m[id] = cancel
	h.isolatedCancelsMu.Unlock()
	return id
}

func (h *Hub) unregisterIsolatedCancel(botID string, id uint64) {
	h.isolatedCancelsMu.Lock()
	if m := h.isolatedCancels[botID]; m != nil {
		delete(m, id)
		if len(m) == 0 {
			delete(h.isolatedCancels, botID)
		}
	}
	h.isolatedCancelsMu.Unlock()
}

func (h *Hub) cancelIsolatedByBot(botID string) int {
	h.isolatedCancelsMu.Lock()
	m := h.isolatedCancels[botID]
	cancels := make([]context.CancelFunc, 0, len(m))
	for _, fn := range m {
		cancels = append(cancels, fn)
	}
	h.isolatedCancelsMu.Unlock()
	for _, fn := range cancels {
		fn()
	}
	return len(cancels)
}

// DesktopWorkerSnapshot returns the configured global desktop worker harness and model.
func (h *Hub) DesktopWorkerSnapshot() (string, string) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	return h.cfg.DesktopHarness, h.cfg.DesktopModel
}

// SetDesktopWorker persists and updates the global computer-use worker harness and model.
func (h *Hub) SetDesktopWorker(harnessKind, model string) error {
	harnessKind = strings.TrimSpace(harnessKind)
	model = strings.TrimSpace(model)
	if harnessKind != "" {
		if _, err := h.registry.Get(harnessKind); err != nil {
			return err
		}
	}

	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	prevExpandedModel := h.cfg.DesktopModel
	if h.cfg.ConfigPath != "" {
		if err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.DesktopHarness = harnessKind
			if model == prevExpandedModel && config.ContainsEnvRef(raw.DesktopModel) {
				return nil
			}
			raw.DesktopModel = model
			return nil
		}); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.DesktopHarness = harnessKind
	h.cfg.DesktopModel = model
	return nil
}

// MagpieWebURL returns the configured Magpie web address, falling back to
// MAGPIE_WEB_URL, the variable `magpie web` itself reads.
func (h *Hub) MagpieWebURL() string {
	h.lanesMu.RLock()
	u := h.cfg.MagpieWebURL
	h.lanesMu.RUnlock()
	if u == "" {
		u = strings.TrimSpace(os.Getenv("MAGPIE_WEB_URL"))
	}
	return u
}

// SetMagpieWebURL persists the Magpie web address; empty clears it.
func (h *Hub) SetMagpieWebURL(raw string) error {
	u := strings.TrimSpace(raw)
	if u != "" {
		parsed, err := url.Parse(u)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("Magpie 网页地址需以 http:// 或 https:// 开头")
		}
	}
	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	if h.cfg.ConfigPath != "" {
		if err := config.UpdateRaw(h.cfg, func(c *config.HubConfig) error {
			c.MagpieWebURL = u
			return nil
		}); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.MagpieWebURL = u
	return nil
}

const defaultDesktopTargetTTL = 30 * time.Minute

// ActiveDesktopTarget describes which desktop is currently active for screen sharing
// and default computer-use tasks.
type ActiveDesktopTarget struct {
	Target    string `json:"target"`
	Name      string `json:"name,omitempty"`
	Temporary bool   `json:"temporary"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

func (h *Hub) findRemoteDesktopNode(target string) (node.NodeInfo, bool) {
	if h.nodes == nil {
		return node.NodeInfo{}, false
	}
	for _, n := range h.nodes.List() {
		if n.ID != target || !n.Connected {
			continue
		}
		for _, p := range n.Permissions {
			if p == node.PermDesktop {
				return n, true
			}
		}
	}
	return node.NodeInfo{}, false
}

func (h *Hub) resetActiveDesktopTargetLocked() ActiveDesktopTarget {
	if h.activeDesktopTimer != nil {
		h.activeDesktopTimer.Stop()
		h.activeDesktopTimer = nil
	}
	h.activeDesktopTarget = "local"
	h.activeDesktopExpires = time.Time{}
	return ActiveDesktopTarget{Target: "local", Temporary: false}
}

func formatVirtualTargetName(hostName, slot string) string {
	if slot == "" {
		return hostName
	}
	if hostName == "" || hostName == "local" {
		return "虚拟屏 · " + slot
	}
	return hostName + " · 虚拟屏 " + slot
}

// ActiveDesktopTarget returns the currently active desktop target, falling back to
// "local" if a temporary remote target has expired, disconnected, or lost desktop permission.
func (h *Hub) ActiveDesktopTarget() ActiveDesktopTarget {
	h.desktopTargetMu.Lock()
	cur := strings.TrimSpace(h.activeDesktopTarget)
	exp := h.activeDesktopExpires
	spec := desktop.ParseTarget(cur)
	if spec.IsLocal() && !spec.IsVirtualSlot() {
		h.desktopTargetMu.Unlock()
		return ActiveDesktopTarget{Target: "local", Temporary: false}
	}
	if spec.IsLocal() && spec.IsVirtualSlot() {
		supported := h.localDeskPool != nil && h.localDeskPool.VirtualSupported()
		if supported {
			h.desktopTargetMu.Unlock()
			return ActiveDesktopTarget{
				Target:    spec.Raw,
				Name:      formatVirtualTargetName("local", spec.VirtualSlot),
				Temporary: false,
			}
		}
		snap := h.resetActiveDesktopTargetLocked()
		h.desktopTargetMu.Unlock()
		h.broadcast(Event{Type: "desktop_target", Payload: snap})
		return snap
	}
	expired := !exp.IsZero() && !time.Now().Before(exp)
	h.desktopTargetMu.Unlock()

	if !expired {
		if n, ok := h.findRemoteDesktopNode(spec.NodeID); ok && (!spec.IsVirtualSlot() || n.HasCapability(protocol.CapNodeVirtualSlots)) {
			return ActiveDesktopTarget{
				Target:    spec.Raw,
				Name:      formatVirtualTargetName(n.Name, spec.VirtualSlot),
				Temporary: true,
				ExpiresAt: exp.UTC().Format(time.RFC3339),
			}
		}
	}

	h.desktopTargetMu.Lock()
	changed := h.activeDesktopTarget == cur
	snap := ActiveDesktopTarget{Target: "local", Temporary: false}
	if changed {
		snap = h.resetActiveDesktopTargetLocked()
	} else {
		curNow := strings.TrimSpace(h.activeDesktopTarget)
		specNow := desktop.ParseTarget(curNow)
		if specNow.IsLocal() && specNow.IsVirtualSlot() {
			if h.localDeskPool != nil && h.localDeskPool.VirtualSupported() {
				h.desktopTargetMu.Unlock()
				return ActiveDesktopTarget{
					Target:    specNow.Raw,
					Name:      formatVirtualTargetName("local", specNow.VirtualSlot),
					Temporary: false,
				}
			}
		} else if !specNow.IsLocal() {
			expNow := h.activeDesktopExpires
			h.desktopTargetMu.Unlock()
			if n, ok := h.findRemoteDesktopNode(specNow.NodeID); ok && (!specNow.IsVirtualSlot() || n.HasCapability(protocol.CapNodeVirtualSlots)) {
				return ActiveDesktopTarget{
					Target:    specNow.Raw,
					Name:      formatVirtualTargetName(n.Name, specNow.VirtualSlot),
					Temporary: true,
					ExpiresAt: expNow.UTC().Format(time.RFC3339),
				}
			}
			return ActiveDesktopTarget{Target: "local", Temporary: false}
		}
	}
	h.desktopTargetMu.Unlock()
	if changed {
		h.broadcast(Event{Type: "desktop_target", Payload: snap})
	}
	return snap
}

// SetActiveDesktopTarget switches the active desktop target to "local", a local virtual slot,
// or a connected remote node (with optional virtual slot). Non-local targets expire after ttl.
func (h *Hub) SetActiveDesktopTarget(target string, ttl time.Duration) (ActiveDesktopTarget, error) {
	spec := desktop.ParseTarget(target)
	if spec.IsLocal() && !spec.IsVirtualSlot() {
		h.desktopTargetMu.Lock()
		snap := h.resetActiveDesktopTargetLocked()
		h.desktopTargetMu.Unlock()
		h.broadcast(Event{Type: "desktop_target", Payload: snap})
		return snap, nil
	}
	if spec.IsLocal() && spec.IsVirtualSlot() {
		if h.localDeskPool == nil || !h.localDeskPool.VirtualSupported() {
			return ActiveDesktopTarget{}, errors.New("当前主机不支持独立虚拟屏幕（仅 Linux 支持多虚拟屏）")
		}
		h.desktopTargetMu.Lock()
		if h.activeDesktopTimer != nil {
			h.activeDesktopTimer.Stop()
			h.activeDesktopTimer = nil
		}
		h.activeDesktopTarget = spec.Raw
		h.activeDesktopExpires = time.Time{}
		h.desktopTargetMu.Unlock()
		snap := ActiveDesktopTarget{
			Target:    spec.Raw,
			Name:      formatVirtualTargetName("local", spec.VirtualSlot),
			Temporary: false,
		}
		h.broadcast(Event{Type: "desktop_target", Payload: snap})
		return snap, nil
	}
	n, ok := h.findRemoteDesktopNode(spec.NodeID)
	if !ok {
		return ActiveDesktopTarget{}, fmt.Errorf("remote desktop target %q unavailable", spec.NodeID)
	}
	if spec.IsVirtualSlot() && !n.HasCapability(protocol.CapNodeVirtualSlots) {
		if n.OS != "linux" {
			return ActiveDesktopTarget{}, fmt.Errorf("remote node %q (%s) does not support virtual screens", spec.NodeID, n.OS)
		}
		return ActiveDesktopTarget{}, fmt.Errorf("远端节点 %q 版本过低，不支持独立虚拟屏，请升级该节点或在 Bot 详情中改绑其主桌面: %w", spec.NodeID, ErrNodeUpgradeRequired)
	}
	if ttl <= 0 {
		ttl = defaultDesktopTargetTTL
	}
	exp := time.Now().Add(ttl)

	h.desktopTargetMu.Lock()
	if h.activeDesktopTimer != nil {
		h.activeDesktopTimer.Stop()
	}
	h.activeDesktopTarget = spec.Raw
	h.activeDesktopExpires = exp
	h.activeDesktopTimer = time.AfterFunc(ttl, h.reconcileActiveDesktopTarget)
	h.desktopTargetMu.Unlock()

	snap := ActiveDesktopTarget{
		Target:    spec.Raw,
		Name:      formatVirtualTargetName(n.Name, spec.VirtualSlot),
		Temporary: true,
		ExpiresAt: exp.UTC().Format(time.RFC3339),
	}
	h.broadcast(Event{Type: "desktop_target", Payload: snap})
	return snap, nil
}

func (h *Hub) reconcileActiveDesktopTarget() {
	h.closeMu.Lock()
	closed := h.closed
	h.closeMu.Unlock()
	if closed {
		return
	}
	_ = h.ActiveDesktopTarget()
}

func (h *Hub) touchActiveDesktopTarget(target string) {
	spec := desktop.ParseTarget(target)
	if spec.IsLocal() {
		return
	}
	h.desktopTargetMu.Lock()
	curSpec := desktop.ParseTarget(h.activeDesktopTarget)
	if curSpec.IsLocal() || curSpec.HostKey() != spec.HostKey() {
		h.desktopTargetMu.Unlock()
		return
	}
	exp := time.Now().Add(defaultDesktopTargetTTL)
	if h.activeDesktopTimer != nil {
		h.activeDesktopTimer.Stop()
	}
	h.activeDesktopExpires = exp
	h.activeDesktopTimer = time.AfterFunc(defaultDesktopTargetTTL, h.reconcileActiveDesktopTarget)
	h.desktopTargetMu.Unlock()

	if n, ok := h.findRemoteDesktopNode(curSpec.NodeID); ok {
		h.broadcast(Event{
			Type: "desktop_target",
			Payload: ActiveDesktopTarget{
				Target:    curSpec.Raw,
				Name:      formatVirtualTargetName(n.Name, curSpec.VirtualSlot),
				Temporary: true,
				ExpiresAt: exp.UTC().Format(time.RFC3339),
			},
		})
	}
}

// ResolveBotDesktopTarget resolves the desktop target for a bot task or takeover.
// Priority: explicitTarget > bot's bound DesktopTarget > global ActiveDesktopTarget.
// Bound remote targets never fall back silently to local when offline.
func (h *Hub) ResolveBotDesktopTarget(botID, explicitTarget string) (string, error) {
	raw := strings.TrimSpace(explicitTarget)
	boundFromBot := false
	if raw == "" && strings.TrimSpace(botID) != "" {
		if bot, ok := h.GetBot(botID); ok && strings.TrimSpace(bot.DesktopTarget) != "" {
			raw = bot.DesktopTarget
			boundFromBot = true
		}
	}
	if raw == "" {
		return h.ActiveDesktopTarget().Target, nil
	}

	spec := desktop.ParseTarget(raw)
	if spec.IsLocal() {
		if spec.IsVirtualSlot() && (h.localDeskPool == nil || !h.localDeskPool.VirtualSupported()) {
			return "", errors.New("当前主机不支持独立虚拟屏幕（仅 Linux 支持多虚拟屏）")
		}
		return spec.Raw, nil
	}

	n, ok := h.findRemoteDesktopNode(spec.NodeID)
	if !ok {
		if boundFromBot {
			return "", fmt.Errorf("Bot %q 绑定的桌面节点 %q 当前离线或未开启桌面权限: %w", botID, spec.NodeID, ErrBoundDesktopOffline)
		}
		return "", fmt.Errorf("remote desktop target %q unavailable: %w", spec.NodeID, ErrBoundDesktopOffline)
	}
	if spec.IsVirtualSlot() && !n.HasCapability(protocol.CapNodeVirtualSlots) {
		if n.OS != "linux" {
			return "", fmt.Errorf("remote node %q (%s) does not support virtual screens", spec.NodeID, n.OS)
		}
		return "", fmt.Errorf("远端节点 %q 版本过低，不支持独立虚拟屏，请升级该节点或在 Bot 详情中改绑其主桌面: %w", spec.NodeID, ErrNodeUpgradeRequired)
	}
	return spec.Raw, nil
}

// BrowserProfileDir returns the persistent isolated browser profile directory for botID.
func (h *Hub) BrowserProfileDir(botID string) string {
	base := ""
	if h.cfg != nil && strings.TrimSpace(h.cfg.StateFile) != "" {
		base = filepath.Dir(h.cfg.StateFile)
	} else if h.cfg != nil && strings.TrimSpace(h.cfg.ProjectRoot) != "" {
		base = filepath.Join(h.cfg.ProjectRoot, "state")
	} else {
		base = os.TempDir()
	}
	return filepath.Join(base, "browsers", botID)
}

func (h *Hub) botBrowserEnv(bot config.BotConfig) map[string]string {
	env := make(map[string]string)
	if strings.TrimSpace(bot.Env["AGENT_BROWSER_SESSION"]) == "" {
		env["AGENT_BROWSER_SESSION"] = "bot-" + bot.ID
	}
	if bot.EffectiveBrowserMode() == "isolated" && strings.TrimSpace(bot.Env["AGENT_BROWSER_PROFILE"]) == "" {
		dir := h.BrowserProfileDir(bot.ID)
		if err := os.MkdirAll(dir, 0o700); err == nil {
			env["AGENT_BROWSER_PROFILE"] = dir
		}
	}
	if strings.TrimSpace(bot.DesktopTarget) != "" && h.localDeskPool != nil {
		spec := desktop.ParseTarget(bot.DesktopTarget)
		if spec.IsLocal() && spec.IsVirtualSlot() {
			for _, kv := range h.localDeskPool.SlotEnv(spec.VirtualSlot) {
				if k, v, ok := strings.Cut(kv, "="); ok && v != "" {
					if _, exists := bot.Env[k]; !exists {
						env[k] = v
					}
				}
			}
		}
	}
	return env
}

// GetBotBrowserInfo returns browser isolation settings and bound remote target details for node_exec forwarding.
func (h *Hub) GetBotBrowserInfo(botID string) (session string, isolated bool, boundNodeID string, boundSlot string) {
	bot, ok := h.GetBot(botID)
	if !ok {
		return "", false, "", ""
	}
	session = strings.TrimSpace(bot.Env["AGENT_BROWSER_SESSION"])
	if session == "" {
		session = "bot-" + bot.ID
	}
	isolated = bot.EffectiveBrowserMode() == "isolated"
	if strings.TrimSpace(bot.DesktopTarget) != "" {
		spec := desktop.ParseTarget(bot.DesktopTarget)
		if !spec.IsLocal() {
			boundNodeID = spec.NodeID
			boundSlot = spec.VirtualSlot
		}
	}
	return session, isolated, boundNodeID, boundSlot
}

func closeBotBrowserSession(botID string) {
	if strings.TrimSpace(botID) == "" {
		return
	}
	bin, err := exec.LookPath("agent-browser")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, bin, "--session", "bot-"+botID, "close").Run()
}

// ActiveDesktopTaskBot returns the bot ID currently running a computer-use worker on target, if any.
func (h *Hub) ActiveDesktopTaskBot(target string) string {
	t := desktop.NormalizeTarget(target)
	h.desktopTaskMu.Lock()
	defer h.desktopTaskMu.Unlock()
	if id := h.activeDesktopTasks[t]; id != "" {
		return id
	}
	if t != "local" && t != "" {
		return h.activeDesktopTasks["_default"]
	}
	return ""
}

// RunDesktopTask executes a multi-step GUI / visible-browser goal in an isolated
// ephemeral worker using the configured lightweight desktop model/harness.
func (h *Hub) RunDesktopTask(ctx context.Context, req DesktopTaskRequest) (*harness.TurnResult, error) {
	botID := strings.TrimSpace(req.BotID)
	if botID == "" {
		return nil, fmt.Errorf("botId is required: %w", ErrBotNotFound)
	}
	if _, ok := h.GetBot(botID); !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		return nil, errors.New("goal is required")
	}

	target, err := h.ResolveBotDesktopTarget(botID, req.Target)
	if err != nil {
		return nil, err
	}
	spec := desktop.ParseTarget(target)
	if !spec.IsLocal() {
		h.touchActiveDesktopTarget(spec.HostKey())
	}

	h.desktopTaskMu.Lock()
	if holder := h.activeDesktopTasks[target]; holder != "" {
		h.desktopTaskMu.Unlock()
		return nil, fmt.Errorf("%w (%s, held by bot %q)", ErrDesktopTaskBusy, target, holder)
	}
	h.activeDesktopTasks[target] = botID
	h.desktopTaskMu.Unlock()
	defer func() {
		h.desktopTaskMu.Lock()
		if h.activeDesktopTasks[target] == botID {
			delete(h.activeDesktopTasks, target)
		}
		h.desktopTaskMu.Unlock()
	}()

	var taskEnv map[string]string
	if spec.IsLocal() && h.localDeskPool != nil {
		eng, err := h.localDeskPool.ForSlot(spec.VirtualSlot)
		if err != nil {
			return nil, err
		}
		release := eng.AcquireTaskLease()
		defer release()
		if err := eng.PrepareForTask(ctx); err != nil && spec.IsVirtualSlot() {
			return nil, err
		}
		if envs := eng.Env(); len(envs) > 0 {
			taskEnv = make(map[string]string, len(envs))
			for _, kv := range envs {
				if k, v, ok := strings.Cut(kv, "="); ok {
					taskEnv[k] = v
				}
			}
		}
	}

	deskHarness, deskModel := h.DesktopWorkerSnapshot()
	var lines []string
	lines = append(lines,
		"[Computer Use Worker]",
		fmt.Sprintf("Target desktop: %s", target),
	)
	if app := strings.TrimSpace(req.App); app != "" {
		lines = append(lines, fmt.Sprintf("Target app: %s", app))
	}
	lines = append(lines,
		"Rules:",
		"- Operate ONLY on the visible desktop via `node_desktop` (never launch headless browsers or hidden background UI sessions) so the user can observe and take over at any moment.",
		"- Capture a screenshot with `node_desktop(op=\"screenshot\")` and inspect it before clicking/typing; verify the final state visually after acting.",
		"- If login/QR/2FA/CAPTCHA/payment confirmation is required, call `request_takeover(reason=...)` and wait for the human to finish.",
		"- Never call `desktop_task` recursively.",
		"- Return a concise factual summary of what was done and observed.",
		"",
		"Goal:",
		goal,
	)

	return h.RunIsolated(ctx, botID, strings.Join(lines, "\n"), IsolatedOptions{
		Source:     "desktop_task",
		Harness:    deskHarness,
		Model:      deskModel,
		MCPRole:    "computer_use",
		TimeoutSec: req.TimeoutSec,
		ExtraEnv:   taskEnv,
		OnToolUse: func(toolName string) {
			if toolName == "" {
				return
			}
			h.broadcast(Event{
				Type:  "tool_use",
				BotID: botID,
				Payload: map[string]any{
					"botId": botID,
					"tool":  "desktop:" + toolName,
				},
			})
		},
	})
}

func isUserDirectSource(opts SendOptions) bool {
	if opts.FromBotID != "" {
		return false
	}
	src := strings.TrimSpace(opts.Source)
	if src == "webhook" ||
		strings.HasPrefix(src, "routine:") ||
		strings.HasPrefix(src, "schedule:") ||
		strings.HasPrefix(src, "bot:") {
		return false
	}
	return true
}

func isInteractiveQuestionSource(opts SendOptions) bool {
	if opts.FromBotID != "" {
		return false
	}
	src := strings.TrimSpace(opts.Source)
	if src == "webhook" || src == "desktop_task" || src == "cli" ||
		strings.HasPrefix(src, "routine:") ||
		strings.HasPrefix(src, "schedule:") ||
		strings.HasPrefix(src, "bot:") {
		return false
	}
	return true
}

func newQuestionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "q_" + hex.EncodeToString(b)
}

func (h *Hub) listQuestionsLocked(botID string) []PendingQuestion {
	items := make([]PendingQuestion, 0, len(h.questions))
	for _, entry := range h.questions {
		if botID != "" && entry.item.BotID != botID {
			continue
		}
		items = append(items, entry.item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt < items[j].CreatedAt
		}
		return items[i].ID < items[j].ID
	})
	return items
}

// ListQuestions returns all pending interactive question cards, optionally filtered by botID.
func (h *Hub) ListQuestions(botID string) []PendingQuestion {
	h.questionsMu.Lock()
	defer h.questionsMu.Unlock()
	return h.listQuestionsLocked(botID)
}

func (h *Hub) broadcastQuestions(items []PendingQuestion) {
	h.broadcast(Event{
		Type:    "questions",
		Payload: map[string]any{"items": items},
	})
}

// AskQuestion publishes a pending question card to connected clients and blocks until answered or cancelled.
func (h *Hub) AskQuestion(ctx context.Context, bot config.BotConfig, req harness.QuestionRequest) (harness.QuestionResponse, error) {
	id := newQuestionID()
	fields := make([]harness.QuestionField, len(req.Fields))
	copy(fields, req.Fields)
	entry := &pendingQuestionEntry{
		item: PendingQuestion{
			ID:        id,
			BotID:     bot.ID,
			BotName:   bot.Name,
			Title:     req.Title,
			Fields:    fields,
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		ch: make(chan harness.QuestionResponse, 1),
	}

	h.questionsMu.Lock()
	h.questions[id] = entry
	items := h.listQuestionsLocked("")
	h.questionsMu.Unlock()
	h.broadcastQuestions(items)

	defer func() {
		h.questionsMu.Lock()
		_, existed := h.questions[id]
		if existed {
			delete(h.questions, id)
			items := h.listQuestionsLocked("")
			h.questionsMu.Unlock()
			h.broadcastQuestions(items)
		} else {
			h.questionsMu.Unlock()
		}
	}()

	select {
	case resp := <-entry.ch:
		return resp, nil
	case <-ctx.Done():
		return harness.QuestionResponse{Cancelled: true}, ctx.Err()
	}
}

// AnswerQuestion delivers a user's response (or cancellation) to a pending question card.
func (h *Hub) AnswerQuestion(id string, answers map[string]any, cancelled bool, message string) ([]PendingQuestion, error) {
	h.questionsMu.Lock()
	entry, ok := h.questions[id]
	if !ok {
		h.questionsMu.Unlock()
		return nil, ErrQuestionNotFound
	}
	delete(h.questions, id)
	items := h.listQuestionsLocked("")
	h.questionsMu.Unlock()

	select {
	case entry.ch <- harness.QuestionResponse{Answers: answers, Cancelled: cancelled, Message: message}:
	default:
	}
	h.broadcastQuestions(items)
	return items, nil
}

func sanitizeUploadFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return "file"
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r == '/' || r == '\\' || r < 0x20:
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "file"
	}
	return out
}

// SaveInboundAttachment stores a user-uploaded file in mediaDir and copies it
// into the bot's .uploads/ directory so the harness can read it directly.
func (h *Hub) SaveInboundAttachment(botID, filename string, raw []byte) (InboundAttachment, error) {
	bot, ok := h.GetBot(botID)
	if !ok {
		return InboundAttachment{}, ErrBotNotFound
	}
	if len(raw) == 0 {
		return InboundAttachment{}, fmt.Errorf("attachment is empty")
	}
	detected := http.DetectContentType(raw)
	_, isImage := imageExtForMIME(detected)

	safeName := sanitizeUploadFilename(filename)
	b64 := base64.StdEncoding.EncodeToString(raw)
	media, err := h.SaveOutboundAttachment(bot.Workspace, OutboundAttachment{
		DataBase64: b64,
		Filename:   safeName,
		Name:       safeName,
	}, isImage)
	if err != nil {
		return InboundAttachment{}, err
	}

	wsRel := ""
	diskPath := filepath.Join(h.mediaDir, media.ID)
	if bot.Workspace != "" {
		uploadDir := filepath.Join(bot.Workspace, ".uploads")
		if err := os.MkdirAll(uploadDir, 0o755); err != nil {
			return InboundAttachment{}, fmt.Errorf("create uploads dir: %w", err)
		}
		uploadName := media.ID + "-" + safeName
		wsAbs := filepath.Join(uploadDir, uploadName)
		if err := os.WriteFile(wsAbs, raw, 0o644); err != nil {
			return InboundAttachment{}, fmt.Errorf("write workspace upload: %w", err)
		}
		diskPath = wsAbs
		wsRel = filepath.ToSlash(filepath.Join(".uploads", uploadName))
	}

	h.inboundAttsMu.Lock()
	if h.inboundAtts == nil {
		h.inboundAtts = make(map[string]inboundAttEntry)
	}
	h.inboundAtts[media.ID] = inboundAttEntry{
		media: media,
		att: harness.Attachment{
			ID:            media.ID,
			Kind:          media.Kind,
			Name:          media.Name,
			MimeType:      media.MimeType,
			Path:          diskPath,
			WorkspacePath: wsRel,
		},
	}
	h.inboundAttsMu.Unlock()

	return InboundAttachment{
		Media:         media,
		WorkspacePath: wsRel,
	}, nil
}

// ListBotCommands aggregates Hub built-in commands with commands/skills exposed by the bot's harness.
func (h *Hub) ListBotCommands(ctx context.Context, botID string) ([]harness.SlashCommand, error) {
	bot, ok := h.GetBot(botID)
	if !ok {
		return nil, ErrBotNotFound
	}

	hubBuiltins := []harness.SlashCommand{
		{Name: "status", Description: "查看当前会话状态与费用", Source: "hub"},
		{Name: "bots", Description: "列出所有 Bot", Source: "hub"},
		{Name: "ask", Description: "委派任务给其他 Bot", ArgumentHint: "<bot> <message>", Source: "hub"},
		{Name: "reset", Description: "归档当前会话并开启新会话", Source: "hub"},
	}
	hubSet := map[string]harness.SlashCommand{}
	for _, c := range hubBuiltins {
		hubSet[c.Name] = c
	}

	var harnessCmds []harness.SlashCommand
	if runner, err := h.registry.Get(bot.Harness); err == nil {
		if lister, ok := runner.(harness.CommandLister); ok {
			lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if list, err := lister.ListCommands(lctx, bot); err == nil {
				harnessCmds = list
			}
		}
	}

	seen := make(map[string]bool)
	out := make([]harness.SlashCommand, 0, len(harnessCmds)+len(hubBuiltins))
	for _, c := range harnessCmds {
		name := strings.TrimPrefix(strings.TrimSpace(c.Name), "/")
		if name == "" || seen[name] {
			continue
		}
		if _, isHub := hubSet[name]; isHub {
			continue
		}
		c.Name = name
		seen[name] = true
		out = append(out, c)
	}
	for _, c := range hubBuiltins {
		if !seen[c.Name] {
			seen[c.Name] = true
			out = append(out, c)
		}
	}
	return out, nil
}

var skipMentionDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".uploads":     true,
	"__pycache__":  true,
	".next":        true,
	"dist":         true,
	"build":        true,
	".venv":        true,
}

func isWithinRoot(root, target string) bool {
	if root == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func scanWorkspaceMentionFiles(workspace string) []string {
	if workspace == "" {
		return nil
	}
	wsReal, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		wsReal = filepath.Clean(workspace)
	}
	var sharedReal string
	if s, err := filepath.EvalSymlinks(filepath.Join(workspace, "_shared")); err == nil {
		sharedReal = s
	}

	const maxEntries = 2000
	const maxDepth = 6

	var files []string
	var walkDir func(absDir, relPrefix string, depth int, allowedRoot string)
	walkDir = func(absDir, relPrefix string, depth int, allowedRoot string) {
		if depth > maxDepth || len(files) >= maxEntries {
			return
		}
		entries, err := os.ReadDir(absDir)
		if err != nil {
			return
		}
		for _, ent := range entries {
			if len(files) >= maxEntries {
				return
			}
			name := ent.Name()
			if skipMentionDirs[name] {
				continue
			}
			relPath := name
			if relPrefix != "" {
				relPath = relPrefix + "/" + name
			}
			fullPath := filepath.Join(absDir, name)

			isDir := ent.IsDir()
			entryRoot := allowedRoot
			if ent.Type()&fs.ModeSymlink != 0 {
				if depth == 0 && name == "_shared" && sharedReal != "" {
					info, err := os.Stat(fullPath)
					if err != nil || !info.IsDir() {
						continue
					}
					isDir = true
					entryRoot = sharedReal
				} else {
					resolved, err := filepath.EvalSymlinks(fullPath)
					if err != nil || !isWithinRoot(allowedRoot, resolved) {
						continue
					}
					info, err := os.Stat(resolved)
					if err != nil {
						continue
					}
					isDir = info.IsDir()
				}
			}

			if isDir {
				walkDir(fullPath, relPath, depth+1, entryRoot)
			} else {
				files = append(files, relPath)
			}
		}
	}

	walkDir(workspace, "", 0, wsReal)
	return files
}

// ListBotMentions returns @-mention candidates (other bots, harness agents, and workspace files).
func (h *Hub) ListBotMentions(ctx context.Context, botID, query string, limit int) ([]MentionCandidate, error) {
	bot, ok := h.GetBot(botID)
	if !ok {
		return nil, ErrBotNotFound
	}
	if limit <= 0 {
		limit = 30
	}
	q := strings.ToLower(strings.TrimSpace(query))

	type scored struct {
		item  MentionCandidate
		score int
	}
	var all []scored
	matchScore := func(value, label string) (int, bool) {
		if q == "" {
			return 0, true
		}
		lv := strings.ToLower(value)
		ll := strings.ToLower(label)
		base := strings.ToLower(filepath.Base(value))
		switch {
		case base == q || lv == q || ll == q:
			return 4, true
		case strings.HasPrefix(base, q) || strings.HasPrefix(ll, q):
			return 3, true
		case strings.HasPrefix(lv, q):
			return 2, true
		case strings.Contains(lv, q) || strings.Contains(ll, q):
			return 1, true
		default:
			return 0, false
		}
	}

	// 1. Other bots in the hub.
	h.lanesMu.RLock()
	for _, id := range h.order {
		if id == botID {
			continue
		}
		lane := h.lanes[id]
		if lane == nil {
			continue
		}
		b := lane.bot
		if sc, ok := matchScore(b.ID, b.Name); ok {
			all = append(all, scored{
				item: MentionCandidate{
					Kind:        "bot",
					Value:       b.ID,
					Label:       b.Name,
					Description: b.Description,
				},
				score: sc + 10,
			})
		}
	}
	h.lanesMu.RUnlock()

	// 2. Harness agents (if supported).
	if runner, err := h.registry.Get(bot.Harness); err == nil {
		if al, ok := runner.(harness.AgentLister); ok {
			actx, cancel := context.WithTimeout(ctx, 2*time.Second)
			agents, err := al.ListAgents(actx, bot)
			cancel()
			if err == nil {
				for _, a := range agents {
					if sc, ok := matchScore(a.Name, a.Name); ok {
						all = append(all, scored{
							item: MentionCandidate{
								Kind:        "agent",
								Value:       a.Name,
								Label:       a.Name,
								Description: a.Description,
							},
							score: sc + 8,
						})
					}
				}
			}
		}
	}

	// 3. Workspace files.
	for _, rel := range scanWorkspaceMentionFiles(bot.Workspace) {
		if sc, ok := matchScore(rel, filepath.Base(rel)); ok {
			all = append(all, scored{
				item: MentionCandidate{
					Kind:        "file",
					Value:       rel,
					Label:       filepath.Base(rel),
					Description: rel,
				},
				score: sc,
			})
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].item.Value < all[j].item.Value
	})

	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]MentionCandidate, 0, len(all))
	for _, s := range all {
		out = append(out, s.item)
	}
	return out, nil
}
