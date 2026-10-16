package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

// A native inspector fact is independent of the product's unknown DB row.
// Positive recovery tests must explicitly provide that terminal evidence.
func proveSeededRunEnded(t *testing.T, h *m2Harness, runID, status string) {
	t.Helper()
	ctx := context.Background()
	run, err := sqlite.GetRun(ctx, h.app.DB, runID)
	if err != nil {
		t.Fatal(err)
	}
	fake := h.ensureFake(t, run.TeamID)
	thread := "persisted-" + run.ID
	fake.SetTurnFact(thread, run.ProviderTurnID, status)
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.UpsertProviderSession(ctx, tx, domain.ProviderSession{BotID: run.BotID, ProviderThreadID: thread, EngineEpoch: run.EngineEpoch}); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryRequiresExactTerminalFactBeforeDecision(t *testing.T) {
	for _, providerStatus := range []string{"", "inProgress", "completed", "failed", "interrupted"} {
		t.Run("provider_"+providerStatus, func(t *testing.T) {
			h := newM2Harness(t)
			ctx := context.Background()
			team, bots := h.createTeam(t, []string{"backend-dev"})
			runID, _ := seedUnresolvedRun(t, h, team, bots[1], "no duplicate", "old-turn", "dead-epoch")
			if providerStatus != "" {
				proveSeededRunEnded(t, h, runID, providerStatus)
			}
			if err := h.app.ReconcileAfterRestart(ctx); err != nil {
				t.Fatal(err)
			}
			card := findRecoveryDecision(t, h, team.ID, runID)
			_, err := h.app.DecideApproval(ctx, h.user, card.ID, domain.RecoveryOptionAbandon, "", card.Revision)
			got, _ := sqlite.GetRun(ctx, h.app.DB, runID)
			if providerStatus == "" || providerStatus == "inProgress" {
				if err == nil || got.Status != domain.RunUnknown {
					t.Fatalf("unproven run released: %+v / %v", got, err)
				}
				pending, _ := sqlite.GetApproval(ctx, h.app.DB, card.ID)
				if pending.RequestState != domain.ApprovalStatePending {
					t.Fatal("unverified attempt consumed decision")
				}
			} else if err != nil || got.Status != providerStatus {
				t.Fatalf("terminal proof lost: %+v / %v", got, err)
			}
		})
	}
}

func TestIdleUnknownAndPredecessorBridgeCannotWrite(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	readToken := h.botToken(t, bots[1])
	args := map[string]any{"body": "unauthorized memory", "idempotencyKey": "idle-write"}
	assertRejected := func(token string) {
		t.Helper()
		status, body := h.callToolRaw(t, token, "remember", args)
		if status == 200 {
			data, _ := body["data"].(map[string]any)
			if data["status"] != "rejected" {
				t.Fatalf("write accepted: %+v", body)
			}
		} else if status != 401 {
			t.Fatalf("unexpected rejection transport: %d / %+v", status, body)
		}
	}
	assertRejected(readToken)
	binding, _ := h.tokens.Resolve(readToken)
	bound, finish := h.scriptExecution(t, binding)
	run, _ := sqlite.FindActiveRun(ctx, h.app.DB, bots[1].ID)
	status, body := h.callToolRaw(t, bound, "remember", args)
	if status != 200 || body["data"].(map[string]any)["status"] != "accepted" {
		t.Fatalf("live write rejected: %+v", body)
	}
	h.app.DB.ExecContext(ctx, "UPDATE runs SET status = ? WHERE id = ?", domain.RunUnknown, run.ID)
	assertRejected(bound)
	finish()
	assertRejected(bound)
	newToken, newFinish := h.scriptExecution(t, auth.BotBinding{BotID: bots[1].ID, TeamID: team.ID, EngineEpoch: binding.EngineEpoch})
	defer newFinish()
	assertRejected(bound)
	if _, err := h.tokens.Resolve(newToken); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicatePredecessorCompletionCannotFinishStartingSuccessor(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	fake := h.ensureFake(t, team.ID)
	send := func(key string) {
		t.Helper()
		result, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[1].ID, Text: key, RequestID: key})
		if err != nil {
			t.Fatal(err)
		}
		waitUntil(t, 3*time.Second, "accepted "+key, func() bool {
			m, e := sqlite.GetMessage(ctx, h.app.DB, result.Message.ID)
			return e == nil && m.DeliveryStatus == domain.DeliveryAccepted
		})
	}
	send("first")
	previous := fake.Turns()[0]
	fake.EmitTurnCompleted(previous.ThreadID, previous.TurnID, "completed", "first finished")
	fake.BeforeStartTurn = func() error {
		fake.EmitTurnCompleted(previous.ThreadID, previous.TurnID, "completed", "late duplicate")
		return nil
	}
	send("second")
	run, err := sqlite.FindActiveRun(ctx, h.app.DB, bots[1].ID)
	if err != nil || run.Status != domain.RunRunning || run.ProviderTurnID == previous.TurnID {
		t.Fatalf("stale predecessor consumed new run: %+v / %v", run, err)
	}
}

