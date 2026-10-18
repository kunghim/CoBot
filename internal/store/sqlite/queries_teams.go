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

// GetTeam reads one team and joins its binding directory.
func GetTeam(ctx context.Context, q Queryer, id string) (domain.Team, string, error) {
	row := q.QueryRowContext(ctx, `SELECT `+prefixedTeamColumns("t")+`, COALESCE(b.directory, '')
		FROM teams t LEFT JOIN directory_bindings b ON b.team_id = t.id WHERE t.id = ?`, id)
	var t domain.Team
	var created, updated, directory string
	err := row.Scan(&t.ID, &t.Name, &t.ProfileInstance, &t.LeaderID, &t.Status, &t.StatusDetail, &t.Revision, &created, &updated, &t.ArchivedAt, &t.PermissionMode, &t.ReasoningEffort, &directory)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Team{}, "", ErrNotFound
	}
	if err != nil {
		return domain.Team{}, "", fmt.Errorf("scan team: %w", err)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	t.Directory = directory
	return t, directory, nil
}

// ListTeams returns non-deleted teams visible to the user, newest first.
func ListTeams(ctx context.Context, q Queryer) ([]domain.Team, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+prefixedTeamColumns("t")+`, COALESCE(b.directory, '')
		FROM teams t LEFT JOIN directory_bindings b ON b.team_id = t.id
		WHERE t.status != ? ORDER BY t.created_at DESC`, domain.TeamStatusDeleted)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()
	var out []domain.Team
	for rows.Next() {
		var t domain.Team
		var created, updated, directory string
		if err := rows.Scan(&t.ID, &t.Name, &t.ProfileInstance, &t.LeaderID, &t.Status, &t.StatusDetail, &t.Revision, &created, &updated, &t.ArchivedAt, &t.PermissionMode, &t.ReasoningEffort, &directory); err != nil {
			return nil, fmt.Errorf("scan team row: %w", err)
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		t.Directory = directory
		out = append(out, t)
	}
	return out, rows.Err()
}

func prefixedTeamColumns(prefix string) string {
	return prefix + `.id, ` + prefix + `.name, ` + prefix + `.profile_instance, ` + prefix + `.leader_id, ` +
		prefix + `.status, ` + prefix + `.status_detail, ` + prefix + `.revision, ` + prefix + `.created_at, ` + prefix + `.updated_at, ` + prefix + `.archived_at, ` + prefix + `.permission_mode, ` + prefix + `.reasoning_effort`
}

// FindActiveTeamByDirectoryKey returns the binding for a canonical directory.
func FindActiveTeamByDirectoryKey(ctx context.Context, q Queryer, key string) (domain.Team, error) {
	row := q.QueryRowContext(ctx, `SELECT `+prefixedTeamColumns("t")+`, b.directory
		FROM directory_bindings b JOIN teams t ON t.id = b.team_id
		WHERE b.directory_key = ? AND t.status != ?`, key, domain.TeamStatusDeleted)
	var t domain.Team
	var created, updated, directory string
	err := row.Scan(&t.ID, &t.Name, &t.ProfileInstance, &t.LeaderID, &t.Status, &t.StatusDetail, &t.Revision, &created, &updated, &t.ArchivedAt, &t.PermissionMode, &t.ReasoningEffort, &directory)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Team{}, ErrNotFound
	}
	if err != nil {
		return domain.Team{}, fmt.Errorf("scan bound team: %w", err)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	t.Directory = directory
	return t, nil
}

// InsertTeam creates the team row, its binding and its bots inside the
// caller's transaction, with the team's status as given.
func InsertTeam(ctx context.Context, tx *sql.Tx, team domain.Team, bindingDirectory, bindingKey string, bots []domain.Bot) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO teams (id, name, profile_instance, leader_id, status, status_detail, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', 1, ?, ?)`,
		team.ID, team.Name, team.ProfileInstance, team.LeaderID, team.Status, now, now); err != nil {
		return fmt.Errorf("insert team: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO directory_bindings (team_id, directory_key, directory, created_at) VALUES (?, ?, ?, ?)`,
		team.ID, bindingKey, bindingDirectory, now); err != nil {
		return fmt.Errorf("insert directory binding: %w", err)
	}
	for _, bot := range bots {
		if err := insertBotRow(ctx, tx, bot, now); err != nil {
			return err
		}
	}
	return nil
}

