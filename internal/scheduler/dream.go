package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/state"
)

const (
	memoryDreamBotID     = "_system"
	memoryDreamRoutineID = "memory_dream"
	memoryDreamJobKey    = "_system/memory_dream"
)

type dreamScheduledJob struct {
	dailyAt string
	cancel  context.CancelFunc

	mu     sync.Mutex
	nextAt time.Time
}

func (j *dreamScheduledJob) setNext(t time.Time) {
	j.mu.Lock()
	j.nextAt = t
	j.mu.Unlock()
}

func (j *dreamScheduledJob) getNext() time.Time {
	j.mu.Lock()
	t := j.nextAt
	j.mu.Unlock()
	return t
}

func (s *Scheduler) syncDreamJobLocked(memCfgEnabled bool, dailyAt string) {
	wantDream := memCfgEnabled && parseDailyAt(dailyAt)
	if s.dreamJob != nil && (!wantDream || s.dreamJob.dailyAt != dailyAt) {
		s.dreamJob.cancel()
		s.dreamJob = nil
		log.Printf("[scheduler] unarmed memory dream")
	}
	if !wantDream || s.dreamJob != nil {
		return
	}
	dctx, cancel := context.WithCancel(s.ctx)
	now := s.currentTime()
	today := now.In(time.Local).Format("2006-01-02")
	ranToday := s.hub.HasDailyRunOnDate(memoryDreamBotID, memoryDreamRoutineID, today)
	initNext := nextDailyTime(now, dailyAt, ranToday)
	dj := &dreamScheduledJob{
		dailyAt: dailyAt,
		cancel:  cancel,
		nextAt:  initNext,
	}
	s.dreamJob = dj
	s.wg.Add(1)
	go s.runDreamJob(dctx, dj, initNext)
	log.Printf("[scheduler] armed memory dream (daily at %s)", dailyAt)
}

// DreamNextRunAt returns the next scheduled memory Dream execution time, or zero if unarmed.
func (s *Scheduler) DreamNextRunAt() time.Time {
	s.mu.Lock()
	dj := s.dreamJob
	s.mu.Unlock()
	if dj == nil {
		return time.Time{}
	}
	return dj.getNext()
}

func (s *Scheduler) runDreamJob(ctx context.Context, dj *dreamScheduledJob, next time.Time) {
	defer s.wg.Done()

	lastDailyDate := ""
	today := s.currentTime().In(time.Local).Format("2006-01-02")
	if s.hub.HasDailyRunOnDate(memoryDreamBotID, memoryDreamRoutineID, today) {
		lastDailyDate = today
	}

	for {
		if next.IsZero() {
			return
		}
		dj.setNext(next)

		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case firedAt := <-timer.C:
			lastDailyDate = firedAt.In(time.Local).Format("2006-01-02")
			scheduledAt := next
			s.wg.Add(1)
			go func(schedAt time.Time) {
				defer s.wg.Done()
				if _, err := s.executeMemoryDream(ctx, schedAt); err != nil && !errors.Is(err, ErrRoutineAlreadyRunning) && ctx.Err() == nil {
					log.Printf("[scheduler] memory dream failed: %v", err)
				}
			}(scheduledAt)

			now := s.currentTime()
			curDay := now.In(time.Local).Format("2006-01-02")
			ranToday := lastDailyDate == curDay || s.hub.HasDailyRunOnDate(memoryDreamBotID, memoryDreamRoutineID, curDay)
			if ranToday {
				lastDailyDate = curDay
			}
			next = nextDailyTime(now, dj.dailyAt, ranToday)
		}
	}
}

func (s *Scheduler) executeMemoryDream(ctx context.Context, scheduledAt time.Time) (bool, error) {
	if !s.tryStartRun(memoryDreamJobKey) {
		return false, ErrRoutineAlreadyRunning
	}
	defer s.finishRun(memoryDreamJobKey)

	memCfg := s.hub.MemoryConfig()
	if !memCfg.Enabled || !memCfg.Dream.Enabled {
		return false, nil
	}

	slotAt := scheduledAt
	if slotAt.IsZero() {
		slotAt = s.currentTime()
	}
	slotKey := fmt.Sprintf("%s:%s:%s", memoryDreamBotID, memoryDreamRoutineID, slotAt.UTC().Format("2006-01-02T15:04"))
	dreamSched := config.ScheduleConfig{
		ID:             memoryDreamRoutineID,
		Name:           "Memory Dream",
		DailyAt:        memCfg.Dream.DailyAt,
		FailoverPolicy: config.RoutineFailoverAuto,
	}
	if !s.shouldRunScheduled(ctx, dreamSched, slotKey) {
		return false, nil
	}

	log.Printf("[scheduler] firing system memory dream")
	started := s.currentTime()
	results, err := s.hub.RunMemoryDream(ctx, "", false)
	finished := s.currentTime()

	var summaries []string
	var errMsgs []string
	for _, r := range results {
		if r.Error != "" {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", r.BotID, r.Error))
		} else if !r.Skipped && r.Summary != "" {
			summaries = append(summaries, fmt.Sprintf("%s: %s", r.BotID, r.Summary))
		}
	}
	status := "ok"
	errStr := ""
	if err != nil {
		status = "error"
		errStr = err.Error()
	} else if len(errMsgs) > 0 {
		status = "error"
		errStr = strings.Join(errMsgs, "; ")
	}
	_, _ = s.hub.RecordRoutineRun(state.RoutineRun{
		BotID:       memoryDreamBotID,
		RoutineID:   memoryDreamRoutineID,
		RoutineName: "Memory Dream",
		Trigger:     "schedule",
		Status:      status,
		Delivery:    config.RoutineDeliveryNone,
		Model:       memCfg.Dream.Model,
		Output:      strings.Join(summaries, "\n"),
		Error:       errStr,
		DurationMs:  finished.Sub(started).Milliseconds(),
		StartedAt:   started.UTC().Format(time.RFC3339Nano),
		FinishedAt:  finished.UTC().Format(time.RFC3339Nano),
		SlotKey:     slotKey,
	})
	return true, err
}
