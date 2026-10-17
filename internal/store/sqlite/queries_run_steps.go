package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// UpsertRunStep records a step's latest form. The first write fixes its
// position (seq) and start time; later writes only move status and detail.
func UpsertRunStep(ctx context.Context, q Queryer, step domain.RunStep) (domain.RunStep, error) {
	now := time.Now().UTC()
	if step.StartedAt.IsZero() {
		step.StartedAt = now
	}
	step.UpdatedAt = now
	detail := string(step.Detail)
	if detail == "" {
		detail = "{}"
	}
	_, err := q.ExecContext(ctx, `INSERT INTO run_steps (run_id, item_id, team_id, bot_id, seq, kind, status, detail, started_at, updated_at)
		VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM run_steps WHERE run_id = ?), ?, ?, ?, ?, ?)
		ON CONFLICT (run_id, item_id) DO UPDATE SET status = excluded.status, detail = excluded.detail, updated_at = excluded.updated_at`,
		step.RunID, step.ID, step.TeamID, step.BotID, step.RunID, step.Kind, step.Status, detail,
		step.StartedAt.Format(time.RFC3339Nano), step.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.RunStep{}, fmt.Errorf("upsert run step: %w", err)
	}
	return GetRunStep(ctx, q, step.RunID, step.ID)
}

// GetRunStep reads one step.
func GetRunStep(ctx context.Context, q Queryer, runID, itemID string) (domain.RunStep, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+runStepColumns+` FROM run_steps WHERE run_id = ? AND item_id = ?`, runID, itemID)
	if err != nil {
		return domain.RunStep{}, err
	}
	steps, err := scanRunSteps(rows)
	if err != nil {
		return domain.RunStep{}, err
	}
	if len(steps) == 0 {
		return domain.RunStep{}, ErrNotFound
	}
	return steps[0], nil
}

// CloseOpenRunSteps settles the steps still in progress when their run
// ended, so a finished run never shows a step as running.
func CloseOpenRunSteps(ctx context.Context, q Queryer, runID, status string) error {
	_, err := q.ExecContext(ctx, `UPDATE run_steps SET status = ?, updated_at = ? WHERE run_id = ? AND status = 'in_progress'`,
		status, time.Now().UTC().Format(time.RFC3339Nano), runID)
	return err
}

// ListRunsWithSteps returns a bot's runs started in [from, until) with their
// steps, oldest first. Empty bounds are open.
func ListRunsWithSteps(ctx context.Context, q Queryer, botID, from, until string) ([]domain.RunWithSteps, error) {
	query := `SELECT id, team_id, bot_id, provider_session_id, provider_turn_id, engine_epoch, status, error_key, started_at, ended_at FROM runs WHERE bot_id = ?`
	args := []any{botID}
	if from != "" {
		query += ` AND started_at >= ?`
		args = append(args, from)
	}
	if until != "" {
		query += ` AND started_at < ?`
		args = append(args, until)
	}
	query += ` ORDER BY started_at ASC, id ASC LIMIT 200`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	var runs []domain.RunWithSteps
	index := map[string]int{}
	for rows.Next() {
		var r domain.Run
		var started, ended string
		if err := rows.Scan(&r.ID, &r.TeamID, &r.BotID, &r.ProviderSessionID, &r.ProviderTurnID, &r.EngineEpoch, &r.Status, &r.ErrorKey, &started, &ended); err != nil {
			rows.Close()
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		r.EndedAt, _ = time.Parse(time.RFC3339Nano, ended)
		index[r.ID] = len(runs)
		runs = append(runs, domain.RunWithSteps{Run: r, Steps: []domain.RunStep{}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(runs) == 0 {
		return runs, err
	}
	stepQuery := `SELECT ` + runStepColumns + ` FROM run_steps WHERE bot_id = ? AND run_id IN (SELECT id FROM runs WHERE bot_id = ?`
	stepArgs := []any{botID, botID}
	if from != "" {
		stepQuery += ` AND started_at >= ?`
		stepArgs = append(stepArgs, from)
	}
	if until != "" {
		stepQuery += ` AND started_at < ?`
		stepArgs = append(stepArgs, until)
	}
	stepQuery += `) ORDER BY run_id, seq`
	stepRows, err := q.QueryContext(ctx, stepQuery, stepArgs...)
	if err != nil {
		return nil, fmt.Errorf("list run steps: %w", err)
	}
	steps, err := scanRunSteps(stepRows)
	if err != nil {
		return nil, err
	}
	for _, step := range steps {
		if i, ok := index[step.RunID]; ok {
			runs[i].Steps = append(runs[i].Steps, step)
		}
	}
	return runs, nil
}

const runStepColumns = `run_id, item_id, team_id, bot_id, seq, kind, status, detail, started_at, updated_at`

func scanRunSteps(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}) ([]domain.RunStep, error) {
	defer rows.Close()
	var out []domain.RunStep
	for rows.Next() {
		var s domain.RunStep
		var detail, started, updated string
		if err := rows.Scan(&s.RunID, &s.ID, &s.TeamID, &s.BotID, &s.Seq, &s.Kind, &s.Status, &detail, &started, &updated); err != nil {
			return nil, err
		}
		s.Detail = []byte(detail)
		s.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		s.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, s)
	}
	return out, rows.Err()
}
