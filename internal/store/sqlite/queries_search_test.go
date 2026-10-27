package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

func TestSearchChats(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

	// Title-only hit, older activity.
	old := domain.Chat{ID: "chat-old", Title: "部署方案", UpdatedAt: base}
	if err := SaveChat(ctx, db, old, true); err != nil {
		t.Fatal(err)
	}
	// Message hit, newer activity; title does not match.
	recent := domain.Chat{ID: "chat-recent", Title: "随手记", UpdatedAt: base.Add(2 * time.Hour)}
	if err := SaveChat(ctx, db, recent, true); err != nil {
		t.Fatal(err)
	}
	run := domain.ChatRun{ID: "run-1", ChatID: recent.ID, Status: "completed"}
	if err := SaveChatRun(ctx, db, run); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("铺垫背景。", 20) + "这里的部署结论出现了。"
	if err := SaveChatMessage(ctx, db, domain.ChatMessage{ID: "msg-1", ChatID: recent.ID, RunID: run.ID, Kind: domain.MessageKindUser, Text: long, CreatedAt: base.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// Archived chat with a matching message: never a hit.
	archived := domain.Chat{ID: "chat-archived", Title: "部署归档", ArchivedAt: "2026-10-02T00:00:00Z", UpdatedAt: base.Add(3 * time.Hour)}
	if err := SaveChat(ctx, db, archived, true); err != nil {
		t.Fatal(err)
	}
	if err := SaveChatRun(ctx, db, domain.ChatRun{ID: "run-2", ChatID: archived.ID, Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveChatMessage(ctx, db, domain.ChatMessage{ID: "msg-2", ChatID: archived.ID, RunID: "run-2", Kind: domain.MessageKindUser, Text: "部署内容", CreatedAt: base.Add(3 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// No match at all.
	if err := SaveChat(ctx, db, domain.Chat{ID: "chat-other", Title: "别的", UpdatedAt: base.Add(4 * time.Hour)}, true); err != nil {
		t.Fatal(err)
	}

	got, err := SearchChats(ctx, db, "部署", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ChatID != "chat-recent" || got[1].ChatID != "chat-old" {
		t.Fatalf("hits = %+v, want recent message hit then older title hit", got)
	}
	if got[0].Snippet == "" || !strings.Contains(got[0].Snippet, "部署结论") || !strings.HasPrefix(got[0].Snippet, "…") {
		t.Fatalf("snippet = %q, want a leading-ellipsis window around the match", got[0].Snippet)
	}
	if got[1].Snippet != "" {
		t.Fatalf("title-only hit should have no snippet, got %q", got[1].Snippet)
	}
	if !got[0].ActivityAt.After(got[1].ActivityAt) {
		t.Fatalf("hits not ordered by activity: %+v", got)
	}

	if limited, _ := SearchChats(ctx, db, "部署", 1); len(limited) != 1 {
		t.Fatalf("limit not applied: %+v", limited)
	}

	// A percent sign in the query stays literal instead of acting as a wildcard.
	pct := domain.Chat{ID: "chat-pct", Title: "进度", UpdatedAt: base.Add(5 * time.Hour)}
	if err := SaveChat(ctx, db, pct, true); err != nil {
		t.Fatal(err)
	}
	if err := SaveChatRun(ctx, db, domain.ChatRun{ID: "run-3", ChatID: pct.ID, Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveChatMessage(ctx, db, domain.ChatMessage{ID: "msg-3", ChatID: pct.ID, RunID: "run-3", Kind: domain.MessageKindUser, Text: "100%完成", CreatedAt: base.Add(5 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := SaveChat(ctx, db, domain.Chat{ID: "chat-wild", Title: "进度2", UpdatedAt: base.Add(6 * time.Hour)}, true); err != nil {
		t.Fatal(err)
	}
	if err := SaveChatRun(ctx, db, domain.ChatRun{ID: "run-4", ChatID: "chat-wild", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveChatMessage(ctx, db, domain.ChatMessage{ID: "msg-4", ChatID: "chat-wild", RunID: "run-4", Kind: domain.MessageKindUser, Text: "10X完成", CreatedAt: base.Add(6 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	hits, _ := SearchChats(ctx, db, "0%完", 5)
	if len(hits) != 1 || hits[0].ChatID != "chat-pct" {
		t.Fatalf("percent query hits = %+v, want only the literal percent chat", hits)
	}
}

func TestSearchMembers(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	teamID, liveBot := seedTeam(t, db)
	otherBot := domain.NewID()
	// A second member in the same team, deleted since.
	if err := InsertBot(ctx, db, domain.Bot{ID: otherBot, TeamID: teamID, Name: "gone", RoleID: "tester"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE bots SET deleted_at=? WHERE id=?`, base.Format(time.RFC3339Nano), otherBot); err != nil {
		t.Fatal(err)
	}

	seed := func(t *testing.T, botID, kind, text string, at time.Time) {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		m := domain.Message{ID: domain.NewID(), TeamID: teamID, BotID: botID, Kind: kind, Blocks: []byte(`[{"type":"text","text":"` + text + `"}]`), CreatedAt: at}
		if err := InsertMessage(ctx, tx, m); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	seed(t, liveBot, domain.MessageKindAssistant, "预算核对完成，见附录。", base)
	seed(t, liveBot, domain.MessageKindUser, "预算又要超了", base.Add(time.Minute))
	seed(t, otherBot, domain.MessageKindAssistant, "预算旧回复", base.Add(2*time.Minute))
	// Collaboration and system text is record-keeping, not conversation content.
	seed(t, liveBot, domain.MessageKindCollaboration, "预算协作记录", base.Add(3*time.Minute))
	seed(t, liveBot, domain.MessageKindSystem, "预算系统记录", base.Add(4*time.Minute))

	got, err := SearchMembers(ctx, db, "预算", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("hits = %+v, want only the live member", got)
	}
	hit := got[0]
	if hit.BotID != liveBot || hit.BotName != "owner" || hit.TeamID != teamID || hit.TeamName != "store-test" {
		t.Fatalf("hit names wrong: %+v", hit)
	}
	if !strings.Contains(hit.Snippet, "预算又要超了") {
		t.Fatalf("snippet = %q, want the latest matching user message", hit.Snippet)
	}

	// An archived team hides its members even when messages match; so does a
	// deleted one.
	for _, status := range []string{"archived", "deleted"} {
		if _, err := db.ExecContext(ctx, `UPDATE teams SET status=? WHERE id=?`, status, teamID); err != nil {
			t.Fatal(err)
		}
		if got, _ = SearchMembers(ctx, db, "预算", 5); len(got) != 0 {
			t.Fatalf("%s team still searchable: %+v", status, got)
		}
	}
}

func TestExcerptKeepsShortTextWhole(t *testing.T) {
	if got := excerpt("短消息 部署", "部署"); got != "短消息 部署" {
		t.Fatalf("short text mangled: %q", got)
	}
	long := strings.Repeat("长", 60)
	if got := excerpt(long+"结尾", "结尾"); !strings.HasPrefix(got, "…") || !strings.Contains(got, "结尾") {
		t.Fatalf("matched tail should stay visible behind a leading ellipsis: %q", got)
	}
	if got := excerpt(long+"结尾", "不存在"); strings.Contains(got, "结尾") {
		t.Fatalf("unmatched long text should clip from the start: %q", got)
	}
}
