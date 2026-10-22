package app

import (
	"context"
	"errors"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/browser"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// UseBrowser runs one call of a member's browser tool in its team's browser.
// The browser belongs to the team (one profile, so a login made once serves
// every member) and each member works in its own tabs. The leader reaches no
// browser: it plans and reports, the members do the work.
func (a *App) UseBrowser(ctx context.Context, principal auth.Principal, req browser.Request) (browser.Result, error) {
	if err := a.ValidateToolRun(ctx, principal); err != nil {
		return browser.Result{}, err
	}
	if a.Browser == nil {
		return browser.Result{}, domain.CapabilityMissing("error.browser.unavailable", nil, nil)
	}
	bot, err := sqlite.GetBot(ctx, a.DB, principal.BotID)
	if err != nil {
		return browser.Result{}, sqlite.NormalizeError(err)
	}
	team, directory, err := sqlite.GetTeam(ctx, a.DB, bot.TeamID)
	if err != nil {
		return browser.Result{}, sqlite.NormalizeError(err)
	}
	if bridgeRole(team, bot) == "leader" {
		return browser.Result{}, domain.Forbidden("error.browser.leader", nil)
	}
	// The guide is read on demand, not carried in every instruction: it is
	// returned on request, and before a run's first real call.
	if req.Help {
		a.browserGuideRead.Store(principal.RunID, true)
		return browser.Result{Guide: browser.Guide()}, nil
	}
	if _, read := a.browserGuideRead.LoadOrStore(principal.RunID, true); !read {
		return browser.Result{Guide: browser.Guide(),
			Note: "这是第一次使用浏览器，先读上面的使用说明（含安全和确认规则），然后按说明重新发起刚才的调用。这次没有执行任何动作。"}, nil
	}
	result, err := a.Browser.Run(ctx, browser.Env{TeamID: team.ID, MemberID: bot.ID, TeamDir: directory}, req)
	switch {
	case err == nil:
		return result, nil
	case errors.Is(err, browser.ErrNotFound):
		return browser.Result{}, domain.CapabilityMissing("error.browser.not_installed", nil, err)
	case errors.Is(err, browser.ErrBadRequest), errors.Is(err, browser.ErrTabNotYour), errors.Is(err, browser.ErrTooMany):
		return browser.Result{}, domain.InvalidRequest("error.browser.bad_request", map[string]any{"reason": err.Error()})
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return browser.Result{}, err
	default:
		return browser.Result{}, domain.EngineUnavailable("error.browser.failed", map[string]any{"reason": err.Error()}, err)
	}
}

// releaseTeamBrowser closes a team's browser (archive and delete both leave
// no idle process behind); a deleted team's login state goes with it.
func (a *App) releaseTeamBrowser(teamID string, purge bool) {
	if a.Browser == nil {
		return
	}
	a.Browser.CloseTeam(teamID)
	if purge {
		if err := a.Browser.PurgeTeam(teamID); err != nil && a.Logger != nil {
			a.Logger.Warn("remove browser profile of deleted team", "teamId", teamID, "error", err)
		}
	}
}
