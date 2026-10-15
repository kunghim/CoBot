package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Team and member lifecycle (B08/B09): archive pauses dispatch and keeps
// work, restore resumes it, delete is a soft removal that unbinds the
// directory but never touches user files, and a member with any unfinished
// work cannot be deleted.

// openWorkError renders the honest refusal: every blocking count is named,
// so the user knows exactly what to finish or transfer first.
func openWorkError(scope string, c sqlite.OpenWorkCounts) error {
	return domain.Conflict("error."+scope+".open_work", map[string]any{
		"activeRuns": c.ActiveRuns, "pendingApprovals": c.PendingApprovals,
		"offeredTransfers": c.OfferedTransfers, "openTasks": c.OpenTasks,
		"queuedDeliveries": c.QueuedDeliveries, "openQuestions": c.OpenQuestions,
	})
}

// PatchTeamName renames a team (user only) with optimistic revision.
func (a *App) PatchTeamName(ctx context.Context, principal auth.Principal, teamID, name string, expectedRevision int64) (domain.Team, error) {
	cached, unlock, writeErr := beginWrite[domain.Team](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Team{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}

	if principal.Kind != auth.KindUser {
		return domain.Team{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return domain.Team{}, domain.InvalidRequest("error.team.name_required", nil)
	}
	if _, _, err := sqlite.GetTeam(ctx, a.DB, teamID); err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	team, err := sqlite.UpdateTeamNameTx(ctx, tx, teamID, trimmed, expectedRevision)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, team); err != nil {
		return domain.Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}
	a.recordEvent(ctx, domain.Event{Type: domain.EventTeamUpdated, TeamID: teamID, ResourceID: teamID, Revision: team.Revision})
	a.Hub.Publish()
	return team, nil
}

// ArchiveTeam pauses a team. While archived no delivery dispatches (the
// scheduler checks team status), tasks and history are preserved, and a
// restore brings the same work back. The precondition census refuses a team
// with running executions, pending approvals or in-flight hand-overs, so an
// archive never freezes a decision someone is waiting on.
func (a *App) ArchiveTeam(ctx context.Context, principal auth.Principal, teamID string, expectedRevision int64) (domain.Team, error) {
	cached, unlock, writeErr := beginWrite[domain.Team](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Team{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}

	if principal.Kind != auth.KindUser {
		return domain.Team{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if team.Status == domain.TeamStatusDeleted {
		return domain.Team{}, domain.NotFound("error.not_found", nil)
	}
	if team.Status != domain.TeamStatusActive && team.Status != domain.TeamStatusArchived {
		return domain.Team{}, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}
	work, err := sqlite.TeamOpenWork(ctx, a.DB, teamID)
	if err != nil {
		return domain.Team{}, err
	}
	if work.OpenQuestions > 0 || work.ActiveRuns > 0 || work.PendingApprovals > 0 || work.OfferedTransfers > 0 {
		return domain.Team{}, openWorkError("team", work)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	work, err = sqlite.TeamOpenWork(ctx, tx, teamID)
	if err != nil {
		return domain.Team{}, err
	}
	if work.OpenQuestions > 0 || work.ActiveRuns > 0 || work.PendingApprovals > 0 || work.OfferedTransfers > 0 {
		return domain.Team{}, openWorkError("team", work)
	}
	archived, err := sqlite.SetTeamStatusTx(ctx, tx, teamID, domain.TeamStatusArchived, "", expectedRevision)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, archived); err != nil {
		return domain.Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}
	a.recordEvent(ctx, domain.Event{Type: domain.EventTeamArchived, TeamID: teamID, ResourceID: teamID, Revision: archived.Revision})
	a.Hub.Publish()
	return archived, nil
}

// RestoreTeam re-activates an archived team and re-wakes the queued work the
// archive held back.
func (a *App) RestoreTeam(ctx context.Context, principal auth.Principal, teamID string, expectedRevision int64) (domain.Team, error) {
	cached, unlock, writeErr := beginWrite[domain.Team](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Team{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}

	if principal.Kind != auth.KindUser {
		return domain.Team{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if team.Status != domain.TeamStatusArchived && team.Status != domain.TeamStatusActive {
		return domain.Team{}, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	restored, err := sqlite.SetTeamStatusTx(ctx, tx, teamID, domain.TeamStatusActive, "", expectedRevision)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, restored); err != nil {
		return domain.Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}
	a.recordEvent(ctx, domain.Event{Type: domain.EventTeamRestored, TeamID: teamID, ResourceID: teamID, Revision: restored.Revision})
	a.Hub.Publish()
	a.ResumeBudgetPausedBots(ctx) // re-wake whatever the archive held
	return restored, nil
}

// DeleteTeam soft-deletes an archived team that has no unfinished work: the
// application rows stay for audit, the directory binding is removed, and the
// user's files (including the marker) are never deleted. Queued deliveries
// are cancelled because their team is going away; their rows stay too.
func (a *App) DeleteTeam(ctx context.Context, principal auth.Principal, teamID string, expectedRevision int64) (domain.Team, error) {
	cached, unlock, writeErr := beginWrite[domain.Team](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Team{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}

	if principal.Kind != auth.KindUser {
		return domain.Team{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if team.Status == domain.TeamStatusDeleted {
		return domain.Team{}, domain.NotFound("error.not_found", nil)
	}
	// Deleting is deliberately a two-step: archive first, so a live team can
	// never disappear in one click (B09).
	if team.Status != domain.TeamStatusArchived {
		return domain.Team{}, domain.Conflict("error.team.not_archived", map[string]any{"status": team.Status})
	}
	work, err := sqlite.TeamOpenWork(ctx, a.DB, teamID)
	if err != nil {
		return domain.Team{}, err
	}
	if work.OpenQuestions > 0 || work.ActiveRuns > 0 || work.PendingApprovals > 0 || work.OfferedTransfers > 0 || work.OpenTasks > 0 {
		return domain.Team{}, openWorkError("team", work)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	// Everything the delete changes — including the lifecycle state with its
	// revision check and a fresh open-work census — happens in this one
	// transaction: a stale expectedRevision must roll the whole delete back
	// instead of leaving a half-removed team (review B7/B11).
	work, err = sqlite.TeamOpenWork(ctx, tx, teamID)
	if err != nil {
		return domain.Team{}, err
	}
	if work.OpenQuestions > 0 || work.ActiveRuns > 0 || work.PendingApprovals > 0 || work.OfferedTransfers > 0 || work.OpenTasks > 0 {
		return domain.Team{}, openWorkError("team", work)
	}
	if _, err := sqlite.CancelQueuedDeliveriesByTeam(ctx, tx, teamID, ""); err != nil {
		return domain.Team{}, err
	}
	if err := sqlite.SoftDeleteBotsOfTeam(ctx, tx, teamID); err != nil {
		return domain.Team{}, err
	}
	if err := sqlite.UnbindTeamDirectory(ctx, tx, teamID); err != nil {
		return domain.Team{}, err
	}
	deleted, err := sqlite.SetTeamStatusTx(ctx, tx, teamID, domain.TeamStatusDeleted, "", expectedRevision)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, deleted); err != nil {
		return domain.Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}
	if a.CollaborationTokens != nil {
		for _, bot := range mustListBots(ctx, a, teamID) {
			a.CollaborationTokens.RevokeOfBot(bot.ID)
		}
	}
	a.recordEvent(ctx, domain.Event{Type: domain.EventTeamDeleted, TeamID: teamID, ResourceID: teamID, Revision: deleted.Revision})
	a.Hub.Publish()
	return deleted, nil
}

// DeleteBot removes a member only when it carries no unfinished work: no
// owned open tasks (so nothing is left ownerless), no offered hand-overs, no
// pending approvals it raised, no live runs, no queued deliveries. The row is
// tombstoned for audit, its bridge credentials are revoked, and undelivered
// messages to it are cancelled; history stays (B08).
func (a *App) DeleteBot(ctx context.Context, principal auth.Principal, botID string, expectedRevision int64) (domain.Bot, error) {
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
	bot, err := sqlite.GetBot(ctx, a.DB, botID)
	if err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	if bot.IsLeader {
		return domain.Bot{}, domain.Conflict("error.bot.leader_delete", nil)
	}
	work, err := sqlite.BotOpenWork(ctx, a.DB, botID)
	if err != nil {
		return domain.Bot{}, err
	}
	if work.OpenQuestions > 0 || work.ActiveRuns > 0 || work.PendingApprovals > 0 || work.OfferedTransfers > 0 || work.OpenTasks > 0 {
		return domain.Bot{}, openWorkError("bot", work)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Bot{}, err
	}
	defer tx.Rollback()
	work, err = sqlite.BotOpenWork(ctx, tx, botID)
	if err != nil {
		return domain.Bot{}, err
	}
	if work.OpenQuestions > 0 || work.ActiveRuns > 0 || work.PendingApprovals > 0 || work.OfferedTransfers > 0 || work.OpenTasks > 0 {
		return domain.Bot{}, openWorkError("bot", work)
	}
	if _, err := sqlite.CancelQueuedDeliveriesByTeam(ctx, tx, bot.TeamID, botID); err != nil {
		return domain.Bot{}, err
	}
	if err := sqlite.SoftDeleteBot(ctx, tx, botID, expectedRevision); err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Bot{}, err
	}
	if a.CollaborationTokens != nil {
		a.CollaborationTokens.RevokeOfBot(botID)
	}
	// The run a deleted member may still be executing is left alone: nothing
	// here interrupts an engine; its events resolve to a missing bot and are
	// reported as such (the census already refused live runs above).
	_ = bot
	a.recordEvent(ctx, domain.Event{Type: domain.EventBotDeleted, TeamID: bot.TeamID, BotID: botID, ResourceID: botID})
	a.Hub.Publish()
	return bot, nil
}

// readTeamMarker loads an on-disk identification marker, reporting presence
// separately from parse errors: any parseable or not, a marker file is a
// claim on the folder (B04).
func readTeamMarker(dir string) (TeamMarker, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".cobot", "team.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return TeamMarker{}, false, nil
		}
		return TeamMarker{}, false, err
	}
	var marker TeamMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		// An unreadable marker still marks the folder; treat it as foreign.
		return TeamMarker{InstanceID: "unknown"}, true, nil
	}
	return marker, true, nil
}

func mustListBots(ctx context.Context, a *App, teamID string) []domain.Bot {
	bots, err := sqlite.ListBots(ctx, a.DB, teamID)
	if err != nil {
		return nil
	}
	return bots
}
