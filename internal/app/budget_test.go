package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// noon is a fixed moment of a local day, so "today" and "yesterday" mean the
// same thing whenever the test runs.
func noon() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.Local)
}

func atNoon(t *testing.T) {
	t.Helper()
	budgetClock = noon
	t.Cleanup(func() { budgetClock = time.Now })
}

// seedTeam creates a team whose first member leads it.
func seedTeam(t *testing.T, a *App, teamID string, botIDs ...string) {
	t.Helper()
	var bots []domain.Bot
	for index, id := range botIDs {
		bots = append(bots, domain.Bot{ID: id, TeamID: teamID, Name: id, RoleID: "fullstack-engineer", IsLeader: index == 0})
	}
	tx, err := a.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	team := domain.Team{ID: teamID, Name: teamID, ProfileInstance: "p", LeaderID: botIDs[0], Status: domain.TeamStatusActive}
	if err := sqlite.InsertTeam(context.Background(), tx, team, "C:/"+teamID, teamID, bots); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// spend puts a turn's usage in the ledger.
func spend(t *testing.T, a *App, teamID, botID, turn string, tokens int64, at time.Time) {
	t.Helper()
	tx, err := a.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = sqlite.UpsertUsage(context.Background(), tx, domain.UsageEntry{
		ID: domain.NewID(), TeamID: teamID, BotID: botID, EngineEpoch: "e", ThreadID: "th", TurnID: turn,
		InputTokens: tokens, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func budgetApp(t *testing.T) *App {
	t.Helper()
	atNoon(t)
	return providerTestApp(t, newFakeBinder())
}

var user = auth.NewPrincipalUser("")

func teamDefault(tokens int64) domain.Settings {
	return domain.Settings{BudgetScope: domain.BudgetScopeTeam, BudgetTokens: tokens}
}

func TestTheBudgetCountsOnlyTodayAndStartsAgainTomorrow(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1")
	settings := teamDefault(100)

	spend(t, a, "t1", "b1", "yesterday", 50, noon().Add(-14*time.Hour))
	if pause, err := a.budgetPaused(context.Background(), "t1", "b1", settings); err != nil || pause != nil {
		t.Fatalf("yesterday's spend held today's work: %v %v", pause, err)
	}
	spend(t, a, "t1", "b1", "today", 120, noon().Add(-6*time.Hour))
	pause, err := a.budgetPaused(context.Background(), "t1", "b1", settings)
	if err != nil || pause == nil || pause.Reason != "exceeded" || pause.Scope != "team" {
		t.Fatalf("today's spend over the limit = %v %v, want the team's limit exceeded", pause, err)
	}
	// The next day the same spend no longer counts.
	budgetClock = func() time.Time { return noon().Add(24 * time.Hour) }
	if pause, err := a.budgetPaused(context.Background(), "t1", "b1", settings); err != nil || pause != nil {
		t.Fatalf("a new day still held the team: %v %v", pause, err)
	}
}

func TestNoBudgetByDefaultAndTheSettingsNameWhereItApplies(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1", "b-other")
	seedTeam(t, a, "t2", "b2")
	spend(t, a, "t1", "b1", "big", 1_000_000_000, noon().Add(-time.Hour))
	ctx := context.Background()
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", domain.Settings{}); pause != nil {
		t.Fatal("no budget is the default and must hold nothing")
	}
	if pause, _ := a.budgetPaused(ctx, "t2", "b2", teamDefault(100)); pause != nil {
		t.Fatal("another team's spend held this one")
	}
	member := domain.Settings{BudgetScope: domain.BudgetScopeBot, BudgetTokens: 100}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", member); pause == nil || pause.Scope != "bot" {
		t.Fatalf("the member that spent is held by the member default: %v", pause)
	}
	if pause, _ := a.budgetPaused(ctx, "t1", "b-other", member); pause != nil {
		t.Fatal("a member that spent nothing is held by someone else's spend")
	}
}

func TestATeamOrMemberCanSetItsOwnLimit(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1")
	ctx := context.Background()
	settings := teamDefault(100)
	spend(t, a, "t1", "b1", "today", 150, noon().Add(-time.Hour))

	if pause, _ := a.budgetPaused(ctx, "t1", "b1", settings); pause == nil {
		t.Fatal("the default limit should hold the team")
	}
	raised := int64(1000)
	if _, err := a.SetTeamBudget(ctx, user, "t1", &raised); err != nil {
		t.Fatal(err)
	}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", settings); pause != nil {
		t.Fatalf("a raised team limit still held it: %v", pause)
	}
	none := int64(0)
	if _, err := a.SetTeamBudget(ctx, user, "t1", &none); err != nil {
		t.Fatal(err)
	}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", settings); pause != nil {
		t.Fatal("an explicit no-limit still held the team")
	}
	// The team has none, so a member may set its own.
	small := int64(10)
	if _, err := a.SetBotBudget(ctx, user, "b1", &small); err != nil {
		t.Fatal(err)
	}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", settings); pause == nil || pause.Scope != "bot" {
		t.Fatalf("the member's own limit did not hold it: %v", pause)
	}
	if _, err := a.SetBotBudget(ctx, user, "b1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetTeamBudget(ctx, user, "t1", nil); err != nil {
		t.Fatal(err)
	}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", settings); pause == nil {
		t.Fatal("following the default again should hold the team as before")
	}
}

