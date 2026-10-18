package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/avatars"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// avatarSyncTimeout bounds one catalog sync, startup or manual.
const avatarSyncTimeout = 60 * time.Second

// AvatarSeries is one cached series as the interface reads it.
type AvatarSeries struct {
	ID      string            `json:"id"`
	Name    map[string]string `json:"name"`
	Kind    string            `json:"kind"`
	Version int               `json:"version"`
	Data    json.RawMessage   `json:"data"`
}

// AvatarCatalog is this profile's avatar catalog and its last sync.
type AvatarCatalog struct {
	Series []AvatarSeries `json:"series"`
	Status avatars.Status `json:"status"`
}

// AvatarCatalog returns the cached catalog; it never downloads.
func (a *App) AvatarCatalog(ctx context.Context, principal auth.Principal) (AvatarCatalog, error) {
	if principal.Kind != auth.KindUser {
		return AvatarCatalog{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	return a.avatarCatalog()
}

// SyncAvatarCatalog checks for a newer catalog now. A failed download is
// reported in the returned status, not as an error: the cached catalog stays.
func (a *App) SyncAvatarCatalog(ctx context.Context, principal auth.Principal) (AvatarCatalog, error) {
	if principal.Kind != auth.KindUser {
		return AvatarCatalog{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	a.syncAvatars(ctx)
	return a.avatarCatalog()
}

// StartAvatarSync checks for a newer catalog in the background, once, as the
// core starts.
func (a *App) StartAvatarSync() {
	go a.syncAvatars(context.Background())
}

func (a *App) syncAvatars(ctx context.Context) {
	if a.Avatars == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, avatarSyncTimeout)
	defer cancel()
	changed, err := a.Avatars.Sync(ctx)
	if err != nil && a.Logger != nil {
		a.Logger.Warn("avatar catalog not updated", "error", err)
	}
	// Only a new catalog is an event: clients redraw avatars. The sync status
	// itself is read on demand.
	if changed {
		if err := a.recordEvent(ctx, domain.Event{ID: domain.NewID(), Type: domain.EventAvatarsUpdated, ResourceID: "avatars"}); err == nil {
			a.Hub.Publish()
		}
	}
}

func (a *App) avatarCatalog() (AvatarCatalog, error) {
	out := AvatarCatalog{Series: []AvatarSeries{}}
	if a.Avatars == nil {
		return out, nil
	}
	cached, err := a.Avatars.Load()
	if err != nil {
		return AvatarCatalog{}, err
	}
	for _, series := range cached {
		out.Series = append(out.Series, AvatarSeries{
			ID: series.ID, Name: series.Name, Kind: series.Kind, Version: series.Version, Data: series.Data,
		})
	}
	out.Status = a.Avatars.Status()
	return out, nil
}

// SetBotAvatar sets a member's own avatar; nil makes it follow its role.
func (a *App) SetBotAvatar(ctx context.Context, principal auth.Principal, botID string, ref *domain.AvatarRef, expectedRevision int64) (domain.Bot, error) {
	cached, unlock, writeErr := beginWrite[domain.Bot](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Bot{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindUser {
		return domain.Bot{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if ref != nil {
		if err := ref.Validate(); err != nil {
			return domain.Bot{}, err
		}
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Bot{}, err
	}
	defer tx.Rollback()
	current, err := sqlite.GetBot(ctx, tx, botID)
	if err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	if expectedRevision > 0 && current.Revision != expectedRevision {
		return domain.Bot{}, domain.RevisionConflict("error.bot.revision_conflict", map[string]any{"expected": expectedRevision, "actual": current.Revision})
	}
	if err := sqlite.SetBotAvatar(ctx, tx, botID, ref); err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	bot, err := sqlite.GetBot(ctx, tx, botID)
	if err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Bot{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventBotUpdated, TeamID: bot.TeamID, BotID: bot.ID, ResourceID: bot.ID, Revision: bot.Revision})
	a.Hub.Publish()
	return bot, nil
}

// SetRoleAvatar sets the avatar of a role in the library; nil returns it to
// the catalog's default. Members that have no avatar of their own follow it.
func (a *App) SetRoleAvatar(ctx context.Context, principal auth.Principal, roleID string, ref *domain.AvatarRef) error {
	_, unlock, writeErr := beginWrite[struct{}](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return writeErr
	}
	if principal.Kind != auth.KindUser {
		return domain.Forbidden("error.forbidden.user_only", nil)
	}
	if !a.roleExists(roleID) {
		return domain.NotFound("error.role.not_found", map[string]any{"roleId": roleID})
	}
	if ref != nil {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.SetRoleAvatar(ctx, tx, roleID, ref); err != nil {
		return err
	}
	if err := a.saveWriteResult(ctx, tx, principal, struct{}{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventRoleUpdated, ResourceID: roleID})
	a.Hub.Publish()
	return nil
}

// roleExists reports whether the library (built-in or this profile's copy)
// has the role.
func (a *App) roleExists(roleID string) bool {
	if _, err := roles.Default(roleID); err == nil {
		return true
	}
	profile, err := roles.LoadProfile(a.Paths.RolesDir())
	if err != nil {
		return false
	}
	for _, role := range profile {
		if role.ID == roleID {
			return true
		}
	}
	return false
}
