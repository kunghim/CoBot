package app

import (
	"context"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// ValidateToolRun requires an exact host-bound live execution. A surviving
// bridge from an ended/unknown predecessor must never write as a later run.
func (a *App) ValidateToolRun(ctx context.Context, principal auth.Principal) error {
	return a.validateToolRunOn(ctx, a.DB, principal)
}

func (a *App) validateToolRunOn(ctx context.Context, q sqlite.Queryer, principal auth.Principal) error {
	if principal.Kind != auth.KindBot || principal.RunID == "" {
		return domain.Forbidden("error.tool.no_active_run", nil)
	}
	run, err := sqlite.FindActiveRun(ctx, q, principal.BotID)
	if err != nil || run.ID != principal.RunID || run.TeamID != principal.TeamID || run.EngineEpoch == "" || run.EngineEpoch != a.EngineEpochOfTeam(principal.TeamID) {
		return domain.Forbidden("error.tool.no_active_run", nil)
	}
	switch run.Status {
	case domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval:
		return nil
	default:
		return domain.Forbidden("error.tool.no_active_run", nil)
	}
}

// ValidateNativeToolSource checks that a call carrying the engine's thread
// and turn comes from the member's live run, and reports whether a
// sub-agent the member spawned made it.
func (a *App) ValidateNativeToolSource(ctx context.Context, principal auth.Principal, threadID, turnID string) (bool, error) {
	if err := a.ValidateToolRun(ctx, principal); err != nil {
		return false, err
	}
	run, err := sqlite.GetRun(ctx, a.DB, principal.RunID)
	if err != nil || turnID == "" || threadID == "" {
		return false, domain.Forbidden("error.tool.no_active_run", nil)
	}
	session, err := sqlite.GetProviderSession(ctx, a.DB, principal.BotID)
	if err != nil || session.EngineEpoch != run.EngineEpoch {
		return false, domain.Forbidden("error.tool.no_active_run", nil)
	}
	if threadID == session.ProviderThreadID {
		if run.ProviderTurnID != turnID {
			return false, domain.Forbidden("error.tool.no_active_run", nil)
		}
		return false, nil
	}
	// A sub-agent the member spawned calls with its own thread and turn; it
	// acts within the member's run, which must still be live.
	if a.Scheduler == nil || a.Scheduler.RootThread(principal.TeamID, threadID) != session.ProviderThreadID {
		return false, domain.Forbidden("error.tool.no_active_run", nil)
	}
	switch run.Status {
	case domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval:
		return true, nil
	}
	return false, domain.Forbidden("error.tool.no_active_run", nil)
}