func refused(t *testing.T, err error, key string) {
	t.Helper()
	var de *domain.Error
	if !errors.As(err, &de) || de.MessageKey != key {
		t.Fatalf("error = %v, want %s", err, key)
	}
}

func TestAMemberNeverExceedsItsTeam(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1", "b2")
	ctx := context.Background()
	team := int64(100)
	if _, err := a.SetTeamBudget(ctx, user, "t1", &team); err != nil {
		t.Fatal(err)
	}

	above := int64(200)
	_, err := a.SetBotBudget(ctx, user, "b1", &above)
	refused(t, err, "error.budget.above_team_limit")
	none := int64(0)
	_, err = a.SetBotBudget(ctx, user, "b1", &none)
	refused(t, err, "error.budget.unlimited_under_team_limit")

	exactly := int64(100)
	if _, err := a.SetBotBudget(ctx, user, "b1", &exactly); err != nil {
		t.Fatalf("a member limit equal to the team's: %v", err)
	}

	// Only a team with no limit lets a member have none.
	unlimited := int64(0)
	if _, err := a.SetTeamBudget(ctx, user, "t1", &unlimited); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetBotBudget(ctx, user, "b2", &none); err != nil {
		t.Fatalf("an unlimited member under an unlimited team: %v", err)
	}
	huge := int64(5000)
	if _, err := a.SetBotBudget(ctx, user, "b1", &huge); err != nil {
		t.Fatalf("any limit under an unlimited team: %v", err)
	}
}

func TestLoweringTheTeamLimitBringsMembersDownWithIt(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1", "b2", "b3")
	ctx := context.Background()
	unlimited := int64(0)
	if _, err := a.SetTeamBudget(ctx, user, "t1", &unlimited); err != nil {
		t.Fatal(err)
	}
	big, small, free := int64(500), int64(40), int64(0)
	for id, value := range map[string]*int64{"b1": &big, "b2": &small, "b3": &free} {
		if _, err := a.SetBotBudget(ctx, user, id, value); err != nil {
			t.Fatal(err)
		}
	}

	team := int64(100)
	result, err := a.SetTeamBudget(ctx, user, "t1", &team)
	if err != nil {
		t.Fatal(err)
	}
	if result.Clamped != 2 {
		t.Fatalf("clamped %d members, want the two above the team's limit (one over, one unlimited)", result.Clamped)
	}
	got, err := a.TeamBudget(ctx, user, "t1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"b1": 100, "b2": 40, "b3": 100}
	for _, member := range got.Members {
		if member.Override == nil || *member.Override != want[member.BotID] {
			t.Errorf("%s override = %v, want %d", member.BotID, member.Override, want[member.BotID])
		}
		if member.Ceiling != 100 {
			t.Errorf("%s ceiling = %d, want the team's 100", member.BotID, member.Ceiling)
		}
	}
}

func TestAMemberWithoutAnOwnLimitIsHeldToItsTeamsAndTheLineSaysSo(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1")
	ctx := context.Background()
	team := int64(100)
	if _, err := a.SetTeamBudget(ctx, user, "t1", &team); err != nil {
		t.Fatal(err)
	}
	got, err := a.TeamBudget(ctx, user, "t1")
	if err != nil {
		t.Fatal(err)
	}
	line := got.Members[0]
	if line.Effective != 100 || line.LimitedBy != "team" || line.Ceiling != 100 {
		t.Fatalf("member line = %+v, want the team's limit as its own", line)
	}
	if got.Team.LimitedBy != "own" {
		t.Fatalf("team limited by %q, want own", got.Team.LimitedBy)
	}
}

func TestResettingStartsTheCountAgainWithoutTouchingTheLedger(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1", "b2")
	ctx := context.Background()
	settings := teamDefault(100)
	spend(t, a, "t1", "b1", "early", 100, noon().Add(-2*time.Hour))
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", settings); pause == nil {
		t.Fatal("the spend should hold the team before the reset")
	}

	if err := a.ResetTeamBudget(ctx, user, "t1"); err != nil {
		t.Fatal(err)
	}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", settings); pause != nil {
		t.Fatalf("a reset team is still held: %v", pause)
	}
	// What is spent after the reset counts again.
	spend(t, a, "t1", "b1", "late", 100, noon().Add(time.Minute))
	budgetClock = func() time.Time { return noon().Add(2 * time.Minute) }
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", settings); pause == nil {
		t.Fatal("spend after the reset should count")
	}
	// The ledger still holds every turn.
	billable, _, err := sqlite.SumUsage(ctx, a.DB, "t1", "", "")
	if err != nil || billable != 200 {
		t.Fatalf("ledger total = %d (%v), want it untouched", billable, err)
	}
}

