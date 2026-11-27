package state

import (
	"fmt"
	"strings"
	"time"
)

const routineRunSelectCols = `
  id, bot_id, routine_id, routine_name, trigger, status, delivery, model,
  output, error, duration_ms, cost_usd, input_tokens, output_tokens,
  started_at, finished_at, injected_at, slot_key
`

func scanRoutineRunRow(scanner interface{ Scan(...any) error }) (RoutineRun, error) {
	var r RoutineRun
	err := scanner.Scan(
		&r.ID,
		&r.BotID,
		&r.RoutineID,
		&r.RoutineName,
		&r.Trigger,
		&r.Status,
		&r.Delivery,
		&r.Model,
		&r.Output,
		&r.Error,
		&r.DurationMs,
		&r.CostUSD,
		&r.InputTokens,
		&r.OutputTokens,
		&r.StartedAt,
		&r.FinishedAt,
		&r.InjectedAt,
		&r.SlotKey,
	)
	return r, err
}

func (s *Store) RecordRoutineRun(run RoutineRun) (RoutineRun, error) {
	if run.ID == "" {
		run.ID = fmt.Sprintf("run_%d_%d", time.Now().UTC().UnixMilli(), s.seq.Add(1))
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if run.StartedAt == "" {
		run.StartedAt = now
	}
	if run.FinishedAt == "" {
		run.FinishedAt = now
	}
	if run.Trigger == "" {
		run.Trigger = "schedule"
	}
	if run.Status == "" {
		run.Status = "ok"
	}
	if run.Delivery == "" {
		run.Delivery = "receipt"
	}
	run.CostUSD = roundCost(run.CostUSD)

	if _, err := s.db.Exec(`
		INSERT INTO routine_runs(`+routineRunSelectCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.BotID, run.RoutineID, run.RoutineName, run.Trigger, run.Status, run.Delivery, run.Model,
		run.Output, run.Error, run.DurationMs, run.CostUSD, run.InputTokens, run.OutputTokens,
		run.StartedAt, run.FinishedAt, run.InjectedAt, run.SlotKey); err != nil {
		return run, err
	}
	return run, nil
}

func (s *Store) ListRoutineRuns(botID, routineID string, limit int) ([]RoutineRun, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	where := []string{"bot_id = ?"}
	args := []any{botID}
	if strings.TrimSpace(routineID) != "" {
		where = append(where, "routine_id = ?")
		args = append(args, strings.TrimSpace(routineID))
	}
	args = append(args, limit)
	rows, err := s.db.Query(`
		SELECT `+routineRunSelectCols+`
		FROM routine_runs
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY started_at DESC, rowid DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RoutineRun{}
	for rows.Next() {
		r, err := scanRoutineRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) LatestRoutineRun(botID, routineID string) (RoutineRun, bool) {
	row := s.db.QueryRow(`
		SELECT `+routineRunSelectCols+`
		FROM routine_runs
		WHERE bot_id = ? AND routine_id = ?
		ORDER BY started_at DESC, rowid DESC
		LIMIT 1
	`, botID, routineID)
	r, err := scanRoutineRunRow(row)
	if err != nil {
		return RoutineRun{}, false
	}
	return r, true
}

func (s *Store) HasDailyRunOnDate(botID, routineID, localDate string) bool {
	rows, err := s.db.Query(`
		SELECT started_at
		FROM routine_runs
		WHERE bot_id = ? AND routine_id = ? AND trigger = 'schedule'
		ORDER BY started_at DESC, rowid DESC
		LIMIT 10
	`, botID, routineID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var startedAt string
		if err := rows.Scan(&startedAt); err != nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
			if t.In(time.Local).Format("2006-01-02") == localDate {
				return true
			}
		} else if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
			if t.In(time.Local).Format("2006-01-02") == localDate {
				return true
			}
		}
	}
	return false
}

func (s *Store) ListPendingReceipts(botID string, limit int) ([]RoutineRun, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Query(`
		SELECT `+routineRunSelectCols+`
		FROM (
			SELECT `+routineRunSelectCols+`, rowid AS _rid
			FROM routine_runs
			WHERE bot_id = ? AND delivery = 'receipt' AND status = 'ok' AND output != '' AND injected_at = ''
			ORDER BY _rid DESC
			LIMIT ?
		) sub
		ORDER BY _rid ASC
	`, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RoutineRun{}
	for rows.Next() {
		r, err := scanRoutineRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) MarkReceiptsInjected(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, now)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	inClause := strings.Join(placeholders, ",")
	if _, err := s.db.Exec(`UPDATE routine_runs SET injected_at = ? WHERE injected_at = '' AND id IN (`+inClause+`)`, args...); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE bot_tasks SET injected_at = ? WHERE injected_at = '' AND id IN (`+inClause+`)`, args...)
	return err
}

func (s *Store) DismissPendingReceipts(botID string) error {
	if _, err := s.db.Exec(`
		UPDATE routine_runs
		SET injected_at = 'dismissed'
		WHERE bot_id = ? AND delivery = 'receipt' AND status = 'ok' AND injected_at = ''
	`, botID); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		UPDATE bot_tasks
		SET injected_at = 'dismissed'
		WHERE bot_id = ? AND delivery = 'receipt' AND status = 'completed' AND injected_at = ''
	`, botID)
	return err
}
