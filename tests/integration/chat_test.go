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

// A chat moved to another model of the same vendor goes on in its thread; one
// moved to another vendor starts a new thread and its first turn says where the
// old record is, as for a team member.
func TestAChatMovedToAnotherVendorStartsANewThread(t *testing.T) {
	h := newM2Harness(t)
	saveVendorProvider(t, h, "alpha", "a1", "a2")
	saveVendorProvider(t, h, "beta", "b1")
	made := attachChats(t, h, nil)
	ctx := context.Background()
	active := func(id string) domain.ChatRun {
		t.Helper()
		var found domain.ChatRun
		waitUntil(t, 5*time.Second, "a running turn", func() bool {
			page, err := h.app.ChatPage(ctx, h.user, id, "")
			if err != nil {
				return false
			}
			for _, run := range page.Runs {
				if run.Status == domain.RunRunning {
					found = run
					return true
				}
			}
			return false
		})
		return found
	}
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "v-1", Text: "你好", ProviderID: "alpha", Model: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	run := active(first.Chat.ID)
	f.EmitTurnCompleted(run.ThreadID, run.TurnID, "completed", "好")
	chatWait(t, h, first.Chat.ID, domain.RunCompleted)

	// Another model of the vendor: the same thread.
	if _, err := h.app.SendChat(ctx, h.user, first.Chat.ID, app.ChatSendInput{RequestID: "v-2", Text: "继续", ProviderID: "alpha", Model: "a2"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the second turn", func() bool { return len(f.Turns()) == 2 })
	if turns := f.Turns(); turns[1].ThreadID != run.ThreadID || turns[1].Model != "a2" {
		t.Fatalf("turns = %+v", turns)
	}
	second := active(first.Chat.ID)
	f.EmitTurnCompleted(second.ThreadID, second.TurnID, "completed", "好")
	waitUntil(t, 5*time.Second, "the second turn to end", func() bool {
		page, _ := h.app.ChatPage(ctx, h.user, first.Chat.ID, "")
		done := 0
		for _, r := range page.Runs {
			if r.Status == domain.RunCompleted {
				done++
			}
		}
		return done == 2
	})

	// Another vendor: a new thread, told of the old record.
	if _, err := h.app.SendChat(ctx, h.user, first.Chat.ID, app.ChatSendInput{RequestID: "v-3", Text: "换个厂商接着聊", ProviderID: "beta", Model: "b1"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the third turn", func() bool { return len(f.Turns()) == 3 })
	third := f.Turns()[2]
	if third.ThreadID == run.ThreadID {
		t.Fatal("another vendor's model was given the old thread")
	}
	if !strings.Contains(third.Text, "接着之前的会话继续工作") || !strings.HasSuffix(third.Text, "换个厂商接着聊") {
		t.Fatalf("the first turn of the new thread = %q", third.Text)
	}
}

// A turn the engine reconnects for is not a failed one: the attempts show as one
// reconnecting step, which closes when the turn goes on, and the turn that then
// finishes is recorded as finished with its reply and no error notice.
func TestAChatTurnThatReconnectsAndFinishesIsNotShownAsFailed(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	sent, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "retry-1", Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	var run domain.ChatRun
	waitUntil(t, 5*time.Second, "a running turn", func() bool {
		page, err := h.app.ChatPage(ctx, h.user, sent.Chat.ID, "")
		if err != nil {
			return false
		}
		for _, r := range page.Runs {
			if r.Status == domain.RunRunning {
				run = r
				return true
			}
		}
		return false
	})
	problem := func(attempt string) engine.Notification {
		return engine.Notification{Method: "error", ThreadID: run.ThreadID, TurnID: run.TurnID,
			Problem: &engine.Problem{Level: "error", Message: "Reconnecting... " + attempt + "/5", Code: "responseStreamDisconnected", WillRetry: true}}
	}
	f.Emit(problem("1"))
	f.Emit(problem("2"))
	stepsOf := func() map[string]domain.ChatStep {
		page, err := h.app.ChatPage(ctx, h.user, sent.Chat.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]domain.ChatStep{}
		for _, r := range page.Runs {
			for _, s := range r.Steps {
				out[s.ID] = s
			}
		}
		return out
	}
	waitUntil(t, 5*time.Second, "the reconnecting step", func() bool {
		s, ok := stepsOf()["cobot-retry"]
		return ok && s.Kind == "retry" && s.Status == engine.StepInProgress && strings.Contains(string(s.Detail), `"attempt":2`)
	})
	f.EmitTurnCompleted(run.ThreadID, run.TurnID, "completed", "已记住")
	page := chatWait(t, h, sent.Chat.ID, domain.RunCompleted)
	for _, r := range page.Runs {
		if r.ErrorKey != "" {
			t.Fatalf("errorKey = %q", r.ErrorKey)
		}
		for _, s := range r.Steps {
			if s.Kind == "notice" {
				t.Fatalf("a reconnect was recorded as a notice: %+v", s)
			}
		}
	}
	if s := stepsOf()["cobot-retry"]; s.Status == engine.StepInProgress {
		t.Fatalf("the reconnecting step stays open after the turn ended: %+v", s)
	}
}

func TestChatEditRollsTheTurnBackAndSendsAgain(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "最初的问题"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	oldRun := page.Runs[0]
	f.EmitTurnCompleted(oldRun.ThreadID, oldRun.TurnID, "completed", "第一轮的回答")
	page = chatWait(t, h, first.Chat.ID, domain.RunCompleted)

	userMessage := ""
	for _, m := range page.Messages {
		if m.Kind == domain.MessageKindUser {
			userMessage = m.ID
		}
	}
	if userMessage == "" {
		t.Fatal("no user message on the page")
	}
	// Editing the finished turn: the thread rolls back, the old record goes,
	// and the new words start a new turn.
	edited, err := h.app.EditChatMessage(ctx, h.user, first.Chat.ID, userMessage, app.ChatEditText{RequestID: "edit-1", Text: "改过的问题"})
	if err != nil {
		t.Fatal(err)
	}
	if reverted := f.Reverted(); len(reverted) != 1 || reverted[0] != oldRun.TurnID {
		t.Fatalf("the engine was not asked to revert: %v", reverted)
	}
	if _, err := sqlite.GetChatRun(ctx, h.app.DB, oldRun.ID); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatal("the old turn survived the edit", err)
	}
	if _, err := sqlite.GetChatMessage(ctx, h.app.DB, userMessage); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatal("the old message survived the edit", err)
	}
	if len(edited.Messages) == 0 || edited.Messages[len(edited.Messages)-1].Text != "改过的问题" {
		t.Fatalf("the edited message is not on the page: %+v", edited.Messages)
	}
	newPage := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	if newPage.Runs[0].ID == oldRun.ID {
		t.Fatal("the new turn reused the old run")
	}
	f.EmitTurnCompleted(newPage.Runs[0].ThreadID, newPage.Runs[0].TurnID, "completed", "新的回答")
	chatWait(t, h, first.Chat.ID, domain.RunCompleted)
}

func TestChatEditOfARunningTurnStopsItBeforeItResends(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "最初的问题"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	oldRun := page.Runs[0]
	userMessage := ""
	for _, m := range page.Messages {
		if m.Kind == domain.MessageKindUser {
			userMessage = m.ID
		}
	}
	if _, err := h.app.EditChatMessage(ctx, h.user, first.Chat.ID, userMessage, app.ChatEditText{RequestID: "edit-live", Text: "改过的问题"}); err != nil {
		t.Fatal(err)
	}
	// The running turn is asked to stop; the edit waits for its end.
	if len(f.Interrupts()) != 1 {
		t.Fatalf("the running turn was not interrupted: %v", f.Interrupts())
	}
	f.EmitTurnCompleted(oldRun.ThreadID, oldRun.TurnID, "interrupted", "")
	newPage := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	if newPage.Runs[0].ID == oldRun.ID {
		t.Fatal("the replacement turn reused the old run")
	}
	if reverted := f.Reverted(); len(reverted) != 1 || reverted[0] != oldRun.TurnID {
		t.Fatalf("the thread was not rolled back after the stop: %v", reverted)
	}
	if _, err := sqlite.GetChatRun(ctx, h.app.DB, oldRun.ID); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatal("the stopped turn's record survived the edit", err)
	}
}

func TestChatEditThatCannotRevertChangesNothing(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, func(f *chatFake) { f.RevertErr = errors.New("thread busy") })
	ctx := context.Background()
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "create", Text: "最初的问题"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	oldRun := page.Runs[0]
	f.EmitTurnCompleted(oldRun.ThreadID, oldRun.TurnID, "completed", "第一轮的回答")
	page = chatWait(t, h, first.Chat.ID, domain.RunCompleted)
	userMessage := ""
	for _, m := range page.Messages {
		if m.Kind == domain.MessageKindUser {
			userMessage = m.ID
		}
	}
	if _, err := h.app.EditChatMessage(ctx, h.user, first.Chat.ID, userMessage, app.ChatEditText{RequestID: "edit-broken", Text: "改过的问题"}); err == nil {
		t.Fatal("a failed revert was reported as an edit")
	}
	if _, err := sqlite.GetChatRun(ctx, h.app.DB, oldRun.ID); err != nil {
		t.Fatal("the old turn was dropped although the revert failed", err)
	}
	if _, err := sqlite.GetChatMessage(ctx, h.app.DB, userMessage); err != nil {
		t.Fatal("the message was dropped although the revert failed", err)
	}
}

