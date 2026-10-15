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
)

func TestCancelledTaskCannotDispatchItsQueuedAssignment(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	owner := bots[1]
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: owner.ID, Text: "hold"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 3*time.Second, "busy owner", func() bool { return h.runStatus(t, owner.ID) == domain.RunRunning })
	task, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: owner.ID, Title: "must not run", AcceptanceCriteria: "cancel before start"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.UpdateTaskStatus(ctx, h.user, task.ID, domain.TaskCancelled, "", task.Revision); err != nil {
		t.Fatal(err)
	}
	var delivery, message string
	h.app.DB.QueryRow(`SELECT d.status,m.delivery_status FROM deliveries d JOIN messages m ON m.id=d.message_id WHERE d.task_id=?`, task.ID).Scan(&delivery, &message)
	if delivery != domain.DeliveryCancelled || message != domain.DeliveryCancelled {
		t.Fatalf("cancel projections diverged: %s/%s", delivery, message)
	}
	// Even a stale queued row must be revalidated at the dispatch boundary.
	h.app.DB.Exec(`UPDATE deliveries SET status=? WHERE task_id=?`, domain.DeliveryQueued, task.ID)
	turn := fake.Turns()[0]
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "held turn ended")
	waitUntil(t, 3*time.Second, "stale assignment cancelled", func() bool { return h.deliveryStatusOf(t, task.ID) == domain.DeliveryCancelled })
	if len(fake.Turns()) != 1 {
		t.Fatal("cancelled task started a provider turn")
	}
}

func TestOpenQuestionParksRelatedWorkButDoesNotConsumeUserMessage(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	owner := bots[1]
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: owner.ID, Text: "hold"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 3*time.Second, "first turn", func() bool { return h.runStatus(t, owner.ID) == domain.RunRunning })
	task, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: owner.ID, Title: "blocked by question", AcceptanceCriteria: "wait for answer"})
	if err != nil {
		t.Fatal(err)
	}
	question, err := h.app.AskUser(ctx, auth.NewPrincipalBot(owner.ID, team.ID), app.AskUserInput{Question: "Which output?", TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	first := fake.Turns()[0]
	fake.EmitTurnCompleted(first.ThreadID, first.TurnID, "completed", "need input")
	waitUntil(t, 3*time.Second, "waiting input projection", func() bool {
		b, e := sqlite.GetBot(ctx, h.app.DB, owner.ID)
		return e == nil && b.Status == domain.BotStatusWaitingInput
	})
	if h.deliveryStatusOf(t, task.ID) != domain.DeliveryQueued || len(fake.Turns()) != 1 {
		t.Fatal("open question did not park assignment")
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: owner.ID, Text: "an unrelated direct message"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 3*time.Second, "direct message bypasses parked automatic work", func() bool { return len(fake.Turns()) == 2 })
	if q, err := sqlite.GetQuestion(ctx, h.app.DB, question.ID); err != nil || q.Status != domain.QuestionOpen {
		t.Fatal("ordinary message silently answered question")
	}
	second := fake.Turns()[1]
	fake.EmitTurnCompleted(second.ThreadID, second.TurnID, "completed", "ordinary reply")
	if _, err := h.app.AnswerQuestion(ctx, h.user, question.ID, "Use Markdown", question.Revision); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 3*time.Second, "answer releases original assignment", func() bool { return len(fake.Turns()) == 3 })
	if text := fake.Turns()[2].Text; !strings.Contains(text, "你被指派了任务：blocked by question") || !strings.Contains(text, task.ID) || !endsWithString(text, "完成标准：wait for answer") {
		t.Fatal("answer failed to release original task")
	}
}

func TestQuestionCountsBlockMemberDeleteAndArchive(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	member := bots[1]
	question, err := h.app.AskUser(ctx, auth.NewPrincipalBot(member.ID, team.ID), app.AskUserInput{Question: "Still waiting"})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []func() error{
		func() error { _, err := h.app.DeleteBot(ctx, h.user, member.ID, member.Revision); return err },
		func() error { _, err := h.app.ArchiveTeam(ctx, h.user, team.ID, team.Revision); return err },
	} {
		err := action()
		var de *domain.Error
		if !errors.As(err, &de) || de.Code != domain.CodeConflict || de.Params["openQuestions"] != int64(1) {
			t.Fatalf("question not counted: %v", err)
		}
	}
	if _, err := h.app.CancelQuestion(ctx, h.user, question.ID, question.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.DeleteBot(ctx, h.user, member.ID, member.Revision); err != nil {
		t.Fatal("work-free member still blocked", err)
	}
}

func TestTerminalTaskCannotReopenOrAcceptUnknownStatus(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	task, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: bots[1].ID, Title: "cancel", AcceptanceCriteria: "no reopening"})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"arbitrary", domain.TaskDone, domain.TaskReview} {
		if _, err := h.app.UpdateTaskStatus(ctx, h.user, task.ID, status, "", 0); err == nil {
			t.Fatalf("invalid todo transition accepted: %s", status)
		}
	}
	if _, err := h.app.UpdateTaskStatus(ctx, h.user, task.ID, domain.TaskCancelled, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.UpdateTaskStatus(ctx, h.user, task.ID, domain.TaskDoing, "", 0); err == nil {
		t.Fatal("terminal task reopened")
	}
}

func TestMessageRetryCannotChangeRecipientOrSender(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[1].ID, Text: "same", RequestID: "user-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[2].ID, Text: "same", RequestID: "user-key"}); err == nil {
		t.Fatal("user retry changed recipient")
	}
	p := auth.NewPrincipalBot(bots[0].ID, team.ID)
	if _, err := h.app.SendMessageFromBot(ctx, p, bots[1].ID, "same", "", "bot-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.SendMessageFromBot(ctx, p, bots[2].ID, "same", "", "bot-key"); err == nil {
		t.Fatal("bot retry changed recipient")
	}
	if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(bots[2].ID, team.ID), bots[1].ID, "same", "", "bot-key"); err == nil {
		t.Fatal("bot retry changed sender")
	}
}
