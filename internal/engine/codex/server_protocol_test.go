package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

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