func insertBotRow(ctx context.Context, tx *sql.Tx, bot domain.Bot, now string) error {
	isLeader := 0
	if bot.IsLeader {
		isLeader = 1
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bots (id, team_id, name, role_id, role_version, is_leader, status, revision, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		bot.ID, bot.TeamID, bot.Name, bot.RoleID, bot.RoleVersion, isLeader, domain.BotStatusIdle, now); err != nil {
		return fmt.Errorf("insert bot %s: %w", bot.Name, err)
	}
	return nil
}

// SetTeamStatus updates lifecycle state with optimistic revision.
func SetTeamStatus(ctx context.Context, db *sql.DB, teamID, status, detail string, expectedRevision int64) (domain.Team, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM teams WHERE id = ?`, teamID).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Team{}, ErrNotFound
		}
		return domain.Team{}, err
	}
	if expectedRevision > 0 && revision != expectedRevision {
		return domain.Team{}, domain.RevisionConflict("error.team.revision_conflict", map[string]any{"expected": expectedRevision, "actual": revision})
	}
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET status = ?, status_detail = ?, revision = revision + 1, updated_at = ?,
		archived_at = CASE WHEN ? = 'archived' THEN ? ELSE '' END WHERE id = ?`,
		status, detail, time.Now().UTC().Format(time.RFC3339Nano), status, time.Now().UTC().Format(time.RFC3339Nano), teamID); err != nil {
		return domain.Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}
	team, _, err := GetTeam(ctx, db, teamID)
	return team, err
}

// InsertBot adds one member to an active team.
func InsertBot(ctx context.Context, db *sql.DB, bot domain.Bot) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := InsertBotTx(ctx, tx, bot); err != nil {
		return err
	}
	return tx.Commit()
}

func InsertBotTx(ctx context.Context, tx *sql.Tx, bot domain.Bot) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := insertBotRow(ctx, tx, bot, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET revision = revision + 1, updated_at = ? WHERE id = ?`, now, bot.TeamID); err != nil {
		return err
	}
	return nil
}

const botColumns = `id, team_id, name, role_id, role_version, is_leader, status, current_run_id, avatar, revision, created_at`

func scanBot(rows *sql.Rows) (domain.Bot, error) {
	var b domain.Bot
	var isLeader int
	var created, avatar string
	if err := rows.Scan(&b.ID, &b.TeamID, &b.Name, &b.RoleID, &b.RoleVersion, &isLeader, &b.Status, &b.CurrentRunID, &avatar, &b.Revision, &created); err != nil {
		return domain.Bot{}, err
	}
	b.IsLeader = isLeader != 0
	b.Avatar = domain.DecodeAvatar(avatar)
	b.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return b, nil
}

// ListBots returns the live members of a team, leader first then by creation.
func ListBots(ctx context.Context, q Queryer, teamID string) ([]domain.Bot, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+botColumns+` FROM bots
		WHERE team_id = ? AND deleted_at = '' ORDER BY is_leader DESC, created_at ASC`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list bots: %w", err)
	}
	defer rows.Close()
	var out []domain.Bot
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetBot reads one member.
func GetBot(ctx context.Context, q Queryer, botID string) (domain.Bot, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+botColumns+` FROM bots WHERE id = ? AND deleted_at = ''`, botID)
	if err != nil {
		return domain.Bot{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return domain.Bot{}, err
		}
		return domain.Bot{}, ErrNotFound
	}
	return scanBot(rows)
}

// UpsertProviderSession stores the bot<->thread mapping.
func UpsertProviderSession(ctx context.Context, tx *sql.Tx, session domain.ProviderSession) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO provider_sessions (bot_id, provider_thread_id, engine_epoch, model, model_provider, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(bot_id) DO UPDATE SET provider_thread_id = excluded.provider_thread_id,
			engine_epoch = excluded.engine_epoch, model = excluded.model,
			model_provider = excluded.model_provider, updated_at = excluded.updated_at`,
		session.BotID, session.ProviderThreadID, session.EngineEpoch, session.Model, session.ModelProvider, now)
	if err != nil {
		return fmt.Errorf("upsert provider session: %w", err)
	}
	return nil
}

