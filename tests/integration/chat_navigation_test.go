package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

func TestChatNavigationOrdersWholeHistoryAndPreservesSections(t *testing.T) {
	h := newM2Harness(t)
	attachChats(t, h, nil)
	ctx := context.Background()
	n, err := h.app.ChatNavigation(ctx, h.user)
	if err != nil {
		t.Fatal(err)
	}
	create := app.ChatNavigationInput{RequestID: "section", Action: "create", Name: "学习", ExpectedRevision: n.Revision}
	n, err = h.app.ChangeChatNavigation(ctx, h.user, create)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := h.app.ChangeChatNavigation(ctx, h.user, create)
	if err != nil || replay.Sections[0].ID != n.Sections[0].ID {
		t.Fatal("section not idempotent", err)
	}
	section := n.Sections[0].ID
	at := time.Now().UTC()
	for i := 0; i < 65; i++ {
		c := domain.Chat{ID: fmt.Sprintf("history-%02d", i), Title: fmt.Sprintf("Chat %02d", i), SectionID: section, Revision: 1, ManualOrder: int64(i + 1), Workspace: "unchanged", CreatedAt: at, UpdatedAt: at.Add(time.Duration(i) * time.Second)}
		if err = sqlite.SaveChat(ctx, h.app.DB, c, true); err != nil {
			t.Fatal(err)
		}
	}
	filter := domain.ChatListFilter{Sort: "manual", SectionID: section}
	first, err := h.app.ListOrganizedChats(ctx, h.user, "", filter)
	if err != nil || len(first.Items) != 50 || first.NextCursor == "" {
		t.Fatal("first page", len(first.Items), err)
	}
	c, err := h.app.ChangeChat(ctx, h.user, "history-64", "reorder", app.ChatActionInput{RequestID: "reorder", ExpectedRevision: 1, BeforeID: "history-00"})
	if err != nil {
		t.Fatal(err)
	}
	first, err = h.app.ListOrganizedChats(ctx, h.user, "", filter)
	if err != nil || first.Items[0].ID != c.ID {
		t.Fatal("unloaded chat not moved globally", err)
	}
	second, err := h.app.ListOrganizedChats(ctx, h.user, first.NextCursor, filter)
	if err != nil || len(second.Items) != 15 {
		t.Fatal("second page", len(second.Items), err)
	}
	seen := map[string]bool{}
	for _, c := range append(first.Items, second.Items...) {
		if seen[c.ID] {
			t.Fatal("duplicate", c.ID)
		}
		seen[c.ID] = true
	}
	if _, err = h.app.ListOrganizedChats(ctx, h.user, first.NextCursor, domain.ChatListFilter{Sort: "recent", SectionID: section}); err == nil {
		t.Fatal("cursor accepted different sort")
	}
	c, err = h.app.ChangeChat(ctx, h.user, c.ID, "pin", app.ChatActionInput{RequestID: "pin", ExpectedRevision: c.Revision})
	if err != nil {
		t.Fatal(err)
	}
	n, err = h.app.ChangeChatNavigation(ctx, h.user, app.ChatNavigationInput{RequestID: "remove-section", Action: "delete", ID: section, ExpectedRevision: n.Revision})
	if err != nil || len(n.Sections) != 0 {
		t.Fatal(err)
	}
	saved, err := sqlite.GetChat(ctx, h.app.DB, c.ID)
	if err != nil || !saved.Pinned || saved.SectionID != "" || saved.Workspace != "unchanged" {
		t.Fatal("section deletion changed context", saved, err)
	}
	if _, err = h.app.ChangeChatNavigation(ctx, h.user, app.ChatNavigationInput{RequestID: "stale", Action: "create", Name: "stale", ExpectedRevision: n.Revision - 1}); err == nil {
		t.Fatal("stale revision accepted")
	}
}

