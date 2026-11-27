package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TaskKindWorker  = "worker"
	TaskKindRoutine = "routine"
	TaskKindDesktop = "desktop"

	TaskIsolationCurrent  = "current"
	TaskIsolationWorktree = "worktree"
	TaskIsolationTempDir  = "temp_dir"

	TaskStatusQueued          = "queued"
	TaskStatusRunning         = "running"
	TaskStatusWaitingApproval = "waiting_approval"
	TaskStatusWaitingQuestion = "waiting_question"
	TaskStatusWaitingTakeover = "waiting_takeover"
	TaskStatusCompleted       = "completed"
	TaskStatusFailed          = "failed"
	TaskStatusCancelled       = "cancelled"

	TaskDeliveryWake    = "wake"
	TaskDeliveryReceipt = "receipt"
	TaskDeliverySilent  = "silent"

	TaskEventStatus   = "status"
	TaskEventChunk    = "chunk"
	TaskEventToolUse  = "tool_use"
	TaskEventMessage  = "message"
	TaskEventApproval = "approval"
	TaskEventQuestion = "question"
	TaskEventTakeover = "takeover"
	TaskEventError    = "error"
)

// ErrTaskNotFound indicates that the requested BotTask does not exist.
var ErrTaskNotFound = errors.New("task not found")

// BotTask represents a durable child-process execution unit under a Bot.
type BotTask struct {
	ID               string   `json:"id"`
	BotID            string   `json:"botId"`
	Kind             string   `json:"kind"`
	Title            string   `json:"title,omitempty"`
	Prompt           string   `json:"prompt"`
	Status           string   `json:"status"`
	DeviceID         string   `json:"deviceId"`
	Harness          string   `json:"harness,omitempty"`
	Model            string   `json:"model,omitempty"`
	PermissionMode   string   `json:"permissionMode,omitempty"`
	Workdir          string   `json:"workdir,omitempty"`
	WorkspaceID      string   `json:"workspaceId,omitempty"`
	Isolation        string   `json:"isolation,omitempty"`
	WorktreeBranch   string   `json:"worktreeBranch,omitempty"`
	WorktreePath     string   `json:"worktreePath,omitempty"`
	CheckpointRef    string   `json:"checkpointRef,omitempty"`
	Delivery         string   `json:"delivery"`
	DependsOn        []string `json:"dependsOn,omitempty"`
	ParentTaskID     string   `json:"parentTaskId,omitempty"`
	RoutineID        string   `json:"routineId,omitempty"`
	RoutineName      string   `json:"routineName,omitempty"`
	Trigger          string   `json:"trigger,omitempty"`
	SlotKey          string   `json:"slotKey,omitempty"`
	RuntimeSessionID string   `json:"runtimeSessionId,omitempty"`
	Output           string   `json:"output,omitempty"`
	Error            string   `json:"error,omitempty"`
	Tools            []string `json:"tools,omitempty"`
	DurationMs       int64    `json:"durationMs,omitempty"`
	CostUSD          float64  `json:"costUsd,omitempty"`
	InputTokens      int      `json:"inputTokens,omitempty"`
	OutputTokens     int      `json:"outputTokens,omitempty"`
	CreatedAt        string   `json:"createdAt"`
	StartedAt        string   `json:"startedAt,omitempty"`
	FinishedAt       string   `json:"finishedAt,omitempty"`
	InjectedAt       string   `json:"injectedAt,omitempty"`
}