// GetProviderSession reads the bot's thread mapping.
func GetProviderSession(ctx context.Context, q Queryer, botID string) (domain.ProviderSession, error) {
	var s domain.ProviderSession
	var updated string
	err := q.QueryRowContext(ctx, `SELECT bot_id, provider_thread_id, engine_epoch, model, model_provider, updated_at
		FROM provider_sessions WHERE bot_id = ?`, botID).
		Scan(&s.BotID, &s.ProviderThreadID, &s.EngineEpoch, &s.Model, &s.ModelProvider, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProviderSession{}, ErrNotFound
	}
	if err != nil {
		return domain.ProviderSession{}, err
	}
	s.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return s, nil
}

// SetBotRunState persists the run-fact status of a member.
func SetBotRunState(ctx context.Context, tx *sql.Tx, botID, status, runID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE bots SET status = ?, current_run_id = ? WHERE id = ?`, status, runID, botID)
	if err != nil {
		return fmt.Errorf("update bot state: %w", err)
	}
	return nil
}

// DirectoryBinding is one bound working directory.
type DirectoryBinding struct {
	TeamID       string
	DirectoryKey string
	Directory    string
}

// ListDirectoryBindings returns every live binding (deleted teams excluded).
func ListDirectoryBindings(ctx context.Context, q Queryer) ([]DirectoryBinding, error) {
	rows, err := q.QueryContext(ctx, `SELECT b.team_id, b.directory_key, b.directory
		FROM directory_bindings b JOIN teams t ON t.id = b.team_id
		WHERE t.status != ?`, domain.TeamStatusDeleted)
	if err != nil {
		return nil, fmt.Errorf("list directory bindings: %w", err)
	}
	defer rows.Close()
	var out []DirectoryBinding
	for rows.Next() {
		var b DirectoryBinding
		if err := rows.Scan(&b.TeamID, &b.DirectoryKey, &b.Directory); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetTeamStatusTx is SetTeamStatus inside the caller's transaction, so a
// lifecycle change commits with the rest of its writes (the pool is a single
// connection; a nested tx here would deadlock).
func SetTeamStatusTx(ctx context.Context, tx *sql.Tx, teamID, status, detail string, expectedRevision int64) (domain.Team, error) {
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM teams WHERE id = ?`, teamID).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Team{}, ErrNotFound
		}
		return domain.Team{}, err
	}
	if expectedRevision > 0 && revision != expectedRevision {
		return domain.Team{}, domain.RevisionConflict("error.team.revision_conflict", map[string]any{"expected": expectedRevision, "actual": revision})
	}
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET status = ?, status_detail = ?, revision = revision + 1, updated_at = ?,
		archived_at = CASE WHEN ? = 'archived' THEN ? ELSE '' END WHERE id = ?`,
		status, detail, time.Now().UTC().Format(time.RFC3339Nano), status, time.Now().UTC().Format(time.RFC3339Nano), teamID); err != nil {
		return domain.Team{}, err
	}
	team, _, err := GetTeam(ctx, tx, teamID)
	return team, err
}

// UnbindTeamDirectory removes the directory binding rows of a team being
// deleted. User files on disk are never touched here (B09).
func UnbindTeamDirectory(ctx context.Context, tx *sql.Tx, teamID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM directory_bindings WHERE team_id = ?`, teamID); err != nil {
		return fmt.Errorf("unbind directory: %w", err)
	}
	return nil
}

