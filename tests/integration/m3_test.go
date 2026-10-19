package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

func TestAskUserDeliversAnswerToAsker(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
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
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
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

// TestCardPermissionEdges pins the permission half of the relay: only the
// card's holder may hand it off, and only the project manager may start a
// requirement.
func TestCardPermissionEdges(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	pm, owner, outsider := bots[0], bots[1], bots[2]

	_, err := h.app.StartRequirement(ctx, auth.NewPrincipalBot(outsider.ID, team.ID), app.StartRequirementInput{
		Title: "越权提需求", Document: "应被拒绝",
		Stages: []domain.Stage{{Kind: domain.StageConfirm}, {Kind: domain.StageAccept}},
	})
	if !errors.As(err, &de) || de.Code != domain.CodeForbidden {
		t.Fatalf("a member started a requirement, got %v", err)
	}

	card, _ := h.handCard(t, team, pm, owner, "归属卡片", "只有持卡人能交接")
	_, err = h.app.Handoff(ctx, auth.NewPrincipalBot(outsider.ID, team.ID), app.HandoffInput{CardID: card.ID, Outcome: domain.HandoffDone, Summary: "越权交接"})
	if !errors.As(err, &de) || de.Code != domain.CodeForbidden {
		t.Fatalf("a non-holder handed the card off, got %v", err)
	}
	if _, err := h.app.Handoff(ctx, auth.NewPrincipalBot(owner.ID, team.ID), app.HandoffInput{CardID: card.ID, Outcome: domain.HandoffDone, Summary: "做完了"}); err != nil {
		t.Fatalf("holder handoff: %v", err)
	}
}

var de *domain.Error

// TestWriteIdempotencyReplaysAndRejectsKeyReuse pins E05 for the MCP write
// path: a retried start_requirement with the same key and same arguments
// replays the stored result (one requirement, same id), and the same key
// with different arguments is rejected instead of creating a second one.
func TestWriteIdempotencyReplaysAndRejectsKeyReuse(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]
	token := h.botToken(t, pm)
	// All retries belong to one model turn.
	binding, err := h.tokens.Resolve(token)
	if err != nil {
		t.Fatal(err)
	}
	token, cleanup := h.scriptExecution(t, binding)
	defer cleanup()

	start := func(title string) (int, map[string]any) {
		return h.callToolRaw(t, token, "start_requirement", map[string]any{
			"title": title, "document": "需求文档",
			"stages":         []map[string]any{{"kind": "confirm"}, {"kind": "implement", "ownerId": dev.ID}, {"kind": "accept"}},
			"idempotencyKey": "e05-key",
		})
	}
	requirementFrom := func(envelope map[string]any) any {
		data, _ := envelope["data"].(map[string]any)
		payload, _ := data["data"].(map[string]any)
		return payload["requirementId"]
	}
	count := func() int {
		var n int
		if err := h.app.DB.QueryRow(`SELECT COUNT(*) FROM requirements WHERE team_id = ?`, team.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	status, first := start("幂等需求")
	created := requirementFrom(first)
	if status != 200 || created == nil || created == "" {
		t.Fatalf("first start = %d %v", status, first)
	}
	// Retry: same key, same arguments -> replay of the same fact.
	if _, second := start("幂等需求"); requirementFrom(second) != created {
		t.Fatalf("replay returned %v, want the original %v", requirementFrom(second), created)
	}
	if n := count(); n != 1 {
		t.Fatalf("requirements after replay = %d, want exactly 1", n)
	}
	// Same key, different arguments -> conflict, still exactly one.
	_, third := start("不同的标题")
	data, _ := third["data"].(map[string]any)
	errObj, _ := data["error"].(map[string]any)
	if data["status"] != "rejected" || errObj["messageKey"] != "error.idempotency.reused_key" {
		t.Fatalf("key reuse with new body = %v, want rejected/error.idempotency.reused_key", data)
	}
	if n := count(); n != 1 {
		t.Fatalf("requirements after conflicting retry = %d, want 1", n)
	}
}