func TestChatArchiveRestorePreservesThreadAndRejectsActiveRuns(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "context"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	run := page.Runs[0]
	if _, err = h.app.ChangeChat(ctx, h.user, first.Chat.ID, "archive", app.ChatActionInput{RequestID: "busy"}); err == nil {
		t.Fatal("archived active turn")
	}
	f.EmitTurnCompleted(run.ThreadID, run.TurnID, "completed", "reply")
	page = chatWait(t, h, first.Chat.ID, domain.RunCompleted)
	original, _ := sqlite.GetChatSession(ctx, h.app.DB, first.Chat.ID)
	in := app.ChatActionInput{RequestID: "archive", ExpectedRevision: page.Chat.Revision}
	c, err := h.app.ChangeChat(ctx, h.user, first.Chat.ID, "archive", in)
	if err != nil || c.ArchivedAt == "" || c.ArchiveOperation != "" {
		t.Fatal("archive", c, err)
	}
	if _, err = h.app.ChangeChat(ctx, h.user, c.ID, "archive", in); err != nil {
		t.Fatal("archive replay", err)
	}
	if len(f.Archived()) != 1 {
		t.Fatal("native archive repeated")
	}
	active, _ := h.app.ListChats(ctx, h.user, "")
	archived, _ := h.app.ListOrganizedChats(ctx, h.user, "", domain.ChatListFilter{Archived: true, Sort: "archived"})
	if len(active.Items) != 0 || len(archived.Items) != 1 {
		t.Fatal("archive filtering")
	}
	if _, err = h.app.SendChat(ctx, h.user, c.ID, app.ChatSendInput{RequestID: "blocked-send", Text: "must not run"}); err == nil {
		t.Fatal("archived chat ran")
	}
	c, err = h.app.ChangeChat(ctx, h.user, c.ID, "restore", app.ChatActionInput{RequestID: "restore", ExpectedRevision: c.Revision})
	if err != nil || c.ArchivedAt != "" {
		t.Fatal("restore", err)
	}
	restored, _ := sqlite.GetChatSession(ctx, h.app.DB, c.ID)
	if restored.ThreadID != original.ThreadID {
		t.Fatal("restore replaced thread")
	}
	page, err = h.app.ChatPage(ctx, h.user, c.ID, "")
	if err != nil || len(page.Messages) != 2 {
		t.Fatal("lost transcript", err)
	}
}

type archiveFacts struct {
	sync.Mutex
	archived bool
	calls    int
	fail     bool
}
type archiveFaultServer struct {
	*fakeengine.Server
	facts *archiveFacts
}

func (s *archiveFaultServer) ArchiveThread(ctx context.Context, id string) error {
	s.facts.Lock()
	defer s.facts.Unlock()
	s.facts.archived = true
	s.facts.calls++
	if s.facts.fail {
		return errors.New("lost native response")
	}
	return nil
}
func (s *archiveFaultServer) ThreadArchiveState(ctx context.Context, id string) (bool, error) {
	s.facts.Lock()
	defer s.facts.Unlock()
	return s.facts.archived, nil
}

func TestChatUncertainArchiveSurvivesRestartAndIsVerifiedWithoutReplay(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	facts := &archiveFacts{fail: true}
	created := make(chan *archiveFaultServer, 3)
	factory := func(spec app.ServerSpec) (engine.Server, error) {
		s := &archiveFaultServer{Server: fakeengine.New(spec.Callbacks, "archive-fault"), facts: facts}
		created <- s
		return s, nil
	}
	h.app.Chats = app.NewChatRunner(h.app, factory)
	if err := h.app.Chats.Start(); err != nil {
		t.Fatal(err)
	}
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "archive"})
	if err != nil {
		t.Fatal(err)
	}
	s := <-created
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	s.EmitTurnCompleted(page.Runs[0].ThreadID, page.Runs[0].TurnID, "completed", "done")
	page = chatWait(t, h, first.Chat.ID, domain.RunCompleted)
	c, err := h.app.ChangeChat(ctx, h.user, first.Chat.ID, "archive", app.ChatActionInput{RequestID: "lost", ExpectedRevision: page.Chat.Revision})
	if err != nil || c.ArchiveOperation != "archive" || c.ArchivedAt != "" || c.ArchiveError == "" {
		t.Fatal("uncertain archive hidden", c, err)
	}
	if _, err = h.app.ChangeChat(ctx, h.user, c.ID, "delete", app.ChatActionInput{RequestID: "delete-pending"}); err == nil {
		t.Fatal("deleted unverified archive")
	}
	if err = h.app.Chats.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	h.app.Chats = app.NewChatRunner(h.app, factory)
	if err = h.app.Chats.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.app.Chats.Stop(ctx)
	c, err = h.app.ChangeChat(ctx, h.user, c.ID, "archive", app.ChatActionInput{RequestID: "verify", ExpectedRevision: c.Revision})
	if err != nil || c.ArchiveOperation != "" || c.ArchivedAt == "" {
		t.Fatal("verify", c, err)
	}
	facts.Lock()
	defer facts.Unlock()
	if facts.calls != 1 {
		t.Fatal("uncertain native effect replayed", facts.calls)
	}
}