// SoftDeleteBotsOfTeam tombstones every member of a deleted team; rows stay
// for audit and history (B09).
func SoftDeleteBotsOfTeam(ctx context.Context, tx *sql.Tx, teamID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE bots SET deleted_at = ? WHERE team_id = ? AND deleted_at = ''`, now, teamID); err != nil {
		return fmt.Errorf("soft delete bots: %w", err)
	}
	return nil
}

// SoftDeleteBot tombstones one member with optimistic revision.
func SoftDeleteBot(ctx context.Context, tx *sql.Tx, botID string, expectedRevision int64) error {
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM bots WHERE id = ?`, botID).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if expectedRevision > 0 && revision != expectedRevision {
		return domain.RevisionConflict("error.bot.revision_conflict", map[string]any{"expected": expectedRevision, "actual": revision})
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE bots SET deleted_at = ?, revision = revision + 1 WHERE id = ? AND deleted_at = ''`, now, botID)
	if err != nil {
		return fmt.Errorf("soft delete bot: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CancelQueuedDeliveriesByTeam cancels the still-undelivered work of a team
// (or one target bot when botID is set), mirroring each delivery's status
// onto its message so the records stay consistent.
func CancelQueuedDeliveriesByTeam(ctx context.Context, tx *sql.Tx, teamID, botID string) (int64, error) {
	query := `SELECT id, message_id FROM deliveries WHERE status IN (?, ?)`
	args := []any{domain.DeliveryQueued, domain.DeliveryWaitingDependency}
	if botID != "" {
		query += ` AND target_bot_id = ?`
		args = append(args, botID)
	} else {
		query += ` AND team_id = ?`
		args = append(args, teamID)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET status = ?, updated_at = ? WHERE id = ?`,
			domain.DeliveryCancelled, now, item.deliveryID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET delivery_status = ? WHERE id = ?`,
			domain.DeliveryCancelled, item.messageID); err != nil {
			return 0, err
		}
	}
	return int64(len(items)), nil
}

// UpdateTeamName renames a team with optimistic revision.
func UpdateTeamName(ctx context.Context, db *sql.DB, teamID, name string, expectedRevision int64) (domain.Team, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	team, err := UpdateTeamNameTx(ctx, tx, teamID, name, expectedRevision)
	if err != nil {
		return domain.Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}
	return team, nil
}

// UpdateTeamPermissionModeTx sets how freely the team's members may act.
func UpdateTeamPermissionModeTx(ctx context.Context, tx *sql.Tx, teamID, mode string, expectedRevision int64) (domain.Team, error) {
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM teams WHERE id = ?`, teamID).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Team{}, ErrNotFound
		}
		return domain.Team{}, err
	}
	if expectedRevision > 0 && revision != expectedRevision {
		return domain.Team{}, domain.RevisionConflict("error.team.revision_conflict", map[string]any{"expected": expectedRevision, "actual": revision})
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET permission_mode = ?, revision = revision + 1, updated_at = ? WHERE id = ?`, mode, now, teamID); err != nil {
		return domain.Team{}, err
	}
	team, _, err := GetTeam(ctx, tx, teamID)
	return team, err
}

func UpdateTeamNameTx(ctx context.Context, tx *sql.Tx, teamID, name string, expectedRevision int64) (domain.Team, error) {
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM teams WHERE id = ?`, teamID).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Team{}, ErrNotFound
		}
		return domain.Team{}, err
	}
	if expectedRevision > 0 && revision != expectedRevision {
		return domain.Team{}, domain.RevisionConflict("error.team.revision_conflict", map[string]any{"expected": expectedRevision, "actual": revision})
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET name = ?, revision = revision + 1, updated_at = ? WHERE id = ?`, name, now, teamID); err != nil {
		return domain.Team{}, err
	}
	team, _, err := GetTeam(ctx, tx, teamID)
	return team, err
}

// GetTeamByID reads the team row without its directory join.
func GetTeamByID(ctx context.Context, q Queryer, id string) (domain.Team, error) {
	var t domain.Team
	var created, updated string
	err := q.QueryRowContext(ctx, `SELECT `+prefixedTeamColumns("t")+` FROM teams t WHERE t.id = ?`, id).
		Scan(&t.ID, &t.Name, &t.ProfileInstance, &t.LeaderID, &t.Status, &t.StatusDetail, &t.Revision, &created, &updated, &t.ArchivedAt, &t.PermissionMode, &t.ReasoningEffort)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Team{}, ErrNotFound
	}
	if err != nil {
		return domain.Team{}, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return t, nil
}

// OpenWorkCounts is the "unfinished work" census that gates archive and
// delete decisions (B08/B09).
type OpenWorkCounts struct {
	OpenQuestions    int64 `json:"openQuestions"`
	ActiveRuns       int64 `json:"activeRuns"`
	PendingApprovals int64 `json:"pendingApprovals"`
	OfferedTransfers int64 `json:"offeredTransfers"`
	OpenTasks        int64 `json:"openTasks"`
	QueuedDeliveries int64 `json:"queuedDeliveries"`
}

// TeamOpenWork counts a team's blocking work.
func TeamOpenWork(ctx context.Context, q Queryer, teamID string) (OpenWorkCounts, error) {
	var c OpenWorkCounts
	err := q.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM runs WHERE team_id = ? AND status IN (?, ?, ?, ?, ?)),
		(SELECT COUNT(*) FROM approvals WHERE team_id = ? AND request_state = 'pending'),
		(SELECT COUNT(*) FROM transfers t JOIN tasks k ON k.id = t.task_id WHERE k.team_id = ? AND t.status = 'offered'),
		(SELECT COUNT(*) FROM tasks WHERE team_id = ? AND status NOT IN ('done', 'cancelled')),
		(SELECT COUNT(*) FROM deliveries WHERE team_id = ? AND status IN (?, ?)),
        (SELECT COUNT(*) FROM questions WHERE team_id = ? AND status = 'open')`,
		teamID, domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval, domain.RunWaitingInput, domain.RunUnknown,
		teamID, teamID, teamID, teamID,
		domain.DeliveryQueued, domain.DeliveryWaitingDependency, teamID).Scan(
		&c.ActiveRuns, &c.PendingApprovals, &c.OfferedTransfers, &c.OpenTasks, &c.QueuedDeliveries, &c.OpenQuestions)
	if err != nil {
		return OpenWorkCounts{}, err
	}
	return c, nil
}

