package app

import (
	"context"
	"database/sql"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The budget is a daily token limit. It counts what was spent since local
// midnight, so a held team starts again the next day without anyone touching
// it. The settings hold the default and say whether it applies to teams or to
// members; a team or a member can set its own limit, or no limit.
//
// A member lives inside its team, so its limit never exceeds the team's: a
// team with a limit has no member without one, and no member whose limit is
// above it. Both limits are always enforced; this rule keeps the numbers a
// person sets from promising more than the team can spend.

// maxDailyTokens bounds what a limit may be set to; far above any real spend,
// it only keeps a typo from overflowing a sum.
const maxDailyTokens = int64(1_000_000_000_000_000)

// What governs a line's limit.
const (
	limitedByNone    = "none"    // no limit applies
	limitedByDefault = "default" // the default in the settings
	limitedByOwn     = "own"     // the limit it set itself
	limitedByTeam    = "team"    // the team's limit, which is lower or the only one
)

// BudgetLine is one limit as the interface shows it.
type BudgetLine struct {
	// Override is the limit this team or member set itself: nil follows the
	// default, 0 is no limit.
	Override *int64 `json:"override"`
	// Default is what it follows when it sets none (0 for no limit).
	Default int64 `json:"default"`
	// Effective is the limit in force now (0 for none).
	Effective int64 `json:"effective"`
	// LimitedBy says what the effective limit comes from.
	LimitedBy string `json:"limitedBy"`
	// Ceiling is the most a member may set for itself: its team's limit, 0
	// when the team has none. Only member lines carry it.
	Ceiling int64 `json:"ceiling"`
	// UsedToday is what was spent since local midnight, or since the last
	// reset if that was later.
	UsedToday int64 `json:"usedToday"`
}

// MemberBudget is a member's line.
type MemberBudget struct {
	BotID string `json:"botId"`
	BudgetLine
}

// TeamBudget is a team's line and its members'.
type TeamBudget struct {
	Team    BudgetLine     `json:"team"`
	Members []MemberBudget `json:"members"`
}

// BudgetResult is what a limit change reports back.
type BudgetResult struct {
	// Clamped is how many members had a limit above the team's, or none, and
	// were brought down to the team's.
	Clamped int `json:"clamped"`
}

// budgetClock is the time the budget is counted against; tests replace it.
var budgetClock = time.Now

// ledgerStamp writes a moment in the one fixed-width form the budget compares
// times in: UTC to the millisecond. The ledger keeps its times with a fraction
// of any length, which does not compare as text, so SQL brings those to this
// form (see SumUsage) and everything here is written in it.
func ledgerStamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000")
}

// dayStart is local midnight of the day containing now, as a ledger stamp.
func dayStart(now time.Time) string {
	local := now.Local()
	return ledgerStamp(time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()))
}

// latest is the later of several ledger stamps (they compare as text).
func latest(stamps ...string) string {
	out := ""
	for _, stamp := range stamps {
		if stamp > out {
			out = stamp
		}
	}
	return out
}

// defaultLimit is the settings' limit for one kind, 0 when it applies to the other.
func defaultLimit(settings domain.Settings, kind string) int64 {
	if (kind == sqlite.BudgetKindTeam && settings.BudgetScope == domain.BudgetScopeTeam) ||
		(kind == sqlite.BudgetKindBot && settings.BudgetScope == domain.BudgetScopeBot) {
		return settings.BudgetTokens
	}
	return 0
}

// limitOf resolves the limit a team or member holds by itself.
func limitOf(settings domain.Settings, kind string, overrides map[string]int64, id string) BudgetLine {
	line := BudgetLine{Default: defaultLimit(settings, kind), LimitedBy: limitedByNone}
	line.Effective = line.Default
	if line.Default > 0 {
		line.LimitedBy = limitedByDefault
	}
	if value, ok := overrides[id]; ok {
		v := value
		line.Override = &v
		line.Effective = value
		line.LimitedBy = limitedByOwn
		if value == 0 {
			line.LimitedBy = limitedByNone
		}
	}
	return line
}