type completionBeforeReply struct{ *fakeengine.Server }

func (s *completionBeforeReply) StartTurn(ctx context.Context, thread, text, model string) (string, error) {
	id, err := s.Server.StartTurn(ctx, thread, text, model)
	if err == nil {
		s.EmitTurnCompleted(thread, id, "completed", "finished before response")
	}
	return id, err
}

type approvalBeforeReply struct{ *fakeengine.Server }

func (s *approvalBeforeReply) StartTurn(ctx context.Context, thread, text, model string) (string, error) {
	id, err := s.Server.StartTurn(ctx, thread, text, model)
	if err == nil {
		s.EmitApproval("early-approval", thread, id, "approval before response")
	}
	return id, err
}

func TestApprovalFactSurvivesLateStartReply(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	h.app.Scheduler.Stop(ctx)
	var fake *fakeengine.Server
	s := app.NewScheduler(h.app, func(spec app.ServerSpec) (engine.Server, error) {
		fake = fakeengine.New(spec.Callbacks, "early-approval-epoch")
		return &approvalBeforeReply{fake}, nil
	})
	h.app.Scheduler = s
	s.Start()
	defer s.Stop(ctx)
	team, bots := h.createTeam(t, []string{"backend-dev"})
	sent, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[1].ID, Text: "please approve"})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 3*time.Second, "ack", func() bool {
		msg, e := sqlite.GetMessage(ctx, h.app.DB, sent.Message.ID)
		return e == nil && msg.DeliveryStatus == domain.DeliveryAccepted
	})
	run, err := sqlite.FindActiveRun(ctx, h.app.DB, bots[1].ID)
	if err != nil || run.Status != domain.RunWaitingApproval || run.ProviderTurnID == "" {
		t.Fatalf("approval overwritten: %+v / %v", run, err)
	}
	turn := fake.Turns()[0]
	fake.EmitApproval("early-approval", turn.ThreadID, turn.TurnID, "duplicate request")
	cards, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	if len(cards) != 1 {
		t.Fatalf("duplicate approval created %d cards", len(cards))
	}
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "one answer")
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "duplicate answer")
	var count int
	h.app.DB.QueryRow("SELECT count(*) FROM messages WHERE run_id = ? AND kind = ?", run.ID, domain.MessageKindAssistant).Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate completion wrote %d answers", count)
	}
}

func TestCompletedFactSurvivesLateStartReply(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	h.app.Scheduler.Stop(ctx)
	s := app.NewScheduler(h.app, func(spec app.ServerSpec) (engine.Server, error) {
		return &completionBeforeReply{fakeengine.New(spec.Callbacks, "early-completion")}, nil
	})
	h.app.Scheduler = s
	s.Start()
	defer s.Stop(ctx)
	_, bots := h.createTeam(t, []string{"backend-dev"})
	result, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[1].ID, Text: "quick", RequestID: "early-result"})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 3*time.Second, "run created", func() bool {
		var n int
		h.app.DB.QueryRow("SELECT count(*) FROM runs WHERE bot_id = ?", bots[1].ID).Scan(&n)
		return n == 1
	})
	waitUntil(t, 3*time.Second, "start response persisted", func() bool {
		m, e := sqlite.GetMessage(ctx, h.app.DB, result.Message.ID)
		return e == nil && m.DeliveryStatus == domain.DeliveryAccepted
	})
	if r, e := sqlite.FindActiveRun(ctx, h.app.DB, bots[1].ID); !errors.Is(e, sqlite.ErrNotFound) {
		t.Fatalf("late response resurrected run %+v: %v", r, e)
	}
}

type slowStartReply struct {
	*fakeengine.Server
	entered chan struct{}
}

func (s *slowStartReply) StartTurn(ctx context.Context, thread, text, model string) (string, error) {
	if strings.HasSuffix(text, "slow") {
		close(s.entered)
		<-ctx.Done()
		return "", ctx.Err()
	}
	return s.Server.StartTurn(ctx, thread, text, model)
}

func TestSlowEngineReplyDoesNotBlockOtherMemberOrStop(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	h.app.Scheduler.Stop(ctx)
	entered := make(chan struct{})
	var fake *fakeengine.Server
	s := app.NewScheduler(h.app, func(spec app.ServerSpec) (engine.Server, error) {
		fake = fakeengine.New(spec.Callbacks, "slow-start")
		return &slowStartReply{fake, entered}, nil
	})
	h.app.Scheduler = s
	s.Start()
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.Stop(c)
	})
	_, bots := h.createTeam(t, []string{"backend-dev"})
	h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[0].ID, Text: "slow", RequestID: "slow"})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("slow turn never started")
	}
	h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[1].ID, Text: "fast", RequestID: "fast"})
	waitUntil(t, 2*time.Second, "another member dispatch", func() bool { return len(fake.Turns()) == 1 })
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	started := time.Now()
	s.Stop(stopCtx)
	if time.Since(started) > 2500*time.Millisecond {
		t.Fatal("stop ignored deadline")
	}
}
