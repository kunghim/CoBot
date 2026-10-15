package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

func TestNativeReplyCannotUndoNewerCompletionOrApproval(t *testing.T) {
	for _, later := range []string{"completed", "approval"} {
		t.Run(later, func(t *testing.T) {
			h := newM2Harness(t)
			ctx := context.Background()
			team, bots := h.createTeam(t, nil)
			bot := bots[0]
			if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "native approval"}); err != nil {
				t.Fatal(err)
			}
			fake := h.fakeFor(t, team.ID)
			waitUntil(t, 5*time.Second, "native turn", func() bool { return len(fake.Turns()) == 1 })
			turn := fake.Turns()[0]
			fake.EmitApproval("first", turn.ThreadID, turn.TurnID, "first request")
			waitUntil(t, 5*time.Second, "native card", func() bool { cards, _ := h.app.PendingApprovals(ctx, h.user, team.ID, ""); return len(cards) == 1 })
			cards, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
			fake.BeforeReplyReturn = func() {
				if later == "completed" {
					fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "done before reply ack")
					waitUntil(t, 5*time.Second, "early completion", func() bool {
						_, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID)
						return errors.Is(err, sqlite.ErrNotFound)
					})
				} else {
					fake.EmitApproval("second", turn.ThreadID, turn.TurnID, "later request")
					waitUntil(t, 5*time.Second, "later approval", func() bool {
						pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
						return len(pending) == 1 && pending[0].ID != cards[0].ID
					})
				}
			}
			p, err := app.WithWriteRequest(h.user, "test.native-decision", "one-reply", map[string]any{"id": cards[0].ID})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := h.app.DecideApproval(ctx, p, cards[0].ID, "accept", "", cards[0].Revision); err != nil {
					t.Fatal(err)
				}
			}
			if len(fake.Replies()) != 1 {
				t.Fatal("retry replied to native request twice")
			}
			if later == "completed" {
				if run, err := sqlite.GetRun(ctx, h.app.DB, cards[0].RunID); err != nil || run.Status != domain.RunCompleted {
					t.Fatalf("completion overwritten: %+v %v", run, err)
				}
			} else if run, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID); err != nil || run.Status != domain.RunWaitingApproval {
				t.Fatalf("later approval overwritten: %+v %v", run, err)
			}
		})
	}
}

func seedMerge(t *testing.T, h *m2Harness) (domain.Team, domain.Bot, domain.Task, domain.Evidence) {
	t.Helper()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	task := domain.Task{ID: domain.NewID(), TeamID: team.ID, OwnerID: bots[1].ID, Title: "exact candidate", AcceptanceCriteria: "tested and reviewed", Status: domain.TaskReview, Revision: 1, CreatedBy: domain.TaskAuthorUser}
	evidence := domain.Evidence{ID: domain.NewID(), TaskID: task.ID, AuthorID: bots[1].ID, Summary: "candidate report", CandidateRef: "candidate-sha", TestResult: "tests passed", ReviewResult: "review passed", ArtifactRefs: []string{"report.md"}, CreatedAt: time.Now().UTC()}
	tx, err := h.app.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := sqlite.InsertTask(context.Background(), tx, task); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.InsertEvidence(context.Background(), tx, evidence); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return team, bots[1], task, evidence
}

