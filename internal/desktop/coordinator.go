package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultAgentGrace = 8 * time.Second

// ScreenState tracks who currently controls a target desktop ("idle" | "agent" | "human").
type ScreenState struct {
	Target   string `json:"target"`
	Owner    string `json:"owner"` // idle | agent | human
	BotID    string `json:"botId,omitempty"`
	BotName  string `json:"botName,omitempty"`
	Tool     string `json:"tool,omitempty"`
	LockedAt string `json:"lockedAt,omitempty"`
}

// TakeoverRequest represents a paused bot waiting for human action on the remote screen.
type TakeoverRequest struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	BotID     string `json:"botId"`
	BotName   string `json:"botName,omitempty"`
	Reason    string `json:"reason"`
	App       string `json:"app,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type screenSlot struct {
	state      ScreenState
	graceTimer *time.Timer
	seq        uint64
}

type takeoverWaiter struct {
	req    TakeoverRequest
	doneCh chan string
}

// Coordinator arbitrates ScreenLock and human TakeoverManager across targets ("local" and remote nodes).
type Coordinator struct {
	mu               sync.Mutex
	grace            time.Duration
	screens          map[string]*screenSlot
	takeovers        map[string]*takeoverWaiter
	onScreenChange   func(ScreenState)
	onTakeoverChange func([]TakeoverRequest)
	onSetInput       func(target string, enabled bool)
}

func NewCoordinator(
	onScreenChange func(ScreenState),
	onTakeoverChange func([]TakeoverRequest),
	onSetInput func(target string, enabled bool),
) *Coordinator {
	return &Coordinator{
		grace:            defaultAgentGrace,
		screens:          make(map[string]*screenSlot),
		takeovers:        make(map[string]*takeoverWaiter),
		onScreenChange:   onScreenChange,
		onTakeoverChange: onTakeoverChange,
		onSetInput:       onSetInput,
	}
}

func (c *Coordinator) ScreenState(target string) ScreenState {
	t := NormalizeTarget(target)
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.screens[t]; ok {
		return s.state
	}
	return ScreenState{Target: t, Owner: "idle"}
}

func (c *Coordinator) slotLocked(target string) *screenSlot {
	t := NormalizeTarget(target)
	s, ok := c.screens[t]
	if !ok {
		s = &screenSlot{state: ScreenState{Target: t, Owner: "idle"}}
		c.screens[t] = s
	}
	return s
}

func (c *Coordinator) BeginAgentTool(target, botID, botName, tool string) {
	t := NormalizeTarget(target)
	c.mu.Lock()
	s := c.slotLocked(t)
	if s.graceTimer != nil {
		s.graceTimer.Stop()
		s.graceTimer = nil
	}
	s.seq++
	s.state = ScreenState{
		Target:   t,
		Owner:    "agent",
		BotID:    botID,
		BotName:  botName,
		Tool:     tool,
		LockedAt: time.Now().UTC().Format(time.RFC3339),
	}
	st := s.state
	c.mu.Unlock()

	if c.onSetInput != nil {
		go c.onSetInput(t, false)
	}
	if c.onScreenChange != nil {
		go c.onScreenChange(st)
	}
}

func (c *Coordinator) EndAgentTool(target, botID string) {
	t := NormalizeTarget(target)
	c.mu.Lock()
	s := c.slotLocked(t)
	if s.state.Owner != "agent" || (botID != "" && s.state.BotID != botID) {
		c.mu.Unlock()
		return
	}
	seq := s.seq
	if s.graceTimer != nil {
		s.graceTimer.Stop()
	}
	grace := c.grace
	if grace <= 0 {
		s.state = ScreenState{Target: t, Owner: "idle"}
		st := s.state
		c.mu.Unlock()
		if c.onSetInput != nil {
			go c.onSetInput(t, true)
		}
		if c.onScreenChange != nil {
			go c.onScreenChange(st)
		}
		return
	}
	s.graceTimer = time.AfterFunc(grace, func() {
		c.mu.Lock()
		cur := c.slotLocked(t)
		if cur.seq != seq || cur.state.Owner != "agent" {
			c.mu.Unlock()
			return
		}
		cur.graceTimer = nil
		cur.state = ScreenState{Target: t, Owner: "idle"}
		st := cur.state
		c.mu.Unlock()
		if c.onSetInput != nil {
			go c.onSetInput(t, true)
		}
		if c.onScreenChange != nil {
			go c.onScreenChange(st)
		}
	})
	c.mu.Unlock()
}

// CheckHumanInput returns ErrAgentBusy when an agent currently locks target.
var ErrAgentBusy = errors.New("AI 正在操作屏幕，请点击「强制接管」后再操作")

func (c *Coordinator) CheckHumanInput(target string) error {
	t := NormalizeTarget(target)
	c.mu.Lock()
	defer c.mu.Unlock()
	if s := c.screens[t]; s != nil && s.state.Owner == "agent" {
		return ErrAgentBusy
	}
	return nil
}

// ForceTakeover transitions target ownership immediately to human and returns
// the botID that was operating the screen (if any) so the caller can interrupt it.
func (c *Coordinator) ForceTakeover(target string) string {
	t := NormalizeTarget(target)
	c.mu.Lock()
	s := c.slotLocked(t)
	pausedBot := s.state.BotID
	if s.graceTimer != nil {
		s.graceTimer.Stop()
		s.graceTimer = nil
	}
	s.seq++
	s.state = ScreenState{Target: t, Owner: "human", LockedAt: time.Now().UTC().Format(time.RFC3339)}
	st := s.state
	c.mu.Unlock()

	if c.onSetInput != nil {
		go c.onSetInput(t, true)
	}
	if c.onScreenChange != nil {
		go c.onScreenChange(st)
	}
	return pausedBot
}

// ListTakeovers returns all pending human takeover requests sorted oldest-first.
func (c *Coordinator) ListTakeovers() []TakeoverRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]TakeoverRequest, 0, len(c.takeovers))
	for _, w := range c.takeovers {
		out = append(out, w.req)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// RequestTakeover pauses until a human clicks "完成并恢复 AI" or ctx is cancelled.
// While waiting, target screen lock is handed to "human" so the viewer can interact.
func (c *Coordinator) RequestTakeover(ctx context.Context, target, botID, botName, reason, app string) (string, error) {
	t := NormalizeTarget(target)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "需要人工在屏幕上确认或登录"
	}
	id := newTakeoverID()
	w := &takeoverWaiter{
		req: TakeoverRequest{
			ID:        id,
			Target:    t,
			BotID:     botID,
			BotName:   botName,
			Reason:    reason,
			App:       app,
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		doneCh: make(chan string, 1),
	}

	c.mu.Lock()
	// Cancel any stale pending request from the same bot.
	for oldID, oldW := range c.takeovers {
		if botID != "" && oldW.req.BotID == botID {
			delete(c.takeovers, oldID)
			select {
			case oldW.doneCh <- "replaced by newer takeover request":
			default:
			}
		}
	}
	c.takeovers[id] = w
	s := c.slotLocked(t)
	if s.graceTimer != nil {
		s.graceTimer.Stop()
		s.graceTimer = nil
	}
	s.seq++
	s.state = ScreenState{Target: t, Owner: "human", BotID: botID, BotName: botName}
	st := s.state
	list := c.snapshotTakeoversLocked()
	c.mu.Unlock()

	if c.onSetInput != nil {
		go c.onSetInput(t, true)
	}
	if c.onScreenChange != nil {
		go c.onScreenChange(st)
	}
	if c.onTakeoverChange != nil {
		go c.onTakeoverChange(list)
	}

	select {
	case note, ok := <-w.doneCh:
		if !ok {
			return "", context.Canceled
		}
		return note, nil
	case <-ctx.Done():
		c.removeTakeover(id)
		return "", ctx.Err()
	}
}

// ResolveTakeover wakes a pending takeover by request ID or botID.
func (c *Coordinator) ResolveTakeover(idOrBotID, note string) bool {
	key := strings.TrimSpace(idOrBotID)
	note = strings.TrimSpace(note)
	if note == "" {
		note = "人工操作已完成，请继续执行。"
	}
	c.mu.Lock()
	var resolved []*takeoverWaiter
	for id, w := range c.takeovers {
		if key == "" || id == key || w.req.BotID == key {
			resolved = append(resolved, w)
			delete(c.takeovers, id)
		}
	}
	if len(resolved) == 0 {
		c.mu.Unlock()
		return false
	}
	for _, w := range resolved {
		t := w.req.Target
		s := c.slotLocked(t)
		if s.state.Owner == "human" {
			s.state = ScreenState{Target: t, Owner: "idle"}
			st := s.state
			if c.onScreenChange != nil {
				go c.onScreenChange(st)
			}
		}
		select {
		case w.doneCh <- note:
		default:
		}
	}
	list := c.snapshotTakeoversLocked()
	c.mu.Unlock()

	if c.onTakeoverChange != nil {
		go c.onTakeoverChange(list)
	}
	return true
}

// CancelTakeoversByBot aborts any pending takeover associated with botID.
func (c *Coordinator) CancelTakeoversByBot(botID string) {
	if strings.TrimSpace(botID) == "" {
		return
	}
	c.mu.Lock()
	changed := false
	for id, w := range c.takeovers {
		if w.req.BotID == botID {
			delete(c.takeovers, id)
			close(w.doneCh)
			changed = true
		}
	}
	var list []TakeoverRequest
	if changed {
		list = c.snapshotTakeoversLocked()
	}
	c.mu.Unlock()
	if changed && c.onTakeoverChange != nil {
		go c.onTakeoverChange(list)
	}
}

func (c *Coordinator) removeTakeover(id string) {
	c.mu.Lock()
	_, ok := c.takeovers[id]
	delete(c.takeovers, id)
	var list []TakeoverRequest
	if ok {
		list = c.snapshotTakeoversLocked()
	}
	c.mu.Unlock()
	if ok && c.onTakeoverChange != nil {
		go c.onTakeoverChange(list)
	}
}

func (c *Coordinator) snapshotTakeoversLocked() []TakeoverRequest {
	out := make([]TakeoverRequest, 0, len(c.takeovers))
	for _, w := range c.takeovers {
		out = append(out, w.req)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

func newTakeoverID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "to_" + hex.EncodeToString(b[:])
}