// BotTaskEvent represents a durable sequenced event emitted during a BotTask run.
type BotTaskEvent struct {
	Seq       int64  `json:"seq"`
	TaskID    string `json:"taskId"`
	BotID     string `json:"botId"`
	EventType string `json:"eventType"`
	Payload   any    `json:"payload,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// TaskListFilter narrows BotTask queries.
type TaskListFilter struct {
	Kind   string
	Status string
	Limit  int
}

const botTaskSelectCols = `
  id, bot_id, kind, title, prompt, status, device_id, harness, model, permission_mode,
  workdir, workspace_id, isolation, worktree_branch, worktree_path, checkpoint_ref,
  delivery, depends_on_json, parent_task_id, routine_id, routine_name, trigger,
  slot_key, runtime_session_id, output, error, tools_json, duration_ms, cost_usd,
  input_tokens, output_tokens, created_at, started_at, finished_at, injected_at
`

func IsTaskTerminalStatus(status string) bool {
	switch status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func encodeStringSliceJSON(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeStringSliceJSON(raw string) []string {
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil || len(out) == 0 {
		return nil
	}
	return out
}

func scanBotTaskRow(scanner interface{ Scan(...any) error }) (BotTask, error) {
	var t BotTask
	var depsJSON, toolsJSON string
	err := scanner.Scan(
		&t.ID,
		&t.BotID,
		&t.Kind,
		&t.Title,
		&t.Prompt,
		&t.Status,
		&t.DeviceID,
		&t.Harness,
		&t.Model,
		&t.PermissionMode,
		&t.Workdir,
		&t.WorkspaceID,
		&t.Isolation,
		&t.WorktreeBranch,
		&t.WorktreePath,
		&t.CheckpointRef,
		&t.Delivery,
		&depsJSON,
		&t.ParentTaskID,
		&t.RoutineID,
		&t.RoutineName,
		&t.Trigger,
		&t.SlotKey,
		&t.RuntimeSessionID,
		&t.Output,
		&t.Error,
		&toolsJSON,
		&t.DurationMs,
		&t.CostUSD,
		&t.InputTokens,
		&t.OutputTokens,
		&t.CreatedAt,
		&t.StartedAt,
		&t.FinishedAt,
		&t.InjectedAt,
	)
	if err != nil {
		return BotTask{}, err
	}
	t.DependsOn = decodeStringSliceJSON(depsJSON)
	t.Tools = decodeStringSliceJSON(toolsJSON)
	return t, nil
}

// CreateBotTask inserts a new BotTask into SQLite.
func (s *Store) CreateBotTask(t BotTask) (BotTask, error) {
	if s == nil || s.db == nil {
		return t, errors.New("state store unavailable")
	}
	if strings.TrimSpace(t.ID) == "" {
		t.ID = fmt.Sprintf("tsk_%d_%d", time.Now().UTC().UnixMilli(), s.seq.Add(1))
	}
	if strings.TrimSpace(t.Kind) == "" {
		t.Kind = TaskKindWorker
	}
	if strings.TrimSpace(t.Status) == "" {
		t.Status = TaskStatusQueued
	}
	if strings.TrimSpace(t.DeviceID) == "" {
		t.DeviceID = "local"
	}
	if strings.TrimSpace(t.Delivery) == "" {
		t.Delivery = TaskDeliveryReceipt
	}
	if strings.TrimSpace(t.CreatedAt) == "" {
		t.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	t.CostUSD = roundCost(t.CostUSD)

	_, err := s.db.Exec(`
		INSERT INTO bot_tasks(`+botTaskSelectCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		t.ID, t.BotID, t.Kind, t.Title, t.Prompt, t.Status, t.DeviceID, t.Harness, t.Model, t.PermissionMode,
		t.Workdir, t.WorkspaceID, t.Isolation, t.WorktreeBranch, t.WorktreePath, t.CheckpointRef,
		t.Delivery, encodeStringSliceJSON(t.DependsOn), t.ParentTaskID, t.RoutineID, t.RoutineName, t.Trigger,
		t.SlotKey, t.RuntimeSessionID, t.Output, t.Error, encodeStringSliceJSON(t.Tools), t.DurationMs, t.CostUSD,
		t.InputTokens, t.OutputTokens, t.CreatedAt, t.StartedAt, t.FinishedAt, t.InjectedAt,
	)
	return t, err
}

// GetBotTask loads a single BotTask by ID.
func (s *Store) GetBotTask(taskID string) (BotTask, bool, error) {
	if s == nil || s.db == nil {
		return BotTask{}, false, errors.New("state store unavailable")
	}
	row := s.db.QueryRow(`SELECT `+botTaskSelectCols+` FROM bot_tasks WHERE id = ?`, strings.TrimSpace(taskID))
	t, err := scanBotTaskRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return BotTask{}, false, nil
	}
	if err != nil {
		return BotTask{}, false, err
	}
	return t, true, nil
}

// UpdateBotTask persists mutable fields of t back to SQLite.
func (s *Store) UpdateBotTask(t BotTask) error {
	if s == nil || s.db == nil {
		return errors.New("state store unavailable")
	}
	t.CostUSD = roundCost(t.CostUSD)
	res, err := s.db.Exec(`
		UPDATE bot_tasks
		SET title = ?, prompt = ?, status = ?, device_id = ?, harness = ?, model = ?,
		    permission_mode = ?, workdir = ?, workspace_id = ?, isolation = ?,
		    worktree_branch = ?, worktree_path = ?, checkpoint_ref = ?, delivery = ?, runtime_session_id = ?,
		    output = ?, error = ?, tools_json = ?, duration_ms = ?, cost_usd = ?,
		    input_tokens = ?, output_tokens = ?, started_at = ?, finished_at = ?, injected_at = ?
		WHERE id = ?
	`,
		t.Title, t.Prompt, t.Status, t.DeviceID, t.Harness, t.Model,
		t.PermissionMode, t.Workdir, t.WorkspaceID, t.Isolation,
		t.WorktreeBranch, t.WorktreePath, t.CheckpointRef, t.Delivery, t.RuntimeSessionID,
		t.Output, t.Error, encodeStringSliceJSON(t.Tools), t.DurationMs, t.CostUSD,
		t.InputTokens, t.OutputTokens, t.StartedAt, t.FinishedAt, t.InjectedAt,
		t.ID,
	)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// ListBotTasks returns tasks optionally filtered by botID, kind, and status.
func (s *Store) ListBotTasks(botID string, filter TaskListFilter) ([]BotTask, error) {
	if s == nil || s.db == nil {
		return []BotTask{}, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var where []string
	var args []any
	if b := strings.TrimSpace(botID); b != "" {
		where = append(where, "bot_id = ?")
		args = append(args, b)
	}
	if k := strings.TrimSpace(filter.Kind); k != "" {
		where = append(where, "kind = ?")
		args = append(args, k)
	}
	if st := strings.TrimSpace(filter.Status); st != "" {
		where = append(where, "status = ?")
		args = append(args, st)
	}
	clause := ""
	if len(where) > 0 {
		clause = "WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit)
	rows, err := s.db.Query(`
		SELECT `+botTaskSelectCols+`
		FROM bot_tasks
		`+clause+`
		ORDER BY created_at DESC, rowid DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []BotTask{}
	for rows.Next() {
		t, err := scanBotTaskRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AppendBotTaskEvent records a sequenced task event into bot_task_events.
func (s *Store) AppendBotTaskEvent(taskID, botID, eventType string, payload any) (BotTaskEvent, error) {
	if s == nil || s.db == nil {
		return BotTaskEvent{}, errors.New("state store unavailable")
	}
	payloadJSON := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadJSON = string(b)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`
		INSERT INTO bot_task_events(task_id, bot_id, event_type, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, taskID, botID, eventType, payloadJSON, now)
	if err != nil {
		return BotTaskEvent{}, err
	}
	seq, _ := res.LastInsertId()
	return BotTaskEvent{
		Seq:       seq,
		TaskID:    taskID,
		BotID:     botID,
		EventType: eventType,
		Payload:   payload,
		CreatedAt: now,
	}, nil
}

// ListBotTaskEvents returns sequenced task events with seq > afterSeq in ascending order.
func (s *Store) ListBotTaskEvents(taskID string, afterSeq int64, limit int) ([]BotTaskEvent, error) {
	if s == nil || s.db == nil {
		return []BotTaskEvent{}, nil
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.Query(`
		SELECT seq, task_id, bot_id, event_type, payload_json, created_at
		FROM bot_task_events
		WHERE task_id = ? AND seq > ?
		ORDER BY seq ASC
		LIMIT ?
	`, strings.TrimSpace(taskID), afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []BotTaskEvent{}
	for rows.Next() {
		var ev BotTaskEvent
		var payloadJSON string
		if err := rows.Scan(&ev.Seq, &ev.TaskID, &ev.BotID, &ev.EventType, &payloadJSON, &ev.CreatedAt); err != nil {
			return nil, err
		}
		if payloadJSON != "" && payloadJSON != "{}" && payloadJSON != "null" {
			var decoded any
			if json.Unmarshal([]byte(payloadJSON), &decoded) == nil {
				ev.Payload = decoded
			} else {
				ev.Payload = payloadJSON
			}
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// SettleInterruptedTasks transitions any non-terminal tasks left over from a prior
// hub instance into failed with reason and finished_at stamped.
func (s *Store) SettleInterruptedTasks(reason string) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "interrupted by hub restart"
	}
	rows, err := s.db.Query(`
		SELECT id, bot_id
		FROM bot_tasks
		WHERE status IN ('queued', 'running', 'waiting_approval', 'waiting_question', 'waiting_takeover')
	`)
	if err != nil {
		return 0, err
	}
	type pair struct {
		id, botID string
	}
	var pending []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.botID); err == nil {
			pending = append(pending, p)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()
	if len(pending) == 0 {
		return 0, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`
		UPDATE bot_tasks
		SET status = 'failed',
		    error = CASE WHEN error = '' THEN ? ELSE error END,
		    finished_at = CASE WHEN finished_at = '' THEN ? ELSE finished_at END
		WHERE status IN ('queued', 'running', 'waiting_approval', 'waiting_question', 'waiting_takeover')
	`, reason, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	for _, p := range pending {
		_, _ = s.AppendBotTaskEvent(p.id, p.botID, TaskEventStatus, map[string]any{
			"status": TaskStatusFailed,
			"error":  reason,
		})
	}
	return int(n), nil
}

// ListPendingTaskReceipts returns uninjected completed tasks with delivery='receipt'
// that do not already have a matching routine_runs receipt row.
func (s *Store) ListPendingTaskReceipts(botID string, limit int) ([]BotTask, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Query(`
		SELECT `+botTaskSelectCols+`
		FROM (
			SELECT `+botTaskSelectCols+`, rowid AS _rid
			FROM bot_tasks
			WHERE bot_id = ?
			  AND delivery = 'receipt'
			  AND status = 'completed'
			  AND output != ''
			  AND injected_at = ''
			  AND NOT EXISTS (SELECT 1 FROM routine_runs rr WHERE rr.id = bot_tasks.id)
			ORDER BY _rid DESC
			LIMIT ?
		) sub
		ORDER BY _rid ASC
	`, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BotTask
	for rows.Next() {
		t, err := scanBotTaskRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
