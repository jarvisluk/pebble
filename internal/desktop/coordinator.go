package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

// ScreenState tracks whether a target desktop is idle or awaiting human takeover.
type ScreenState struct {
	Target  string `json:"target"`
	Owner   string `json:"owner"` // idle | human
	BotID   string `json:"botId,omitempty"`
	BotName string `json:"botName,omitempty"`
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

type takeoverWaiter struct {
	req    TakeoverRequest
	doneCh chan string
}

// Coordinator manages human takeover handoffs across local and remote desktop targets.
type Coordinator struct {
	mu               sync.Mutex
	takeovers        map[string]*takeoverWaiter
	onScreenChange   func(ScreenState)
	onTakeoverChange func([]TakeoverRequest)
}

func NewCoordinator(
	onScreenChange func(ScreenState),
	onTakeoverChange func([]TakeoverRequest),
) *Coordinator {
	return &Coordinator{
		takeovers:        make(map[string]*takeoverWaiter),
		onScreenChange:   onScreenChange,
		onTakeoverChange: onTakeoverChange,
	}
}

func (c *Coordinator) ScreenState(target string) ScreenState {
	t := NormalizeTarget(target)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.screenStateLocked(t)
}

func (c *Coordinator) screenStateLocked(t string) ScreenState {
	var best *TakeoverRequest
	for _, w := range c.takeovers {
		if w.req.Target != t {
			continue
		}
		if best == nil || w.req.CreatedAt < best.CreatedAt {
			req := w.req
			best = &req
		}
	}
	if best != nil {
		return ScreenState{
			Target:  t,
			Owner:   "human",
			BotID:   best.BotID,
			BotName: best.BotName,
		}
	}
	return ScreenState{Target: t, Owner: "idle"}
}

// ListTakeovers returns all pending human takeover requests sorted oldest-first.
func (c *Coordinator) ListTakeovers() []TakeoverRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotTakeoversLocked()
}

// RequestTakeover pauses until a human clicks "完成并恢复 AI" or ctx is cancelled.
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
	st := c.screenStateLocked(t)
	list := c.snapshotTakeoversLocked()
	c.mu.Unlock()

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
	targets := make(map[string]struct{})
	for id, w := range c.takeovers {
		if key == "" || id == key || w.req.BotID == key {
			resolved = append(resolved, w)
			targets[w.req.Target] = struct{}{}
			delete(c.takeovers, id)
		}
	}
	if len(resolved) == 0 {
		c.mu.Unlock()
		return false
	}
	var states []ScreenState
	for t := range targets {
		states = append(states, c.screenStateLocked(t))
	}
	for _, w := range resolved {
		select {
		case w.doneCh <- note:
		default:
		}
	}
	list := c.snapshotTakeoversLocked()
	c.mu.Unlock()

	if c.onScreenChange != nil {
		for _, st := range states {
			st := st
			go c.onScreenChange(st)
		}
	}
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
	targets := make(map[string]struct{})
	for id, w := range c.takeovers {
		if w.req.BotID == botID {
			targets[w.req.Target] = struct{}{}
			delete(c.takeovers, id)
			close(w.doneCh)
			changed = true
		}
	}
	var list []TakeoverRequest
	var states []ScreenState
	if changed {
		list = c.snapshotTakeoversLocked()
		for t := range targets {
			states = append(states, c.screenStateLocked(t))
		}
	}
	c.mu.Unlock()
	if changed {
		if c.onScreenChange != nil {
			for _, st := range states {
				st := st
				go c.onScreenChange(st)
			}
		}
		if c.onTakeoverChange != nil {
			go c.onTakeoverChange(list)
		}
	}
}

func (c *Coordinator) removeTakeover(id string) {
	c.mu.Lock()
	w, ok := c.takeovers[id]
	var target string
	if ok {
		target = w.req.Target
	}
	delete(c.takeovers, id)
	var list []TakeoverRequest
	var st ScreenState
	if ok {
		list = c.snapshotTakeoversLocked()
		st = c.screenStateLocked(target)
	}
	c.mu.Unlock()
	if ok {
		if c.onScreenChange != nil {
			go c.onScreenChange(st)
		}
		if c.onTakeoverChange != nil {
			go c.onTakeoverChange(list)
		}
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
