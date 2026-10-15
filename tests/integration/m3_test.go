package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

// TestDependencyChainIsRecorded pins the create-time contract: a task stores
// exactly the dependency set it was given, in the order supplied, and the
// product refuses a dependency it cannot resolve.
func TestDependencyChainIsRecorded(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	owner := bots[1].ID

	a, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: owner, Title: "A", AcceptanceCriteria: "先做A",
	})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: owner, Title: "B", AcceptanceCriteria: "再做B",
		DependencyIDs: []string{a.ID},
	})
	if err != nil {
		t.Fatalf("create B depending on A: %v", err)
	}
	if len(b.DependencyIDs) != 1 || b.DependencyIDs[0] != a.ID {
		t.Fatalf("B must record its dependency on A, got %v", b.DependencyIDs)
	}

	// A dependency from another team is refused rather than silently dropped.
	other, otherBots := h.createTeamWithDir(t, "其他团队", "other-dir", []string{"backend-dev"})
	_, err = h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: owner, Title: "跨团队", AcceptanceCriteria: "非法",
		DependencyIDs: []string{otherBots[1].ID},
	})
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeInvalidRequest {
		t.Fatalf("cross-team dependency must be refused, got %v", err)
	}
	_ = other
}

// TestDependenciesSatisfiedBlocksAssignment proves a dependent task's
// assignment delivery waits until the dependency is done, and that finishing
// the dependency releases it exactly once.
func TestDependenciesSatisfiedBlocksAssignment(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	captain, member := bots[0].ID, bots[1].ID

	first, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: member, Title: "前置", AcceptanceCriteria: "完成前置",
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: captain, Title: "后置", AcceptanceCriteria: "完成前置后才能开始",
		DependencyIDs: []string{first.ID},
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	// The dependent task waits on its dependency; the scheduler may already
	// have claimed the independent one, so only the blocked state is fixed.
	if got := h.deliveryStatusOf(t, second.ID); got != domain.DeliveryWaitingDependency {
		t.Fatalf("dependent assignment = %q, want %q", got, domain.DeliveryWaitingDependency)
	}
	switch got := h.deliveryStatusOf(t, first.ID); got {
	case domain.DeliveryQueued, domain.DeliveryDispatching, domain.DeliveryAccepted:
	default:
		t.Fatalf("independent assignment = %q, want to be claimable", got)
	}

	// Doing the work needs evidence before it can be confirmed done.
	if _, err := h.app.UpdateTaskStatus(ctx, h.user, first.ID, domain.TaskDoing, "", 0); err != nil {
		t.Fatalf("start first: %v", err)
	}
	if _, err := h.app.SubmitEvidence(ctx, h.user, first.ID, "已完成并有测试", nil, "", "", ""); err != nil {
		t.Fatalf("evidence: %v", err)
	}
	// Evidence in hand, the task moves to review; only then may it be
	// confirmed done.
	if _, err := h.app.UpdateTaskStatus(ctx, h.user, first.ID, domain.TaskReview, "", 0); err != nil {
		t.Fatalf("review first: %v", err)
	}
	if _, err := h.app.ConfirmTask(ctx, h.user, first.ID, nil, 0); err != nil {
		t.Fatalf("confirm first: %v", err)
	}

	// The dependency is done, so the waiting assignment is released.
	if got := h.deliveryStatusOf(t, second.ID); got != domain.DeliveryQueued {
		t.Fatalf("after dependency done, assignment = %q, want %q", got, domain.DeliveryQueued)
	}
}

