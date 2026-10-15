package app

import (
	"context"
	"time"

	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// RefreshLeases rewrites the local lease of every live member of every active
// team. The scheduler ticks it at platform.LeaseInterval while the core runs,
// so a lease that stops being refreshed is exactly the fact external helpers
// use to refuse side effects (H06).
func (a *App) RefreshLeases(ctx context.Context) int {
	if a.Paths.Root == "" {
		return 0
	}
	bots, err := sqlite.ListLeaseBots(ctx, a.DB)
	if err != nil {
		return 0
	}
	now := time.Now().UTC()
	written := 0
	for _, bot := range bots {
		lease := platform.Lease{
			BotID: bot.ID, TeamID: bot.TeamID, InstanceID: a.InstanceID,
			Epoch: a.EngineEpochOfTeam(bot.TeamID), UpdatedAt: now,
		}
		if err := platform.WriteLease(a.Paths.Root, lease); err != nil {
			a.Logger.Warn("write lease", "botId", bot.ID, "error", err)
			continue
		}
		written++
	}
	return written
}

// TouchLease refreshes one member's lease immediately, so a bridge spawned
// for a just-claimed turn never races the scheduler's ticker (H06).
func (a *App) TouchLease(ctx context.Context, botID string) {
	if a.Paths.Root == "" {
		return
	}
	bot, err := sqlite.GetBot(ctx, a.DB, botID)
	if err != nil {
		return
	}
	lease := platform.Lease{
		BotID: bot.ID, TeamID: bot.TeamID, InstanceID: a.InstanceID,
		Epoch: a.EngineEpochOfTeam(bot.TeamID), UpdatedAt: time.Now().UTC(),
	}
	if err := platform.WriteLease(a.Paths.Root, lease); err != nil && a.Logger != nil {
		a.Logger.Warn("touch lease", "botId", botID, "error", err)
	}
}

// LeaseDir exposes this profile's lease directory for bridge and hook
// environment wiring.
func (a *App) LeaseDir() string {
	return platform.LeaseDir(a.Paths.Root)
}

// CheckLease reports why a member's lease cannot be trusted right now, or ""
// when it is fresh for this instance. The reason travels as an i18n key to
// the refusing helper.
func (a *App) CheckLease(botID string) string {
	_, reason, err := platform.ReadLease(a.Paths.Root, botID, a.InstanceID, time.Now().UTC())
	if err != nil && reason == "" {
		return platform.LeaseReasonCorrupt
	}
	return reason
}
