package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
)

var (
	// ErrRoutineAlreadyRunning is returned by RunNow if the target routine is already executing.
	ErrRoutineAlreadyRunning = errors.New("routine is already running")
)

type Notifier interface {
	Notify(ctx context.Context, bot config.BotConfig, sched config.ScheduleConfig, text string) error
}

type RoutineRuntimeStatus struct {
	Running   bool              `json:"running"`
	NextRunAt string            `json:"nextRunAt,omitempty"`
	LastRun   *state.RoutineRun `json:"lastRun,omitempty"`
}

type scheduledJob struct {
	key    string
	botID  string
	spec   config.ScheduleConfig
	cancel context.CancelFunc

	mu     sync.Mutex
	nextAt time.Time
}

func (j *scheduledJob) setNext(t time.Time) {
	j.mu.Lock()
	j.nextAt = t
	j.mu.Unlock()
}

func (j *scheduledJob) getNext() time.Time {
	j.mu.Lock()
	t := j.nextAt
	j.mu.Unlock()
	return t
}

type Scheduler struct {
	hub      *hub.Hub
	notifier Notifier

	mu     sync.Mutex
	ctx    context.Context
	jobs   map[string]*scheduledJob
	active map[string]bool
	wg     sync.WaitGroup
}

func New(h *hub.Hub, notifier Notifier) *Scheduler {
	return &Scheduler{
		hub:      h,
		notifier: notifier,
		jobs:     make(map[string]*scheduledJob),
		active:   make(map[string]bool),
	}
}

func jobKey(botID, routineID string) string {
	return botID + "/" + routineID
}

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	s.Sync()
}

// Sync reconciles active timer goroutines against the hub's current bot schedules.
func (s *Scheduler) Sync() {
	s.mu.Lock()
	rootCtx := s.ctx
	s.mu.Unlock()
	if rootCtx == nil || rootCtx.Err() != nil {
		return
	}

	type wantEntry struct {
		bot  config.BotConfig
		spec config.ScheduleConfig
	}
	desired := make(map[string]wantEntry)
	for _, bot := range s.hub.Bots() {
		for _, sched := range bot.Schedules {
			if !sched.IsEnabled() {
				continue
			}
			hasInterval := sched.EveryMinutes > 0
			hasDaily := parseDailyAt(sched.DailyAt)
			if !hasInterval && !hasDaily {
				continue
			}
			key := jobKey(bot.ID, sched.EffectiveID())
			desired[key] = wantEntry{bot: bot, spec: sched}
		}
	}

	s.mu.Lock()
	if s.ctx == nil || s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	for key, job := range s.jobs {
		want, ok := desired[key]
		if !ok || !reflect.DeepEqual(job.spec, want.spec) {
			job.cancel()
			delete(s.jobs, key)
			log.Printf("[scheduler] unarmed %s", key)
		}
	}
	started := 0
	for key, want := range desired {
		if _, exists := s.jobs[key]; exists {
			continue
		}
		jctx, cancel := context.WithCancel(s.ctx)
		initInterval, initDaily, initNext := s.computeNext(want.bot.ID, want.spec, time.Now())
		job := &scheduledJob{
			key:    key,
			botID:  want.bot.ID,
			spec:   want.spec,
			cancel: cancel,
			nextAt: initNext,
		}
		s.jobs[key] = job
		s.wg.Add(1)
		go s.runJob(jctx, job, initInterval, initDaily, initNext)
		started++
		log.Printf("[scheduler] armed %s (%s)", key, describeSchedule(want.spec))
	}
	total := len(s.jobs)
	s.mu.Unlock()

	if total == 0 && started == 0 {
		log.Printf("[scheduler] no active schedules configured")
	}
}

func (s *Scheduler) Wait() {
	s.wg.Wait()
}

// Status returns runtime scheduling metadata keyed by routine ID for botID.
func (s *Scheduler) Status(botID string) map[string]RoutineRuntimeStatus {
	out := make(map[string]RoutineRuntimeStatus)
	bot, ok := s.hub.GetBot(botID)
	if !ok {
		return out
	}

	s.mu.Lock()
	for _, sc := range bot.Schedules {
		rid := sc.EffectiveID()
		key := jobKey(botID, rid)
		st := RoutineRuntimeStatus{
			Running: s.active[key],
		}
		if job := s.jobs[key]; job != nil {
			if nt := job.getNext(); !nt.IsZero() {
				st.NextRunAt = nt.UTC().Format(time.RFC3339)
			}
		}
		out[rid] = st
	}
	s.mu.Unlock()

	for rid, st := range out {
		if last, ok := s.hub.LatestRoutineRun(botID, rid); ok {
			copyLast := last
			st.LastRun = &copyLast
			out[rid] = st
		}
	}
	return out
}

