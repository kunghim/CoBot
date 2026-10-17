package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Team and member lifecycle (B08/B09): only an idle team can be archived,
// restore brings it back, and deleting an archived team (or one whose folder
// is gone) removes it completely. An idle member is deleted completely; a
// member with unfinished work is refused.

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
	// The folder marker carries the team name; keep it in step.
	team = a.checkTeamDirectory(ctx, team)
	a.recordEvent(ctx, domain.Event{Type: domain.EventTeamUpdated, TeamID: teamID, ResourceID: teamID, Revision: team.Revision})
	a.Hub.Publish()
	return team, nil
}

// PatchTeamPermissionMode changes how freely the team's members may act.
// Each member's next turn carries the new mode; a turn already running keeps
// the mode it started with.
func (a *App) PatchTeamPermissionMode(ctx context.Context, principal auth.Principal, teamID, mode string, expectedRevision int64) (domain.Team, error) {
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
	if !domain.ValidPermissionMode(mode) {
		return domain.Team{}, domain.InvalidRequest("error.team.permission_mode_invalid", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	team, err := sqlite.UpdateTeamPermissionModeTx(ctx, tx, teamID, mode, expectedRevision)
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

// archiveBlockers is the one census for archiving. A team may be archived
// only when it is completely idle: no running member, no queued message, no
// pending approval, no open question, no offered hand-over and no unfinished
// task. Passing it is what makes an archived team safe to delete outright.
func archiveBlockers(c sqlite.OpenWorkCounts) bool {
	return c.ActiveRuns > 0 || c.QueuedDeliveries > 0 || c.PendingApprovals > 0 ||
		c.OpenQuestions > 0 || c.OfferedTransfers > 0 || c.OpenTasks > 0
}

// ArchiveTeam sets an idle team aside. It leaves the sidebar for the
// archived list in Settings, keeps its folder (no other team may take it),
// and its engine is shut down. Anything unfinished refuses the archive with
// every blocking count named.
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
	if team.Status != domain.TeamStatusActive {
		return domain.Team{}, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	work, err := sqlite.TeamOpenWork(ctx, tx, teamID)
	if err != nil {
		return domain.Team{}, err
	}
	if archiveBlockers(work) {
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
	a.releaseTeamRuntime(ctx, teamID)
	a.recordEvent(ctx, domain.Event{Type: domain.EventTeamArchived, TeamID: teamID, ResourceID: teamID, Revision: archived.Revision})
	a.Hub.Publish()
	return archived, nil
}

// RestoreTeam brings an archived team back to the sidebar. Archiving
// required it to be idle, so there is no held-back work to resume; if its
// folder is gone meanwhile it comes back as directory_missing.
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
	if team.Status != domain.TeamStatusArchived {
		return domain.Team{}, domain.Conflict("error.team.not_archived", map[string]any{"status": team.Status})
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
	restored = a.checkTeamDirectory(ctx, restored)
	a.recordEvent(ctx, domain.Event{Type: domain.EventTeamRestored, TeamID: teamID, ResourceID: teamID, Revision: restored.Revision})
	a.Hub.Publish()
	return restored, nil
}

// DeleteTeam removes an archived team, or one whose folder is gone,
// completely and for good: every row of the team, its members' engine
// threads and the .cobot folder CoBot keeps in the project. Archiving
// already proved the team idle, so nothing is checked again here; a team
// whose folder is gone cannot finish its work and loses it with the team.
// Project files outside .cobot are never touched.
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
	team, directory, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if team.Status != domain.TeamStatusArchived && team.Status != domain.TeamStatusDirectoryMissing {
		return domain.Team{}, domain.Conflict("error.team.not_archived", map[string]any{"status": team.Status})
	}
	if expectedRevision > 0 && team.Revision != expectedRevision {
		return domain.Team{}, domain.RevisionConflict("error.team.revision_conflict", map[string]any{"expected": expectedRevision, "actual": team.Revision})
	}
	a.releaseTeamRuntime(ctx, teamID)
	threads, err := sqlite.TeamThreadIDs(ctx, a.DB, teamID)
	if err != nil {
		return domain.Team{}, err
	}
	bots := mustListBots(ctx, a, teamID)

	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	if err := sqlite.PurgeTeamTx(ctx, tx, teamID); err != nil {
		return domain.Team{}, err
	}
	team.Status = domain.TeamStatusDeleted
	if err := a.saveWriteResult(ctx, tx, principal, team); err != nil {
		return domain.Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}

	if a.CollaborationTokens != nil {
		for _, bot := range bots {
			a.CollaborationTokens.RevokeOfBot(bot.ID)
		}
	}
	a.deleteEngineThreads(ctx, teamID, threads)
	a.removeTeamFolderData(directory, teamID)
	a.Hub.Publish()
	return team, nil
}

// releaseTeamRuntime shuts the team's engine down and revokes nothing else;
// archive and delete both leave no idle process behind.
func (a *App) releaseTeamRuntime(ctx context.Context, teamID string) {
	if a.Scheduler == nil {
		return
	}
	if err := a.Scheduler.StopTeam(ctx, teamID); err != nil && a.Logger != nil {
		a.Logger.Warn("stop team engine", "teamId", teamID, "error", err)
	}
}

// deleteEngineThreads removes a deleted team's threads from the private
// engine environment. It is best effort: a thread the engine cannot delete
// now is logged, never a reason to keep the team.
func (a *App) deleteEngineThreads(ctx context.Context, teamID string, threads []string) {
	if len(threads) == 0 {
		return
	}
	server, err := a.control(ctx)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Warn("engine threads of deleted team kept: engine unavailable", "teamId", teamID, "threads", len(threads), "error", err)
		}
		return
	}
	for _, thread := range threads {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if err := server.DeleteThread(callCtx, thread); err != nil && a.Logger != nil {
			a.Logger.Warn("delete engine thread of deleted team", "teamId", teamID, "threadId", thread, "error", err)
		}
		cancel()
	}
}

// removeTeamFolderData deletes the .cobot folder CoBot keeps in a deleted
// team's project, unless its marker names another team. Nothing else in the
// project is touched; a folder that is gone is simply skipped.
func (a *App) removeTeamFolderData(dir, teamID string) {
	if dir == "" {
		return
	}
	marker, present, err := readTeamMarker(dir)
	if err != nil {
		return
	}
	if present && (marker.InstanceID != a.InstanceID || marker.TeamID != teamID) {
		return
	}
	if err := os.RemoveAll(filepath.Join(dir, ".cobot")); err != nil && a.Logger != nil {
		a.Logger.Warn("remove .cobot of deleted team", "teamId", teamID, "error", err)
	}
}

// DeleteBot removes a member completely, under the same rule as archiving
// a team: it must be idle — no live run, no message waiting for it, no
// pending approval or open question it raised, no hand-over it is part of
// and no unfinished task it owns — or the delete is refused with every
// blocker named. What is its own in this team goes with it (see
// sqlite.PurgeBotTx) together with its engine thread; messages it sent to
// other members stay as their history. Its role definition is untouched and
// stays available to every team. The leader cannot be deleted (B08).
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
	if expectedRevision > 0 && bot.Revision != expectedRevision {
		return domain.Bot{}, domain.RevisionConflict("error.bot.revision_conflict", map[string]any{"expected": expectedRevision, "actual": bot.Revision})
	}
	threads, err := sqlite.BotThreadIDs(ctx, a.DB, botID)
	if err != nil {
		return domain.Bot{}, err
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Bot{}, err
	}
	defer tx.Rollback()
	work, err := sqlite.BotOpenWork(ctx, tx, botID)
	if err != nil {
		return domain.Bot{}, err
	}
	if archiveBlockers(work) {
		return domain.Bot{}, openWorkError("bot", work)
	}
	if err := sqlite.PurgeBotTx(ctx, tx, botID); err != nil {
		return domain.Bot{}, err
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
	a.deleteMemberThreads(ctx, bot.TeamID, threads)
	// The event names the removal for live views, not the member's data.
	a.recordEvent(ctx, domain.Event{Type: domain.EventBotDeleted, TeamID: bot.TeamID, ResourceID: botID})
	a.Hub.Publish()
	return bot, nil
}

// deleteMemberThreads deletes a removed member's engine threads through
// the team's running engine when there is one, else the control engine.
func (a *App) deleteMemberThreads(ctx context.Context, teamID string, threads []string) {
	if len(threads) == 0 {
		return
	}
	if a.Scheduler != nil {
		if handled, err := a.Scheduler.DeleteThreads(ctx, teamID, threads); handled {
			if err != nil && a.Logger != nil {
				a.Logger.Warn("delete engine thread of deleted member", "teamId", teamID, "error", err)
			}
			return
		}
	}
	a.deleteEngineThreads(ctx, teamID, threads)
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