// BotOpenWork counts the blocking work of one member (B08).
func BotOpenWork(ctx context.Context, q Queryer, botID string) (OpenWorkCounts, error) {
	var c OpenWorkCounts
	err := q.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM runs WHERE bot_id = ? AND status IN (?, ?, ?, ?, ?)),
		(SELECT COUNT(*) FROM approvals WHERE requester_bot_id = ? AND request_state = 'pending'),
		(SELECT COUNT(*) FROM transfers WHERE (from_bot_id = ? OR to_bot_id = ?) AND status = 'offered'),
		(SELECT COUNT(*) FROM tasks WHERE owner_id = ? AND status NOT IN ('done', 'cancelled')),
		(SELECT COUNT(*) FROM deliveries WHERE target_bot_id = ? AND status IN (?, ?)),
        (SELECT COUNT(*) FROM questions WHERE requester_bot_id = ? AND status = 'open')`,
		botID, domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval, domain.RunWaitingInput, domain.RunUnknown,
		botID, botID, botID, botID,
		botID, domain.DeliveryQueued, domain.DeliveryWaitingDependency, botID).Scan(
		&c.ActiveRuns, &c.PendingApprovals, &c.OfferedTransfers, &c.OpenTasks, &c.QueuedDeliveries, &c.OpenQuestions)
	if err != nil {
		return OpenWorkCounts{}, err
	}
	return c, nil
}

// LeaseBot is the identity needed to write one member's local lease.
type LeaseBot struct {
	ID     string
	TeamID string
}

// ListLeaseBots returns every live member of an active team — exactly the
// members whose leases the running core keeps fresh (H06).
func ListLeaseBots(ctx context.Context, q Queryer) ([]LeaseBot, error) {
	rows, err := q.QueryContext(ctx, `SELECT b.id, b.team_id FROM bots b
		JOIN teams t ON t.id = b.team_id
		WHERE b.deleted_at = '' AND t.status = ?`, domain.TeamStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaseBot
	for rows.Next() {
		var b LeaseBot
		if err := rows.Scan(&b.ID, &b.TeamID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// TeamThreadIDs lists the engine threads of a team's members, so deleting
// the team can delete them from the engine too.
func TeamThreadIDs(ctx context.Context, q Queryer, teamID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT p.provider_thread_id FROM provider_sessions p
		JOIN bots b ON b.id = p.bot_id WHERE b.team_id = ? AND p.provider_thread_id != ''`, teamID)
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

