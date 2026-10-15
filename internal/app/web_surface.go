package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The Web-facing completeness surface (M5): team activity, the directory
// picker/check that keeps the Web client from needing arbitrary file access,
// live bridge authorizations with real revocation, and an explicit directory
// rebind that goes through the same conflict rules as creation.

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
	if principal.Kind != auth.KindUser {
		return domain.Forbidden("error.forbidden.user_only", nil)
	}
	if _, err := sqlite.GetBot(ctx, a.DB, botID); err != nil {
		return sqlite.NormalizeError(err)
	}
	if a.CollaborationTokens == nil {
		return nil
	}
	a.CollaborationTokens.RevokeOfBot(botID)
	a.recordEvent(ctx, domain.Event{Type: "authorization.revoked", BotID: botID, ResourceID: botID})
	a.Hub.Publish()
	return nil
}

// CheckDirectory validates a directory for team creation or rebind without
// touching anything: canonical form, existence, reserved paths, duplicate
// and nested bindings, and a foreign/unknown marker that would need an
// explicit adoption (B04).
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
		"duplicate": false, "nested": false, "markerPresent": false, "adoptRequired": false,
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
	if marker, present, merr := readTeamMarker(canon.Display); merr != nil {
		return nil, merr
	} else if present {
		result["markerPresent"] = true
		result["markerInstance"] = marker.InstanceID
		if marker.InstanceID != a.InstanceID {
			result["adoptRequired"] = true
		}
	}
	return result, nil
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

// RebindTeamDirectory moves a team's working directory through the same
// conflict rules creation uses, then rewrites the marker for the new folder
// and updates the binding atomically. The old directory keeps its marker and
// files: nothing is deleted on a move (B04/B09).
func (a *App) RebindTeamDirectory(ctx context.Context, principal auth.Principal, teamID, raw string, adoptMarker bool, expectedRevision int64) (domain.Team, error) {
	if principal.Kind != auth.KindUser {
		return domain.Team{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	// Serialize binding changes with runtime creation and delivery claims.
	// No engine RPC is performed while a SQL transaction is held.
	if a.Scheduler != nil {
		lock := a.Scheduler.teamLock(teamID)
		lock.Lock()
		defer lock.Unlock()
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if team.Status == domain.TeamStatusDeleted {
		return domain.Team{}, domain.NotFound("error.not_found", nil)
	}
	if expectedRevision > 0 && team.Revision != expectedRevision {
		return domain.Team{}, domain.RevisionConflict("error.team.revision_conflict", map[string]any{"expected": expectedRevision, "actual": team.Revision})
	}
	canon, err := platform.CanonicalDirFrom(raw)
	if err != nil {
		return domain.Team{}, domain.InvalidRequest("error.team.directory_invalid", map[string]any{"detail": err.Error()})
	}
	if info, err := os.Stat(canon.Display); err != nil || !info.IsDir() {
		return domain.Team{}, domain.InvalidRequest("error.team.directory_missing", map[string]any{"directory": canon.Display})
	}
	if reserved, ok := platform.IsReservedDir(a.Paths.Reserved(), canon); ok {
		return domain.Team{}, domain.InvalidRequest("error.team.directory_reserved", map[string]any{"reserved": reserved})
	}
	if runs, err := sqlite.ListActiveRunsByTeam(ctx, a.DB, teamID); err != nil {
		return domain.Team{}, err
	} else if len(runs) != 0 {
		return domain.Team{}, domain.Conflict("error.team.active_runs", nil)
	}
	if a.Scheduler != nil {
		if rt := a.Scheduler.runtime(teamID); rt != nil {
			if err := rt.server.Shutdown(ctx); err != nil {
				return domain.Team{}, domain.EngineUnavailable("error.engine.shutdown_failed", nil, err)
			}
			a.Scheduler.mu.Lock()
			if a.Scheduler.teams[teamID] == rt {
				delete(a.Scheduler.teams, teamID)
			}
			a.Scheduler.mu.Unlock()
		}
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	if existing, err := sqlite.FindActiveTeamByDirectoryKey(ctx, tx, canon.Key); err == nil && existing.ID != teamID {
		return domain.Team{}, domain.Conflict("error.team.directory_bound", map[string]any{
			"teamId": existing.ID, "teamName": existing.Name, "directory": existing.Directory,
		})
	} else if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return domain.Team{}, err
	}
	if conflict, err := a.findNestedBindingExcluding(ctx, tx, canon, teamID); err != nil {
		return domain.Team{}, err
	} else if conflict != nil {
		return domain.Team{}, domain.Conflict("error.team.directory_nested", map[string]any{
			"teamId": conflict.ID, "teamName": conflict.Name, "directory": conflict.Directory,
		})
	}
	if marker, present, merr := readTeamMarker(canon.Display); merr != nil {
		return domain.Team{}, merr
	} else if present && marker.InstanceID != a.InstanceID && !adoptMarker {
		return domain.Team{}, domain.InvalidRequest("error.team.marker_adopt_required", map[string]any{
			"instanceId": marker.InstanceID, "directory": canon.Display,
		})
	}
	// The marker for the new folder is written before the binding moves, so
	// a failure leaves the team exactly where it was.
	if err := roles.EnsureProfileCopy(a.Paths.RolesDir()); err != nil {
		return domain.Team{}, err
	}
	if err := a.writeTeamMarker(team, canon); err != nil {
		return domain.Team{}, err
	}
	if err := sqlite.UpdateDirectoryBinding(ctx, tx, teamID, canon.Display, canon.Key); err != nil {
		return domain.Team{}, err
	}
	updated, err := sqlite.SetTeamStatusTx(ctx, tx, teamID, domain.TeamStatusActive, "", expectedRevision)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}
	// Reachability is verified after the binding moved and the transaction
	// closed: a folder that cannot be stat'ed is reported honestly rather
	// than left claiming active (the single-connection pool never nests a
	// query inside an open transaction).
	if !a.VerifyTeamDirectory(teamID) {
		if missing, merr := sqlite.SetTeamStatus(ctx, a.DB, teamID, domain.TeamStatusDirectoryMissing, canon.Display, 0); merr == nil {
			updated = missing
		}
	}
	// Best-effort Git exclude for the new folder; a non-Git folder is valid.
	if excludePath, err := platform.GitExcludePath(canon.Display); err == nil {
		_ = platform.EnsureLocalCobotExclude(excludePath)
	}
	a.recordEvent(ctx, domain.Event{Type: "team.directory_rebound", TeamID: teamID, ResourceID: teamID, Revision: updated.Revision})
	if a.Scheduler != nil {
		if bots, err := sqlite.ListBots(ctx, a.DB, teamID); err == nil {
			for _, bot := range bots {
				a.Scheduler.Wake(bot.ID)
			}
		}
	}
	a.Hub.Publish()
	return updated, nil
}

// writeTeamMarker stamps the identification marker for a team in its new
// folder; context/resources subdirs are created so a fresh folder is usable.
func (a *App) writeTeamMarker(team domain.Team, canon platform.CanonicalDir) error {
	cobotDir := filepath.Join(canon.Display, ".cobot")
	if err := os.MkdirAll(cobotDir, 0o755); err != nil {
		return err
	}
	marker := TeamMarker{
		SchemaVersion: 1, InstanceID: a.InstanceID, Profile: string(a.Paths.Environment),
		TeamID: team.ID, Directory: canon.Display,
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cobotDir, "team.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	for _, sub := range []string{"context", "resources"} {
		if err := os.MkdirAll(filepath.Join(cobotDir, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}