// RunNow triggers a routine immediately in an isolated turn.
func (s *Scheduler) RunNow(ctx context.Context, botID, routineID string) (*state.RoutineRun, error) {
	bot, ok := s.hub.GetBot(botID)
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, hub.ErrBotNotFound)
	}
	var target *config.ScheduleConfig
	for i := range bot.Schedules {
		if bot.Schedules[i].EffectiveID() == routineID {
			sc := bot.Schedules[i]
			target = &sc
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("routine %q not found: %w", routineID, hub.ErrRoutineNotFound)
	}
	return s.execute(ctx, botID, *target, "manual")
}

func (s *Scheduler) computeNext(botID string, spec config.ScheduleConfig, now time.Time) (intervalNext, dailyNext, next time.Time) {
	if spec.EveryMinutes > 0 {
		intervalNext = now.Add(time.Duration(spec.EveryMinutes) * time.Minute)
	}
	if parseDailyAt(spec.DailyAt) {
		ranToday := s.hub.HasDailyRunOnDate(botID, spec.EffectiveID(), now.In(time.Local).Format("2006-01-02"))
		dailyNext = nextDailyTime(now, spec.DailyAt, ranToday)
	}
	return intervalNext, dailyNext, earliestNonZero(intervalNext, dailyNext)
}

func (s *Scheduler) runJob(ctx context.Context, job *scheduledJob, intervalNext, dailyNext, next time.Time) {
	defer s.wg.Done()

	hasDaily := parseDailyAt(job.spec.DailyAt)
	lastDailyDate := ""
	if hasDaily {
		today := time.Now().In(time.Local).Format("2006-01-02")
		if s.hub.HasDailyRunOnDate(job.botID, job.spec.EffectiveID(), today) {
			lastDailyDate = today
		}
	}

	for {
		if next.IsZero() {
			return
		}
		job.setNext(next)

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
			if !intervalNext.IsZero() && !firedAt.Before(intervalNext) {
				intervalNext = firedAt.Add(time.Duration(job.spec.EveryMinutes) * time.Minute)
			}
			if !dailyNext.IsZero() && !firedAt.Before(dailyNext) {
				lastDailyDate = firedAt.In(time.Local).Format("2006-01-02")
			}
			// Run asynchronously so a slow LLM turn never delays the timer loop.
			s.wg.Add(1)
			go func(spec config.ScheduleConfig) {
				defer s.wg.Done()
				if _, err := s.execute(ctx, job.botID, spec, "schedule"); err != nil && !errors.Is(err, ErrRoutineAlreadyRunning) && ctx.Err() == nil {
					log.Printf("[scheduler] failed %s/%s: %v", job.botID, spec.EffectiveID(), err)
				}
			}(job.spec)

			now := time.Now()
			dailyNext = time.Time{}
			if hasDaily {
				today := now.In(time.Local).Format("2006-01-02")
				ranToday := lastDailyDate == today || s.hub.HasDailyRunOnDate(job.botID, job.spec.EffectiveID(), today)
				if ranToday {
					lastDailyDate = today
				}
				dailyNext = nextDailyTime(now, job.spec.DailyAt, ranToday)
			}
			next = earliestNonZero(intervalNext, dailyNext)
		}
	}
}

func (s *Scheduler) tryStartRun(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[key] {
		return false
	}
	s.active[key] = true
	return true
}

func (s *Scheduler) finishRun(key string) {
	s.mu.Lock()
	delete(s.active, key)
	s.mu.Unlock()
}

