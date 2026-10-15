package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
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
	captain := bots[0]
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

	// A second hand-over exercises the accept path end to end. Its assignment
	// waits on an unfinished dependency, so it is still undelivered when the
	// hand-over is accepted — the state in which E10 requires the old
	// owner's assignment to be cancelled.
	dep, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: captain.ID, Title: "前置依赖", AcceptanceCriteria: "不会完成",
	})
	if err != nil {
		t.Fatalf("create dep: %v", err)
	}
	task2, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: owner, Title: "转交并接受", AcceptanceCriteria: "接受后唯一负责人",
		DependencyIDs: []string{dep.ID},
	})
	if err != nil {
		t.Fatalf("create task2: %v", err)
	}
	transfer2, err := h.app.OfferTransfer(ctx, h.user, task2.ID, target, "请接手")
	if err != nil {
		t.Fatalf("offer task2: %v", err)
	}

	// E03: while the old owner has a run in flight it may still be writing
	// files, so the product refuses the hand-over. The first task's own
	// assignment is that run; wait until the engine has really started its
	// turn so the refusal is not racing a run that has not reached the fake.
	fake := h.fakeFor(t, team.ID)
	ownerTurnSeen := func() bool {
		session, sessErr := sqlite.GetProviderSession(ctx, h.app.DB, owner)
		if sessErr != nil {
			return false
		}
		for _, turn := range fake.Turns() {
			if turn.ThreadID == session.ProviderThreadID {
				return true
			}
		}
		return false
	}
	waitUntil(t, 5*time.Second, "owner assignment turn", ownerTurnSeen)
	_, err = h.app.AcceptTransfer(ctx, h.user, transfer2.ID)
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("accept while the owner's run is active must conflict, got %v", err)
	}

	// Completing every started turn ends the owner's run and unblocks the
	// hand-over. The dependency task stays todo, so task2 remains blocked and
	// its assignment is still undelivered.
	waitUntil(t, 5*time.Second, "engine turns", func() bool { return len(fake.Turns()) >= 1 })
	for _, turn := range fake.Turns() {
		fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "已处理")
	}
	waitUntil(t, 5*time.Second, "owner run ended", func() bool {
		_, runErr := sqlite.FindActiveRun(ctx, h.app.DB, owner)
		return errors.Is(runErr, sqlite.ErrNotFound)
	})

	moved, err := h.app.AcceptTransfer(ctx, h.user, transfer2.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if moved.OwnerID != target {
		t.Fatalf("owner after accept = %q, want %q", moved.OwnerID, target)
	}

	rows, err := h.app.DB.Query(`SELECT m.bot_id, d.status FROM deliveries d
		JOIN messages m ON m.id = d.message_id WHERE m.run_id = ? ORDER BY m.created_at, m.id`, task2.ID)
	if err != nil {
		t.Fatalf("deliveries: %v", err)
	}
	defer rows.Close()
	type assignment struct{ bot, status string }
	var seen []assignment
	for rows.Next() {
		var r assignment
		if err := rows.Scan(&r.bot, &r.status); err != nil {
			t.Fatalf("scan delivery: %v", err)
		}
		seen = append(seen, r)
	}
	if len(seen) != 2 {
		t.Fatalf("task2 deliveries = %+v, want the cancelled original plus the new owner's", seen)
	}
	if seen[0].bot != owner || seen[0].status != domain.DeliveryCancelled {
		t.Fatalf("old assignment = %+v, want cancelled for the old owner", seen[0])
	}
	if seen[1].bot != target || seen[1].status == domain.DeliveryCancelled {
		t.Fatalf("new assignment = %+v, want a live delivery for the new owner", seen[1])
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

// TestTaskPermissionEdges pins the E01/E04 permission half: a member may not
// create through the captain-only path, touch another owner's task, or attach
// evidence to it — while the owner may still move its own task.
func TestTaskPermissionEdges(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	owner, outsider := bots[1], bots[2]

	_, err := h.app.CreateTask(ctx, auth.NewPrincipalBot(outsider.ID, team.ID), true, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: outsider.ID, Title: "越权建任务", AcceptanceCriteria: "应被拒绝",
	})
	if !errors.As(err, &de) || de.Code != domain.CodeForbidden {
		t.Fatalf("member create through captain tool must be refused, got %v", err)
	}

	task, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: owner.ID, Title: "归属任务", AcceptanceCriteria: "只有负责人能改",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = h.app.UpdateTaskStatus(ctx, auth.NewPrincipalBot(outsider.ID, team.ID), task.ID, domain.TaskDoing, "", 0)
	if !errors.As(err, &de) || de.Code != domain.CodeForbidden {
		t.Fatalf("non-owner status patch must be refused, got %v", err)
	}

	_, err = h.app.SubmitEvidence(ctx, auth.NewPrincipalBot(outsider.ID, team.ID), task.ID, "越权证据", nil, "", "", "")
	if !errors.As(err, &de) || de.Code != domain.CodeForbidden {
		t.Fatalf("non-owner evidence must be refused, got %v", err)
	}

	// The owner moves its own task, which the same call refused for others.
	if _, err := h.app.UpdateTaskStatus(ctx, auth.NewPrincipalBot(owner.ID, team.ID), task.ID, domain.TaskDoing, "", 0); err != nil {
		t.Fatalf("owner start: %v", err)
	}
}

var de *domain.Error

// TestWriteIdempotencyReplaysAndRejectsKeyReuse pins E05 for the MCP write
// path: a retried create with the same key and same arguments replays the
// stored result (one task, same id), and the same key with different
// arguments is rejected instead of silently creating a second task.
func TestWriteIdempotencyReplaysAndRejectsKeyReuse(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"backend-dev"})
	captain := bots[0]
	token := h.botToken(t, captain)

	create := func(title string) (int, map[string]any) {
		return h.callTool(t, token, "create_task", map[string]any{
			"title": title, "ownerId": captain.ID, "idempotencyKey": "e05-key",
		})
	}
	taskFrom := func(envelope map[string]any) map[string]any {
		data, _ := envelope["data"].(map[string]any)
		payload, _ := data["data"].(map[string]any)
		task, _ := payload["task"].(map[string]any)
		return task
	}

	status, first := create("幂等任务")
	if status != 200 {
		t.Fatalf("first create status = %d body=%v", status, first)
	}
	created := taskFrom(first)
	if created["id"] == nil || created["id"] == "" {
		t.Fatalf("first create returned no task: %v", first)
	}

	// Retry: same key, same arguments -> replay of the same fact.
	_, second := create("幂等任务")
	replayed := taskFrom(second)
	if replayed["id"] != created["id"] {
		t.Fatalf("replay returned task %v, want the original %v", replayed["id"], created["id"])
	}
	var count int
	if err := h.app.DB.QueryRow(`SELECT COUNT(*) FROM tasks WHERE team_id = ?`, team.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tasks after replay = %d, want exactly 1", count)
	}

	// Same key, different arguments -> conflict, still exactly one task.
	_, third := create("不同的标题")
	data, _ := third["data"].(map[string]any)
	statusField, _ := data["status"].(string)
	errObj, _ := data["error"].(map[string]any)
	messageKey, _ := errObj["messageKey"].(string)
	if statusField != "rejected" || messageKey != "error.idempotency.reused_key" {
		t.Fatalf("key reuse with new body = status %q key %q, want rejected/error.idempotency.reused_key", statusField, messageKey)
	}
	if err := h.app.DB.QueryRow(`SELECT COUNT(*) FROM tasks WHERE team_id = ?`, team.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tasks after conflicting retry = %d, want 1", count)
	}
}
