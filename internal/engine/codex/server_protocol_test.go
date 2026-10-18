package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/engine"
)

func TestFileApprovalUsesExactNativeItemPreview(t *testing.T) {
	var received engine.ApprovalRequest
	server := &Server{cwd: "team-directory", pending: map[string]pendingApproval{}, callbacks: engine.Callbacks{OnApproval: func(req engine.ApprovalRequest) { received = req }}}
	item := json.RawMessage(`{"threadId":"th","turnId":"turn","item":{"id":"item","type":"fileChange","status":"inProgress","changes":[{"path":"file.txt","kind":{"type":"add"},"diff":"+expected"}]}}`)
	server.handleNotify("item/started", item)
	server.handleRequest(InboundRequest{ID: json.RawMessage(`1`), Method: "item/fileChange/requestApproval", Params: json.RawMessage(`{"threadId":"th","turnId":"turn","itemId":"item"}`)})
	if len(received.Paths) != 1 || received.Paths[0] != "file.txt" || len(received.Changes) != 1 || received.Changes[0].Diff != "+expected" || received.Cwd != "team-directory" {
		t.Fatalf("missing exact preview: %+v", received)
	}
	server.handleRequest(InboundRequest{ID: json.RawMessage(`2`), Method: "item/fileChange/requestApproval", Params: json.RawMessage(`{"threadId":"th","turnId":"other","itemId":"item"}`)})
	if len(received.Paths) != 0 {
		t.Fatal("a prior turn's file preview was reused")
	}
	server.handleNotify("turn/completed", json.RawMessage(`{"threadId":"th","turn":{"id":"turn","status":"completed"}}`))
	if len(server.fileItems) != 0 {
		t.Fatal("terminal turn retained native file previews")
	}
}

