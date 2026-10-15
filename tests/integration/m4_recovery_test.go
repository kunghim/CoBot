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

// seedUnresolvedRun fabricates the exact state a previous process leaves
// behind: a live run row, a claimed delivery, and (optionally) the caller
// adds approvals — the reconciliation must not need anything else to work.
func seedUnresolvedRun(t *testing.T, h *m2Harness, team domain.Team, bot domain.Bot, text, turnID, epoch string) (runID, deliveryID string) {
	t.Helper()
	ctx := context.Background()
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("tx: %v", err)
	}
	defer tx.Rollback()
	run := domain.Run{
		ID: domain.NewID(), TeamID: team.ID, BotID: bot.ID,
		Status: domain.RunRunning, StartedAt: time.Now().UTC(),
		ProviderTurnID: turnID, EngineEpoch: epoch,
	}
	if err := sqlite.InsertRun(ctx, tx, run); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	blocks, _ := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	message := domain.Message{
		ID: domain.NewID(), TeamID: team.ID, BotID: bot.ID,
		Kind: domain.MessageKindSystem, Blocks: blocks, DeliveryStatus: domain.DeliveryQueued,
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		t.Fatalf("insert message: %v", err)
	}
	deliveryID = domain.NewID()
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: deliveryID, MessageID: message.ID, TeamID: team.ID,
		TargetBotID: bot.ID, Status: domain.DeliveryQueued,
	}); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
	if err := sqlite.ClaimDeliveryRun(ctx, tx, deliveryID, run.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return run.ID, deliveryID
}