func TestResettingAMemberLeavesTheTeamsCountAlone(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1", "b2")
	ctx := context.Background()
	spend(t, a, "t1", "b1", "early", 100, noon().Add(-2*time.Hour))
	member := domain.Settings{BudgetScope: domain.BudgetScopeBot, BudgetTokens: 100}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", member); pause == nil {
		t.Fatal("the member should be held before the reset")
	}
	if err := a.ResetBotBudget(ctx, user, "b1"); err != nil {
		t.Fatal(err)
	}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", member); pause != nil {
		t.Fatalf("a reset member is still held: %v", pause)
	}
	// The team's own count includes that spend and was not reset.
	team := domain.Settings{BudgetScope: domain.BudgetScopeTeam, BudgetTokens: 100}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", team); pause == nil {
		t.Fatal("resetting the member must not reset the team's count")
	}
}

func TestOnlyATeamOrMemberThatExistsCanBeReset(t *testing.T) {
	a := budgetApp(t)
	if err := a.ResetTeamBudget(context.Background(), user, "nobody"); err == nil {
		t.Fatal("a team that does not exist was reset")
	}
	if err := a.ResetBotBudget(context.Background(), user, "nobody"); err == nil {
		t.Fatal("a member that does not exist was reset")
	}
}

func TestALimitOutOfRangeIsRefused(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1")
	negative := int64(-1)
	huge := maxDailyTokens + 1
	for _, value := range []*int64{&negative, &huge} {
		if _, err := a.SetTeamBudget(context.Background(), user, "t1", value); err == nil {
			t.Fatalf("limit %d accepted", *value)
		}
	}
}

func TestTheDayStartsAtLocalMidnightInTheLedgersFormat(t *testing.T) {
	got := dayStart(time.Date(2026, 10, 24, 15, 30, 0, 0, time.Local))
	want := time.Date(2026, 10, 24, 0, 0, 0, 0, time.Local).UTC().Format("2006-01-02 15:04:05.000")
	if got != want {
		t.Fatalf("dayStart = %q, want %q", got, want)
	}
}

// openCard puts a hold in front of the user the way the scheduler does.
func openCard(t *testing.T, a *App, teamID, botID string, pause *BudgetPause) *domain.Approval {
	t.Helper()
	tx, err := a.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	card, err := a.requestBudgetDecision(context.Background(), tx, teamID, botID, pause)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return card
}

func TestAHoldAsksOnceForTheLimitThatHeldIt(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1", "b2")
	team := &BudgetPause{Reason: "exceeded", Scope: "team"}
	member := &BudgetPause{Reason: "exceeded", Scope: "bot"}

	first := openCard(t, a, "t1", "b1", team)
	if first == nil || first.Type != domain.ApprovalBudget || first.Summary != "approval.card.budget_team" || first.Reason != "budget_exceeded" {
		t.Fatalf("card = %+v", first)
	}
	if len(first.Options) != 2 || first.Options[0].ID != domain.BudgetOptionReset {
		t.Fatalf("options = %+v, want reset first", first.Options)
	}
	// The team's limit holds every member; they share the one question.
	if openCard(t, a, "t1", "b1", team) != nil || openCard(t, a, "t1", "b2", team) != nil {
		t.Fatal("a second card was opened for the same team limit")
	}
	// A member's own limit is its own question.
	if openCard(t, a, "t1", "b1", member) == nil {
		t.Fatal("no card for a member's own limit")
	}
	if openCard(t, a, "t1", "b2", member) == nil {
		t.Fatal("no card for the other member's own limit")
	}
	if openCard(t, a, "t1", "b1", member) != nil {
		t.Fatal("a second card for the same member")
	}
}