func (s *Scheduler) execute(ctx context.Context, botID string, sched config.ScheduleConfig, trigger string) (*state.RoutineRun, error) {
	rid := sched.EffectiveID()
	key := jobKey(botID, rid)
	if !s.tryStartRun(key) {
		return nil, ErrRoutineAlreadyRunning
	}
	defer s.finishRun(key)

	bot, ok := s.hub.GetBot(botID)
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, hub.ErrBotNotFound)
	}

	log.Printf("[scheduler] firing %s/%s (%s)", botID, rid, trigger)
	prompt := fmt.Sprintf(
		"[Scheduled Routine: %s]\n"+
			"You are running inside an isolated, ephemeral routine turn (separate from the main chat session) with your AGENTS.md and memory/.\n"+
			"- Use recall_session(sessionId=\"active\") if you need recent context from the main chat session.\n"+
			"- If there is nothing noteworthy to report or no action is needed, reply with ONLY `NO_REPLY`.\n"+
			"- Otherwise write a concise, actionable update.\n\n%s",
		sched.Name, sched.Prompt,
	)

	started := time.Now()
	res, err := s.hub.RunIsolated(ctx, botID, prompt, hub.IsolatedOptions{
		Source: "routine:" + sched.Name,
		Model:  sched.Model,
	})
	finished := time.Now()

	delivery := sched.EffectiveMainDelivery()
	modelUsed := strings.TrimSpace(sched.Model)
	if modelUsed == "" {
		modelUsed = bot.Model
	}
	run := state.RoutineRun{
		BotID:       botID,
		RoutineID:   rid,
		RoutineName: sched.Name,
		Trigger:     trigger,
		Delivery:    delivery,
		Model:       modelUsed,
		DurationMs:  finished.Sub(started).Milliseconds(),
		StartedAt:   started.UTC().Format(time.RFC3339Nano),
		FinishedAt:  finished.UTC().Format(time.RFC3339Nano),
	}

	if err != nil {
		run.Status = "error"
		run.Error = err.Error()
		if res != nil {
			run.Output = strings.TrimSpace(res.Text)
			run.CostUSD = res.Usage.CostUSD
			run.InputTokens = res.Usage.InputTokens
			run.OutputTokens = res.Usage.OutputTokens
		}
		saved, recErr := s.hub.RecordRoutineRun(run)
		if recErr == nil {
			run = saved
		}
		s.hub.BroadcastRoutineRun(run)
		return &run, err
	}

	run.CostUSD = res.Usage.CostUSD
	run.InputTokens = res.Usage.InputTokens
	run.OutputTokens = res.Usage.OutputTokens
	if res.DurationMs > 0 {
		run.DurationMs = res.DurationMs
	}

	if isSilentOutput(res.Text) {
		run.Status = "silent"
		saved, recErr := s.hub.RecordRoutineRun(run)
		if recErr == nil {
			run = saved
		}
		s.hub.BroadcastRoutineRun(run)
		log.Printf("[scheduler] silent %s/%s in %dms", botID, rid, run.DurationMs)
		return &run, nil
	}

	run.Status = "ok"
	run.Output = strings.TrimSpace(res.Text)

	if delivery == config.RoutineDeliveryNotify {
		msg := fmt.Sprintf("[Routine 通知: %s]\n%s", sched.Name, run.Output)
		if _, sendErr := s.hub.Send(ctx, botID, msg, hub.SendOptions{Source: "routine:" + sched.Name}); sendErr != nil {
			log.Printf("[scheduler] main notify failed %s/%s: %v", botID, rid, sendErr)
		} else {
			run.InjectedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}

	saved, recErr := s.hub.RecordRoutineRun(run)
	if recErr == nil {
		run = saved
	}

	if delivery == config.RoutineDeliveryReceipt {
		s.hub.AppendRoutineReceipt(botID, sched.Name, run.Output, run.DurationMs, run.CostUSD, run.InputTokens, run.OutputTokens)
	}

	s.hub.BroadcastRoutineRun(run)
	log.Printf("[scheduler] done %s/%s in %dms (delivery=%s)", botID, rid, run.DurationMs, delivery)

	if s.notifier != nil && strings.TrimSpace(sched.NotifyTelegramChatID) != "" {
		if nerr := s.notifier.Notify(ctx, bot, sched, run.Output); nerr != nil {
			log.Printf("[scheduler] notify failed %s/%s: %v", botID, rid, nerr)
		}
	}
	return &run, nil
}

func isSilentOutput(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}
	clean := strings.Trim(trimmed, " \t\r\n`\"'.。")
	return strings.EqualFold(clean, "NO_REPLY")
}

func parseDailyAt(v string) bool {
	if len(v) != 5 || v[2] != ':' {
		return false
	}
	hh, err1 := strconv.Atoi(v[:2])
	mm, err2 := strconv.Atoi(v[3:])
	return err1 == nil && err2 == nil && hh >= 0 && hh <= 23 && mm >= 0 && mm <= 59
}

func nextDailyTime(now time.Time, hhmm string, ranToday bool) time.Time {
	hh, _ := strconv.Atoi(hhmm[:2])
	mm, _ := strconv.Atoi(hhmm[3:])
	locNow := now.In(time.Local)
	if !ranToday && locNow.Format("15:04") == hhmm {
		return now
	}
	todayTarget := time.Date(locNow.Year(), locNow.Month(), locNow.Day(), hh, mm, 0, 0, locNow.Location())
	if !ranToday && locNow.Before(todayTarget) {
		return todayTarget
	}
	return todayTarget.AddDate(0, 0, 1)
}

func earliestNonZero(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() || a.Before(b) {
		return a
	}
	return b
}

func describeSchedule(spec config.ScheduleConfig) string {
	var parts []string
	if spec.EveryMinutes > 0 {
		parts = append(parts, fmt.Sprintf("every %dm", spec.EveryMinutes))
	}
	if spec.DailyAt != "" {
		parts = append(parts, "daily at "+spec.DailyAt)
	}
	return strings.Join(parts, ", ")
}
