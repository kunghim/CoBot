package app

import (
	"context"

	"github.com/kunghim/CoBot/internal/app/preview"
	"github.com/kunghim/CoBot/internal/auth"
)

func (a *App) previewService() preview.Service {
	return preview.Service{DB: a.DB, Table: &a.previews}
}

// StartPreview: see preview.Service.Start.
func (a *App) StartPreview(ctx context.Context, principal auth.Principal, teamID, command string) (map[string]any, error) {
	return a.previewService().Start(ctx, principal, teamID, command)
}

// StopPreview: see preview.Service.Stop.
func (a *App) StopPreview(ctx context.Context, principal auth.Principal, teamID, command string) error {
	return a.previewService().Stop(ctx, principal, teamID, command)
}

func (a *App) stopTeamPreviews(teamID string) { a.previewService().StopTeam(teamID) }

func (a *App) stopAllPreviews() { a.previewService().StopAll() }
