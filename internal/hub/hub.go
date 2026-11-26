package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/desktop"
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
	"bot-hub/internal/push"
	"bot-hub/internal/state"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const MaxBotHopDepth = 4

// ErrBotNotFound indicates that the requested bot ID is not registered in the hub.
var ErrBotNotFound = errors.New("bot not found")

// ErrRoutineNotFound indicates that the requested routine ID does not exist on the bot.
var ErrRoutineNotFound = errors.New("routine not found")

// ErrDesktopTaskBusy indicates a computer-use worker is already running on the target desktop.
var ErrDesktopTaskBusy = errors.New("desktop task already running for target")

// ErrLocalDesktopDisabled indicates local host desktop automation is turned off in settings.
var ErrLocalDesktopDisabled = errors.New("local desktop control is disabled in Settings -> Connection & Devices")

// ErrBoundDesktopOffline indicates a bot's explicitly bound desktop target is offline or unavailable.
var ErrBoundDesktopOffline = errors.New("bound desktop node is offline or desktop permission is disabled")

// ErrNodeUpgradeRequired indicates the target remote node is running an older version that lacks a required capability.
var ErrNodeUpgradeRequired = errors.New("remote node version is too old for this feature; please upgrade the node")

// ErrForbidden indicates the caller is not permitted to access another bot's private sessions.
var ErrForbidden = errors.New("forbidden")

// ErrQuestionNotFound indicates that the requested pending question does not exist or has already been settled.
var ErrQuestionNotFound = errors.New("question not found")

// ErrApprovalNotFound indicates that the requested pending approval does not exist or has already been settled.
var ErrApprovalNotFound = errors.New("approval not found")

type Event struct {
	Type    string `json:"type"` // "message" | "tool_use" | "bot_status" | "bot_created" | "bot_deleted"
	BotID   string `json:"botId,omitempty"`
	Payload any    `json:"payload"`
}

type Hub struct {
	cfg           *config.HubConfig
	store         *state.Store
	messages      *state.MessageStore
	mediaDir      string
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

	approvalsMu sync.Mutex
	approvals   map[string]*pendingApprovalEntry

	allowlistMu  sync.Mutex
	sessionAllow map[string]map[string]bool

	inboundAttsMu sync.Mutex
	inboundAtts   map[string]inboundAttEntry

	stageMu      sync.RWMutex
	latestStages map[string]*state.StageRef
}

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
	mediaDir := filepath.Join(os.TempDir(), "pebble-media")
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
		approvals:          make(map[string]*pendingApprovalEntry),
		sessionAllow:       make(map[string]map[string]bool),
		inboundAtts:        make(map[string]inboundAttEntry),
		latestStages:       make(map[string]*state.StageRef),
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

func (h *Hub) BroadcastUpdateStatus(payload any) {
	h.broadcast(Event{
		Type:    "update_status",
		Payload: payload,
	})
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