// TeamExists reports whether a team row is still present.
func TeamExists(ctx context.Context, q Queryer, teamID string) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM teams WHERE id = ?`, teamID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// PurgeTeamTx removes every row of a team: members, conversations, runs,
// tasks, approvals, memories, usage, events and its directory binding.
// Children go before parents because foreign keys are enforced.
func PurgeTeamTx(ctx context.Context, tx *sql.Tx, teamID string) error {
	const teamBots = `SELECT id FROM bots WHERE team_id = ?`
	const teamTasks = `SELECT id FROM tasks WHERE team_id = ?`
	const teamApprovals = `SELECT id FROM approvals WHERE team_id = ?`
	statements := []string{
		`DELETE FROM approval_decisions WHERE approval_id IN (` + teamApprovals + `)`,
		`DELETE FROM approval_responses WHERE approval_id IN (` + teamApprovals + `)`,
		`DELETE FROM approvals WHERE team_id = ?`,
		`DELETE FROM chain_wakes WHERE chain_id IN (SELECT id FROM chains WHERE team_id = ?)`,
		`DELETE FROM chains WHERE team_id = ?`,
		`DELETE FROM deliveries WHERE team_id = ?`,
		`DELETE FROM task_dependencies WHERE task_id IN (` + teamTasks + `) OR depends_on_task_id IN (` + teamTasks + `)`,
		`DELETE FROM task_evidence WHERE task_id IN (` + teamTasks + `)`,
		`DELETE FROM transfers WHERE task_id IN (` + teamTasks + `)`,
		`DELETE FROM tasks WHERE team_id = ?`,
		`DELETE FROM questions WHERE team_id = ?`,
		`DELETE FROM memories WHERE team_id = ?`,
		`DELETE FROM messages WHERE team_id = ?`,
		`DELETE FROM run_steps WHERE team_id = ?`,
		`DELETE FROM runs WHERE team_id = ?`,
		`DELETE FROM usage_ledger WHERE team_id = ?`,
		`DELETE FROM events WHERE team_id = ?`,
		`DELETE FROM provider_sessions WHERE bot_id IN (` + teamBots + `)`,
		`DELETE FROM directory_bindings WHERE team_id = ?`,
		`DELETE FROM bots WHERE team_id = ?`,
		`DELETE FROM teams WHERE id = ?`,
	}
	for _, statement := range statements {
		args := make([]any, strings.Count(statement, "?"))
		for i := range args {
			args[i] = teamID
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("purge team: %w", err)
		}
	}
	return nil
}

// BotThreadIDs lists one member's engine threads.
func BotThreadIDs(ctx context.Context, q Queryer, botID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT provider_thread_id FROM provider_sessions WHERE bot_id = ? AND provider_thread_id != ''`, botID)
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

