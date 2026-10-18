package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// TestRerunFromAcceptedDelivery pins the real crash scenario the first fix
// round missed: the engine took the turn (delivery accepted) and the process
// died mid-execution. Rerun must requeue it, not refuse (review B blocker1).
func TestRerunFromAcceptedDelivery(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	lost := bots[1]

	runID, deliveryID := seedUnresolvedRun(t, h, team, lost, "崩溃时已接受的派发", "turn-x", "epoch-dead")
	proveSeededRunEnded(t, h, runID, domain.RunInterrupted)
	// The turn was accepted before the crash: the delivery mirrors that.
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SetDeliveryStatus(ctx, tx, deliveryID, domain.DeliveryAccepted); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := h.app.ReconcileAfterRestart(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	decision := findRecoveryDecision(t, h, team.ID, runID)
	if _, err := h.app.DecideApproval(ctx, h.user, decision.ID, domain.RecoveryOptionRerun, "", decision.Revision); err != nil {
		t.Fatalf("rerun decision: %v", err)
	}
	requeued, err := sqlite.GetDelivery(ctx, h.app.DB, deliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if requeued.Status != domain.DeliveryQueued {
		t.Fatalf("delivery after rerun = %s, want queued", requeued.Status)
	}
	run, err := sqlite.GetRun(ctx, h.app.DB, runID)
	if err != nil || run.Status != domain.RunInterrupted {
		t.Fatalf("run after rerun = %+v (err %v), want interrupted", run, err)
	}
}

// TestReplayStuckChainDecisionOnStartup pins the fix for the dead replay
// helper: a chain whose decision was recorded but whose side effect died with
// the process must be completed at startup, not stranded forever (review B
// blocker2).
func TestReplayStuckChainDecisionOnStartup(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	member := bots[1]

	// Seed exactly the crash window: chain awaiting, card decided, bound
	// delivery still queued.
	chain := domain.Chain{
		ID: "chain-stuck-1", TeamID: team.ID, OriginMessageID: "origin-msg",
		Epoch: 1, Status: domain.ChainAwaitingApproval, WakeCount: 2,
		LimitCount: 6, RiskStart: 5,
	}
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.InsertChain(ctx, tx, chain); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: team.ID, BotID: member.ID,
		Kind: domain.MessageKindSystem, DeliveryStatus: domain.DeliveryQueued,
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	deliveryID := domain.NewID()
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: deliveryID, MessageID: message.ID, TeamID: team.ID,
		TargetBotID: member.ID, Status: domain.DeliveryQueued, ChainID: chain.ID,
	}); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	binding, err := json.Marshal(map[string]any{
		"chainId": chain.ID, "deliveryId": deliveryID, "botId": member.ID,
	})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	card := domain.Approval{
		ID: domain.NewID(), TeamID: team.ID, RequesterBotID: member.ID,
		EngineRequestID: binding,
		Type:            domain.ApprovalChain, Summary: "approval.card.chain_risk",
		Reason: "risk_zone", RequestState: domain.ApprovalStateDecided,
	}
	if err := sqlite.InsertApproval(ctx, tx, card); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	// The crash happened between the decision CAS and the side effect: the
	// decision row exists while the chain still waits.
	if _, err := tx.Exec(
		`INSERT INTO approval_decisions (approval_id, option_id, comment, actor, decided_at) VALUES (?, ?, '', 'user-test', ?)`,
		card.ID, domain.ChainOptionContinue, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := h.app.ReconcileAfterRestart(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	after, err := sqlite.GetChain(ctx, h.app.DB, chain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.ChainActive {
		all, _ := sqlite.ListChains(ctx, h.app.DB, "", "", 10)
		var cards int
		_ = h.app.DB.QueryRow(`SELECT COUNT(*) FROM approvals WHERE type = 'chain'`).Scan(&cards)
		var decisions int
		_ = h.app.DB.QueryRow(`SELECT COUNT(*) FROM approval_decisions`).Scan(&decisions)
		var decisionOpt string
		_ = h.app.DB.QueryRow(`SELECT option_id FROM approval_decisions LIMIT 1`).Scan(&decisionOpt)
		t.Fatalf("stuck chain after reconcile = %s; chains=%d cards=%d decisions=%d opt=%q",
			after.Status, len(all), cards, decisions, decisionOpt)
	}
	if after.WakeCount != 3 {
		t.Fatalf("wake count after replay = %d, want the authorized wake counted once (3)", after.WakeCount)
	}
}

// findRecoveryDecision returns the pending recovery card bound to a run.
func findRecoveryDecision(t *testing.T, h *m2Harness, teamID, runID string) domain.Approval {
	t.Helper()
	pending, err := h.app.PendingApprovals(context.Background(), h.user, teamID, "")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	for _, card := range pending {
		if card.Type == domain.ApprovalRecovery {
			return card
		}
	}
	t.Fatalf("no recovery card for run %s", runID)
	return domain.Approval{}
}

// TestDecisionKeptWhenReplyFails restores the F03 evidence the first fix
// round accidentally removed: the decision is committed first and the reply
// failure only marks the response unknown — a second decision still
// conflicts (review A/B).
func TestDecisionKeptWhenReplyFails(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "干活"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitApproval("req-f03", turn.ThreadID, turn.TurnID, "dangerous thing")
	waitUntil(t, 5*time.Second, "pending", func() bool {
		items, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return len(items) == 1
	})
	pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")

	// The RPC delivery fails; the decision must still stand, truthfully
	// reporting that nothing reached the engine.
	fake.ReplyErr = errors.New("rpc connection lost")
	decided, err := h.app.DecideApproval(ctx, h.user, pending[0].ID, "accept", "", 0)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if decided.RequestState != domain.ApprovalStateDecided || decided.Decision == nil {
		t.Fatalf("decision must be kept: %+v", decided)
	}
	if decided.ResponseState != domain.ResponseUnknown {
		t.Fatalf("response = %q, want unknown after a failed reply", decided.ResponseState)
	}
	// Exactly once: the second decision conflicts.
	if _, err := h.app.DecideApproval(ctx, h.user, pending[0].ID, "decline", "", 0); err == nil {
		t.Fatal("second decision must conflict")
	}
	fake.ReplyErr = nil
}

// TestDecidedHistorySurvivesEngineExit: cards decided BEFORE a crash keep
// their decision and revision through the exit-time invalidation, which only
// touches pending requests (F04/F06 history rule).
func TestDecidedHistorySurvivesEngineExit(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "干活"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitApproval("req-hist", turn.ThreadID, turn.TurnID, "thing")
	waitUntil(t, 5*time.Second, "pending", func() bool {
		items, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return len(items) == 1
	})
	pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	decided, err := h.app.DecideApproval(ctx, h.user, pending[0].ID, "accept", "", 0)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	beforeRevision := decided.Revision

	// The connection dies afterwards: decided history must be untouched.
	fake.EmitExit(errors.New("process died"))
	time.Sleep(200 * time.Millisecond)
	after, err := sqlite.GetApproval(ctx, h.app.DB, decided.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RequestState != domain.ApprovalStateDecided || after.Decision == nil {
		t.Fatalf("decided history after exit = %+v, want decided with decision kept", after)
	}
	if after.Revision != beforeRevision {
		t.Fatalf("revision changed across exit: %d -> %d", beforeRevision, after.Revision)
	}
}
