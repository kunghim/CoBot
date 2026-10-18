package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// ErrNotFound is returned when a singleton row is missing (corrupt database).
var ErrNotFound = errors.New("row not found")

// ---- meta ----

// GetMeta reads a profile metadata value.
func GetMeta(ctx context.Context, q Queryer, key string) (string, error) {
	var value string
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read meta %s: %w", key, err)
	}
	return value, nil
}

// SetMeta upserts a profile metadata value inside tx.
func SetMeta(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("write meta %s: %w", key, err)
	}
	return nil
}

// Queryer is satisfied by both *sql.DB and *sql.Tx.
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ---- settings ----

const settingsColumns = `revision, language, color_mode, theme, default_model, member_limit, chain_limit, review_reject_limit, budget_scope, budget_tokens, engine_path`

func scanSettings(row *sql.Row) (domain.Settings, error) {
	var s domain.Settings
	err := row.Scan(&s.Revision, &s.Language, &s.ColorMode, &s.Theme, &s.DefaultModel, &s.MemberLimit, &s.ChainLimit, &s.ReviewRejectLimit, &s.BudgetScope, &s.BudgetTokens, &s.EnginePath)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Settings{}, ErrNotFound
	}
	if err != nil {
		return domain.Settings{}, fmt.Errorf("scan settings: %w", err)
	}
	return s, nil
}

// GetSettings reads the singleton settings row.
func GetSettings(ctx context.Context, q Queryer) (domain.Settings, error) {
	return scanSettings(q.QueryRowContext(ctx, `SELECT `+settingsColumns+` FROM settings WHERE id = 1`))
}

// UpdateSettings applies a validated patch with optimistic concurrency and
// returns the stored value. expectedRevision 0 skips the check (bootstrap).
func UpdateSettings(ctx context.Context, db *sql.DB, patch domain.SettingsPatch, expectedRevision int64) (domain.Settings, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("begin settings tx: %w", err)
	}
	defer tx.Rollback()

	saved, err := UpdateSettingsTx(ctx, tx, patch, expectedRevision)
	if err != nil {
		return domain.Settings{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Settings{}, err
	}
	return saved, nil
}

func UpdateSettingsTx(ctx context.Context, tx *sql.Tx, patch domain.SettingsPatch, expectedRevision int64) (domain.Settings, error) {
	current, err := GetSettings(ctx, tx)
	if err != nil {
		return domain.Settings{}, err
	}
	if expectedRevision > 0 && current.Revision != expectedRevision {
		return domain.Settings{}, domain.RevisionConflict("error.settings.revision_conflict", map[string]any{
			"expected": expectedRevision, "actual": current.Revision,
		})
	}
	next := current.Apply(patch)
	if err := next.Validate(); err != nil {
		return domain.Settings{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `
		UPDATE settings SET
			revision = revision + 1,
			language = ?, color_mode = ?, theme = ?, default_model = ?,
			member_limit = ?, chain_limit = ?, review_reject_limit = ?,
			budget_scope = ?, budget_tokens = ?, engine_path = ?, updated_at = ?
		WHERE id = 1`,
		next.Language, next.ColorMode, next.Theme, next.DefaultModel,
		next.MemberLimit, next.ChainLimit, next.ReviewRejectLimit,
		next.BudgetScope, next.BudgetTokens, next.EnginePath, now)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("update settings: %w", err)
	}
	saved, err := GetSettings(ctx, tx)
	if err != nil {
		return domain.Settings{}, err
	}
	return saved, nil
}

// ---- events ----

// InsertEvent appends a durable event inside tx and returns the stored row.
func InsertEvent(ctx context.Context, tx *sql.Tx, instanceID string, ev domain.Event) (domain.Event, error) {
	if ev.ID == "" {
		ev.ID = domain.NewID()
	}
	if ev.InstanceID == "" {
		ev.InstanceID = instanceID
	}
	payload := ev.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	// An imported event keeps its original timestamp; a fresh one gets now.
	createdAt := ev.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO events (id, instance_id, type, team_id, bot_id, resource_id, revision, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.InstanceID, ev.Type, ev.TeamID, ev.BotID, ev.ResourceID, ev.Revision, string(payload), createdAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.Event{}, fmt.Errorf("insert event: %w", err)
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return domain.Event{}, fmt.Errorf("read event seq: %w", err)
	}
	ev.Seq = seq
	ev.CreatedAt = createdAt
	ev.Payload = payload
	return ev, nil
}

// ListEvents returns up to limit events after a cursor, scoped by team/bot.
// A filtered stream may skip global seq numbers; clients must treat gaps as
// legal unless the cursor itself is outside retention.
func ListEvents(ctx context.Context, q Queryer, scope domain.Scope, after int64, limit int) ([]domain.Event, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	query := `SELECT seq, id, instance_id, type, team_id, bot_id, resource_id, revision, payload, created_at
		FROM events WHERE seq > ?`
	args := []any{after}
	if scope.TeamID != "" {
		query += ` AND team_id = ?`
		args = append(args, scope.TeamID)
	}
	if scope.BotID != "" {
		query += ` AND bot_id = ?`
		args = append(args, scope.BotID)
	}
	query += ` ORDER BY seq ASC LIMIT ?`
	args = append(args, limit)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		var payload string
		var createdAt string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.InstanceID, &ev.Type, &ev.TeamID, &ev.BotID, &ev.ResourceID, &ev.Revision, &payload, &createdAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.Payload = json.RawMessage(payload)
		t, err := time.Parse(time.RFC3339Nano, createdAt)
		if err == nil {
			ev.CreatedAt = t
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return out, nil
}

// GlobalSeq reads the current high-water mark of the event log.
func GlobalSeq(ctx context.Context, q Queryer) (int64, error) {
	var seq sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MAX(seq) FROM events`).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read global seq: %w", err)
	}
	if !seq.Valid {
		return 0, nil
	}
	return seq.Int64, nil
}

// NormalizeError maps store errors onto product errors where meaningful.
func NormalizeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return domain.NotFound("error.not_found", nil)
	}
	if strings.Contains(err.Error(), "database is locked") {
		return domain.Conflict("error.database_busy", nil)
	}
	return err
}

// ImportEventRow inserts an event by its explicit id, ignoring duplicates:
// the same event arriving from the live stream and again from a ledger file
// is one row (H08). It reports whether the row was new.
func ImportEventRow(ctx context.Context, db *sql.DB, ev domain.Event) (bool, error) {
	payload := ev.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	createdAt := ev.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	res, err := db.ExecContext(ctx, `INSERT INTO events (id, instance_id, type, team_id, bot_id, resource_id, revision, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		ev.ID, ev.InstanceID, ev.Type, ev.TeamID, ev.BotID, ev.ResourceID, ev.Revision, string(payload), createdAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, fmt.Errorf("import event: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListRecentTeamEvents returns a team's newest events for the activity feed,
// newest first (M5).
func ListRecentTeamEvents(ctx context.Context, q Queryer, teamID string, limit int) ([]domain.Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := q.QueryContext(ctx, `SELECT seq, id, instance_id, type, team_id, bot_id, resource_id, revision, payload, created_at
		FROM events WHERE team_id = ? ORDER BY seq DESC LIMIT ?`, teamID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		var payload, createdAt string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.InstanceID, &ev.Type, &ev.TeamID, &ev.BotID, &ev.ResourceID, &ev.Revision, &payload, &createdAt); err != nil {
			return nil, err
		}
		ev.Payload = json.RawMessage(payload)
		ev.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		out = append(out, ev)
	}
	return out, rows.Err()
}
