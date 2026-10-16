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
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

func TestRecruitmentFeedbackKeepsTaskChainAndQuestionGate(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "root"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 3*time.Second, "root run", func() bool { return len(fake.Turns()) == 1 })
	principal := auth.NewPrincipalBot(bot.ID, team.ID)
	task, err := h.app.CreateTask(ctx, principal, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: bot.ID, Title: "task requesting recruitment"})
	if err != nil {
		t.Fatal(err)
	}
	first := fake.Turns()[0]
	fake.EmitTurnCompleted(first.ThreadID, first.TurnID, "completed", "assigned")
	waitUntil(t, 3*time.Second, "task run", func() bool { return len(fake.Turns()) == 2 })
	run, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	principal.RunID = run.ID
	chainID, err := sqlite.ChainOfRun(ctx, h.app.DB, run.ID)
	if err != nil || chainID == "" {
		t.Fatal("task lost root chain", err)
	}
	card, err := h.app.RequestMember(ctx, principal, "backend-dev", "help this task")
	if err != nil {
		t.Fatal(err)
	}
	question, err := h.app.AskUser(ctx, principal, app.AskUserInput{TaskID: task.ID, Question: "keep waiting"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.DecideApproval(ctx, h.user, card.ID, "reject", "", card.Revision); err != nil {
		t.Fatal(err)
	}
	var gotTask, gotChain, source, status string
	if err := h.app.DB.QueryRow(`SELECT task_id,chain_id,source_run_id,status FROM deliveries WHERE kind='approval_result' AND source_run_id=?`, run.ID).Scan(&gotTask, &gotChain, &source, &status); err != nil {
		t.Fatal(err)
	}
	if gotTask != task.ID || gotChain != chainID || source != run.ID || status != domain.DeliveryQueued {
		t.Fatalf("feedback escaped source: %q %q %q %q", gotTask, gotChain, source, status)
	}
	second := fake.Turns()[1]
	fake.EmitTurnCompleted(second.ThreadID, second.TurnID, "completed", "waiting")
	waitUntil(t, 3*time.Second, "question state", func() bool {
		b, _ := sqlite.GetBot(ctx, h.app.DB, bot.ID)
		return b.Status == domain.BotStatusWaitingInput
	})
	if len(fake.Turns()) != 2 {
		t.Fatal("feedback bypassed open task question")
	}
	if _, err := h.app.AnswerQuestion(ctx, h.user, question.ID, "ready", question.Revision); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 3*time.Second, "feedback released", func() bool { return len(fake.Turns()) == 3 })
	if !strings.Contains(fake.Turns()[2].Text, "approval_result") {
		t.Fatal("feedback not delivered after answer")
	}
}

func TestPausedChainDoesNotBlockNewUserChain(t *testing.T) {
	for _, held := range []string{domain.ChainAwaitingApproval, domain.ChainEvaluating} {
		t.Run(held, func(t *testing.T) {
			h := newM2Harness(t)
			ctx := context.Background()
			team, bots := h.createTeam(t, []string{"backend-dev"})
			if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[0].ID, Text: "old root"}); err != nil {
				t.Fatal(err)
			}
			fake := h.fakeFor(t, team.ID)
			waitUntil(t, 3*time.Second, "root", func() bool { return len(fake.Turns()) == 1 })
			run, _ := sqlite.FindActiveRun(ctx, h.app.DB, bots[0].ID)
			chainID, err := sqlite.ChainOfRun(ctx, h.app.DB, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Seed the persisted gate state; the subsequent messages/dispatch are real app calls.
			if _, err := h.app.DB.Exec(`UPDATE chains SET status=? WHERE id=?`, held, chainID); err != nil {
				t.Fatal(err)
			}
			if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(bots[0].ID, team.ID), bots[1].ID, "old held work", "", "old-held"); err != nil {
				t.Fatal(err)
			}
			if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[1].ID, Text: "new independent input"}); err != nil {
				t.Fatal(err)
			}
			waitUntil(t, 3*time.Second, "independent user dispatch", func() bool { return len(fake.Turns()) == 2 })
			if !strings.Contains(fake.Turns()[1].Text, "new independent input") {
				t.Fatal("dispatched held work")
			}
			chain, _ := sqlite.GetChain(ctx, h.app.DB, chainID)
			if chain.Status != held {
				t.Fatal("new input changed old gate")
			}
			var n int
			if err := h.app.DB.QueryRow(`SELECT count(*) FROM deliveries WHERE chain_id=? AND target_bot_id=? AND status='queued'`, chainID, bots[1].ID).Scan(&n); err != nil || n != 1 {
				t.Fatalf("held work changed: %d %v", n, err)
			}
		})
	}
}

