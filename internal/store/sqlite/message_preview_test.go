package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// The team list previews each member's latest message: its exchange with the
// user, a card or note another member sent it, or a card the user
// dispatched. A plain system notice — no sender, no card — never shows.
func TestLatestMessagePreviews(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	teamID, botID := seedTeam(t, db)
	start := time.Now().UTC()
	long := strings.Repeat("长", previewRunes+10)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range []domain.Message{
		{Kind: domain.MessageKindUser, Blocks: []byte(`[{"type":"text","text":"first"}]`)},
		{Kind: domain.MessageKindAssistant, Blocks: []byte(`[{"type":"text","text":"  two\n\nlines  "},{"type":"text","text":"more"}]`)},
		{Kind: domain.MessageKindSystem, Blocks: []byte(`[{"type":"text","text":"system note"}]`)},
		{Kind: domain.MessageKindCollaboration, Blocks: []byte(`[{"type":"text","text":"from a peer"}]`), SourceBotID: "peer"},
	} {
		m.ID, m.TeamID, m.BotID = domain.NewID(), teamID, botID
		m.CreatedAt = start.Add(time.Duration(i) * time.Second)
		if err := InsertMessage(ctx, tx, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	previews, err := LatestMessagePreviews(ctx, db, teamID)
	if err != nil {
		t.Fatal(err)
	}
	got := previews[botID]
	// The latest message wins: the peer's note, with its sender named.
	if got.Kind != domain.MessageKindCollaboration || got.SenderBotID != "peer" || got.Text != "from a peer" {
		t.Fatalf("preview = %+v, want the peer's note with its sender", got)
	}

	if text := previewText(`[{"type":"text","text":"` + long + `"}]`); len([]rune(text)) != previewRunes+1 || !strings.HasSuffix(text, "…") {
		t.Fatalf("long preview not cut: %d runes", len([]rune(text)))
	}
	if text := previewText(`[{"type":"task","task":{}}]`); text != "" {
		t.Fatalf("a task block previewed as %q", text)
	}
}

// A card message previews as the card's title, and a plain system notice
// without a sender or a card is not a preview at all.
func TestLatestMessagePreviewsCardsAndNotes(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	teamID, botID := seedTeam(t, db)
	start := time.Now().UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(i int, m domain.Message) {
		t.Helper()
		m.ID, m.TeamID, m.BotID = domain.NewID(), teamID, botID
		m.CreatedAt = start.Add(time.Duration(i) * time.Second)
		if err := InsertMessage(ctx, tx, m); err != nil {
			t.Fatal(err)
		}
	}
	card := `[{"type":"text","text":"【接力卡片】实现登录接口"},{"type":"task","text":"{\"taskId\":\"c1\",\"title\":\"实现登录接口\"}"}]`
	// The plain notice is the newest: it must not become the preview, so the
	// card before it is what shows. A card the user dispatched has no
	// sender and still previews, by its title.
	insert(0, domain.Message{Kind: domain.MessageKindSystem, Blocks: []byte(card)})
	insert(1, domain.Message{Kind: domain.MessageKindSystem, Blocks: []byte(`[{"type":"text","text":"你取消了需求"}]`)})
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	previews, err := LatestMessagePreviews(ctx, db, teamID)
	if err != nil {
		t.Fatal(err)
	}
	got := previews[botID]
	if got.Kind != domain.MessageKindSystem || got.SenderBotID != "" || got.Text != "实现登录接口" {
		t.Fatalf("preview = %+v, want the dispatched card's title without a sender", got)
	}
}
