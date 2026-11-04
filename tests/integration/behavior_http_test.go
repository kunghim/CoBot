package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// These speak HTTP the way the product does, so what they exercise is also
// compared with api/openapi.yaml (see tests/contract).

func (h *m2Harness) userCall(t *testing.T, method, path string, body any, want int) map[string]any {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, h.api.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != want {
		t.Fatalf("%s %s = %d %v, want %d", method, path, resp.StatusCode, out, want)
	}
	data, _ := out["data"].(map[string]any)
	return data
}

func TestBudgetOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	dev := bots[1]
	h.userCall(t, http.MethodGet, "/api/v1/teams/"+team.ID+"/budget", nil, http.StatusOK)

	set := h.userCall(t, http.MethodPut, "/api/v1/teams/"+team.ID+"/budget", map[string]any{"limit": 5000, "requestId": "budget-team"}, http.StatusOK)
	if set["limit"] != float64(5000) {
		t.Fatalf("team limit = %v", set)
	}
	member := h.userCall(t, http.MethodPut, "/api/v1/bots/"+dev.ID+"/budget", map[string]any{"limit": 1000, "requestId": "budget-bot"}, http.StatusOK)
	if member["limit"] != float64(1000) {
		t.Fatalf("member limit = %v", member)
	}
	h.userCall(t, http.MethodPut, "/api/v1/bots/"+dev.ID+"/budget", map[string]any{"limit": nil, "requestId": "budget-bot-null"}, http.StatusOK)
	h.userCall(t, http.MethodPut, "/api/v1/teams/"+team.ID+"/budget", map[string]any{"requestId": "budget-missing"}, http.StatusBadRequest)
	h.userCall(t, http.MethodPost, "/api/v1/teams/"+team.ID+"/budget/reset", map[string]any{"requestId": "reset-team"}, http.StatusOK)
	h.userCall(t, http.MethodPost, "/api/v1/bots/"+dev.ID+"/budget/reset", map[string]any{"requestId": "reset-bot"}, http.StatusOK)
}

func TestChatLifeOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)

	h.userCall(t, http.MethodGet, "/api/v1/chats", nil, http.StatusOK)
	created := h.userCall(t, http.MethodPost, "/api/v1/chats", map[string]any{"requestId": "http-chat", "text": "你好"}, http.StatusAccepted)
	chat, _ := created["chat"].(map[string]any)
	id, _ := chat["id"].(string)
	if id == "" {
		t.Fatalf("created = %v", created)
	}
	f := chatCreated(t, made)
	path := "/api/v1/chats/" + id

	var run domain.ChatRun
	waitUntil(t, 5*time.Second, "a running turn", func() bool {
		page := h.userCall(t, http.MethodGet, path, nil, http.StatusOK)
		raw, _ := json.Marshal(page)
		var decoded domain.ChatPage
		if json.Unmarshal(raw, &decoded) != nil {
			return false
		}
		for _, r := range decoded.Runs {
			if r.Status == domain.RunRunning {
				run = r
				return true
			}
		}
		return false
	})
	revision := func() int64 {
		t.Helper()
		raw, _ := json.Marshal(h.userCall(t, http.MethodGet, path, nil, http.StatusOK))
		var decoded domain.ChatPage
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded.Chat.Revision
	}
	h.userCall(t, http.MethodPatch, path, map[string]any{"requestId": "http-rename", "title": "新名字", "expectedRevision": revision()}, http.StatusOK)

	f.EmitApproval("9", run.ThreadID, run.TurnID, "go test")
	var request string
	var requestRevision int64
	waitUntil(t, 5*time.Second, "the card", func() bool {
		raw, _ := json.Marshal(h.userCall(t, http.MethodGet, path, nil, http.StatusOK))
		var decoded domain.ChatPage
		if json.Unmarshal(raw, &decoded) != nil || len(decoded.Requests) == 0 {
			return false
		}
		request, requestRevision = decoded.Requests[0].ID, decoded.Requests[0].Revision
		return true
	})
	h.userCall(t, http.MethodPost, path+"/requests/"+request+"/reply", map[string]any{"requestId": "http-reply", "optionId": domain.DecisionDeclineEngine, "expectedRevision": requestRevision}, http.StatusOK)

	h.userCall(t, http.MethodPost, path+"/runs/"+run.ID+"/interrupt", map[string]any{"requestId": "http-stop"}, http.StatusOK)
	f.EmitTurnCompleted(run.ThreadID, run.TurnID, "interrupted", "")
	chatWait(t, h, id, domain.RunInterrupted)

	h.userCall(t, http.MethodPost, path+"/messages", map[string]any{"requestId": "http-second", "text": "再问一句"}, http.StatusAccepted)
	chatWaitTurns(t, f, 2)
	turn := f.Turns()[1]
	f.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "好")
	chatWait(t, h, id, domain.RunCompleted)

	navigation := h.userCall(t, http.MethodGet, "/api/v1/chat-navigation", nil, http.StatusOK)
	h.userCall(t, http.MethodPost, "/api/v1/chat-navigation", map[string]any{"requestId": "http-nav", "action": "preferences", "sort": "recent", "grouping": "sections", "expectedRevision": navigation["revision"]}, http.StatusOK)
	h.userCall(t, http.MethodPost, path+"/actions/pin", map[string]any{"requestId": "http-pin", "expectedRevision": revision()}, http.StatusOK)
	// The client sends a delete the way it sends every delete: no body, the
	// request id and the revision in headers.
	req, err := http.NewRequest(http.MethodDelete, h.api.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	req.Header.Set("Idempotency-Key", "http-delete")
	req.Header.Set("If-Match", strconv.FormatInt(revision(), 10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete = %d %s", resp.StatusCode, body)
	}
}

func chatWaitTurns(t *testing.T, f *chatFake, n int) {
	t.Helper()
	waitUntil(t, 5*time.Second, "the next chat turn", func() bool { return len(f.Turns()) >= n })
}

func TestEditingAndOpeningOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	var opened []string
	h.app.OpenLocal = func(path string, reveal bool) error {
		opened = append(opened, path)
		return nil
	}

	created := h.userCall(t, http.MethodPost, "/api/v1/chats", map[string]any{"requestId": "edit-chat", "text": "你好"}, http.StatusAccepted)
	raw, _ := json.Marshal(created)
	var sent struct {
		Chat domain.Chat `json:"chat"`
	}
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	chatWaitTurns(t, f, 1)
	turn := f.Turns()[0]
	f.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "好")
	chatWait(t, h, sent.Chat.ID, domain.RunCompleted)
	path := "/api/v1/chats/" + sent.Chat.ID

	if err := os.WriteFile(filepath.Join(sent.Chat.Workspace, "note.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.userCall(t, http.MethodPost, path+"/open-path", map[string]any{"requestId": "open-note", "path": "note.txt"}, http.StatusOK)
	if len(opened) != 1 {
		t.Fatalf("opened = %v", opened)
	}
	pageRaw, _ := json.Marshal(h.userCall(t, http.MethodGet, path, nil, http.StatusOK))
	var page domain.ChatPage
	if err := json.Unmarshal(pageRaw, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) == 0 {
		t.Fatal("no messages")
	}
	h.userCall(t, http.MethodPost, path+"/messages/"+page.Messages[0].ID+"/edit", map[string]any{"requestId": "edit-note", "text": "改过的"}, http.StatusOK)
	chatWaitTurns(t, f, 2)

	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	dev := bots[1]
	sendAndFinish(t, h, fake, dev, "原来的话")
	messages, err := sqlite.ListMessages(context.Background(), h.app.DB, dev.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	var last string
	for _, m := range messages {
		if m.Kind == domain.MessageKindUser {
			last = m.ID
		}
	}
	h.userCall(t, http.MethodPost, "/api/v1/messages/"+last+"/edit", map[string]any{"requestId": "edit-member", "text": "新的话"}, http.StatusOK)
	waitUntil(t, 5*time.Second, "the edited turn", func() bool { return len(fake.Turns()) == 2 })
}
