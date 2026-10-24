package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

type chatFake struct {
	*fakeengine.Server
	callbacks engine.Callbacks
}

func attachChats(t *testing.T, h *m2Harness, before func(*chatFake)) <-chan *chatFake {
	t.Helper()
	made := make(chan *chatFake, 20)
	h.app.Chats = app.NewChatRunner(h.app, func(spec app.ServerSpec) (engine.Server, error) {
		f := &chatFake{Server: fakeengine.New(spec.Callbacks, "chat-"+filepath.Base(spec.Cwd)), callbacks: spec.Callbacks}
		if before != nil {
			before(f)
		}
		made <- f
		return f, nil
	})
	if err := h.app.Chats.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.app.Chats.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return made
}
func chatWait(t *testing.T, h *m2Harness, id, status string) domain.ChatPage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, err := h.app.ChatPage(context.Background(), h.user, id, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range page.Runs {
			if run.Status == status {
				return page
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("chat %s did not reach %s", id, status)
	return domain.ChatPage{}
}
func chatCreated(t *testing.T, made <-chan *chatFake) *chatFake {
	t.Helper()
	select {
	case f := <-made:
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("chat engine did not start")
		return nil
	}
}

func TestChatIdempotencyContextAndNativeRequests(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	in := app.ChatSendInput{RequestID: "first-chat", Text: "你好，讨论一下"}
	first, err := h.app.SendChat(ctx, h.user, "", in)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := h.app.SendChat(ctx, h.user, "", in)
	if err != nil || replay.Chat.ID != first.Chat.ID {
		t.Fatalf("duplicate creation: %+v %v", replay, err)
	}
	in.Text = "different"
	if _, err = h.app.SendChat(ctx, h.user, "", in); err == nil {
		t.Fatal("changed idempotent input was accepted")
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	run := page.Runs[0]
	if f.Instructions()[0] != "" {
		t.Fatal("chat received team or chat-only instructions")
	}
	if page.Chat.Workspace != filepath.Join(h.app.Paths.Root, "chat-workspaces", first.Chat.ID) {
		t.Fatal("wrong workspace", page.Chat.Workspace)
	}
	var count int
	_ = h.app.DB.QueryRowContext(ctx, "SELECT count(*) FROM teams").Scan(&count)
	if count != 0 {
		t.Fatal("chat created a hidden team")
	}
	if _, err = h.app.SendChat(ctx, h.user, first.Chat.ID, app.ChatSendInput{RequestID: "busy", Text: "next"}); err == nil {
		t.Fatal("busy chat accepted a second turn")
	}
	f.EmitApproval("native-1", run.ThreadID, run.TurnID, "echo hello")
	page = chatWait(t, h, first.Chat.ID, domain.RunWaitingApproval)
	if len(page.Requests) != 1 {
		t.Fatal(page.Requests)
	}
	req := page.Requests[0]
	_, err = h.app.DecideChatRequest(ctx, h.user, first.Chat.ID, req.ID, app.ChatActionInput{RequestID: "allow", OptionID: "accept", ExpectedRevision: req.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Replies()) != 1 {
		t.Fatal("native request not answered")
	}
	f.Emit(engine.Notification{Method: "error", ThreadID: run.ThreadID, TurnID: run.TurnID, Problem: &engine.Problem{Level: "error", Code: "httpConnectionFailed", FailureCode: "quota", Message: "vendor echoed private-key-should-not-persist"}})
	f.Emit(engine.Notification{Method: "item/started", ThreadID: run.ThreadID, TurnID: run.TurnID, Step: &engine.Step{ID: "unfinished", Kind: engine.StepCommand, Status: engine.StepInProgress, Detail: json.RawMessage(`{"command":"echo pending"}`)}})
	f.EmitTurnCompleted(run.ThreadID, run.TurnID, "completed", "你好！")
	page = chatWait(t, h, first.Chat.ID, domain.RunCompleted)
	if len(page.Messages) != 2 || page.Messages[1].Text != "你好！" {
		t.Fatal(page.Messages)
	}
	for _, step := range page.Runs[0].Steps {
		if strings.Contains(string(step.Detail), "private-key-should-not-persist") {
			t.Fatal("vendor error body was persisted")
		}
		if step.ID == "unfinished" && step.Status == "in_progress" {
			t.Fatal("terminal run retained a running step")
		}
	}
	_, err = h.app.SendChat(ctx, h.user, first.Chat.ID, app.ChatSendInput{RequestID: "second", Text: "继续"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(f.Turns()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(f.Turns()) != 2 || f.Turns()[1].ThreadID != run.ThreadID {
		t.Fatal("context was not resumed", f.Turns())
	}
}

func TestChatUnknownIsNotReplayedAndStaleApprovalsExpire(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "do something"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	run := page.Runs[0]
	f.EmitApproval("pending", run.ThreadID, run.TurnID, "echo something")
	page = chatWait(t, h, first.Chat.ID, domain.RunWaitingApproval)
	req := page.Requests[0]
	f.EmitExit(errors.New("connection lost"))
	page = chatWait(t, h, first.Chat.ID, domain.RunUnknown)
	if page.Requests[0].State != "invalidated" {
		t.Fatal("dead connection request stayed actionable")
	}
	if _, err = h.app.DecideChatRequest(ctx, h.user, first.Chat.ID, req.ID, app.ChatActionInput{RequestID: "late", OptionID: "accept", ExpectedRevision: req.Revision}); err == nil {
		t.Fatal("stale approval was accepted")
	}
	if _, err = h.app.SendChat(ctx, h.user, first.Chat.ID, app.ChatSendInput{RequestID: "again", Text: "again"}); err == nil {
		t.Fatal("unknown run was silently bypassed")
	}
	if len(f.Turns()) != 1 {
		t.Fatal("unknown turn replayed")
	}
	if _, err = h.app.ChangeChat(ctx, h.user, first.Chat.ID, "delete", app.ChatActionInput{RequestID: "delete"}); err == nil {
		t.Fatal("unresolved execution was deleted")
	}
}

func TestChatStopPersistsIntentAndDoesNotPretendTheTurnEnded(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "long response"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	run := page.Runs[0]
	stopped, err := h.app.ChatRunAction(ctx, h.user, first.Chat.ID, run.ID, "interrupt", app.ChatActionInput{RequestID: "stop"})
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.StopRequested || stopped.Status == domain.RunInterrupted {
		t.Fatal("claimed stopped before native confirmation", stopped)
	}
	f.EmitTurnCompleted(run.ThreadID, run.TurnID, "interrupted", "partial answer")
	page = chatWait(t, h, first.Chat.ID, domain.RunInterrupted)
	if page.Messages[len(page.Messages)-1].Text != "partial answer" {
		t.Fatal("partial response lost")
	}
	if _, err = h.app.ChatRunAction(ctx, h.user, first.Chat.ID, run.ID, "interrupt", app.ChatActionInput{RequestID: "stop"}); err != nil {
		t.Fatal("stop retry not idempotent", err)
	}
	if _, reason, _ := platform.ReadChatLease(h.app.Paths.Root, first.Chat.ID, h.app.InstanceID, time.Now()); reason != platform.LeaseReasonMissing {
		t.Fatal("finished chat kept a tool lease", reason)
	}
}

func TestChatInputsAndForeignRequests(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "ask a question"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	run := page.Runs[0]
	f.callbacks.OnInput(engine.ApprovalRequest{Method: "item/tool/requestUserInput", RequestID: json.RawMessage(`"input-1"`), ThreadID: run.ThreadID, TurnID: run.TurnID, RawParams: json.RawMessage(`{"questions":[{"id":"color","header":"Color","question":"Which color?"}]}`)})
	page = chatWait(t, h, first.Chat.ID, domain.RunWaitingInput)
	req := page.Requests[0]
	if _, err = h.app.DecideChatRequest(ctx, h.user, "other-chat", req.ID, app.ChatActionInput{RequestID: "foreign", ExpectedRevision: req.Revision, Answers: map[string][]string{"color": {"blue"}}}); err == nil {
		t.Fatal("foreign chat request accepted")
	}
	if _, err = h.app.DecideChatRequest(ctx, h.user, first.Chat.ID, req.ID, app.ChatActionInput{RequestID: "answer", ExpectedRevision: req.Revision, Answers: map[string][]string{"color": {"blue"}}}); err != nil {
		t.Fatal(err)
	}
	if len(f.Replies()) != 1 {
		t.Fatal("input not answered")
	}
	if _, err := sqlite.GetChatRun(ctx, h.app.DB, run.ID); err != nil {
		t.Fatal(err)
	}
}

func TestChatRestartDoesNotReplayAnAcceptedTurn(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "keep running"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	chatWait(t, h, first.Chat.ID, domain.RunRunning)
	if err = h.app.Chats.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := app.NewChatRunner(h.app, func(app.ServerSpec) (engine.Server, error) {
		t.Error("restart launched an engine for an unverified turn")
		return nil, errors.New("unexpected engine start")
	})
	h.app.Chats = restarted
	if err = restarted.Start(); err != nil {
		t.Fatal(err)
	}
	defer restarted.Stop(ctx)
	page := chatWait(t, h, first.Chat.ID, domain.RunUnknown)
	if page.Runs[0].ErrorKey != "error.chat.restarted" || len(f.Turns()) != 1 {
		t.Fatal("restart replayed or misclassified the turn")
	}
}

func TestChatHistoryPagingAndDeletingOnlyConversationContent(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "history"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	run := page.Runs[0]
	f.EmitTurnCompleted(run.ThreadID, run.TurnID, "completed", "done")
	chatWait(t, h, first.Chat.ID, domain.RunCompleted)
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		at := time.Now().UTC().Add(time.Duration(i) * time.Second)
		m := domain.ChatMessage{ID: domain.NewID(), ChatID: first.Chat.ID, RunID: run.ID, Kind: "assistant", Text: "history", CreatedAt: at}
		if err = sqlite.SaveChatMessage(ctx, tx, m); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	page, err = h.app.ChatPage(ctx, h.user, first.Chat.ID, "")
	if err != nil || len(page.Messages) != 50 || page.NextCursor == "" {
		t.Fatalf("bad first page: %d %v", len(page.Messages), err)
	}
	older, err := h.app.ChatPage(ctx, h.user, first.Chat.ID, page.NextCursor)
	if err != nil || len(older.Messages) != 12 || older.NextCursor != "" {
		t.Fatalf("bad next page: %d %v", len(older.Messages), err)
	}
	seen := map[string]bool{}
	for _, m := range page.Messages {
		seen[m.ID] = true
	}
	for _, m := range older.Messages {
		if seen[m.ID] {
			t.Fatal("overlapping cursor pages")
		}
	}
	file := filepath.Join(first.Chat.Workspace, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = h.app.ChangeChat(ctx, h.user, first.Chat.ID, "delete", app.ChatActionInput{RequestID: "delete"}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(file); err != nil {
		t.Fatal("deleting conversation removed workspace output")
	}
	if _, err = h.app.ChatPage(ctx, h.user, first.Chat.ID, ""); err == nil {
		t.Fatal("deleted chat remained readable")
	}
	f.EmitTurnCompleted(run.ThreadID, run.TurnID, "completed", "late reply")
	if _, err = h.app.ChatPage(ctx, h.user, first.Chat.ID, ""); err == nil {
		t.Fatal("late callback resurrected deleted chat")
	}
}

// What is chosen with a message (model, thinking level, permission) holds from
// that turn on, and goes to the engine with the turn.
func TestAChatKeepsWhatIsChosenWithEachMessage(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "choose-1", Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	run := page.Runs[0]
	f.EmitTurnCompleted(run.ThreadID, run.TurnID, "completed", "好")
	chatWait(t, h, first.Chat.ID, domain.RunCompleted)

	if _, err := h.app.SendChat(ctx, h.user, first.Chat.ID, app.ChatSendInput{RequestID: "choose-bad", Text: "x", PermissionMode: "root"}); err == nil {
		t.Fatal("a permission mode that does not exist was accepted")
	}
	second, err := h.app.SendChat(ctx, h.user, first.Chat.ID, app.ChatSendInput{RequestID: "choose-2", Text: "继续", ProviderID: first.Chat.ProviderID, Model: "gpt-test", ReasoningEffort: "high", PermissionMode: domain.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if c := second.Chat; c.Model != "gpt-test" || c.ReasoningEffort != "high" || c.PermissionMode != domain.PermissionReadOnly {
		t.Fatalf("chat = %+v", c)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(f.Turns()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	turns := f.Turns()
	if len(turns) != 2 || turns[1].ThreadID != run.ThreadID || turns[1].Model != "gpt-test" || turns[1].Permission != domain.PermissionReadOnly || turns[1].Effort != "high" {
		t.Fatalf("turns = %+v", turns)
	}
	// A message that says nothing of them leaves them as they are.
	chatWait(t, h, first.Chat.ID, domain.RunRunning)
}
