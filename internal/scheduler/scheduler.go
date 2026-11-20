package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/hub"
)

type Notifier interface {
	Notify(ctx context.Context, bot config.BotConfig, sched config.ScheduleConfig, text string) error
}

type Scheduler struct {
	hub      *hub.Hub
	notifier Notifier
	wg       sync.WaitGroup
}

func New(h *hub.Hub, notifier Notifier) *Scheduler {
	return &Scheduler{hub: h, notifier: notifier}
}

func (s *Scheduler) Start(ctx context.Context) {
	count := 0
	for _, bot := range s.hub.Bots() {
		for _, sched := range bot.Schedules {
			if !sched.IsEnabled() {
				continue
			}
			if sched.EveryMinutes > 0 {
				count++
				s.wg.Add(1)
				go s.runInterval(ctx, bot, sched)
				log.Printf("[scheduler] armed %s/%s every %dm", bot.ID, sched.Name, sched.EveryMinutes)
			}
			if len(sched.DailyAt) == 5 && sched.DailyAt[2] == ':' {
				count++
				s.wg.Add(1)
				go s.runDaily(ctx, bot, sched)
				log.Printf("[scheduler] armed %s/%s daily at %s", bot.ID, sched.Name, sched.DailyAt)
			}
		}
	}
	if count == 0 {
		log.Printf("[scheduler] no active schedules configured")
	}
}

func (s *Scheduler) Wait() {
	s.wg.Wait()
}

func (s *Scheduler) runInterval(ctx context.Context, bot config.BotConfig, sched config.ScheduleConfig) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Duration(sched.EveryMinutes) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.fire(ctx, bot, sched)
		}
	}
}

func (s *Scheduler) runDaily(ctx context.Context, bot config.BotConfig, sched config.ScheduleConfig) {
	defer s.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	lastDay := ""
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			hhmm := now.Format("15:04")
			day := now.Format("2006-01-02")
			if hhmm == sched.DailyAt && lastDay != day {
				lastDay = day
				s.fire(ctx, bot, sched)
			}
		}
	}
}

func (s *Scheduler) fire(ctx context.Context, bot config.BotConfig, sched config.ScheduleConfig) {
	log.Printf("[scheduler] firing %s/%s", bot.ID, sched.Name)
	prompt := fmt.Sprintf(
		"[Scheduled Routine: %s]\nNote: Keep the main session responsive — delegate heavy/slow work to a subagent if needed.\n\n%s",
		sched.Name, sched.Prompt,
	)
	res, err := s.hub.Send(ctx, bot.ID, prompt, hub.SendOptions{Source: "schedule:" + sched.Name})
	if err != nil {
		log.Printf("[scheduler] failed %s/%s: %v", bot.ID, sched.Name, err)
		return
	}
	log.Printf("[scheduler] done %s/%s in %dms (session=%s)", bot.ID, sched.Name, res.DurationMs, res.SessionID)
	if s.notifier != nil && res.Text != "" {
		if err := s.notifier.Notify(ctx, bot, sched, res.Text); err != nil {
			log.Printf("[scheduler] notify failed %s/%s: %v", bot.ID, sched.Name, err)
		}
	}
}
