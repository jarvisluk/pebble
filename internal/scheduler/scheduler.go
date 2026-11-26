package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
	"bot-hub/internal/tunnel"
	"github.com/robfig/cron/v3"
)

var (
	// ErrRoutineAlreadyRunning is returned by RunNow if the target routine is already executing.
	ErrRoutineAlreadyRunning = errors.New("routine is already running")
)

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
	hub *hub.Hub

	mu     sync.Mutex
	ctx    context.Context
	jobs   map[string]*scheduledJob
	active map[string]bool
	wg     sync.WaitGroup

	probeMu          sync.Mutex
	primaryProbe     func(context.Context) bool
	peerProbe        func(context.Context, string) bool
	lastHigherAlive  time.Time
	nowFn            func() time.Time
}

func New(h *hub.Hub) *Scheduler {
	return &Scheduler{
		hub:    h,
		jobs:   make(map[string]*scheduledJob),
		active: make(map[string]bool),
		nowFn:  time.Now,
	}
}

// SetPrimaryProbe overrides the liveness probe used by standby hosts before failing over a routine.
func (s *Scheduler) SetPrimaryProbe(fn func(context.Context) bool) {
	s.probeMu.Lock()
	s.primaryProbe = fn
	s.probeMu.Unlock()
}

// SetPeerProbe overrides the per-peer liveness probe used when checking higher-priority hosts.
func (s *Scheduler) SetPeerProbe(fn func(context.Context, string) bool) {
	s.probeMu.Lock()
	s.peerProbe = fn
	s.probeMu.Unlock()
}

