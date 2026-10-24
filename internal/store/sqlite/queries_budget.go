package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Budget kinds: what a daily limit is set on.
const (
	BudgetKindTeam = "team"
	BudgetKindBot  = "bot"
)

// GetBudgetOverride reads the daily limit a team or member set for itself.
// found is false when it follows the default.
func GetBudgetOverride(ctx context.Context, q Queryer, kind, id string) (tokens int64, found bool, err error) {
	err = q.QueryRowContext(ctx, `SELECT daily_tokens FROM budget_overrides WHERE kind = ? AND id = ?`, kind, id).Scan(&tokens)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read budget override: %w", err)
	}
	return tokens, true, nil
}

// SetBudgetOverride stores a daily limit; nil removes it, so the team or
// member follows the default again. 0 is "no limit".
func SetBudgetOverride(ctx context.Context, tx *sql.Tx, kind, id string, tokens *int64) error {
	if tokens == nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM budget_overrides WHERE kind = ? AND id = ?`, kind, id); err != nil {
			return fmt.Errorf("clear budget override: %w", err)
		}
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO budget_overrides (kind, id, daily_tokens, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(kind, id) DO UPDATE SET daily_tokens = excluded.daily_tokens, updated_at = excluded.updated_at`,
		kind, id, *tokens, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("set budget override: %w", err)
	}
	return nil
}

// BudgetOverrides lists every override of one kind, by id.
func BudgetOverrides(ctx context.Context, q Queryer, kind string) (map[string]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, daily_tokens FROM budget_overrides WHERE kind = ?`, kind)
	if err != nil {
		return nil, fmt.Errorf("list budget overrides: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var tokens int64
		if err := rows.Scan(&id, &tokens); err != nil {
			return nil, err
		}
		out[id] = tokens
	}
	return out, rows.Err()
}

// BudgetResets lists when each team's and member's count was last started
// again by hand, keyed "kind/id", as ledger stamps.
func BudgetResets(ctx context.Context, q Queryer) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT kind, id, reset_at FROM budget_resets`)
	if err != nil {
		return nil, fmt.Errorf("list budget resets: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var kind, id, at string
		if err := rows.Scan(&kind, &id, &at); err != nil {
			return nil, err
		}
		out[kind+"/"+id] = at
	}
	return out, rows.Err()
}

// SetBudgetReset records that the count started again at a ledger stamp.
func SetBudgetReset(ctx context.Context, tx *sql.Tx, kind, id, at string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO budget_resets (kind, id, reset_at) VALUES (?, ?, ?)
		ON CONFLICT(kind, id) DO UPDATE SET reset_at = excluded.reset_at`, kind, id, at)
	if err != nil {
		return fmt.Errorf("set budget reset: %w", err)
	}
	return nil
}
