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

func TestPausedChainDoesNotBlockNewUserChain(t *testing.T) {
	for _, held := range []string{domain.ChainAwaitingApproval, domain.ChainEvaluating} {
		t.Run(held, func(t *testing.T) {
			h := newM2Harness(t)
			ctx := context.Background()
			team, bots := h.createTeam(t, []string{"fullstack-engineer"})
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
			if _, err := h.app.DecideApproval(ctx, h.user, card.ID, domain.RecoveryOptionAbandon, "", card.Revision, nil); err == nil {
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
				if _, err := h.app.DecideApproval(ctx, h.user, card.ID, domain.RecoveryOptionAbandon, "", card.Revision, nil); err != nil {
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
