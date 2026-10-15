package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/engine"
)

func TestUnsupportedServerRequestsDoNotBecomeCommandApprovals(t *testing.T) {
	for _, method := range []string{"mcpServer/elicitation/request", "unknown/request"} {
		var output bytes.Buffer
		client := &Client{stdin: nopCloser{&output}, closed: make(chan struct{})}
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
	if p := (<-requests)["params"].(map[string]any); p["cwd"] != "new-cwd" || p["model"] != "new-model" || p["developerInstructions"] != "current-role" || p["config"] == nil {
		t.Fatal("resume lost activation config", p)
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
