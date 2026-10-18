package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/avatars"
	"github.com/kunghim/CoBot/internal/domain"
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
