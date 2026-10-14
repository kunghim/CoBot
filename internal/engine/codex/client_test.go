package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func writeJSONLine(w io.Writer, payload map[string]any) {
	data, _ := json.Marshal(payload)
	_, _ = w.Write(append(data, '\n'))
}

func readJSONLine(r io.Reader) map[string]any {
	line, err := bufio.NewReader(r).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(line, &out)
	return out
}

func TestClientCallAndResult(t *testing.T) {
	clientStdinR, clientStdinW := io.Pipe()
	serverStdoutR, serverStdoutW := io.Pipe()
	go func() {
		req := readJSONLine(clientStdinR)
		if req == nil || req["method"] != "initialize" {
			return
		}
		writeJSONLine(serverStdoutW, map[string]any{"id": req["id"], "result": map[string]any{"ok": true}})
	}()
	client := NewClient(nopCloser{clientStdinW}, serverStdoutR, ClientHooks{})
	t.Cleanup(func() {
		_ = clientStdinW.Close()
		_ = serverStdoutW.Close()
		client.Wait()
	})

	var out struct {
		OK bool `json:"ok"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Call(ctx, "initialize", InitializeParams{ClientInfo: ClientInfo{Name: "test", Version: "0"}}, &out); err != nil {
		t.Fatalf("call: %v", err)
	}
	if !out.OK {
		t.Fatal("result not decoded")
	}
}

func TestClientRoutesServerRequestsAndNotifications(t *testing.T) {
	requests := make(chan InboundRequest, 2)
	notifies := make(chan string, 1)
	clientStdinR, clientStdinW := io.Pipe()
	serverStdoutR, serverStdoutW := io.Pipe()
	go func() {
		// Drain whatever the client writes (replies).
		go func() { _, _ = io.Copy(io.Discard, clientStdinR) }()
		writeJSONLine(serverStdoutW, map[string]any{
			"id": 7, "method": "item/commandExecution/requestApproval",
			"params": map[string]any{"threadId": "th-1", "command": "echo hi"},
		})
		writeJSONLine(serverStdoutW, map[string]any{
			"id": "abc", "method": "item/fileChange/requestApproval",
			"params": map[string]any{"threadId": "th-1", "itemId": "i1"},
		})
		writeJSONLine(serverStdoutW, map[string]any{
			"method": "turn/completed", "params": map[string]any{"threadId": "th-1"},
		})
	}()
	client := NewClient(nopCloser{clientStdinW}, serverStdoutR, ClientHooks{
		OnRequest: func(req InboundRequest) { requests <- req },
		OnNotify:  func(method string, params json.RawMessage) { notifies <- method },
	})
	t.Cleanup(func() {
		_ = clientStdinW.Close()
		_ = serverStdoutW.Close()
		client.Wait()
	})

	first := <-requests
	if string(first.ID) != "7" {
		t.Fatalf("numeric id must be preserved verbatim, got %s", first.ID)
	}
	if first.Method != "item/commandExecution/requestApproval" {
		t.Fatalf("method = %s", first.Method)
	}
	second := <-requests
	if string(second.ID) != `"abc"` {
		t.Fatalf("string id must be preserved verbatim, got %s", second.ID)
	}
	if got := <-notifies; got != "turn/completed" {
		t.Fatalf("notification = %s", got)
	}
	if err := client.Reply(first.ID, map[string]any{"decision": "accept"}); err != nil {
		t.Fatalf("reply: %v", err)
	}
}

func TestClientCallTimesOut(t *testing.T) {
	clientStdinR, clientStdinW := io.Pipe()
	serverStdoutR, serverStdoutW := io.Pipe()
	go func() { _, _ = io.Copy(io.Discard, clientStdinR) }()
	client := NewClient(nopCloser{clientStdinW}, serverStdoutR, ClientHooks{})
	t.Cleanup(func() {
		_ = clientStdinW.Close()
		_ = serverStdoutW.Close()
		client.Wait()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := client.Call(ctx, "model/list", map[string]any{}, nil)
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("want deadline error, got %v", err)
	}
}

func TestClientFailsPendingCallsOnExit(t *testing.T) {
	clientStdinR, clientStdinW := io.Pipe()
	serverStdoutR, serverStdoutW := io.Pipe()
	go func() { _, _ = io.Copy(io.Discard, clientStdinR) }()
	client := NewClient(nopCloser{clientStdinW}, serverStdoutR, ClientHooks{})
	// Simulate process exit by closing the server stream.
	_ = serverStdoutW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.Call(ctx, "account/read", map[string]any{}, nil)
	if err == nil {
		t.Fatal("call over a dead stream must fail")
	}
}