// PurgeBotTx removes one member and what is its own in its team: its
// conversation and deliveries, runs, memories, the approvals and questions
// it raised, the finished tasks it owned with their hand-overs, its events
// and its engine session. Messages it sent into other members'
// conversations stay as their history, and the usage ledger stays because
// team budgets are counted from it. Role definitions are never touched.
// Children go before parents because foreign keys are enforced.
func PurgeBotTx(ctx context.Context, tx *sql.Tx, botID string) error {
	const ownTasks = `SELECT id FROM tasks WHERE owner_id = ?`
	const ownApprovals = `SELECT id FROM approvals WHERE requester_bot_id = ?`
	const ownMessages = `SELECT id FROM messages WHERE bot_id = ?`
	statements := []string{
		`DELETE FROM approval_decisions WHERE approval_id IN (` + ownApprovals + `)`,
		`DELETE FROM approval_responses WHERE approval_id IN (` + ownApprovals + `)`,
		`DELETE FROM approvals WHERE requester_bot_id = ?`,
		`DELETE FROM deliveries WHERE target_bot_id = ? OR message_id IN (` + ownMessages + `)`,
		`DELETE FROM transfers WHERE from_bot_id = ? OR to_bot_id = ? OR task_id IN (` + ownTasks + `)`,
		`DELETE FROM task_dependencies WHERE task_id IN (` + ownTasks + `) OR depends_on_task_id IN (` + ownTasks + `)`,
		`DELETE FROM task_evidence WHERE task_id IN (` + ownTasks + `)`,
		`DELETE FROM questions WHERE requester_bot_id = ? OR task_id IN (` + ownTasks + `)`,
		`DELETE FROM tasks WHERE owner_id = ?`,
		`DELETE FROM memories WHERE bot_id = ?`,
		`DELETE FROM messages WHERE bot_id = ?`,
		`DELETE FROM run_steps WHERE bot_id = ?`,
		`DELETE FROM runs WHERE bot_id = ?`,
		`DELETE FROM events WHERE bot_id = ?`,
		`DELETE FROM provider_sessions WHERE bot_id = ?`,
		`DELETE FROM bots WHERE id = ?`,
	}
	for _, statement := range statements {
		args := make([]any, strings.Count(statement, "?"))
		for i := range args {
			args[i] = botID
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("purge member: %w", err)
		}
	}
	return nil
}

// SessionContextDigest reads what a member's thread has already been told.
func SessionContextDigest(ctx context.Context, q Queryer, botID string) string {
	var digest string
	_ = q.QueryRowContext(ctx, `SELECT context_digest FROM provider_sessions WHERE bot_id = ?`, botID).Scan(&digest)
	return digest
}

// SetSessionContextDigest records what a member's thread has been told; a new
// thread resets it to what its instructions carried.
func SetSessionContextDigest(ctx context.Context, q Queryer, botID, digest string) error {
	if _, err := q.ExecContext(ctx, `UPDATE provider_sessions SET context_digest = ? WHERE bot_id = ?`, digest, botID); err != nil {
		return fmt.Errorf("set session context digest: %w", err)
	}
	return nil
}

// SessionUsageSnapshot reads a member's latest engine usage snapshot.
func SessionUsageSnapshot(ctx context.Context, q Queryer, botID string) string {
	var snapshot string
	_ = q.QueryRowContext(ctx, `SELECT usage_snapshot FROM provider_sessions WHERE bot_id = ?`, botID).Scan(&snapshot)
	return snapshot
}

// SetSessionUsageSnapshot stores a member's latest engine usage snapshot.
func SetSessionUsageSnapshot(ctx context.Context, q Queryer, botID, snapshot string) error {
	if _, err := q.ExecContext(ctx, `UPDATE provider_sessions SET usage_snapshot = ? WHERE bot_id = ?`, snapshot, botID); err != nil {
		return fmt.Errorf("set session usage snapshot: %w", err)
	}
	return nil
}

// UpdateTeamReasoningEffortTx sets how hard the team's members think.
func UpdateTeamReasoningEffortTx(ctx context.Context, tx *sql.Tx, teamID, effort string, expectedRevision int64) (domain.Team, error) {
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM teams WHERE id = ?`, teamID).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Team{}, ErrNotFound
		}
		return domain.Team{}, err
	}
	if expectedRevision > 0 && revision != expectedRevision {
		return domain.Team{}, domain.RevisionConflict("error.team.revision_conflict", map[string]any{"expected": expectedRevision, "actual": revision})
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET reasoning_effort = ?, revision = revision + 1, updated_at = ? WHERE id = ?`, effort, now, teamID); err != nil {
		return domain.Team{}, err
	}
	team, _, err := GetTeam(ctx, tx, teamID)
	return team, err
}