func TestEditingAMembersLastMessageRollsItsTurnBack(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	captain := bots[0]
	fake := h.ensureFake(t, team.ID)

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "最初的问题"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn 1", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "第一轮的回答")
	waitUntil(t, 5*time.Second, "turn 1 done", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return err != nil
	})
	var runID, userID string
	if err := h.app.DB.QueryRowContext(ctx, `SELECT id FROM runs WHERE bot_id = ? ORDER BY started_at DESC LIMIT 1`, captain.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := h.app.DB.QueryRowContext(ctx, `SELECT id FROM messages WHERE bot_id = ? AND kind = 'user' ORDER BY created_at DESC LIMIT 1`, captain.ID).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	if _, err := h.app.EditMessage(ctx, h.user, userID, app.MessageEditText{RequestID: "edit-1", Text: "改过的问题"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the thread rolled back", func() bool { return len(fake.Reverted()) == 1 })
	waitUntil(t, 5*time.Second, "turn 2", func() bool { return len(fake.Turns()) == 2 })
	second := fake.Turns()[1]
	fake.EmitTurnCompleted(second.ThreadID, second.TurnID, "completed", "新的回答")
	waitUntil(t, 5*time.Second, "turn 2 done", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return err != nil
	})
	if _, err := sqlite.GetRun(ctx, h.app.DB, runID); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatal("the old run survived the edit", err)
	}
	var text string
	if err := h.app.DB.QueryRowContext(ctx, `SELECT json_extract(blocks,'$[0].text') FROM messages WHERE bot_id = ? AND kind = 'user' ORDER BY created_at DESC LIMIT 1`, captain.ID).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != "改过的问题" {
		t.Fatalf("the edited message did not go out: %q", text)
	}
}