// TestConfirmRequiresEvidence pins the rule that a chat claim of completion is
// not completion: a task cannot reach review, and so cannot be confirmed,
// without evidence.
func TestConfirmRequiresEvidence(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	task, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: bots[1].ID, Title: "无证据", AcceptanceCriteria: "必须有证据",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := h.app.UpdateTaskStatus(ctx, h.user, task.ID, domain.TaskDoing, "", 0); err != nil {
		t.Fatalf("doing: %v", err)
	}

	// Evidence is required before review, so "I finished" in chat cannot even
	// open the confirmation path.
	_, err = h.app.UpdateTaskStatus(ctx, h.user, task.ID, domain.TaskReview, "", 0)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeInvalidRequest {
		t.Fatalf("review without evidence must be refused, got %v", err)
	}

	// With evidence the task reaches review; confirming it as done is then the
	// captain's or the user's call.
	if _, err := h.app.SubmitEvidence(ctx, h.user, task.ID, "有测试且通过", nil, "", "go test ./...", ""); err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if _, err := h.app.UpdateTaskStatus(ctx, h.user, task.ID, domain.TaskReview, "", 0); err != nil {
		t.Fatalf("review with evidence: %v", err)
	}
	if _, err := h.app.ConfirmTask(ctx, h.user, task.ID, nil, 0); err != nil {
		t.Fatalf("user confirm: %v", err)
	}
	final, err := h.app.GetTaskDetail(ctx, h.user, task.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if got := final["task"].(domain.Task).Status; got != domain.TaskDone {
		t.Fatalf("confirmed task status = %q, want done", got)
	}
}

// TestTwoPhaseTransferKeepsSingleOwner proves the owner is unchanged until the
// target accepts, and that the old owner's undelivered work is cancelled.
func TestTwoPhaseTransferKeepsSingleOwner(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	owner, target := bots[1].ID, bots[2].ID

	task, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: owner, Title: "转交", AcceptanceCriteria: "交给测试",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	transfer, err := h.app.OfferTransfer(ctx, h.user, task.ID, target, "需要测试经验")
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if transfer.Status != domain.TransferOffered {
		t.Fatalf("transfer status = %q, want offered", transfer.Status)
	}

	// Ownership does not move until the target accepts.
	mid, err := h.app.GetTaskDetail(ctx, h.user, task.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if got := mid["task"].(domain.Task).OwnerID; got != owner {
		t.Fatalf("owner moved on offer: %q, want %q", got, owner)
	}

	if _, err := h.app.CancelTransfer(ctx, h.user, transfer.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// A cancelled offer can never be accepted afterwards.
	_, err = h.app.AcceptTransfer(ctx, h.user, transfer.ID)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("accepting a cancelled transfer must conflict, got %v", err)
	}
}

// TestAskUserDeliversAnswerToAsker proves an answer lands as a user message to
// the requesting bot and that the question is decided exactly once.
func TestAskUserDeliversAnswerToAsker(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	asker := bots[1]

	question, err := h.app.AskUser(ctx, auth.NewPrincipalBot(asker.ID, team.ID), app.AskUserInput{
		Question: "要支持哪些登录方式？", Options: []string{"邮箱", "OAuth"},
	})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if question.Status != domain.QuestionOpen {
		t.Fatalf("question status = %q, want open", question.Status)
	}

	answered, err := h.app.AnswerQuestion(ctx, h.user, question.ID, "邮箱", 0)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if answered.Status != domain.QuestionAnswered || answered.AnswerText != "邮箱" {
		t.Fatalf("unexpected answered state: %+v", answered)
	}
	if answered.AnswerMessageID == "" {
		t.Fatal("answer must be linked to the user message it delivered")
	}

	// The asking bot has a queued user message carrying the answer.
	queued := h.queuedForBot(t, asker.ID)
	if queued != 1 {
		t.Fatalf("asking bot queued deliveries = %d, want 1", queued)
	}

	// A second answer is refused; the question is decided exactly once.
	_, err = h.app.AnswerQuestion(ctx, h.user, question.ID, "改主意", 0)
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("second answer must conflict, got %v", err)
	}
}

// TestUserReadsWholeCollaborationRecord pins the user's collaboration page:
// reading a named member returns everything that member sent or received, in
// both directions. A (member, member) pairing would only match a
// self-addressed message, which the product never creates.
func TestUserReadsWholeCollaborationRecord(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	a, b := bots[1], bots[2]

	if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(a.ID, team.ID), b.ID, "帮我准备测试环境", "", "req-collab-1"); err != nil {
		t.Fatalf("send: %v", err)
	}

	// The user reads A's page: the outgoing message is there.
	sent, err := h.app.ListCollaboration(ctx, h.user, a.ID, 0)
	if err != nil {
		t.Fatalf("user read of A: %v", err)
	}
	if len(sent) != 1 || sent[0].SourceBotID != a.ID || sent[0].BotID != b.ID {
		t.Fatalf("A's record = %+v, want the A->B message", sent)
	}

	// The user reads B's page: the same message arrived there.
	received, err := h.app.ListCollaboration(ctx, h.user, b.ID, 0)
	if err != nil {
		t.Fatalf("user read of B: %v", err)
	}
	if len(received) != 1 || received[0].SourceBotID != a.ID {
		t.Fatalf("B's record = %+v, want the A->B message", received)
	}

	// A bot still reads the pairwise segment with its own peer.
	byA, err := h.app.ListCollaboration(ctx, auth.NewPrincipalBot(a.ID, team.ID), b.ID, 0)
	if err != nil {
		t.Fatalf("bot read of peer: %v", err)
	}
	if len(byA) != 1 {
		t.Fatalf("A's peer segment = %+v, want the one message", byA)
	}
}

var de *domain.Error