// underTeam applies the team's limit to a member's line: the member is held to
// whichever is lower, and one with no limit of its own is held to the team's.
func underTeam(line BudgetLine, teamLimit int64) BudgetLine {
	line.Ceiling = teamLimit
	if teamLimit > 0 && (line.Effective == 0 || line.Effective > teamLimit) {
		line.Effective = teamLimit
		line.LimitedBy = limitedByTeam
	}
	return line
}

// budgetPaused evaluates the daily budget against the ledger for the member's
// next turn: its team's limit and its own, whichever are in force. With none in
// force nothing is paused (the default). Unknown usage today pauses too: a
// budget cannot be trusted without knowing the spend, and the product says so
// instead of guessing. The sum may overshoot under concurrency; no precise
// hard cap is promised.
func (a *App) budgetPaused(ctx context.Context, teamID, botID string, settings domain.Settings) (*BudgetPause, error) {
	teamOverrides, err := sqlite.BudgetOverrides(ctx, a.DB, sqlite.BudgetKindTeam)
	if err != nil {
		return nil, err
	}
	botOverrides, err := sqlite.BudgetOverrides(ctx, a.DB, sqlite.BudgetKindBot)
	if err != nil {
		return nil, err
	}
	teamLimit := limitOf(settings, sqlite.BudgetKindTeam, teamOverrides, teamID).Effective
	botLimit := limitOf(settings, sqlite.BudgetKindBot, botOverrides, botID).Effective
	if teamLimit <= 0 && botLimit <= 0 {
		return nil, nil
	}
	resets, err := sqlite.BudgetResets(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	day := dayStart(budgetClock())
	teamSince := latest(day, resets[sqlite.BudgetKindTeam+"/"+teamID])
	botSince := latest(teamSince, resets[sqlite.BudgetKindBot+"/"+botID])
	// A turn cannot be stopped once it runs, so a new one only starts when
	// what is left covers about what this member's recent turns cost. It is
	// an estimate: a turn may still cost more than its predecessors.
	estimate, _, err := sqlite.AverageTurnUsage(ctx, a.DB, botID, reserveTurns)
	if err != nil {
		return nil, err
	}
	for _, scope := range []struct {
		name  string
		bot   string
		limit int64
		since string
	}{{"team", "", teamLimit, teamSince}, {"bot", botID, botLimit, botSince}} {
		if scope.limit <= 0 {
			continue
		}
		billable, unknown, err := sqlite.SumUsage(ctx, a.DB, teamID, scope.bot, scope.since)
		if err != nil {
			return nil, err
		}
		if unknown > 0 {
			return &BudgetPause{Reason: "usage_unknown", Scope: scope.name}, nil
		}
		if billable >= scope.limit {
			return &BudgetPause{Reason: "exceeded", Scope: scope.name}, nil
		}
		// Once something is spent, a turn waits if what is left would not cover
		// about what turns cost. With nothing spent, the first turn always
		// starts: a limit below a single turn's cost would otherwise leave the
		// team unable to work at all, however often it is reset.
		if estimate > 0 && billable > 0 && billable+estimate > scope.limit {
			return &BudgetPause{Reason: "reserve", Scope: scope.name}, nil
		}
	}
	return nil, nil
}

// TeamBudget reports a team's limit and each member's, with today's spend.
func (a *App) TeamBudget(ctx context.Context, principal auth.Principal, teamID string) (TeamBudget, error) {
	if principal.Kind != auth.KindUser {
		return TeamBudget{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if _, _, err := sqlite.GetTeam(ctx, a.DB, teamID); err != nil {
		return TeamBudget{}, sqlite.NormalizeError(err)
	}
	settings, err := sqlite.GetSettings(ctx, a.DB)
	if err != nil {
		return TeamBudget{}, err
	}
	teamOverrides, err := sqlite.BudgetOverrides(ctx, a.DB, sqlite.BudgetKindTeam)
	if err != nil {
		return TeamBudget{}, err
	}
	botOverrides, err := sqlite.BudgetOverrides(ctx, a.DB, sqlite.BudgetKindBot)
	if err != nil {
		return TeamBudget{}, err
	}
	resets, err := sqlite.BudgetResets(ctx, a.DB)
	if err != nil {
		return TeamBudget{}, err
	}
	teamSince := latest(dayStart(budgetClock()), resets[sqlite.BudgetKindTeam+"/"+teamID])
	out := TeamBudget{Team: limitOf(settings, sqlite.BudgetKindTeam, teamOverrides, teamID), Members: []MemberBudget{}}
	if out.Team.UsedToday, _, err = sqlite.SumUsage(ctx, a.DB, teamID, "", teamSince); err != nil {
		return TeamBudget{}, err
	}
	bots, err := sqlite.ListBots(ctx, a.DB, teamID)
	if err != nil {
		return TeamBudget{}, err
	}
	for _, bot := range bots {
		line := underTeam(limitOf(settings, sqlite.BudgetKindBot, botOverrides, bot.ID), out.Team.Effective)
		since := latest(teamSince, resets[sqlite.BudgetKindBot+"/"+bot.ID])
		if line.UsedToday, _, err = sqlite.SumUsage(ctx, a.DB, teamID, bot.ID, since); err != nil {
			return TeamBudget{}, err
		}
		out.Members = append(out.Members, MemberBudget{BotID: bot.ID, BudgetLine: line})
	}
	return out, nil
}

// SetTeamBudget sets a team's own daily limit; nil follows the default again
// and 0 is no limit. A member whose limit the new one undercuts is brought
// down to it in the same change. Held work the change frees is re-woken.
func (a *App) SetTeamBudget(ctx context.Context, principal auth.Principal, teamID string, limit *int64) (BudgetResult, error) {
	return a.setBudget(ctx, principal, sqlite.BudgetKindTeam, teamID, limit)
}

// SetBotBudget sets a member's own daily limit, the same way. It may not be
// above its team's limit, nor be none while the team has one.
func (a *App) SetBotBudget(ctx context.Context, principal auth.Principal, botID string, limit *int64) (BudgetResult, error) {
	return a.setBudget(ctx, principal, sqlite.BudgetKindBot, botID, limit)
}

func (a *App) setBudget(ctx context.Context, principal auth.Principal, kind, id string, limit *int64) (BudgetResult, error) {
	replay, unlock, writeErr := beginWrite[BudgetResult](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return BudgetResult{}, writeErr
	}
	if replay != nil {
		return *replay, nil
	}
	if principal.Kind != auth.KindUser {
		return BudgetResult{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if limit != nil && (*limit < 0 || *limit > maxDailyTokens) {
		return BudgetResult{}, domain.InvalidRequest("error.budget.limit_range", map[string]any{"max": maxDailyTokens})
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return BudgetResult{}, err
	}
	defer tx.Rollback()
	settings, err := sqlite.GetSettings(ctx, tx)
	if err != nil {
		return BudgetResult{}, err
	}
	var result BudgetResult
	switch kind {
	case sqlite.BudgetKindTeam:
		if _, _, err := sqlite.GetTeam(ctx, tx, id); err != nil {
			return BudgetResult{}, sqlite.NormalizeError(err)
		}
		if err := sqlite.SetBudgetOverride(ctx, tx, kind, id, limit); err != nil {
			return BudgetResult{}, err
		}
		result.Clamped, err = a.clampMembersToTeam(ctx, tx, settings, id)
		if err != nil {
			return BudgetResult{}, err
		}
	default:
		bot, err := sqlite.GetBot(ctx, tx, id)
		if err != nil {
			return BudgetResult{}, sqlite.NormalizeError(err)
		}
		teamOverrides, err := sqlite.BudgetOverrides(ctx, tx, sqlite.BudgetKindTeam)
		if err != nil {
			return BudgetResult{}, err
		}
		if ceiling := limitOf(settings, sqlite.BudgetKindTeam, teamOverrides, bot.TeamID).Effective; ceiling > 0 && limit != nil {
			if *limit == 0 {
				return BudgetResult{}, domain.InvalidRequest("error.budget.unlimited_under_team_limit", map[string]any{"teamLimit": ceiling})
			}
			if *limit > ceiling {
				return BudgetResult{}, domain.InvalidRequest("error.budget.above_team_limit", map[string]any{"teamLimit": ceiling})
			}
		}
		if err := sqlite.SetBudgetOverride(ctx, tx, kind, id, limit); err != nil {
			return BudgetResult{}, err
		}
	}
	if err := a.saveWriteResult(ctx, tx, principal, result); err != nil {
		return BudgetResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return BudgetResult{}, err
	}
	a.Hub.Publish()
	// A limit raised or removed can free work the old one held.
	a.ResumeBudgetPausedBots(ctx)
	return result, nil
}

// clampMembersToTeam brings the members of a team whose own limit is above
// the team's, or none, down to the team's limit. It reports how many it moved.
func (a *App) clampMembersToTeam(ctx context.Context, tx *sql.Tx, settings domain.Settings, teamID string) (int, error) {
	teamOverrides, err := sqlite.BudgetOverrides(ctx, tx, sqlite.BudgetKindTeam)
	if err != nil {
		return 0, err
	}
	ceiling := limitOf(settings, sqlite.BudgetKindTeam, teamOverrides, teamID).Effective
	if ceiling <= 0 {
		return 0, nil
	}
	botOverrides, err := sqlite.BudgetOverrides(ctx, tx, sqlite.BudgetKindBot)
	if err != nil {
		return 0, err
	}
	bots, err := sqlite.ListBots(ctx, tx, teamID)
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, bot := range bots {
		value, ok := botOverrides[bot.ID]
		if !ok || (value > 0 && value <= ceiling) {
			continue
		}
		down := ceiling
		if err := sqlite.SetBudgetOverride(ctx, tx, sqlite.BudgetKindBot, bot.ID, &down); err != nil {
			return 0, err
		}
		moved++
	}
	return moved, nil
}

// ResetTeamBudget starts counting today's spend again from now, for the team
// and, with it, every member. The ledger keeps what was spent; only what the
// budget counts moves.
func (a *App) ResetTeamBudget(ctx context.Context, principal auth.Principal, teamID string) error {
	return a.resetBudget(ctx, principal, sqlite.BudgetKindTeam, teamID)
}

// ResetBotBudget starts counting one member's spend again from now. The team's
// own count, which that member's spend is part of, is not touched.
func (a *App) ResetBotBudget(ctx context.Context, principal auth.Principal, botID string) error {
	return a.resetBudget(ctx, principal, sqlite.BudgetKindBot, botID)
}

func (a *App) resetBudget(ctx context.Context, principal auth.Principal, kind, id string) error {
	replay, unlock, writeErr := beginWrite[struct{}](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return writeErr
	}
	if replay != nil {
		return nil
	}
	if principal.Kind != auth.KindUser {
		return domain.Forbidden("error.forbidden.user_only", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if kind == sqlite.BudgetKindTeam {
		_, _, err = sqlite.GetTeam(ctx, tx, id)
	} else {
		_, err = sqlite.GetBot(ctx, tx, id)
	}
	if err != nil {
		return sqlite.NormalizeError(err)
	}
	if err := sqlite.SetBudgetReset(ctx, tx, kind, id, ledgerStamp(budgetClock())); err != nil {
		return err
	}
	if err := a.saveWriteResult(ctx, tx, principal, struct{}{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.Hub.Publish()
	// What the budget held can run again now.
	a.ResumeBudgetPausedBots(ctx)
	return nil
}

// StartBudgetDayWatch wakes held work when the day turns over: the budget is
// counted per day, and a team it held yesterday should not wait for a message
// to find out it may run. It checks every minute, so a computer that slept
// through midnight still catches up.
func (a *App) StartBudgetDayWatch(ctx context.Context) {
	go func() {
		day := dayStart(budgetClock())
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if now := dayStart(budgetClock()); now != day {
					day = now
					a.ResumeBudgetPausedBots(ctx)
				}
			}
		}
	}()
}