func TestMergeDecisionBindsEvidenceAndDeliversOnce(t *testing.T) {
	for _, option := range []string{"approve", "reject"} {
		t.Run(option, func(t *testing.T) {
			h := newM2Harness(t)
			ctx := context.Background()
			team, bot, task, e := seedMerge(t, h)
			fake := h.ensureFake(t, team.ID)
			card, err := h.app.SubmitMergeRequest(ctx, auth.NewPrincipalBot(bot.ID, team.ID), task.ID, e.CandidateRef, "main", team.Directory, []string{e.ID})
			if err != nil {
				t.Fatal(err)
			}
			if card.Details == nil || card.Details.TaskRevision != task.Revision || len(card.Details.Evidence) != 1 {
				t.Fatalf("missing authorization snapshot: %+v", card)
			}
			p, err := app.WithWriteRequest(h.user, "test.decision", "same-decision", map[string]any{"id": card.ID, "option": option})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				result, err := h.app.DecideApproval(ctx, p, card.ID, option, "reviewed", card.Revision)
				if err != nil || result.Decision == nil || result.Decision.OptionID != option {
					t.Fatalf("decision: %+v %v", result, err)
				}
			}
			waitUntil(t, 5*time.Second, "approval feedback", func() bool { return len(fake.Turns()) == 1 })
			var payload map[string]any
			if json.Unmarshal([]byte(fake.Turns()[0].Text), &payload) == nil {
				t.Fatal("engine input should include context as well as feedback")
			}
			var count int
			if err := h.app.DB.QueryRow(`SELECT count(*) FROM deliveries WHERE kind = 'approval_result' AND task_id = ?`, task.ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("feedback count=%d err=%v", count, err)
			}
			waitUntil(t, 5*time.Second, "feedback acknowledged", func() bool {
				got, _ := sqlite.GetApproval(ctx, h.app.DB, card.ID)
				return got.ResponseState == domain.ResponseSent
			})
			var blocks []domain.TextBlock
			var raw string
			if err := h.app.DB.QueryRow(`SELECT m.blocks FROM messages m JOIN deliveries d ON d.message_id=m.id WHERE d.kind='approval_result' AND d.task_id=?`, task.ID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(blocks[0].Text), &payload); err != nil {
				t.Fatal(err)
			}
			if payload["decision"] != option || payload["authorizedCandidate"] != (option == "approve") {
				t.Fatalf("incorrect feedback: %+v", payload)
			}
		})
	}
}

func TestChangedMergeFactsDoNotConsumeDecision(t *testing.T) {
	for _, change := range []string{"evidence", "revision", "directory"} {
		t.Run(change, func(t *testing.T) {
			h := newM2Harness(t)
			ctx := context.Background()
			team, bot, task, e := seedMerge(t, h)
			card, err := h.app.SubmitMergeRequest(ctx, auth.NewPrincipalBot(bot.ID, team.ID), task.ID, e.CandidateRef, "main", team.Directory, []string{e.ID})
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "evidence":
				_, err = h.app.DB.Exec(`UPDATE task_evidence SET candidate_ref = 'changed-sha' WHERE id=?`, e.ID)
			case "revision":
				_, err = h.app.DB.Exec(`UPDATE tasks SET revision=revision+1 WHERE id=?`, task.ID)
			case "directory":
				_, err = h.app.DB.Exec(`UPDATE directory_bindings SET directory=? WHERE team_id=?`, t.TempDir(), team.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.app.DecideApproval(ctx, h.user, card.ID, "approve", "", card.Revision); err == nil {
				t.Fatal("changed facts authorized")
			}
			got, err := sqlite.GetApproval(ctx, h.app.DB, card.ID)
			if err != nil || got.Decision != nil || got.RequestState != domain.ApprovalStatePending {
				t.Fatalf("invalid approval consumed: %+v %v", got, err)
			}
			if _, err := h.app.DecideApproval(ctx, h.user, card.ID, "reject", "stale", card.Revision); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecruitmentFeedbackFailureRollsBackMemberAndDecision(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	card, err := h.app.RequestMember(ctx, auth.NewPrincipalBot(bots[0].ID, team.ID), "backend-dev", "help")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.DB.Exec(`CREATE TRIGGER fail_feedback BEFORE INSERT ON deliveries WHEN NEW.kind='approval_result' BEGIN SELECT RAISE(ABORT,'feedback persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.DecideApproval(ctx, h.user, card.ID, "approve", "", card.Revision); err == nil {
		t.Fatal("partial decision committed")
	}
	got, _ := sqlite.GetApproval(ctx, h.app.DB, card.ID)
	members, _ := sqlite.ListBots(ctx, h.app.DB, team.ID)
	if got.Decision != nil || len(members) != 1 {
		t.Fatalf("partial commit: card=%+v members=%d", got, len(members))
	}
}
