package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// UpsertUsage records one turn's token snapshot. Repeated events for the same
// (engine epoch, thread, turn) replace the row with the newest snapshot —
// they never add to it, which is how cumulative engine values stay a single
// truth instead of being summed again on every update (G06).
func UpsertUsage(ctx context.Context, tx *sql.Tx, e domain.UsageEntry) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created := e.CreatedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	purpose := e.Purpose
	if purpose == "" {
		purpose = domain.UsagePurposeRun
	}
	status := e.Status
	if status == "" {
		status = domain.UsageStatusRecorded
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_ledger
		(id, team_id, bot_id, purpose, engine_epoch, thread_id, turn_id,
		 input_tokens, output_tokens, cached_input_tokens, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(engine_epoch, thread_id, turn_id) DO UPDATE SET
			bot_id = CASE WHEN excluded.bot_id != '' THEN excluded.bot_id ELSE usage_ledger.bot_id END,
			purpose = CASE WHEN excluded.purpose != '' THEN excluded.purpose ELSE usage_ledger.purpose END,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cached_input_tokens = excluded.cached_input_tokens,
			status = excluded.status,
			updated_at = excluded.updated_at`,
		e.ID, e.TeamID, e.BotID, purpose, e.EngineEpoch, e.ThreadID, e.TurnID,
		e.InputTokens, e.OutputTokens, e.CachedInputTokens, status,
		created.Format(time.RFC3339Nano), now)
	if err != nil {
		return fmt.Errorf("upsert usage: %w", err)
	}
	return nil
}

// MarkUnknownUsageIfMissing writes the "this turn never reported tokens"
// marker for a finished turn, unless a real usage row already arrived. It
// must run on the caller's transaction (single-connection pool).
func MarkUnknownUsageIfMissing(ctx context.Context, tx *sql.Tx, e domain.UsageEntry) error {
	purpose := e.Purpose
	if purpose == "" {
		purpose = domain.UsagePurposeRun
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_ledger
		(id, team_id, bot_id, purpose, engine_epoch, thread_id, turn_id,
		 input_tokens, output_tokens, cached_input_tokens, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0, 0, ?, ?, ?)
		ON CONFLICT(engine_epoch, thread_id, turn_id) DO NOTHING`,
		domain.NewID(), e.TeamID, e.BotID, purpose, e.EngineEpoch, e.ThreadID, e.TurnID,
		domain.UsageStatusUnknown, now, now)
	if err != nil {
		return fmt.Errorf("mark unknown usage: %w", err)
	}
	return nil
}

// SumUsage totals billable tokens (input+output, cached never double counted)
// over a scope and counts unknown rows. botID == "" scopes the whole team
// including system sessions; a bot scope counts only that bot's business
// turns (G06).
func SumUsage(ctx context.Context, q Queryer, teamID, botID string) (billable int64, unknown int, err error) {
	query := `SELECT
		COALESCE(SUM(input_tokens + output_tokens), 0),
		COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		FROM usage_ledger WHERE team_id = ?`
	args := []any{domain.UsageStatusUnknown, teamID}
	if botID != "" {
		query += ` AND bot_id = ? AND purpose = ?`
		args = append(args, botID, domain.UsagePurposeRun)
	}
	err = q.QueryRowContext(ctx, query, args...).Scan(&billable, &unknown)
	if err != nil {
		return 0, 0, err
	}
	return billable, unknown, nil
}

// GetUsage reads one ledger row by its natural key, for tests and tooling.
func GetUsage(ctx context.Context, q Queryer, epoch, threadID, turnID string) (domain.UsageEntry, error) {
	var e domain.UsageEntry
	var created, updated string
	err := q.QueryRowContext(ctx, `SELECT id, team_id, bot_id, purpose, engine_epoch, thread_id, turn_id,
		input_tokens, output_tokens, cached_input_tokens, status, created_at, updated_at
		FROM usage_ledger WHERE engine_epoch = ? AND thread_id = ? AND turn_id = ?`,
		epoch, threadID, turnID).Scan(
		&e.ID, &e.TeamID, &e.BotID, &e.Purpose, &e.EngineEpoch, &e.ThreadID, &e.TurnID,
		&e.InputTokens, &e.OutputTokens, &e.CachedInputTokens, &e.Status, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.UsageEntry{}, ErrNotFound
	}
	if err != nil {
		return domain.UsageEntry{}, err
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	e.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return e, nil
}

// QueuedDeliveryBots lists bots with queued deliveries, so raising a budget
// can resume exactly the work it paused.
func QueuedDeliveryBots(ctx context.Context, q Queryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT target_bot_id FROM deliveries WHERE status = ?`,
		domain.DeliveryQueued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
