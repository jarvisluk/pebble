package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/hub"
)

type TelegramManager struct {
	hub     *hub.Hub
	clients map[string]*telegramPoller
	wg      sync.WaitGroup
}

func NewTelegramManager(h *hub.Hub) *TelegramManager {
	m := &TelegramManager{
		hub:     h,
		clients: make(map[string]*telegramPoller),
	}
	for _, bot := range h.Bots() {
		if bot.Telegram == nil || strings.TrimSpace(bot.Telegram.Token) == "" {
			continue
		}
		if !hasNonEmpty(bot.Telegram.AllowedUserIDs) && !hasNonEmpty(bot.Telegram.AllowedChatIDs) {
			log.Printf("[telegram:%s] token set but allowedUserIds/allowedChatIds empty; refusing to start (would accept anyone)", bot.ID)
			continue
		}
		m.clients[bot.ID] = newTelegramPoller(h, bot)
	}
	return m
}

func (m *TelegramManager) Start(ctx context.Context) {
	if len(m.clients) == 0 {
		log.Printf("[telegram] no telegram tokens configured (skipping)")
		return
	}
	for _, p := range m.clients {
		m.wg.Add(1)
		go func(poller *telegramPoller) {
			defer m.wg.Done()
			poller.run(ctx)
		}(p)
	}
}

func (m *TelegramManager) Wait() {
	m.wg.Wait()
}

// Notify implements scheduler.Notifier so scheduled routines can push updates to Telegram.
func (m *TelegramManager) Notify(ctx context.Context, bot config.BotConfig, sched config.ScheduleConfig, text string) error {
	if sched.NotifyTelegramChatID == "" {
		return nil
	}
	poller, ok := m.clients[bot.ID]
	if !ok {
		return fmt.Errorf("telegram not configured for bot %q", bot.ID)
	}
	chatID, err := strconv.ParseInt(sched.NotifyTelegramChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid notifyTelegramChatId %q: %w", sched.NotifyTelegramChatID, err)
	}
	return poller.sendMessage(ctx, chatID, fmt.Sprintf("[%s]\n%s", sched.Name, text))
}

type telegramPoller struct {
	hub        *hub.Hub
	bot        config.BotConfig
	apiBase    string
	httpClient *http.Client
	allowedUID map[string]struct{}
	allowedCID map[string]struct{}
}

func newTelegramPoller(h *hub.Hub, bot config.BotConfig) *telegramPoller {
	uidSet := make(map[string]struct{})
	for _, id := range bot.Telegram.AllowedUserIDs {
		if s := strings.TrimSpace(id); s != "" {
			uidSet[s] = struct{}{}
		}
	}
	cidSet := make(map[string]struct{})
	for _, id := range bot.Telegram.AllowedChatIDs {
		if s := strings.TrimSpace(id); s != "" {
			cidSet[s] = struct{}{}
		}
	}
	return &telegramPoller{
		hub:        h,
		bot:        bot,
		apiBase:    "https://api.telegram.org/bot" + strings.TrimSpace(bot.Telegram.Token),
		httpClient: &http.Client{Timeout: 45 * time.Second},
		allowedUID: uidSet,
		allowedCID: cidSet,
	}
}

type tgUpdatesResp struct {
	OK     bool       `json:"ok"`
	Result []tgUpdate `json:"result"`
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From *struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
}

func (p *telegramPoller) run(ctx context.Context) {
	log.Printf("[telegram:%s] starting long polling", p.bot.ID)
	var offset int64
	for {
		if ctx.Err() != nil {
			return
		}
		updates, err := p.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[telegram:%s] poll error: %v", p.bot.ID, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, upd := range updates {
			if upd.UpdateID >= offset {
				offset = upd.UpdateID + 1
			}
			if upd.Message == nil || strings.TrimSpace(upd.Message.Text) == "" {
				continue
			}
			go p.handleMessage(ctx, upd.Message)
		}
	}
}

func (p *telegramPoller) isAllowed(msg *tgMessage) bool {
	if len(p.allowedUID) == 0 && len(p.allowedCID) == 0 {
		return false // fail closed
	}
	if len(p.allowedCID) > 0 {
		if _, ok := p.allowedCID[strconv.FormatInt(msg.Chat.ID, 10)]; ok {
			return true
		}
	}
	if len(p.allowedUID) > 0 && msg.From != nil {
		if _, ok := p.allowedUID[strconv.FormatInt(msg.From.ID, 10)]; ok {
			return true
		}
	}
	return false
}

func (p *telegramPoller) handleMessage(ctx context.Context, msg *tgMessage) {
	if !p.isAllowed(msg) {
		uid := int64(0)
		if msg.From != nil {
			uid = msg.From.ID
		}
		log.Printf("[telegram:%s] ignored unauthorized message from user=%d chat=%d", p.bot.ID, uid, msg.Chat.ID)
		return
	}

	typingCtx, stopTyping := context.WithCancel(ctx)
	defer stopTyping()
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		_ = p.sendChatAction(typingCtx, msg.Chat.ID, "typing")
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				_ = p.sendChatAction(typingCtx, msg.Chat.ID, "typing")
			}
		}
	}()

	res, err := p.hub.HandleInput(ctx, p.bot.ID, msg.Text, hub.SendOptions{Source: "telegram"})
	stopTyping()

	if err != nil {
		_ = p.sendMessage(ctx, msg.Chat.ID, fmt.Sprintf("Error: %v", err))
		return
	}
	reply := res.Text
	if reply == "" {
		reply = "(empty reply)"
	}
	if err := p.sendMessage(ctx, msg.Chat.ID, reply); err != nil {
		log.Printf("[telegram:%s] send error: %v", p.bot.ID, err)
	}
}

func (p *telegramPoller) getUpdates(ctx context.Context, offset int64) ([]tgUpdate, error) {
	url := fmt.Sprintf("%s/getUpdates?timeout=30&offset=%d", p.apiBase, offset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed tgUpdatesResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if !parsed.OK {
		return nil, fmt.Errorf("telegram getUpdates returned ok=false (status %d)", resp.StatusCode)
	}
	return parsed.Result, nil
}

func (p *telegramPoller) sendChatAction(ctx context.Context, chatID int64, action string) error {
	return p.postJSON(ctx, "sendChatAction", map[string]any{
		"chat_id": chatID,
		"action":  action,
	})
}

func (p *telegramPoller) sendMessage(ctx context.Context, chatID int64, text string) error {
	for _, chunk := range splitTelegramText(text, 4000) {
		if err := p.postJSON(ctx, "sendMessage", map[string]any{
			"chat_id": chatID,
			"text":    chunk,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *telegramPoller) postJSON(ctx context.Context, method string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiBase+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("telegram %s failed (%d): %s", method, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func splitTelegramText(s string, maxRunes int) []string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return []string{s}
	}
	var out []string
	for len(runes) > 0 {
		n := maxRunes
		if len(runes) < n {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

func hasNonEmpty(ids []string) bool {
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			return true
		}
	}
	return false
}