func TestEditingAMembersMessageThatMovedCollaborationIsRefused(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	captain := bots[0]
	fake := h.ensureFake(t, team.ID)

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "最初的问题"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn 1", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "第一轮的回答")
	waitUntil(t, 5*time.Second, "turn 1 done", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return err != nil
	})
	var runID, userID string
	if err := h.app.DB.QueryRowContext(ctx, `SELECT id FROM runs WHERE bot_id = ? ORDER BY started_at DESC LIMIT 1`, captain.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := h.app.DB.QueryRowContext(ctx, `SELECT id FROM messages WHERE bot_id = ? AND kind = 'user' ORDER BY created_at DESC LIMIT 1`, captain.ID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	// The turn handed a requirement to the project manager: its starting
	// message cannot be rolled back.
	if _, err := sqlite.UpsertRunStep(ctx, h.app.DB, domain.RunStep{ID: "req-1", RunID: runID, TeamID: team.ID, BotID: captain.ID, Kind: "tool", Status: "completed", Detail: []byte(`{"server":"cobot","tool":"start_requirement"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.EditMessage(ctx, h.user, userID, app.MessageEditText{RequestID: "edit-1", Text: "改过的问题"}); err == nil {
		t.Fatal("a turn with collaboration was edited anyway")
	}
	if len(fake.Reverted()) != 0 {
		t.Fatal("the thread was rolled back although the turn had collaboration")
	}
}