func TestResettingFromTheCardCountsAgainAndWaitingKeepsTheTeamHeldForTheDay(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1")
	ctx := context.Background()
	settings := teamDefault(100)
	spend(t, a, "t1", "b1", "early", 100, noon().Add(-2*time.Hour))
	pause, _ := a.budgetPaused(ctx, "t1", "b1", settings)
	if pause == nil {
		t.Fatal("expected a hold")
	}

	// Waiting leaves it held, and the question is not asked again today.
	card := openCard(t, a, "t1", "b1", pause)
	decided, err := a.DecideApproval(ctx, user, card.ID, domain.BudgetOptionWait, "", 0, nil)
	if err != nil || decided.RequestState != domain.ApprovalStateDecided {
		t.Fatalf("wait = %+v %v", decided, err)
	}
	if again, _ := a.budgetPaused(ctx, "t1", "b1", settings); again == nil {
		t.Fatal("waiting must not free the team")
	}
	if openCard(t, a, "t1", "b1", pause) != nil {
		t.Fatal("asked again the same day after the user chose to wait")
	}
	// Tomorrow the user's answer is a day old and the question stands.
	budgetClock = func() time.Time { return noon().Add(24 * time.Hour) }
	spend(t, a, "t1", "b1", "next", 100, noon().Add(25*time.Hour))
	budgetClock = func() time.Time { return noon().Add(26 * time.Hour) }
	if openCard(t, a, "t1", "b1", pause) == nil {
		t.Fatal("yesterday's wait carried over to a new day")
	}
}

func TestChoosingResetOnTheCardStartsTheCountAgain(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1")
	ctx := context.Background()
	settings := teamDefault(100)
	spend(t, a, "t1", "b1", "early", 100, noon().Add(-2*time.Hour))
	pause, _ := a.budgetPaused(ctx, "t1", "b1", settings)
	card := openCard(t, a, "t1", "b1", pause)

	if _, err := a.DecideApproval(ctx, user, card.ID, domain.BudgetOptionReset, "", 0, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := a.budgetPaused(ctx, "t1", "b1", settings); again != nil {
		t.Fatalf("the team is still held after the reset: %v", again)
	}
	// An answer cannot be given twice.
	if _, err := a.DecideApproval(ctx, user, card.ID, domain.BudgetOptionReset, "", 0, nil); err == nil {
		t.Fatal("a decided card was decided again")
	}
}

func TestACardThatNoLongerHoldsAnyoneIsClosed(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1")
	ctx := context.Background()
	if _, err := a.DB.Exec(`UPDATE settings SET budget_scope = 'team', budget_tokens = 100 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	settings := teamDefault(100)
	spend(t, a, "t1", "b1", "early", 100, noon().Add(-2*time.Hour))
	pause, _ := a.budgetPaused(ctx, "t1", "b1", settings)
	card := openCard(t, a, "t1", "b1", pause)

	// The limit is raised from somewhere else: the question has answered itself.
	raised := int64(1000)
	if _, err := a.SetTeamBudget(ctx, user, "t1", &raised); err != nil {
		t.Fatal(err)
	}
	got, err := sqlite.GetApproval(ctx, a.DB, card.ID)
	if err != nil || got.RequestState != domain.ApprovalStateInvalidated {
		t.Fatalf("card state = %q (%v), want invalidated once the team was free", got.RequestState, err)
	}
}

func TestTheFirstTurnOfTheDayStartsEvenWhenOneTurnCostsMoreThanTheLimit(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1")
	ctx := context.Background()
	// A turn costs 500; the limit is 100. Nothing is spent today.
	spend(t, a, "t1", "b1", "yesterday", 500, noon().Add(-14*time.Hour))
	if pause, err := a.budgetPaused(ctx, "t1", "b1", teamDefault(100)); err != nil || pause != nil {
		t.Fatalf("the first turn of the day was held by the reserve: %v %v", pause, err)
	}
	// After it, the limit is spent and holds the team.
	spend(t, a, "t1", "b1", "today", 500, noon().Add(-time.Hour))
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", teamDefault(100)); pause == nil || pause.Reason != "exceeded" {
		t.Fatalf("spend over the limit did not hold the team: %v", pause)
	}
}

// A reset and the spend around it can fall in the same second; the order inside
// the second still decides what counts.
func TestAResetSeparatesSpendWithinTheSameSecond(t *testing.T) {
	a := budgetApp(t)
	seedTeam(t, a, "t1", "b1")
	ctx := context.Background()
	second := noon().Add(-time.Hour)
	spend(t, a, "t1", "b1", "before", 100, second.Add(200*time.Millisecond))
	budgetClock = func() time.Time { return second.Add(900 * time.Millisecond) }
	if err := a.ResetTeamBudget(ctx, user, "t1"); err != nil {
		t.Fatal(err)
	}
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", teamDefault(100)); pause != nil {
		t.Fatalf("spend a moment before the reset still counts: %v", pause)
	}
	spend(t, a, "t1", "b1", "after", 100, second.Add(950*time.Millisecond))
	budgetClock = func() time.Time { return second.Add(time.Second) }
	if pause, _ := a.budgetPaused(ctx, "t1", "b1", teamDefault(100)); pause == nil {
		t.Fatal("spend a moment after the reset should count")
	}
}