func TestUnknownExecutionHasRecoveryCardWithoutCoreRestart(t *testing.T) {
	for _, mode := range []string{"start", "exit"} {
		t.Run(mode, func(t *testing.T) {
			h := newM2Harness(t)
			ctx := context.Background()
			team, bots := h.createTeam(t, nil)
			if mode == "start" {
				h.onFake = func(fake *fakeengine.Server) {
					fake.BeforeStartTurn = func() error { return errors.New("lost start reply") }
				}
			}
			if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[0].ID, Text: "work"}); err != nil {
				t.Fatal(err)
			}
			fake := h.fakeFor(t, team.ID)
			if mode == "exit" {
				waitUntil(t, 3*time.Second, "running", func() bool { return len(fake.Turns()) == 1 })
				fake.EmitExit(errors.New("connection gone"))
			}
			var card domain.Approval
			waitUntil(t, 3*time.Second, "live recovery card", func() bool {
				cards, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
				for _, c := range cards {
					if c.Type == domain.ApprovalRecovery {
						card = c
						return true
					}
				}
				return false
			})
			run, err := sqlite.FindActiveRun(ctx, h.app.DB, bots[0].ID)
			if err != nil || run.Status != domain.RunUnknown {
				t.Fatal(run, err)
			}
			if _, err := h.app.DecideApproval(ctx, h.user, card.ID, domain.RecoveryOptionAbandon, "", card.Revision); err == nil {
				t.Fatal("unverified execution was released")
			}
			cards, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
			if len(cards) != 1 || cards[0].ID != card.ID {
				t.Fatal("failed verification consumed/duplicated card")
			}
			if mode == "exit" {
				// The failed verification created a fresh inspector, not proof of termination.
				session, err := sqlite.GetProviderSession(ctx, h.app.DB, bots[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				current := h.fakeFor(t, team.ID)
				current.SetTurnFact(session.ProviderThreadID, run.ProviderTurnID, domain.RunInterrupted)
				if _, err := h.app.DecideApproval(ctx, h.user, card.ID, domain.RecoveryOptionAbandon, "", card.Revision); err != nil {
					t.Fatal(err)
				}
				if _, err := sqlite.FindActiveRun(ctx, h.app.DB, bots[0].ID); !errors.Is(err, sqlite.ErrNotFound) {
					t.Fatal("verified run remained locked", err)
				}
			}
		})
	}
}

func TestSnapshotCannotSkipCommittedSettingsEvent(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	writes := make(chan error, 1)
	write := func(language string) error {
		tx, err := h.app.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		s, err := sqlite.GetSettings(ctx, tx)
		if err != nil {
			return err
		}
		saved, err := sqlite.UpdateSettingsTx(ctx, tx, domain.SettingsPatch{Language: &language}, s.Revision)
		if err != nil {
			return err
		}
		if _, err := sqlite.InsertEvent(ctx, tx, h.app.InstanceID, domain.Event{Type: domain.EventSettingsUpdated, Revision: saved.Revision, ResourceID: "settings"}); err != nil {
			return err
		}
		return tx.Commit()
	}
	go func() {
		for i := 0; i < 80; i++ {
			language := "en"
			if i%2 == 0 {
				language = "zh-CN"
			}
			if err := write(language); err != nil {
				writes <- err
				return
			}
		}
		writes <- nil
	}()
	for i := 0; i < 160; i++ {
		snapshot, err := h.app.Snapshot(ctx, h.user)
		if err != nil {
			t.Fatal(err)
		}
		var revision int64
		if err := h.app.DB.QueryRow(`SELECT coalesce(max(revision),0) FROM events WHERE seq<=? AND type=?`, snapshot.Seq, domain.EventSettingsUpdated).Scan(&revision); err != nil {
			t.Fatal(err)
		}
		if snapshot.Settings.Revision < revision {
			t.Fatalf("snapshot revision %d skipped event revision %d at seq %d", snapshot.Settings.Revision, revision, snapshot.Seq)
		}
	}
	if err := <-writes; err != nil {
		t.Fatal(err)
	}
}
