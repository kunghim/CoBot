package app

import (
	"context"
	"errors"
	"io/fs"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/internal/workspace"
)

func (a *App) workspaceDirectory(ctx context.Context, principal auth.Principal, scope, id string) (string, error) {
	if principal.Kind != auth.KindUser {
		return "", domain.Forbidden("error.forbidden.user_only", nil)
	}
	switch scope {
	case "team":
		team, _, err := sqlite.GetTeam(ctx, a.DB, id)
		if err != nil {
			return "", sqlite.NormalizeError(err)
		}
		return team.Directory, nil
	case "chat":
		chat, err := sqlite.GetChat(ctx, a.DB, id)
		if err != nil {
			return "", sqlite.NormalizeError(err)
		}
		return chat.Workspace, nil
	default:
		return "", domain.InvalidRequest("error.workspace.invalid_scope", nil)
	}
}

func workspaceError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, workspace.ErrOutside) || errors.Is(err, fs.ErrPermission) {
		return domain.Forbidden("error.workspace.outside", nil)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return domain.NotFound("error.workspace.missing", nil)
	}
	if errors.Is(err, fs.ErrInvalid) {
		return domain.InvalidRequest("error.workspace.invalid_path", nil)
	}
	return domain.CapabilityMissing("error.workspace.unreadable", nil, err)
}

func (a *App) ListWorkspaceEntries(ctx context.Context, principal auth.Principal, scope, id, path, cursor string, hidden bool) (workspace.Directory, error) {
	directory, err := a.workspaceDirectory(ctx, principal, scope, id)
	if err != nil {
		return workspace.Directory{}, err
	}
	result, err := workspace.List(ctx, directory, path, cursor, hidden)
	return result, workspaceError(err)
}

func (a *App) OpenWorkspaceFile(ctx context.Context, principal auth.Principal, scope, id, path string) (*workspace.Opened, error) {
	directory, err := a.workspaceDirectory(ctx, principal, scope, id)
	if err != nil {
		return nil, err
	}
	result, err := workspace.Open(directory, path)
	return result, workspaceError(err)
}