func TestMissingFilePreviewReadsExactItemWithoutBlockingReader(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	received := make(chan engine.ApprovalRequest, 1)
	server := &Server{pending: map[string]pendingApproval{}, callbacks: engine.Callbacks{OnApproval: func(req engine.ApprovalRequest) { received <- req }}}
	client := NewClient(nopCloser{inW}, outR, ClientHooks{OnRequest: server.handleRequest})
	server.client = client
	t.Cleanup(func() { inW.Close(); outW.Close(); client.Wait() })
	go func() {
		scanner := bufio.NewScanner(inR)
		for scanner.Scan() {
			var req map[string]any
			if json.Unmarshal(scanner.Bytes(), &req) != nil {
				return
			}
			if req["method"] != "thread/read" {
				continue
			}
			writeJSONLine(outW, map[string]any{"id": req["id"], "result": map[string]any{"thread": map[string]any{"turns": []any{
				map[string]any{"id": "other", "items": []any{map[string]any{"id": "item", "type": "fileChange", "changes": []any{map[string]any{"path": "wrong.txt", "diff": "wrong"}}}}},
				map[string]any{"id": "turn", "items": []any{map[string]any{"id": "item", "type": "fileChange", "changes": []any{map[string]any{"path": "expected.txt", "diff": "+right"}}}}},
			}}}})
		}
	}()
	writeJSONLine(outW, map[string]any{"id": "native-request", "method": "item/fileChange/requestApproval", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "item"}})
	select {
	case req := <-received:
		if len(req.Changes) != 1 || req.Changes[0].Path != "expected.txt" || req.Changes[0].Diff != "+right" {
			t.Fatalf("wrong file preview: %+v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("file inspection blocked the RPC reader")
	}
}

func TestUnsupportedServerRequestsDoNotBecomeCommandApprovals(t *testing.T) {
	for _, method := range []string{"mcpServer/elicitation/request", "unknown/request"} {
		var output bytes.Buffer
		client := &Client{stdin: nopCloser{&output}, closed: make(chan struct{}), writeGate: make(chan struct{}, 1)}
		approval := false
		server := &Server{client: client, callbacks: engine.Callbacks{OnApproval: func(engine.ApprovalRequest) { approval = true }}}
		server.handleRequest(InboundRequest{ID: json.RawMessage(`"request"`), Method: method, Params: json.RawMessage(`{}`)})
		if approval {
			t.Fatal("unsupported request became actionable command card")
		}
		var response map[string]any
		if err := json.Unmarshal(output.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if method == "mcpServer/elicitation/request" {
			result, _ := response["result"].(map[string]any)
			if result["action"] != "cancel" || result["decision"] != nil {
				t.Fatal("wrong elicitation response", response)
			}
		} else if response["error"] == nil {
			t.Fatal("unknown request was silently accepted")
		}
	}
}

func TestAdapterUsesRequiredProtocolFields(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	client := NewClient(nopCloser{inW}, outR, ClientHooks{})
	t.Cleanup(func() { inW.Close(); outW.Close(); client.Wait() })
	requests := make(chan map[string]any, 8)
	go func() {
		scanner := bufio.NewScanner(inR)
		for scanner.Scan() {
			var req map[string]any
			if json.Unmarshal(scanner.Bytes(), &req) != nil {
				return
			}
			requests <- req
			result := map[string]any{}
			switch req["method"] {
			case "account/login/start":
				result = map[string]any{"type": "chatgpt", "loginId": "login-1", "authUrl": "https://auth.openai.com/example"}
			case "model/list":
				p := req["params"].(map[string]any)
				if p["cursor"] == nil {
					result = map[string]any{"data": []any{map[string]any{"id": "m1", "model": "one"}}, "nextCursor": "page-2"}
				} else {
					result = map[string]any{"data": []any{map[string]any{"id": "m2", "model": "two"}}, "nextCursor": nil}
				}
			case "thread/read":
				result = map[string]any{"thread": map[string]any{"turns": []any{map[string]any{"id": "turn-9", "status": "interrupted"}}}}
			}
			writeJSONLine(outW, map[string]any{"id": req["id"], "result": result})
		}
	}()
	s := &Server{client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	login, err := s.AccountLoginStart(ctx)
	if err != nil || login.LoginID != "login-1" || login.AuthURL == "" {
		t.Fatalf("login result %+v %v", login, err)
	}
	<-requests
	if err = s.AccountLoginCancel(ctx, login.LoginID); err != nil {
		t.Fatal(err)
	}
	if p := (<-requests)["params"].(map[string]any); p["loginId"] != "login-1" {
		t.Fatal("missing login id", p)
	}
	if err = s.InterruptTurn(ctx, "th-1", "turn-9"); err != nil {
		t.Fatal(err)
	}
	if p := (<-requests)["params"].(map[string]any); p["threadId"] != "th-1" || p["turnId"] != "turn-9" {
		t.Fatal("wrong turn identity", p)
	}
	if err = s.ResumeThread(ctx, "th-1", engine.ThreadSpec{Cwd: "new-cwd", Model: "new-model", DeveloperInstructions: "current-role", Sandbox: "workspace-write", Config: map[string]any{"mcp_servers.cobot.enabled": true}}); err != nil {
		t.Fatal(err)
	}
	if p := (<-requests)["params"].(map[string]any); p["cwd"] != "new-cwd" || p["model"] != "new-model" || p["developerInstructions"] != "current-role" || p["config"] == nil || p["excludeTurns"] != true {
		t.Fatal("resume lost activation config", p)
	}
	// A resumed thread is loaded until Codex closes or unloads it.
	if !s.ThreadLoaded("th-1") {
		t.Fatal("resumed thread not loaded")
	}
	s.handleNotify("thread/status/changed", json.RawMessage(`{"threadId":"th-1","status":{"type":"idle"}}`))
	if !s.ThreadLoaded("th-1") {
		t.Fatal("an idle thread is still loaded")
	}
	s.handleNotify("thread/status/changed", json.RawMessage(`{"threadId":"th-1","status":{"type":"notLoaded"}}`))
	if s.ThreadLoaded("th-1") {
		t.Fatal("an unloaded thread must be resumed again")
	}
	s.setLoaded("th-1", true)
	s.handleNotify("thread/closed", json.RawMessage(`{"threadId":"th-1"}`))
	if s.ThreadLoaded("th-1") {
		t.Fatal("a closed thread must be resumed again")
	}
	if status, err := s.InspectTurn(ctx, "th-1", "turn-9"); err != nil || status != "interrupted" {
		t.Fatalf("terminal fact = %q / %v", status, err)
	}
	if p := (<-requests)["params"].(map[string]any); p["includeTurns"] != true || p["threadId"] != "th-1" {
		t.Fatal("inspection lost identity", p)
	}
	models, err := s.ModelList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Data []any `json:"data"`
	}
	json.Unmarshal(models, &page)
	if len(page.Data) != 2 {
		t.Fatal("model pages lost", string(models))
	}
	<-requests
	if p := (<-requests)["params"].(map[string]any); p["cursor"] != "page-2" {
		t.Fatal("model cursor lost", p)
	}
}

// A turn/start Codex answers with an error never started, unlike one whose
// answer never came; the refusal also drops the thread's loaded mark.
func TestTurnStartRefusalIsTellableFromAnUnknownOutcome(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := &Server{pending: map[string]pendingApproval{}, intents: map[string]string{}}
	client := NewClient(nopCloser{inW}, outR, ClientHooks{})
	server.client = client
	t.Cleanup(func() { inW.Close(); outW.Close(); client.Wait() })
	go func() {
		scanner := bufio.NewScanner(inR)
		for scanner.Scan() {
			var req map[string]any
			if json.Unmarshal(scanner.Bytes(), &req) != nil {
				return
			}
			// The refused thread gets an error; the other is never answered.
			if params, _ := req["params"].(map[string]any); params["threadId"] == "refused" {
				writeJSONLine(outW, map[string]any{"id": req["id"], "error": map[string]any{"code": -32600, "message": "thread not found"}})
			}
		}
	}()
	server.setLoaded("refused", true)
	_, err := server.StartTurn(context.Background(), "refused", "hi", engine.TurnOptions{})
	if !errors.Is(err, engine.ErrRejected) {
		t.Fatalf("refusal = %v, want ErrRejected", err)
	}
	if server.ThreadLoaded("refused") {
		t.Fatal("a refused thread must be resumed before its next turn")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := server.StartTurn(ctx, "silent", "hi", engine.TurnOptions{}); err == nil || errors.Is(err, engine.ErrRejected) {
		t.Fatalf("no answer = %v, want an unknown outcome", err)
	}
}
