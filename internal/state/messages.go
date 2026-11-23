package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type MessageMedia struct {
	ID       string `json:"id"`
	Kind     string `json:"kind,omitempty"` // "image" | "file"
	URL      string `json:"url"`
	MimeType string `json:"mimeType,omitempty"`
	Name     string `json:"name,omitempty"`
	Alt      string `json:"alt,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

type ChatMessage struct {
	ID           string         `json:"id"`
	BotID        string         `json:"botId"`
	Role         string         `json:"role"` // "user" | "assistant" | "bot_in" | "bot_out" | "system" | "internal"
	From         string         `json:"from,omitempty"`
	To           string         `json:"to,omitempty"`
	Text         string         `json:"text"`
	Media        []MessageMedia `json:"media,omitempty"`
	SessionID    string         `json:"sessionId,omitempty"`
	Turn         int            `json:"turn,omitempty"`
	Tools        []string       `json:"tools,omitempty"`
	DurationMs   int64          `json:"durationMs,omitempty"`
	CostUSD      float64        `json:"costUsd,omitempty"`
	InputTokens  int            `json:"inputTokens,omitempty"`
	OutputTokens int            `json:"outputTokens,omitempty"`
	CreatedAt    string         `json:"createdAt"`
}

// MessageStore provides a standalone JSONL fallback when Hub is constructed
// without a SQLite-backed Store.
type MessageStore struct {
	mu  sync.Mutex
	dir string
	seq atomic.Uint64
}

func NewMessageStore(dir string) (*MessageStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create messages dir %s: %w", dir, err)
	}
	return &MessageStore{dir: dir}, nil
}

func (s *MessageStore) fileFor(botID string) string {
	return filepath.Join(s.dir, botID+".jsonl")
}

func (s *MessageStore) Append(msg ChatMessage) (ChatMessage, error) {
	if msg.ID == "" {
		n := s.seq.Add(1)
		msg.ID = fmt.Sprintf("msg_%d_%d", time.Now().UnixMilli(), n)
	}
	if msg.CreatedAt == "" {
		msg.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	line, err := json.Marshal(msg)
	if err != nil {
		return msg, err
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.fileFor(msg.BotID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return msg, err
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return msg, err
	}
	return msg, nil
}

func (s *MessageStore) List(botID string, limit int) ([]ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.fileFor(botID))
	if os.IsNotExist(err) {
		return []ChatMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var all []ChatMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var m ChatMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if m.Role == "internal" {
			continue
		}
		all = append(all, m)
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}

func (s *MessageStore) ListCollab(botID, peerID string, limit int) ([]ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.fileFor(botID))
	if os.IsNotExist(err) {
		return []ChatMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	peerTag := "bot:" + peerID
	out := []ChatMessage{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var m ChatMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if (m.Role == "bot_out" || m.Role == "bot_in") && (m.From == peerTag || m.To == peerTag) {
			out = append(out, m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}
