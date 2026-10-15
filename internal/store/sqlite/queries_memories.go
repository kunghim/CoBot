package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// InsertMemory stores one memory. A duplicate (same owner, source and
// normalized body) violates idx_memories_dedupe — including when the existing
// row is a tombstone, which is how "never recreated automatically" is
// enforced by the database itself.
func InsertMemory(ctx context.Context, tx *sql.Tx, m domain.Memory) error {
	created := m.CreatedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO memories
		(id, team_id, bot_id, body, source_run_id, source_task_id, norm_key, deleted_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)`,
		m.ID, m.TeamID, m.BotID, m.Body, m.SourceRunID, m.SourceTaskID, m.NormKey,
		created.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}
	return nil
}

func scanMemory(row interface{ Scan(...any) error }) (domain.Memory, error) {
	var m domain.Memory
	var created string
	err := row.Scan(&m.ID, &m.TeamID, &m.BotID, &m.Body, &m.SourceRunID, &m.SourceTaskID, &m.NormKey, &m.DeletedAt, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Memory{}, ErrNotFound
	}
	if err != nil {
		return domain.Memory{}, fmt.Errorf("scan memory: %w", err)
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return m, nil
}

const memoryColumns = `id, team_id, bot_id, body, source_run_id, source_task_id, norm_key, deleted_at, created_at`

// GetMemory reads one memory by id (tombstones included, for audits).
func GetMemory(ctx context.Context, q Queryer, id string) (domain.Memory, error) {
	return scanMemory(q.QueryRowContext(ctx, `SELECT `+memoryColumns+` FROM memories WHERE id = ?`, id))
}

// ListLiveMemories returns a member's live memories, newest first. The
// ownership filter is part of the query, never a client-side trim.
func ListLiveMemories(ctx context.Context, q Queryer, teamID, botID string, limit int) ([]domain.Memory, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := q.QueryContext(ctx, `SELECT `+memoryColumns+` FROM memories
		WHERE team_id = ? AND bot_id = ? AND deleted_at = ''
		ORDER BY created_at DESC, id DESC LIMIT ?`, teamID, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectMemories(rows)
}

// SearchLiveMemories retrieves within one member's ownership scope with a
// light substring relevance filter. Tombstones are never returned; an empty
// query is the recent list.
func SearchLiveMemories(ctx context.Context, q Queryer, teamID, botID, query string, limit int) ([]domain.Memory, error) {
	if limit <= 0 || limit > domain.MemorySearchMaxItems {
		limit = domain.MemorySearchMaxItems
	}
	stmt := `SELECT ` + memoryColumns + ` FROM memories
		WHERE team_id = ? AND bot_id = ? AND deleted_at = ''`
	args := []any{teamID, botID}
	if query != "" {
		stmt += ` AND body LIKE ? ESCAPE '\'`
		like := `%` + escapeLike(query) + `%`
		args = append(args, like)
	}
	stmt += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectMemories(rows)
}

func collectMemories(rows *sql.Rows) ([]domain.Memory, error) {
	var out []domain.Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '%', '_':
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// TombstoneMemory marks a memory deleted. The row survives so its dedupe key
// keeps blocking an automatic re-creation of the same fact (G04).
func TombstoneMemory(ctx context.Context, tx *sql.Tx, id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE memories SET deleted_at = ? WHERE id = ? AND deleted_at = ''`, now, id)
	if err != nil {
		return fmt.Errorf("tombstone memory: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
