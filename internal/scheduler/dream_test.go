package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"bot-hub/internal/hub"
)

func TestSchedulerDreamTimerSyncAndFailover(t *testing.T) {
	h := newTestFailoverHub(t, "standby", 60)
	s := New(h)

	var clockMu sync.Mutex
	now := time.Date(2026, 11, 27, 5, 15, 0, 0, time.UTC)
	s.nowFn = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}
	advance := func(d time.Duration) time.Time {
		clockMu.Lock()
		now = now.Add(d)
		cur := now
		clockMu.Unlock()
		return cur
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		s.Wait()
	}()
	h.SetOnSchedulesChanged(s.Sync)
	s.Start(ctx)

	// Armed by default at 03:30.
	firstNext := s.DreamNextRunAt()
	if firstNext.IsZero() {
		t.Fatal("expected system Dream timer armed on Start")
	}

	// Updating DailyAt via SetMemoryConfig re-arms Dream timer via Sync().
	newTime := "05:15"
	if err := h.SetMemoryConfig(hub.MemoryConfigPatch{
		Dream: &hub.MemoryDreamConfigPatch{DailyAt: &newTime},
	}); err != nil {
		t.Fatal(err)
	}
	secondNext := s.DreamNextRunAt()
	if secondNext.IsZero() || secondNext.In(time.Local).Format("15:04") != "05:15" {
		t.Fatalf("expected Dream timer re-armed to 05:15, got %v", secondNext)
	}

	// Standby failover guard: while primary is alive, executeMemoryDream skips.
	var probeMu sync.Mutex
	primaryAlive := true
	s.SetPrimaryProbe(func(context.Context) bool {
		probeMu.Lock()
		defer probeMu.Unlock()
		return primaryAlive
	})
	setPrimaryAlive := func(v bool) {
		probeMu.Lock()
		primaryAlive = v
		probeMu.Unlock()
	}

	ran, err := s.executeMemoryDream(ctx, s.currentTime())
	if err != nil || ran {
		t.Fatalf("expected standby Dream skipped while primary alive, got ran=%v err=%v", ran, err)
	}

	// Primary goes down; before timeout still skips; after failover timeout takes over.
	setPrimaryAlive(false)
	cur := advance(30 * time.Second)
	ran, err = s.executeMemoryDream(ctx, cur)
	if err != nil || ran {
		t.Fatalf("expected standby Dream wait before failover timeout, got ran=%v err=%v", ran, err)
	}

	cur = advance(40 * time.Second)
	ran, err = s.executeMemoryDream(ctx, cur)
	if err != nil || !ran {
		t.Fatalf("expected standby Dream takeover after failover timeout, got ran=%v err=%v", ran, err)
	}

	// Same slot minute deduplicated via SlotKey.
	ran2, err := s.executeMemoryDream(ctx, cur)
	if err != nil || ran2 {
		t.Fatalf("expected duplicate Dream slot skipped, got ran=%v err=%v", ran2, err)
	}

	// Disabling Dream unarms the timer.
	disabled := false
	if err := h.SetMemoryConfig(hub.MemoryConfigPatch{
		Dream: &hub.MemoryDreamConfigPatch{Enabled: &disabled},
	}); err != nil {
		t.Fatal(err)
	}
	if next := s.DreamNextRunAt(); !next.IsZero() {
		t.Fatalf("expected Dream timer unarmed when disabled, got %v", next)
	}
}
