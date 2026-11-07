package app

import (
	"context"

	"github.com/kunghim/CoBot/internal/app/avatarservice"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

// AvatarSeries is one cached series as the interface reads it.
type AvatarSeries = avatarservice.Series

// AvatarCatalog is this profile's avatar catalog and its last sync.
type AvatarCatalog = avatarservice.Catalog

func (a *App) avatarService() avatarservice.Service {
	return avatarservice.Service{Core: a.core(), Paths: a.Paths, Catalog: a.Avatars}
}

// AvatarCatalog: see avatarservice.Service.Read.
func (a *App) AvatarCatalog(ctx context.Context, principal auth.Principal) (AvatarCatalog, error) {
	return a.avatarService().Read(ctx, principal)
}

// SyncAvatarCatalog: see avatarservice.Service.Sync.
func (a *App) SyncAvatarCatalog(ctx context.Context, principal auth.Principal) (AvatarCatalog, error) {
	return a.avatarService().Sync(ctx, principal)
}

// StartAvatarSync: see avatarservice.Service.StartSync.
func (a *App) StartAvatarSync() { a.avatarService().StartSync() }

// SetBotAvatar: see avatarservice.Service.SetBot.
func (a *App) SetBotAvatar(ctx context.Context, principal auth.Principal, botID string, ref *domain.AvatarRef, expectedRevision int64) (domain.Bot, error) {
	return a.avatarService().SetBot(ctx, principal, botID, ref, expectedRevision)
}

// SetRoleAvatar: see avatarservice.Service.SetRole.
func (a *App) SetRoleAvatar(ctx context.Context, principal auth.Principal, roleID string, ref *domain.AvatarRef) error {
	return a.avatarService().SetRole(ctx, principal, roleID, ref)
}

// SaveAvatarUpload: see avatarservice.Service.SaveUpload.
func (a *App) SaveAvatarUpload(ctx context.Context, principal auth.Principal, data []byte) (string, error) {
	return a.avatarService().SaveUpload(ctx, principal, data)
}

// AvatarUploadPath: see avatarservice.Service.UploadPath.
func (a *App) AvatarUploadPath(principal auth.Principal, name string) (string, error) {
	return a.avatarService().UploadPath(principal, name)
}

// CleanAvatarUploads: see avatarservice.Service.CleanUploads.
func (a *App) CleanAvatarUploads(ctx context.Context) { a.avatarService().CleanUploads(ctx) }
