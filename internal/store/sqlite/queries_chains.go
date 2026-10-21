package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// InsertChain stores a new causal chain.
func InsertChain(ctx context.Context, tx *sql.Tx, c domain.Chain) error {
	now := c.CreatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO chains
		(id, team_id, origin_message_id, origin_bot_id, epoch, status, wake_count, limit_count, risk_start, stop_reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.TeamID, c.OriginMessageID, c.OriginBotID, c.Epoch, c.Status, c.WakeCount, c.LimitCount, c.RiskStart, c.StopReason,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert chain: %w", err)
	}
	return nil
}

func scanChain(row interface{ Scan(...any) error }) (domain.Chain, error) {
	var c domain.Chain
	var created, updated string
	err := row.Scan(&c.ID, &c.TeamID, &c.OriginMessageID, &c.OriginBotID, &c.Epoch, &c.Status,
		&c.WakeCount, &c.LimitCount, &c.RiskStart, &c.StopReason, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Chain{}, ErrNotFound
	}
	if err != nil {
		return domain.Chain{}, fmt.Errorf("scan chain: %w", err)
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return c, nil
}

const chainColumns = `id, team_id, origin_message_id, origin_bot_id, epoch, status, wake_count, limit_count, risk_start, stop_reason, created_at, updated_at`

// GetChain reads one chain.
func GetChain(ctx context.Context, q Queryer, id string) (domain.Chain, error) {
	return scanChain(q.QueryRowContext(ctx, `SELECT `+chainColumns+` FROM chains WHERE id = ?`, id))
}

// ListChains pages a team's chains, newest first. An empty teamID means
// every team (the startup replay needs a profile-wide view); status filters
// on top of either scope.
func ListChains(ctx context.Context, q Queryer, teamID, status string, limit int) ([]domain.Chain, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + chainColumns + ` FROM chains WHERE 1 = 1`
	var args []any
	if teamID != "" {
		query += ` AND team_id = ?`
		args = append(args, teamID)
	}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Chain
	for rows.Next() {
		c, err := scanChain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountChainWake records one counted wake. The unique (chain, dispatchKey)
// pair makes a redelivery a no-op: counted=false means it was already counted
// and the caller must not increment the chain again.
func CountChainWake(ctx context.Context, tx *sql.Tx, chainID, dispatchKey, deliveryID, wokenBotID string) (bool, error) {
	if dispatchKey == "" {
		dispatchKey = deliveryID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO chain_wakes (id, chain_id, dispatch_key, delivery_id, woken_bot_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		domain.NewID(), chainID, dispatchKey, deliveryID, wokenBotID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return false, nil
		}
		return false, fmt.Errorf("count chain wake: %w", err)
	}
	return true, nil
}

// ChainWakeCounted reports whether a delivery's wake was already counted on
// the chain, which is how an approval-authorized wake dispatches without a
// second count.
func ChainWakeCounted(ctx context.Context, q Queryer, chainID, dispatchKey string) (bool, error) {
	if dispatchKey == "" {
		return false, nil
	}
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM chain_wakes WHERE chain_id = ? AND dispatch_key = ?`,
		chainID, dispatchKey).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// IncrementChainWake adds one counted wake and stamps the chain.
func IncrementChainWake(ctx context.Context, tx *sql.Tx, chainID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE chains SET wake_count = wake_count + 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), chainID); err != nil {
		return fmt.Errorf("increment chain wake: %w", err)
	}
	return nil
}

// SetChainStatus moves a chain between active/awaiting_approval/stopped.
func SetChainStatus(ctx context.Context, tx *sql.Tx, chainID, status, stopReason string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE chains SET status = ?, stop_reason = ?, updated_at = ? WHERE id = ?`,
		status, stopReason, time.Now().UTC().Format(time.RFC3339Nano), chainID); err != nil {
		return fmt.Errorf("set chain status: %w", err)
	}
	return nil
}

// BumpChainEpoch starts a new authorization generation on the chain. Only a
// user approval does this; a model can never extend its own budget.
func BumpChainEpoch(ctx context.Context, tx *sql.Tx, chainID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE chains SET epoch = epoch + 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), chainID); err != nil {
		return fmt.Errorf("bump chain epoch: %w", err)
	}
	return nil
}

// GrantChainWake lets exactly one more wake start on this chain, the effect
// of a user-approved continue past the limit.
func GrantChainBudget(ctx context.Context, tx *sql.Tx, chainID string, budget int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE chains SET limit_count = wake_count + ?, risk_start = wake_count + ? + 1, updated_at = ? WHERE id = ?`,
		budget, budget, time.Now().UTC().Format(time.RFC3339Nano), chainID); err != nil {
		return fmt.Errorf("grant chain budget: %w", err)
	}
	return nil
}

// ChainOfRun reports the causal chain of the run executing a delivery, or ""
// when the delivery carries no chain (user-originated work).
func ChainOfRun(ctx context.Context, q Queryer, runID string) (string, error) {
	if runID == "" {
		return "", nil
	}
	var chainID string
	err := q.QueryRowContext(ctx, `SELECT chain_id FROM deliveries WHERE run_id = ? ORDER BY created_at DESC LIMIT 1`, runID).Scan(&chainID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return chainID, nil
}

// ChainOfTask reports the chain of a task's most recent assignment, so a
// hand-over continues the chain that produced the work instead of starting a
// fresh budget.
func ChainOfTask(ctx context.Context, q Queryer, taskID string) (string, error) {
	if taskID == "" {
		return "", nil
	}
	var chainID string
	err := q.QueryRowContext(ctx, `SELECT d.chain_id FROM deliveries d
		JOIN messages m ON m.id = d.message_id
		WHERE m.run_id = ? AND m.kind = ? AND d.chain_id != ''
		ORDER BY d.created_at DESC LIMIT 1`, taskID, domain.MessageKindSystem).Scan(&chainID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return chainID, nil
}

// CancelChainDeliveries cancels the chain's still-undelivered work and mirrors
// the status onto the messages. Accepted (already running) work is left alone;
// stopping the chain interrupts that separately.
func CancelChainDeliveries(ctx context.Context, tx *sql.Tx, chainID string) (int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, message_id FROM deliveries
		WHERE chain_id = ? AND status IN (?, ?)`, chainID, domain.DeliveryQueued, domain.DeliveryWaitingDependency)
	if err != nil {
		return 0, err
	}
	type pending struct{ deliveryID, messageID string }
	var items []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.deliveryID, &p.messageID); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, item := range items {
		if err := SetDeliveryStatus(ctx, tx, item.deliveryID, domain.DeliveryCancelled); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET delivery_status = ? WHERE id = ?`,
			domain.DeliveryCancelled, item.messageID); err != nil {
			return 0, err
		}
	}
	return int64(len(items)), nil
}

// ActiveChainDeliveries lists the chain's accepted deliveries, whose bots may
// still be running work that belongs to this chain.
func ActiveChainDeliveries(ctx context.Context, q Queryer, chainID string) ([]DeliveryRecord, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE chain_id = ? AND status = ?`,
		chainID, domain.DeliveryAccepted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeliveryRecord
	for rows.Next() {
		r, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResetEvaluatingChains returns interrupted risk evaluations to active: after
// a restart no evaluation is in flight, and a chain must never wait forever
// on a session that died with the previous process.
func ResetEvaluatingChains(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM chains WHERE status = ?`, "evaluating")
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `UPDATE chains SET status = 'active', updated_at = ? WHERE status = ?`, now, "evaluating"); err != nil {
		return nil, err
	}
	return ids, nil
}
