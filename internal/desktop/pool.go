package desktop

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// VirtualSlotSnapshot describes an active virtual screen slot in an EnginePool.
type VirtualSlotSnapshot struct {
	Slot       string     `json:"slot"`
	Target     string     `json:"target"`
	PowerState PowerState `json:"powerState"`
	Virtual    bool       `json:"virtual"`
	KeepAwake  bool       `json:"keepAwake"`
}

// EnginePool manages the default desktop Engine alongside optional per-slot virtual Engines (Linux).
type EnginePool struct {
	cfg        VirtualConfig
	newBackend func() virtualBackend
	defaultEng *Engine

	mu          sync.Mutex
	slots       map[string]*Engine
	hasTakeover func(target string) bool
}

// NewEnginePool creates an EnginePool using the platform virtual display backend.
func NewEnginePool(cfg VirtualConfig) *EnginePool {
	return NewEnginePoolWithFactory(cfg, nil)
}

// NewEnginePoolWithFactory creates an EnginePool with a custom virtualBackend factory (used in tests).
func NewEnginePoolWithFactory(cfg VirtualConfig, factory func() virtualBackend) *EnginePool {
	var defBackend virtualBackend
	if factory != nil {
		defBackend = factory()
	}
	return &EnginePool{
		cfg:        cfg,
		newBackend: factory,
		defaultEng: NewEngineWithBackend(cfg, defBackend),
		slots:      make(map[string]*Engine),
	}
}

// Default returns the default host desktop Engine ("local").
func (p *EnginePool) Default() *Engine {
	if p == nil {
		return nil
	}
	return p.defaultEng
}

// VirtualSupported reports whether the host supports managed virtual screens.
func (p *EnginePool) VirtualSupported() bool {
	if p == nil || p.defaultEng == nil {
		return false
	}
	return p.defaultEng.VirtualSupported()
}

// SetHasTakeover registers a predicate reporting whether target currently has an active human takeover.
func (p *EnginePool) SetHasTakeover(fn func(target string) bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.hasTakeover = fn
	slotsCopy := make(map[string]*Engine, len(p.slots))
	for k, v := range p.slots {
		slotsCopy[k] = v
	}
	p.mu.Unlock()

	if p.defaultEng != nil {
		if fn != nil {
			p.defaultEng.SetHasTakeover(func() bool { return fn("local") })
		} else {
			p.defaultEng.SetHasTakeover(nil)
		}
	}
	for slot, eng := range slotsCopy {
		target := "virtual:" + slot
		if fn != nil {
			eng.SetHasTakeover(func() bool { return fn(target) })
		} else {
			eng.SetHasTakeover(nil)
		}
	}
}

// ForSlot returns the Engine for a virtual slot (creating it on demand) or Default() when slot is empty.
func (p *EnginePool) ForSlot(slot string) (*Engine, error) {
	if p == nil {
		return nil, errors.New("desktop engine unavailable")
	}
	s := strings.TrimSpace(slot)
	if s == "" {
		return p.defaultEng, nil
	}
	if !p.VirtualSupported() {
		return nil, errors.New("当前主机不支持独立虚拟屏幕（仅 Linux 支持多虚拟屏）")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if eng, ok := p.slots[s]; ok && eng != nil {
		return eng, nil
	}
	slotCfg := p.cfg
	slotCfg.Mode = "virtual"
	var backend virtualBackend
	if p.newBackend != nil {
		backend = p.newBackend()
	}
	eng := NewEngineWithBackend(slotCfg, backend)
	if fn := p.hasTakeover; fn != nil {
		target := "virtual:" + s
		eng.SetHasTakeover(func() bool { return fn(target) })
	}
	if p.slots == nil {
		p.slots = make(map[string]*Engine)
	}
	p.slots[s] = eng
	return eng, nil
}

// PeekSlot returns an existing slot Engine without creating a new one.
func (p *EnginePool) PeekSlot(slot string) *Engine {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(slot)
	if s == "" {
		return p.defaultEng
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.slots[s]
}

// SlotEnv returns active environment variables (e.g. DISPLAY) for a slot without waking a stopped desktop.
func (p *EnginePool) SlotEnv(slot string) []string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(slot)
	if s == "" {
		if p.defaultEng != nil {
			return p.defaultEng.Env()
		}
		return nil
	}
	eng := p.PeekSlot(s)
	if eng == nil {
		return nil
	}
	return eng.Env()
}

// ListSlots returns sorted snapshots of all initialized virtual screen slots.
func (p *EnginePool) ListSlots() []VirtualSlotSnapshot {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	keys := make([]string, 0, len(p.slots))
	for k := range p.slots {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	engines := make([]*Engine, len(keys))
	for i, k := range keys {
		engines[i] = p.slots[k]
	}
	p.mu.Unlock()

	out := make([]VirtualSlotSnapshot, 0, len(keys))
	for i, k := range keys {
		vst := engines[i].VirtualStatus()
		out = append(out, VirtualSlotSnapshot{
			Slot:       k,
			Target:     "virtual:" + k,
			PowerState: vst.PowerState,
			Virtual:    vst.Virtual,
			KeepAwake:  vst.KeepAwake,
		})
	}
	return out
}

// RemoveSlot closes and removes a virtual slot engine if present.
func (p *EnginePool) RemoveSlot(slot string) {
	if p == nil {
		return
	}
	s := strings.TrimSpace(slot)
	if s == "" {
		return
	}
	p.mu.Lock()
	eng := p.slots[s]
	delete(p.slots, s)
	p.mu.Unlock()
	if eng != nil {
		eng.Close()
	}
}

// Close closes the default engine and all virtual slot engines.
func (p *EnginePool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	slots := make([]*Engine, 0, len(p.slots))
	for k, eng := range p.slots {
		slots = append(slots, eng)
		delete(p.slots, k)
	}
	p.mu.Unlock()

	for _, eng := range slots {
		if eng != nil {
			eng.Close()
		}
	}
	if p.defaultEng != nil {
		p.defaultEng.Close()
	}
}
