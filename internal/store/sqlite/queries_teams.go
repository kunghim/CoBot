package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

const teamColumns = `id, name, profile_instance, leader_id, status, status_detail, revision, created_at, updated_at`

func scanTeam(row *sql.Row) (domain.Team, error) {
	var t domain.Team
	var created, updated string
	err := row.Scan(&t.ID, &t.Name, &t.ProfileInstance, &t.LeaderID, &t.Status, &t.StatusDetail, &t.Revision, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Team{}, ErrNotFound
	}
	if err != nil {
		return domain.Team{}, fmt.Errorf("scan team: %w", err)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return t, nil
}

// GetTeam reads one team and joins its binding directory.
func GetTeam(ctx context.Context, q Queryer, id string) (domain.Team, string, error) {
	row := q.QueryRowContext(ctx, `SELECT `+prefixedTeamColumns("t")+`, COALESCE(b.directory, '')
		FROM teams t LEFT JOIN directory_bindings b ON b.team_id = t.id WHERE t.id = ?`, id)
	var t domain.Team
	var created, updated, directory string
	err := row.Scan(&t.ID, &t.Name, &t.ProfileInstance, &t.LeaderID, &t.Status, &t.StatusDetail, &t.Revision, &created, &updated, &directory)
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
		if err := rows.Scan(&t.ID, &t.Name, &t.ProfileInstance, &t.LeaderID, &t.Status, &t.StatusDetail, &t.Revision, &created, &updated, &directory); err != nil {
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
		prefix + `.status, ` + prefix + `.status_detail, ` + prefix + `.revision, ` + prefix + `.created_at, ` + prefix + `.updated_at`
}

// FindActiveTeamByDirectoryKey returns the binding for a canonical directory.
func FindActiveTeamByDirectoryKey(ctx context.Context, q Queryer, key string) (domain.Team, error) {
	row := q.QueryRowContext(ctx, `SELECT `+prefixedTeamColumns("t")+`, b.directory
		FROM directory_bindings b JOIN teams t ON t.id = b.team_id
		WHERE b.directory_key = ? AND t.status != ?`, key, domain.TeamStatusDeleted)
	var t domain.Team
	var created, updated, directory string
	err := row.Scan(&t.ID, &t.Name, &t.ProfileInstance, &t.LeaderID, &t.Status, &t.StatusDetail, &t.Revision, &created, &updated, &directory)
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

// FindTeamByMarker returns any team bound to a directory key regardless of
// status (used for takeover/conflict decisions), excluding deleted.
func FindTeamByMarker(ctx context.Context, q Queryer, key string) (domain.Team, error) {
	return FindActiveTeamByDirectoryKey(ctx, q, key)
}

// InsertTeamStaged creates the team row, its binding and its bots inside one
// transaction: the reservation is atomic; resource preparation happens after
// commit and a failure only flips status to error (retryable).
func InsertTeamStaged(ctx context.Context, tx *sql.Tx, team domain.Team, bindingDirectory, bindingKey string, bots []domain.Bot) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO teams (id, name, profile_instance, leader_id, status, status_detail, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', 1, ?, ?)`,
		team.ID, team.Name, team.ProfileInstance, team.LeaderID, domain.TeamStatusCreating, now, now); err != nil {
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
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET status = ?, status_detail = ?, revision = revision + 1, updated_at = ? WHERE id = ?`,
		status, detail, time.Now().UTC().Format(time.RFC3339Nano), teamID); err != nil {
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := insertBotRow(ctx, tx, bot, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET revision = revision + 1, updated_at = ? WHERE id = ?`, now, bot.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}

const botColumns = `id, team_id, name, role_id, role_version, is_leader, status, current_run_id, revision, created_at`

func scanBot(rows *sql.Rows) (domain.Bot, error) {
	var b domain.Bot
	var isLeader int
	var created string
	if err := rows.Scan(&b.ID, &b.TeamID, &b.Name, &b.RoleID, &b.RoleVersion, &isLeader, &b.Status, &b.CurrentRunID, &b.Revision, &created); err != nil {
		return domain.Bot{}, err
	}
	b.IsLeader = isLeader != 0
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
