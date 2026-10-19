package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
)

// Member-to-member messages appear in both conversations, a card
// carries a card block the engine never reads, and the activity page keeps
// only what the user cares about, with the facts to say it plainly.
func TestConversationsShowCollaborationAndActivityIsReadable(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	captain, member := bots[0], bots[1]
	token := h.botToken(t, captain)

	if status, body := h.callTool(t, token, "send_message", map[string]any{"targetBotId": member.ID, "text": "接口定好了吗？", "idempotencyKey": "s2-msg"}); status != 200 {
		t.Fatalf("send_message = %d %v", status, body)
	}
	h.handCard(t, team, captain, member, "实现登录接口", "返回 200")

	page := func(botID string) []domain.Message {
		result, err := h.app.ListPage(ctx, h.user, app.PageRequest{Kind: "messages", BotID: botID})
		if err != nil {
			t.Fatal(err)
		}
		var out []domain.Message
		for _, item := range result["messages"].([]any) {
			out = append(out, item.(domain.Message))
		}
		return out
	}
	var incoming, card bool
	for _, m := range page(member.ID) {
		if m.Kind == domain.MessageKindCollaboration && m.SourceBotID == captain.ID && strings.Contains(m.RenderText(), "接口定好了吗") {
			incoming = true
		}
		if m.Kind == domain.MessageKindSystem && strings.Contains(string(m.Blocks), `"type":"task"`) {
			card = true
			if strings.Contains(m.RenderText(), `"taskId"`) {
				t.Fatal("the task card block leaked into the engine text")
			}
		}
	}
	if !incoming || !card {
		t.Fatalf("member conversation: incoming=%v card=%v", incoming, card)
	}
	outgoing := false
	for _, m := range page(captain.ID) {
		if m.Kind == domain.MessageKindCollaboration && m.BotID == member.ID {
			outgoing = true
		}
	}
	if !outgoing {
		t.Fatal("the sender does not see what it sent")
	}

	// The engine reads the assignment as text only.
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "member woken", func() bool { return len(fake.Turns()) > 0 })
	for _, turn := range fake.Turns() {
		if strings.Contains(turn.Text, `"acceptanceCriteria"`) {
			t.Fatalf("engine input carries the card JSON: %q", turn.Text)
		}
	}

	activity, err := h.app.ListPage(ctx, h.user, app.PageRequest{Kind: "activity", TeamID: team.ID, ViewBotID: captain.ID})
	if err != nil {
		t.Fatal(err)
	}
	var sawMessage, sawTask bool
	for _, item := range activity["items"].([]any) {
		entry := item.(app.ActivityItem)
		switch entry.Type {
		case domain.EventMessageCreated:
			sawMessage = entry.Detail["fromBotId"] == captain.ID && entry.Detail["toBotId"] == member.ID && entry.Detail["text"] == "接口定好了吗？"
		case domain.EventTaskCreated:
			sawTask = entry.Detail["title"] == "实现登录接口" && entry.Detail["ownerId"] == member.ID
		case domain.EventRunUpdated, domain.EventUsageUpdated, domain.EventMessageUpdated:
			t.Fatalf("internal event on the activity page: %s", entry.Type)
		}
	}
	if !sawMessage || !sawTask {
		t.Fatalf("activity: message=%v task=%v", sawMessage, sawTask)
	}
	// A member sees the messages it sent in its own activity view.
	memberView, err := h.app.ListPage(ctx, h.user, app.PageRequest{Kind: "activity", TeamID: team.ID, ViewBotID: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(memberView["items"].([]any)) == 0 {
		t.Fatal("member activity is empty")
	}
}