func (s *Scheduler) currentTime() time.Time {
	s.probeMu.Lock()
	fn := s.nowFn
	s.probeMu.Unlock()
	if fn != nil {
		return fn()
	}
	return time.Now()
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
			hasCron := false
			if strings.TrimSpace(sched.Cron) != "" {
				if _, err := config.ParseCron(sched.Cron); err == nil {
					hasCron = true
				}
			}
			if !hasInterval && !hasDaily && !hasCron {
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
		initInterval, initDaily, initCron, initNext := s.computeNext(want.bot.ID, want.spec, time.Now())
		job := &scheduledJob{
			key:    key,
			botID:  want.bot.ID,
			spec:   want.spec,
			cancel: cancel,
			nextAt: initNext,
		}
		s.jobs[key] = job
		s.wg.Add(1)
		go s.runJob(jctx, job, initInterval, initDaily, initCron, initNext)
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
	return s.trigger(ctx, botID, routineID, "manual", "")
}

// TriggerWebhook triggers a routine via an inbound webhook with an optional payload.
func (s *Scheduler) TriggerWebhook(ctx context.Context, botID, routineID, payload string) (*state.RoutineRun, error) {
	return s.trigger(ctx, botID, routineID, "webhook", payload)
}

func (s *Scheduler) trigger(ctx context.Context, botID, routineID, triggerName, webhookPayload string) (*state.RoutineRun, error) {
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
	return s.execute(ctx, botID, *target, triggerName, webhookPayload)
}

func (s *Scheduler) computeNext(botID string, spec config.ScheduleConfig, now time.Time) (intervalNext, dailyNext, cronNext, next time.Time) {
	if spec.EveryMinutes > 0 {
		intervalNext = now.Add(time.Duration(spec.EveryMinutes) * time.Minute)
	}
	if parseDailyAt(spec.DailyAt) {
		ranToday := s.hub.HasDailyRunOnDate(botID, spec.EffectiveID(), now.In(time.Local).Format("2006-01-02"))
		dailyNext = nextDailyTime(now, spec.DailyAt, ranToday)
	}
	if strings.TrimSpace(spec.Cron) != "" {
		if cs, err := config.ParseCron(spec.Cron); err == nil {
			cronNext = cs.Next(now.In(time.Local))
		}
	}
	return intervalNext, dailyNext, cronNext, earliestNonZero(intervalNext, dailyNext, cronNext)
}

func (s *Scheduler) runJob(ctx context.Context, job *scheduledJob, intervalNext, dailyNext, cronNext, next time.Time) {
	defer s.wg.Done()

	hasDaily := parseDailyAt(job.spec.DailyAt)
	lastDailyDate := ""
	if hasDaily {
		today := time.Now().In(time.Local).Format("2006-01-02")
		if s.hub.HasDailyRunOnDate(job.botID, job.spec.EffectiveID(), today) {
			lastDailyDate = today
		}
	}
	var cronSched cron.Schedule
	if strings.TrimSpace(job.spec.Cron) != "" {
		if cs, err := config.ParseCron(job.spec.Cron); err == nil {
			cronSched = cs
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
			if cronSched != nil && !cronNext.IsZero() && !firedAt.Before(cronNext) {
				cronNext = cronSched.Next(firedAt.In(time.Local))
			}
			// Run asynchronously so a slow LLM turn never delays the timer loop.
			scheduledAt := next
			s.wg.Add(1)
			go func(spec config.ScheduleConfig, schedAt time.Time) {
				defer s.wg.Done()
				if _, err := s.executeWithSlot(ctx, job.botID, spec, "schedule", "", schedAt); err != nil && !errors.Is(err, ErrRoutineAlreadyRunning) && ctx.Err() == nil {
					log.Printf("[scheduler] failed %s/%s: %v", job.botID, spec.EffectiveID(), err)
				}
			}(job.spec, scheduledAt)

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
			next = earliestNonZero(intervalNext, dailyNext, cronNext)
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

func (s *Scheduler) execute(ctx context.Context, botID string, sched config.ScheduleConfig, trigger, webhookPayload string) (*state.RoutineRun, error) {
	return s.executeWithSlot(ctx, botID, sched, trigger, webhookPayload, time.Time{})
}

func (s *Scheduler) executeWithSlot(ctx context.Context, botID string, sched config.ScheduleConfig, trigger, webhookPayload string, scheduledAt time.Time) (*state.RoutineRun, error) {
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

	slotAt := scheduledAt
	if slotAt.IsZero() {
		slotAt = s.currentTime()
	}
	slotKey := ""
	if trigger == "schedule" {
		slotKey = fmt.Sprintf("%s:%s:%s", botID, rid, slotAt.UTC().Format("2006-01-02T15:04"))
		if !s.shouldRunScheduled(ctx, sched, slotKey) {
			return nil, nil
		}
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
	if strings.TrimSpace(webhookPayload) != "" {
		prompt += "\n\n[Webhook Payload]\n" + strings.TrimSpace(webhookPayload)
	}

	delivery := sched.EffectiveMainDelivery()
	taskDelivery := state.TaskDeliveryReceipt
	switch delivery {
	case config.RoutineDeliveryNotify:
		taskDelivery = state.TaskDeliveryWake
	case config.RoutineDeliveryNone:
		taskDelivery = state.TaskDeliverySilent
	}
	modelUsed := strings.TrimSpace(sched.Model)
	if modelUsed == "" {
		modelUsed = bot.Model
	}

	started := time.Now()
	task, err := s.hub.SpawnTask(ctx, hub.TaskSpawnRequest{
		BotID:           botID,
		Kind:            state.TaskKindRoutine,
		Title:           sched.Name,
		Prompt:          prompt,
		Model:           sched.Model,
		PermissionMode:  sched.PermissionMode,
		Delivery:        taskDelivery,
		RoutineID:       rid,
		RoutineName:     sched.Name,
		Trigger:         trigger,
		SlotKey:         slotKey,
		ManagedDelivery: true,
	})
	if err != nil {
		finished := time.Now()
		run := state.RoutineRun{
			BotID:       botID,
			RoutineID:   rid,
			RoutineName: sched.Name,
			Trigger:     trigger,
			Status:      "error",
			Delivery:    delivery,
			Model:       modelUsed,
			Error:       err.Error(),
			DurationMs:  finished.Sub(started).Milliseconds(),
			StartedAt:   started.UTC().Format(time.RFC3339Nano),
			FinishedAt:  finished.UTC().Format(time.RFC3339Nano),
			SlotKey:     slotKey,
		}
		saved, recErr := s.hub.RecordRoutineRun(run)
		if recErr == nil {
			run = saved
		}
		s.hub.BroadcastRoutineRun(run)
		return &run, err
	}

	finishedTask, waitErr := s.hub.WaitTask(ctx, task.ID)
	if waitErr != nil {
		_, _ = s.hub.CancelTask(task.ID)
		finished := time.Now()
		run := state.RoutineRun{
			ID:          task.ID,
			BotID:       botID,
			RoutineID:   rid,
			RoutineName: sched.Name,
			Trigger:     trigger,
			Status:      "error",
			Delivery:    delivery,
			Model:       modelUsed,
			Error:       waitErr.Error(),
			DurationMs:  finished.Sub(started).Milliseconds(),
			StartedAt:   started.UTC().Format(time.RFC3339Nano),
			FinishedAt:  finished.UTC().Format(time.RFC3339Nano),
			SlotKey:     slotKey,
		}
		saved, recErr := s.hub.RecordRoutineRun(run)
		if recErr == nil {
			run = saved
		}
		s.hub.BroadcastRoutineRun(run)
		return &run, waitErr
	}

	finished := time.Now()
	runStartedAt := finishedTask.StartedAt
	if runStartedAt == "" {
		runStartedAt = started.UTC().Format(time.RFC3339Nano)
	}
	runFinishedAt := finishedTask.FinishedAt
	if runFinishedAt == "" {
		runFinishedAt = finished.UTC().Format(time.RFC3339Nano)
	}
	durMs := finishedTask.DurationMs
	if durMs <= 0 {
		durMs = finished.Sub(started).Milliseconds()
	}

	run := state.RoutineRun{
		ID:           finishedTask.ID,
		BotID:        botID,
		RoutineID:    rid,
		RoutineName:  sched.Name,
		Trigger:      trigger,
		Delivery:     delivery,
		Model:        modelUsed,
		DurationMs:   durMs,
		CostUSD:      finishedTask.CostUSD,
		InputTokens:  finishedTask.InputTokens,
		OutputTokens: finishedTask.OutputTokens,
		StartedAt:    runStartedAt,
		FinishedAt:   runFinishedAt,
		SlotKey:      slotKey,
	}

	if finishedTask.Status != state.TaskStatusCompleted {
		errText := finishedTask.Error
		if errText == "" {
			errText = "routine " + finishedTask.Status
		}
		run.Status = "error"
		run.Error = errText
		run.Output = strings.TrimSpace(finishedTask.Output)
		saved, recErr := s.hub.RecordRoutineRun(run)
		if recErr == nil {
			run = saved
		}
		s.hub.BroadcastRoutineRun(run)
		return &run, errors.New(errText)
	}

	if isSilentOutput(finishedTask.Output) {
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
	run.Output = strings.TrimSpace(finishedTask.Output)

	if delivery == config.RoutineDeliveryNotify {
		msg := fmt.Sprintf("[Routine notification: %s]\n%s", sched.Name, run.Output)
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

	if strings.TrimSpace(sched.WebhookURL) != "" {
		if werr := postWebhook(ctx, bot, sched, run); werr != nil {
			log.Printf("[scheduler] webhook post failed %s/%s: %v", botID, rid, werr)
		}
	}
	return &run, nil
}

func (s *Scheduler) shouldRunScheduled(ctx context.Context, sched config.ScheduleConfig, slotKey string) bool {
	cfg := s.hub.Config()
	if cfg == nil {
		return true
	}
	higherPeers := cfg.Sync.HigherPriorityPeers()
	if cfg.Sync.EffectiveRole() != "standby" && len(higherPeers) == 0 {
		return true
	}
	if sched.EffectiveFailoverPolicy() != config.RoutineFailoverAuto {
		log.Printf("[scheduler] standby skipped %s (failoverPolicy=%s)", sched.EffectiveID(), sched.EffectiveFailoverPolicy())
		return false
	}
	if store := s.hub.Store(); store != nil && store.HasRoutineSlotKey(slotKey) {
		log.Printf("[scheduler] standby skipped %s (slotKey=%s already recorded)", sched.EffectiveID(), slotKey)
		return false
	}

	s.probeMu.Lock()
	peerProbe := s.peerProbe
	primaryProbe := s.primaryProbe
	s.probeMu.Unlock()

	pctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	anyHigherAlive := false
	if peerProbe != nil && len(higherPeers) > 0 {
		for _, peer := range higherPeers {
			if peerProbe(pctx, peer.URL) {
				anyHigherAlive = true
				break
			}
		}
	} else if primaryProbe != nil {
		anyHigherAlive = primaryProbe(pctx)
	} else if len(higherPeers) > 0 {
		for _, peer := range higherPeers {
			if probeHealthz(pctx, peer.URL) {
				anyHigherAlive = true
				break
			}
		}
	} else if cfg.Sync != nil && cfg.Sync.PeerURL != "" {
		anyHigherAlive = probeHealthz(pctx, cfg.Sync.PeerURL)
	}
	cancel()

	now := s.currentTime()
	s.probeMu.Lock()
	if anyHigherAlive {
		s.lastHigherAlive = now
		s.probeMu.Unlock()
		return false
	}
	if s.lastHigherAlive.IsZero() {
		s.lastHigherAlive = now
		s.probeMu.Unlock()
		log.Printf("[scheduler] standby waiting for failover timeout before taking over %s", sched.EffectiveID())
		return false
	}
	elapsed := now.Sub(s.lastHigherAlive)
	s.probeMu.Unlock()

	if elapsed < cfg.Sync.EffectiveFailoverTimeout() {
		return false
	}
	log.Printf("[scheduler] host taking over routine %s (higher-priority peers unreachable for %s)", sched.EffectiveID(), elapsed.Round(time.Second))
	return true
}

func probeHealthz(ctx context.Context, peerURL string) bool {
	peerURL = strings.TrimSpace(peerURL)
	if peerURL == "" {
		return false
	}
	cli := &http.Client{Timeout: 3 * time.Second}
	healthURL := strings.TrimRight(peerURL, "/") + "/healthz"
	if strings.HasPrefix(peerURL, "tailcat://") {
		link, _ := tunnel.ExtractTokenFromLink(peerURL)
		dial, closeClient, err := tunnel.Connect(ctx, link)
		if err != nil {
			return false
		}
		defer func() { _ = closeClient() }()
		cli = &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				DialContext: func(dctx context.Context, _, _ string) (net.Conn, error) { return dial(dctx) },
			},
		}
		healthURL = "http://tailcat.local/healthz"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	resp, err := cli.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func isSilentOutput(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}
	clean := strings.Trim(trimmed, " \t\r\n`\"'.。") // i18n:allow-cjk
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

func earliestNonZero(times ...time.Time) time.Time {
	var earliest time.Time
	for _, t := range times {
		if t.IsZero() {
			continue
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}

func describeSchedule(spec config.ScheduleConfig) string {
	var parts []string
	if spec.EveryMinutes > 0 {
		parts = append(parts, fmt.Sprintf("every %dm", spec.EveryMinutes))
	}
	if spec.DailyAt != "" {
		parts = append(parts, "daily at "+spec.DailyAt)
	}
	if strings.TrimSpace(spec.Cron) != "" {
		parts = append(parts, "cron "+strings.TrimSpace(spec.Cron))
	}
	if spec.WebhookTrigger {
		parts = append(parts, "webhook")
	}
	return strings.Join(parts, ", ")
}

var webhookHTTPClient = &http.Client{Timeout: 15 * time.Second}

func postWebhook(ctx context.Context, bot config.BotConfig, sched config.ScheduleConfig, run state.RoutineRun) error {
	rawURL := strings.TrimSpace(sched.WebhookURL)
	if rawURL == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	host := rawURL
	if err == nil && parsed.Host != "" {
		host = parsed.Host
	}

	payload := map[string]any{
		"event":       "routine.completed",
		"botId":       bot.ID,
		"botName":     bot.Name,
		"routineId":   sched.EffectiveID(),
		"routineName": sched.Name,
		"trigger":     run.Trigger,
		"output":      run.Output,
		"text":        fmt.Sprintf("[%s]\n%s", sched.Name, run.Output),
		"durationMs":  run.DurationMs,
		"finishedAt":  run.FinishedAt,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request to %s: %w", host, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := webhookHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("post to %s: %w", host, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("host %s returned HTTP %d", host, resp.StatusCode)
	}
	return nil
}
