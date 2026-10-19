package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// SetBotAvatar stores a member's own avatar; nil follows its role again. It
// returns ErrNotFound for a deleted or unknown member.
func SetBotAvatar(ctx context.Context, tx *sql.Tx, botID string, ref *domain.AvatarRef) error {
	result, err := tx.ExecContext(ctx, `UPDATE bots SET avatar = ?, revision = revision + 1 WHERE id = ? AND deleted_at = ''`, domain.EncodeAvatar(ref), botID)
	if err != nil {
		return fmt.Errorf("set bot avatar: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RoleAvatars returns the avatars the user chose for roles, by role id.
func RoleAvatars(ctx context.Context, q Queryer) (map[string]domain.AvatarRef, error) {
	rows, err := q.QueryContext(ctx, `SELECT role_id, avatar FROM role_avatars`)
	if err != nil {
		return nil, fmt.Errorf("list role avatars: %w", err)
	}
	defer rows.Close()
	out := map[string]domain.AvatarRef{}
	for rows.Next() {
		var roleID, stored string
		if err := rows.Scan(&roleID, &stored); err != nil {
			return nil, err
		}
		if ref := domain.DecodeAvatar(stored); ref != nil {
			out[roleID] = *ref
		}
	}
	return out, rows.Err()
}

// SetRoleAvatar stores a role's avatar; nil returns it to the catalog default.
func SetRoleAvatar(ctx context.Context, tx *sql.Tx, roleID string, ref *domain.AvatarRef) error {
	if ref == nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM role_avatars WHERE role_id = ?`, roleID); err != nil {
			return fmt.Errorf("clear role avatar: %w", err)
		}
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO role_avatars (role_id, avatar, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(role_id) DO UPDATE SET avatar = excluded.avatar, updated_at = excluded.updated_at`,
		roleID, domain.EncodeAvatar(ref), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("set role avatar: %w", err)
	}
	return nil
}
