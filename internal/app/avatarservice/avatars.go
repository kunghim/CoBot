// Package avatarservice is how members and roles get their avatars: the catalog
// as the interface reads it, the choices, and the pictures a user uploads.
package avatarservice

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kunghim/CoBot/internal/app/core"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/avatars"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// avatarSyncTimeout bounds one catalog sync, startup or manual.
const avatarSyncTimeout = 60 * time.Second

// Service answers for avatars on the shared parts it is given.
type Service struct {
	core.Core
	Paths platform.Paths
	// Catalog is this profile's downloaded catalog; nil serves an empty one and
	// never downloads.
	Catalog *avatars.Catalog
}

// Series is one cached series as the interface reads it.
type Series struct {
	ID      string            `json:"id"`
	Name    map[string]string `json:"name"`
	Kind    string            `json:"kind"`
	Version int               `json:"version"`
	Data    json.RawMessage   `json:"data"`
}

// Catalog is this profile's avatar catalog and its last sync.
type Catalog struct {
	Series []Series       `json:"series"`
	Status avatars.Status `json:"status"`
}

// Read returns the cached catalog; it never downloads.
func (s Service) Read(ctx context.Context, principal auth.Principal) (Catalog, error) {
	if principal.Kind != auth.KindUser {
		return Catalog{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	return s.catalog()
}

// Sync checks for a newer catalog now. A failed download is
// reported in the returned status, not as an error: the cached catalog stays.
func (s Service) Sync(ctx context.Context, principal auth.Principal) (Catalog, error) {
	if principal.Kind != auth.KindUser {
		return Catalog{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	s.sync(ctx)
	return s.catalog()
}

// StartSync checks for a newer catalog in the background, once, as the
// core starts.
func (s Service) StartSync() {
	go s.sync(context.Background())
}

func (s Service) sync(ctx context.Context) {
	if s.Catalog == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, avatarSyncTimeout)
	defer cancel()
	changed, err := s.Catalog.Sync(ctx)
	if err != nil && s.Logger != nil {
		s.Logger.Warn("avatar catalog not updated", "error", err)
	}
	// Only a new catalog is an event: clients redraw avatars. The sync status
	// itself is read on demand.
	if changed {
		if err := s.Record(ctx, domain.Event{ID: domain.NewID(), Type: domain.EventAvatarsUpdated, ResourceID: "avatars"}); err == nil {
			s.Hub.Publish()
		}
	}
}

func (s Service) catalog() (Catalog, error) {
	out := Catalog{Series: []Series{}}
	if s.Catalog == nil {
		return out, nil
	}
	cached, err := s.Catalog.Load()
	if err != nil {
		return Catalog{}, err
	}
	for _, series := range cached {
		out.Series = append(out.Series, Series{
			ID: series.ID, Name: series.Name, Kind: series.Kind, Version: series.Version, Data: series.Data,
		})
	}
	out.Status = s.Catalog.Status()
	return out, nil
}

// SetBot sets a member's own avatar; nil makes it follow its role.
func (s Service) SetBot(ctx context.Context, principal auth.Principal, botID string, ref *domain.AvatarRef, expectedRevision int64) (domain.Bot, error) {
	cached, unlock, writeErr := core.Begin[domain.Bot](ctx, s.Core, principal)
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
	if err := s.requireUploadExists(ref); err != nil {
		return domain.Bot{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
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
	if err := s.SaveResult(ctx, tx, principal, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Bot{}, err
	}
	_ = s.Record(ctx, domain.Event{Type: domain.EventBotUpdated, TeamID: bot.TeamID, BotID: bot.ID, ResourceID: bot.ID, Revision: bot.Revision})
	s.Hub.Publish()
	return bot, nil
}

// SetRole sets the avatar of a role in the library; nil returns it to
// the catalog's default. Members that have no avatar of their own follow it.
func (s Service) SetRole(ctx context.Context, principal auth.Principal, roleID string, ref *domain.AvatarRef) error {
	replay, unlock, writeErr := core.Begin[struct{}](ctx, s.Core, principal)
	defer unlock()
	if writeErr != nil {
		return writeErr
	}
	if replay != nil {
		return nil
	}
	if principal.Kind != auth.KindUser {
		return domain.Forbidden("error.forbidden.user_only", nil)
	}
	if !s.roleExists(roleID) {
		return domain.NotFound("error.role.not_found", map[string]any{"roleId": roleID})
	}
	if ref != nil {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	if err := s.requireUploadExists(ref); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.SetRoleAvatar(ctx, tx, roleID, ref); err != nil {
		return err
	}
	if err := s.SaveResult(ctx, tx, principal, struct{}{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.Record(ctx, domain.Event{Type: domain.EventRoleUpdated, ResourceID: roleID})
	s.Hub.Publish()
	return nil
}

// roleExists reports whether the library (built-in or this profile's copy)
// has the role.
func (s Service) roleExists(roleID string) bool {
	if _, err := roles.Default(roleID); err == nil {
		return true
	}
	profile, err := roles.LoadProfile(s.Paths.RolesDir())
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
