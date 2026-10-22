package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// The team list previews each member's latest user or assistant message:
// collaboration and system messages and text-less messages never show.
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
		{Kind: domain.MessageKindCollaboration, Blocks: []byte(`[{"type":"text","text":"from a peer"}]`)},
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
	if got.Kind != domain.MessageKindAssistant || got.Text != "two lines more" {
		t.Fatalf("preview = %+v, want the assistant's text on one line", got)
	}

	if text := previewText(`[{"type":"text","text":"` + long + `"}]`); len([]rune(text)) != previewRunes+1 || !strings.HasSuffix(text, "…") {
		t.Fatalf("long preview not cut: %d runes", len([]rune(text)))
	}
	if text := previewText(`[{"type":"task","task":{}}]`); text != "" {
		t.Fatalf("a task block previewed as %q", text)
	}
}
