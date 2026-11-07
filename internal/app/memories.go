package app

import (
	"context"

	"github.com/kunghim/CoBot/internal/app/memories"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

func (a *App) memoryService() memories.Service { return memories.Service{Core: a.core()} }

// ListMemories: see memories.Service.List.
func (a *App) ListMemories(ctx context.Context, principal auth.Principal, botID string, limit int) ([]domain.Memory, error) {
	return a.memoryService().List(ctx, principal, botID, limit)
}

// DeleteMemory: see memories.Service.Delete.
func (a *App) DeleteMemory(ctx context.Context, principal auth.Principal, memoryID string) error {
	return a.memoryService().Delete(ctx, principal, memoryID)
}

func (a *App) memoriesForContext(ctx context.Context, teamID, botID string) memories.SearchResult {
	return a.memoryService().ForContext(ctx, teamID, botID)
}