// TestRestartReconciliationClassifiesUnresolvedWork pins H03/H04/H09 and the
// proactive half of F04: nothing is taken over, the unknown facts (turn id,
// epoch, delivery) are preserved rather than invented away, dead-connection
// approvals are invalidated while decided history is untouched, the locked
// bot cannot dispatch while an unrelated bot can, and reconciliation is
// idempotent.
func TestRestartReconciliationClassifiesUnresolvedWork(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	captain, lost := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)

	runID, deliveryID := seedUnresolvedRun(t, h, team, lost, "重启前的派发", "turn-7", "epoch-dead")

	// A native approval from the dead connection is pending; a decided one
	// must survive untouched.
	deadApproval := domain.Approval{
		ID: domain.NewID(), TeamID: team.ID, RequesterBotID: lost.ID,
		Type: domain.ApprovalNativeTool, EngineEpoch: "epoch-dead",
		EngineRequestID: json.RawMessage(`"req-1"`),
		Summary:         "旧连接的审批", Options: []domain.ApprovalOption{{ID: "accept"}, {ID: "decline"}},
		RequestState: domain.ApprovalStatePending,
	}
	apTx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("approval tx: %v", err)
	}
	if err := sqlite.InsertApproval(ctx, apTx, deadApproval); err != nil {
		apTx.Rollback()
		t.Fatalf("seed approval: %v", err)
	}
	if err := apTx.Commit(); err != nil {
		t.Fatalf("commit approval: %v", err)
	}

	if err := h.app.ReconcileAfterRestart(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// The unresolved run is unknown, keeps its dead connection's ids, and
	// its delivery is unknown too.
	run, err := sqlite.GetRun(ctx, h.app.DB, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != domain.RunUnknown || run.ErrorKey != "error.run.unknown_after_restart" {
		t.Fatalf("run = %s/%s, want unknown/restart reason", run.Status, run.ErrorKey)
	}
	if run.ProviderTurnID != "turn-7" || run.EngineEpoch != "epoch-dead" {
		t.Fatalf("run lost its original execution identity: turn %q epoch %q", run.ProviderTurnID, run.EngineEpoch)
	}
	delivery, err := sqlite.GetDelivery(ctx, h.app.DB, deliveryID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if delivery.Status != domain.DeliveryUnknown {
		t.Fatalf("delivery = %s, want unknown", delivery.Status)
	}
	member, err := sqlite.GetBot(ctx, h.app.DB, lost.ID)
	if err != nil || member.Status != domain.BotStatusUnknown {
		t.Fatalf("member = %+v (err %v), want unknown status", member, err)
	}

	// The dead connection's approval is invalidated, with its response
	// honestly expired.
	invalidated, err := sqlite.GetApproval(ctx, h.app.DB, deadApproval.ID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if invalidated.RequestState != domain.ApprovalStateInvalidated {
		t.Fatalf("native approval = %s, want invalidated", invalidated.RequestState)
	}
	if invalidated.ResponseState != domain.ResponseExpired {
		t.Fatalf("native approval response = %q, want expired", invalidated.ResponseState)
	}

	// Exactly one recovery card, bound to the run, with the three real
	// capabilities.
	cards, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	var recovery *domain.Approval
	for i := range cards {
		if cards[i].Type == domain.ApprovalRecovery {
			recovery = &cards[i]
		}
	}
	if recovery == nil {
		t.Fatal("unresolved run must produce one recovery card")
	}
	var binding map[string]string
	if err := json.Unmarshal(recovery.EngineRequestID, &binding); err != nil || binding["runId"] != runID {
		t.Fatalf("card binding = %v (err %v), want the unresolved run", binding, err)
	}
	if len(recovery.Options) != 3 {
		t.Fatalf("card options = %d, want continue/rerun/abandon", len(recovery.Options))
	}

	// Idempotence: a second reconcile never stacks a second card.
	if err := h.app.ReconcileAfterRestart(ctx); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	count := 0
	cards, _ = h.app.PendingApprovals(ctx, h.user, team.ID, "")
	for _, c := range cards {
		if c.Type == domain.ApprovalRecovery {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("recovery cards after second reconcile = %d, want 1", count)
	}

	// H04: the locked bot cannot start new work; an unrelated bot can.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: lost.ID, Text: "被锁成员的新消息"}); err != nil {
		t.Fatalf("msg to locked bot: %v", err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "正常成员的消息"}); err != nil {
		t.Fatalf("msg to healthy bot: %v", err)
	}
	waitUntil(t, 5*time.Second, "healthy bot turn", func() bool { return len(fake.Turns()) == 1 })
	time.Sleep(300 * time.Millisecond)
	if len(fake.Turns()) != 1 {
		t.Fatalf("locked bot dispatched anyway: %d turns", len(fake.Turns()))
	}
}

// TestRecoveryDecisionsContinueRerunAbandon pins H05: every choice first
// confirms the old execution ended, then continue preserves the unknown
// delivery as a fact, rerun requeues the original request, abandon cancels
// it, and each choice frees the bot for new work.
func TestRecoveryDecisionsContinueRerunAbandon(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	member := bots[1]
	fake := h.ensureFake(t, team.ID)

	decide := func(runID string, option string) {
		t.Helper()
		proveSeededRunEnded(t, h, runID, domain.RunInterrupted)
		if err := h.app.ReconcileAfterRestart(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		cards, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		if err != nil {
			t.Fatalf("pending: %v", err)
		}
		for _, card := range cards {
			if card.Type != domain.ApprovalRecovery {
				continue
			}
			var binding map[string]string
			_ = json.Unmarshal(card.EngineRequestID, &binding)
			if binding["runId"] != runID {
				continue
			}
			if _, err := h.app.DecideApproval(ctx, h.user, card.ID, option, "", card.Revision); err != nil {
				t.Fatalf("decide %s: %v", option, err)
			}
			return
		}
		t.Fatalf("no recovery card for run %s", runID)
	}

	// continue: run confirmed ended, unknown delivery preserved as-is.
	run1, delivery1 := seedUnresolvedRun(t, h, team, member, "继续的派发", "turn-a", "epoch-dead")
	decide(run1, domain.RecoveryOptionContinue)
	resolved, err := sqlite.GetRun(ctx, h.app.DB, run1)
	if err != nil || resolved.Status != domain.RunInterrupted {
		t.Fatalf("run after continue = %+v (err %v), want interrupted", resolved, err)
	}
	kept, err := sqlite.GetDelivery(ctx, h.app.DB, delivery1)
	if err != nil || kept.Status != domain.DeliveryUnknown {
		t.Fatalf("delivery after continue = %s (err %v), want unknown kept as a fact", kept.Status, err)
	}
	// The bot is free again.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: member.ID, Text: "恢复后的消息"}); err != nil {
		t.Fatalf("post-continue msg: %v", err)
	}
	waitUntil(t, 5*time.Second, "bot works after continue", func() bool { return len(fake.Turns()) == 1 })
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "completed", "好的")
	waitUntil(t, 5*time.Second, "run ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, member.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})

	// rerun: the original request is requeued and reaches the engine again.
	run2, delivery2 := seedUnresolvedRun(t, h, team, member, "重跑的原始派发", "turn-b", "epoch-dead")
	decide(run2, domain.RecoveryOptionRerun)
	waitUntil(t, 5*time.Second, "rerun dispatch", func() bool {
		return len(fake.Turns()) >= 2
	})
	last := fake.Turns()[len(fake.Turns())-1]
	if !endsWithString(last.Text, "重跑的原始派发") {
		t.Fatalf("rerun turn = %q, want the original request", last.Text)
	}
	requeued, err := sqlite.GetDelivery(ctx, h.app.DB, delivery2)
	if err != nil || (requeued.Status != domain.DeliveryDispatching && requeued.Status != domain.DeliveryAccepted) {
		t.Fatalf("delivery after rerun = %s (err %v), want dispatched again", requeued.Status, err)
	}
	fake.EmitTurnCompleted(last.ThreadID, last.TurnID, "completed", "完成")
	waitUntil(t, 5*time.Second, "rerun ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, member.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})

	// abandon: the delivery is cancelled and never redelivered.
	run3, delivery3 := seedUnresolvedRun(t, h, team, member, "放弃的派发", "turn-c", "epoch-dead")
	decide(run3, domain.RecoveryOptionAbandon)
	abandoned, err := sqlite.GetDelivery(ctx, h.app.DB, delivery3)
	if err != nil || abandoned.Status != domain.DeliveryCancelled {
		t.Fatalf("delivery after abandon = %s (err %v), want cancelled", abandoned.Status, err)
	}
	turnsBefore := len(fake.Turns())
	time.Sleep(300 * time.Millisecond)
	if len(fake.Turns()) != turnsBefore {
		t.Fatal("an abandoned delivery must never dispatch")
	}

	// Guard: resolving a run twice is refused; its state is already known.
	var approval domain.Approval
	if cards, err := h.app.ApprovalHistory(ctx, h.user, team.ID, 20); err == nil {
		for _, c := range cards {
			if c.Type == domain.ApprovalRecovery {
				approval = c
				break
			}
		}
	}
	if approval.ID == "" {
		t.Fatal("decided recovery card missing from history")
	}
	err = h.app.ApplyRecoveryDecision(ctx, approval, domain.RecoveryOptionContinue)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("second recovery decision = %v, want conflict not_unknown", err)
	}
}

// endsWithString mirrors the dispatch context prefix: the engine input ends
// with the raw message text.
func endsWithString(text, suffix string) bool {
	return len(text) >= len(suffix) && text[len(text)-len(suffix):] == suffix
}

// TestStopInterruptsInFlightRuns pins H01: shutting the scheduler down
// requests interruption of every in-flight turn, shuts its own engines down,
// and stops starting new work.
func TestStopInterruptsInFlightRuns(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	member := bots[1]
	fake := h.ensureFake(t, team.ID)

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: member.ID, Text: "关闭前的消息"}); err != nil {
		t.Fatalf("msg: %v", err)
	}
	waitUntil(t, 5*time.Second, "in-flight turn", func() bool { return len(fake.Turns()) == 1 })

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.app.Scheduler.Stop(shutdownCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if fake.ShutdownCalls < 1 {
		t.Fatal("stop must shut the team's engine down")
	}
	if len(fake.Interrupts()) == 0 {
		t.Fatal("stop must request interruption of the in-flight turn first")
	}
	// New work no longer dispatches: the loop is gone and the queue stays.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: member.ID, Text: "关闭后的消息"}); err != nil {
		t.Fatalf("queued msg: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if len(fake.Turns()) != 1 {
		t.Fatalf("dispatch continued after stop: %d turns", len(fake.Turns()))
	}
}
