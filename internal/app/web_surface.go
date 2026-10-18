package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/dirpicker"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The Web-facing completeness surface (M5): team activity, the directory
// picker/check that keeps the Web client from needing arbitrary file access,
// and live bridge authorizations with real revocation. A team keeps its
// folder for life; there is no rebind.

// ListTeamActivity projects a team's newest events for the 动态 tab. The Web
// user sees the whole team's record, newest first.
func (a *App) ListTeamActivity(ctx context.Context, principal auth.Principal, teamID string, limit int) ([]domain.Event, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if _, _, err := sqlite.GetTeam(ctx, a.DB, teamID); err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	return sqlite.ListRecentTeamEvents(ctx, a.DB, teamID, limit)
}

// ListAuthorizations reports the live bridge credentials of this instance,
// without their token values; revoking one really invalidates it (I08).
func (a *App) ListAuthorizations() []auth.PublicBinding {
	if a.CollaborationTokens == nil {
		return nil
	}
	return a.CollaborationTokens.List()
}

// RevokeAuthorization invalidates every live credential of one member. The
// bridge holding one of them fails its next call with an unknown token.
func (a *App) RevokeAuthorization(ctx context.Context, principal auth.Principal, botID string) error {
	cached, unlock, writeErr := beginWrite[bool](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return writeErr
	}
	if cached != nil {
		return nil
	}

	if principal.Kind != auth.KindUser {
		return domain.Forbidden("error.forbidden.user_only", nil)
	}
	if _, err := sqlite.GetBot(ctx, a.DB, botID); err != nil {
		return sqlite.NormalizeError(err)
	}
	if a.CollaborationTokens == nil {
		return nil
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := a.saveWriteResult(ctx, tx, principal, true); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.CollaborationTokens.RevokeOfBot(botID)
	a.recordEvent(ctx, domain.Event{Type: "authorization.revoked", BotID: botID, ResourceID: botID})
	a.Hub.Publish()
	return nil
}

// CheckDirectory validates a directory for team creation without
// touching anything: canonical form, existence, reserved paths, duplicate
// and nested bindings, and a marker of a team this instance cannot see,
// which creation refuses the same way (B04).
func (a *App) CheckDirectory(ctx context.Context, principal auth.Principal, raw string) (map[string]any, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	canon, err := platform.CanonicalDirFrom(raw)
	if err != nil {
		return nil, domain.InvalidRequest("error.team.directory_invalid", map[string]any{"detail": err.Error()})
	}
	if info, err := os.Stat(canon.Display); err != nil || !info.IsDir() {
		return nil, domain.InvalidRequest("error.team.directory_missing", map[string]any{"directory": canon.Display})
	}
	result := map[string]any{
		"canonical": canon.Display, "key": canon.Key, "ok": true,
		"duplicate": false, "nested": false, "claimed": false, "archived": false,
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if existing, err := sqlite.FindActiveTeamByDirectoryKey(ctx, tx, canon.Key); err == nil {
		result["duplicate"] = true
		result["ok"] = false
		result["teamId"] = existing.ID
		result["teamName"] = existing.Name
		result["archived"] = existing.Status == domain.TeamStatusArchived
	} else if !errors.Is(err, sqlite.ErrNotFound) {
		return nil, err
	}
	if conflict, err := a.findNestedBinding(ctx, tx, canon); err != nil {
		return nil, err
	} else if conflict != nil {
		result["nested"] = true
		result["ok"] = false
		result["teamId"] = conflict.ID
		result["teamName"] = conflict.Name
	}
	// A marker of a team this instance cannot see (another CoBot, a copied
	// folder) still claims the folder; creation refuses it the same way.
	if result["ok"] == true {
		if claim, err := a.markerClaim(ctx, canon.Display); err != nil {
			return nil, err
		} else if claim != nil {
			result["ok"] = false
			result["claimed"] = true
			result["teamName"] = claim["teamName"]
		}
	}
	return result, nil
}

// PickDirectory shows the native folder chooser on this machine for the
// Web page, which cannot learn a real path from the browser's own picker.
// The result is only a suggestion: creation still validates it.
func (a *App) PickDirectory(ctx context.Context, principal auth.Principal) (map[string]any, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	title := "Select team working directory"
	if settings, err := a.GetSettings(ctx); err == nil && settings.Language == "zh-CN" {
		title = "选择团队工作目录"
	}
	path, err := dirpicker.Pick(ctx, title)
	switch {
	case errors.Is(err, dirpicker.ErrCancelled):
		return map[string]any{"path": "", "cancelled": true}, nil
	case errors.Is(err, dirpicker.ErrBusy):
		return nil, domain.Conflict("error.directory.picker_busy", nil)
	case errors.Is(err, dirpicker.ErrUnavailable):
		return nil, domain.CapabilityMissing("error.directory.picker_unavailable", nil, err)
	case err != nil:
		return nil, domain.CapabilityMissing("error.directory.picker_failed", nil, err)
	}
	return map[string]any{"path": path, "cancelled": false}, nil
}

// ListDirectoryChildren lists a directory's immediate subdirectories for the
// Web picker: names only, hidden entries skipped, bounded — never a file
// listing and never recursive (the client cannot read arbitrary files).
func (a *App) ListDirectoryChildren(ctx context.Context, principal auth.Principal, raw string) (map[string]any, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	base := strings.TrimSpace(raw)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, domain.InvalidRequest("error.team.directory_missing", map[string]any{"directory": ""})
		}
		base = home
	}
	canon, err := platform.CanonicalDirFrom(base)
	if err != nil {
		return nil, domain.InvalidRequest("error.team.directory_invalid", map[string]any{"detail": err.Error()})
	}
	entries, err := os.ReadDir(canon.Display)
	if err != nil {
		return nil, domain.InvalidRequest("error.team.directory_missing", map[string]any{"directory": canon.Display})
	}
	children := make([]string, 0, 16)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		children = append(children, filepath.Join(canon.Display, entry.Name()))
		if len(children) >= 200 {
			break
		}
	}
	sort.Strings(children)
	return map[string]any{"path": canon.Display, "children": children}, nil
}
