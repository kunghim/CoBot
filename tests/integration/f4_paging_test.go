package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

func TestCaptainCanSubmitReviewEvidenceForMemberTask(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	task, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: bots[1].ID, Title: "reviewed candidate"})
	if err != nil {
		t.Fatal(err)
	}
	captain := auth.NewPrincipalBot(bots[0].ID, team.ID)
	if _, err := h.app.SubmitEvidence(ctx, captain, task.ID, "Independent review reported by reviewer", []string{}, "candidate", "", "review passed"); err != nil {
		t.Fatal("captain lost evidence permission", err)
	}
	if _, err := h.app.SubmitEvidence(ctx, auth.NewPrincipalBot(bots[2].ID, team.ID), task.ID, "unowned evidence", nil, "candidate", "", "review"); err == nil {
		t.Fatal("unrelated member bypassed evidence ownership")
	}
}

func TestAssignmentCarriesFullTaskRequirementsAndIdentity(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"backend-dev"})
	task, err := h.app.CreateTask(context.Background(), h.user, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: bots[1].ID, Title: "scoped file", Description: "Use only the selected directory. Write EXACT_CONTENT. Do not run Git.", AcceptanceCriteria: "Read the exact file back"})
	if err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "task requirements dispatched", func() bool { return len(fake.Turns()) == 1 })
	text := fake.Turns()[0].Text
	for _, required := range []string{task.ID, task.OwnerID, "版本：1", task.Description, task.AcceptanceCriteria} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing task requirement: %s", required)
		}
	}
}

func TestScopedPagesKeepAllTiedRecordsAndDecidedHistory(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	stamp := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 120; i++ {
		id := fmt.Sprintf("record-%03d", i)
		owner := bots[1+i%2]
		if err := sqlite.InsertTask(ctx, tx, domain.Task{ID: id, TeamID: team.ID, OwnerID: owner.ID, Title: id, Status: domain.TaskTodo, Revision: 1, CreatedBy: "user"}); err != nil {
			t.Fatal(err)
		}
		if err := sqlite.InsertMessage(ctx, tx, domain.Message{ID: id, TeamID: team.ID, BotID: bots[1].ID, Kind: domain.MessageKindUser, Blocks: []byte(`[]`), CreatedAt: stamp}); err != nil {
			t.Fatal(err)
		}
		if err := sqlite.InsertQuestion(ctx, tx, domain.Question{ID: id, TeamID: team.ID, RequesterBotID: owner.ID, Question: id, Status: domain.QuestionOpen, Revision: 1}); err != nil {
			t.Fatal(err)
		}
		if err := sqlite.InsertMemory(ctx, tx, domain.Memory{ID: id, TeamID: team.ID, BotID: bots[1].ID, Body: id, NormKey: id, CreatedAt: stamp}); err != nil {
			t.Fatal(err)
		}
		card := domain.Approval{ID: id, TeamID: team.ID, RequesterBotID: owner.ID, Type: domain.ApprovalRecruitment, Summary: id, Options: []domain.ApprovalOption{{ID: "reject"}}, CreatedAt: stamp}
		if err := sqlite.InsertApproval(ctx, tx, card); err != nil {
			t.Fatal(err)
		}
		if _, err := sqlite.DecideApprovalTx(ctx, tx, id, "reject", "decision comment", "user", 1); err != nil {
			t.Fatal(err)
		}
		if i%10 == 0 {
			if err := sqlite.SetApprovalRequestState(ctx, tx, id, domain.ApprovalStateInvalidated); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := sqlite.InsertEvent(ctx, tx, "test", domain.Event{ID: id, Type: domain.EventBudgetPaused, TeamID: team.ID, BotID: owner.ID, CreatedAt: stamp}); err != nil {
			t.Fatal(err)
		}
	}
	// A never-decided invalidation belongs to operational facts, not decision history.
	if err := sqlite.InsertApproval(ctx, tx, domain.Approval{ID: "undecided-invalidated", TeamID: team.ID, RequesterBotID: bots[1].ID, Type: domain.ApprovalNativeTool, RequestState: domain.ApprovalStateInvalidated, CreatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"tasks", "questions"} {
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET created_at=?", stamp.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE approval_decisions SET decided_at=?`, stamp.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tasks", "questions", "approvals", "messages", "memories", "activity"} {
		for _, memberView := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/member=%t", kind, memberView), func(t *testing.T) {
				in := app.PageRequest{Kind: kind, TeamID: team.ID, Limit: 13}
				if kind == "approvals" {
					in.State = "history"
				}
				if kind == "questions" {
					in.State = "open"
				}
				if kind == "messages" || kind == "memories" {
					in.BotID = bots[1].ID
				}
				if memberView {
					in.ViewBotID = bots[1].ID
				} else {
					in.ViewBotID = bots[0].ID
				}
				seen := map[string]bool{}
				firstCursor := ""
				for {
					page, err := h.app.ListPage(ctx, h.user, in)
					if err != nil {
						t.Fatal(err)
					}
					key := "items"
					if kind == "messages" {
						key = "messages"
					}
					for _, item := range page[key].([]any) {
						id := ""
						switch value := item.(type) {
						case domain.Task:
							id = value.ID
							if memberView && value.OwnerID != bots[1].ID {
								t.Fatal("other member task")
							}
						case domain.Question:
							id = value.ID
						case domain.Approval:
							id = value.ID
							if value.Decision == nil {
								t.Fatal("undecided record in history")
							}
						case domain.Message:
							id = value.ID
						case domain.Memory:
							id = value.ID
						case app.ActivityItem:
							if value.Type != domain.EventBudgetPaused {
								continue
							}
							id = value.ID
						}
						if seen[id] {
							t.Fatal("duplicate page item", id)
						}
						seen[id] = true
					}
					next := page["nextCursor"].(string)
					if firstCursor == "" {
						firstCursor = next
					}
					if next == "" {
						break
					}
					if next == in.Cursor {
						t.Fatal("cursor did not advance")
					}
					in.Cursor = next
				}
				want := 120
				if memberView && (kind == "tasks" || kind == "questions" || kind == "approvals" || kind == "activity") {
					want = 60
				}
				if len(seen) != want {
					t.Fatalf("records=%d want=%d", len(seen), want)
				}
				in.Cursor = firstCursor
				in.ViewBotID = bots[2].ID
				if _, err := h.app.ListPage(ctx, h.user, in); err == nil {
					t.Fatal("cursor reused across member scopes")
				}
			})
		}
	}
	other, _ := h.createTeamWithDir(t, "other", "other-team", nil)
	status, _ := h.userGet(t, "/api/v1/teams/"+other.ID+"/tasks?viewBotId="+bots[1].ID)
	if status != 400 {
		t.Fatalf("foreign view scope=%d", status)
	}
}
