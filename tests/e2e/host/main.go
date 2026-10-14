// Command host is a fake codex app-server used only by browser E2E tests.
// It speaks the same stdio JSON-RPC protocol the product expects, so the
// whole pipeline (scheduler, runs, approvals, events) runs for real; only the
// model is scripted. It is never part of the production binary.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  any             `json:"error,omitempty"`
}

var (
	mu        sync.Mutex
	out       = bufio.NewWriter(os.Stdout)
	turnSeq   int
	threadSeq int
	threads   = map[string]bool{}

	// FAKE_MODE controls the scripted behavior:
	//   reply (default): complete turns with a canned assistant message
	//   approve: request one command approval per turn, then accept it
	//   deny: request one command approval per turn, then decline it
	//   fail: end turns with status failed
	mode = os.Getenv("FAKE_MODE")

	pendingMu sync.Mutex
	pending   = map[string]chan string{}
)

// activeMode returns the mode for the current turn. FAKE_MODE sets the start
// default; a control file lets a test switch a running engine per turn without
// restarting the core. The control file wins while it exists.
func activeMode() string {
	if b, err := os.ReadFile(controlPath()); err == nil {
		if m := strings.TrimSpace(string(b)); m != "" {
			return m
		}
	}
	if mode == "" {
		return "reply"
	}
	return mode
}

// controlPath is where a test drops the next mode. FAKE_CONTROL must point at
// it explicitly; with no control file the engine stays in its default mode, so
// a run can never inherit a stale mode from an earlier test.
func controlPath() string {
	return os.Getenv("FAKE_CONTROL")
}

func send(msg message) {
	mu.Lock()
	defer mu.Unlock()
	data, _ := json.Marshal(msg)
	_, _ = out.Write(data)
	_, _ = out.Write([]byte{'\n'})
	_ = out.Flush()
}

func reply(id json.RawMessage, result any) {
	data, _ := json.Marshal(result)
	send(message{ID: id, Result: data})
}

func notify(method string, params any) {
	data, _ := json.Marshal(params)
	send(message{Method: method, Params: data})
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		switch {
		case msg.Method != "" && msg.ID != nil:
			handleRequest(msg)
		case msg.Method != "":
			// client notification (initialized, ...): nothing to do
		case msg.ID != nil:
			routeReply(msg)
		}
	}
}

func routeReply(msg message) {
	key := string(msg.ID)
	pendingMu.Lock()
	ch, ok := pending[key]
	pendingMu.Unlock()
	if !ok {
		return
	}
	var result struct {
		Decision string `json:"decision"`
	}
	_ = json.Unmarshal(msg.Result, &result)
	select {
	case ch <- result.Decision:
	default:
	}
}

func handleRequest(req message) {
	switch req.Method {
	case "initialize":
		reply(req.ID, map[string]any{"serverInfo": map[string]any{"name": "fake-codex", "version": "0.0.1"}})
	case "thread/start":
		var params struct {
			Cwd string `json:"cwd"`
		}
		_ = json.Unmarshal(req.Params, &params)
		threadSeq++
		id := fmt.Sprintf("fake-thread-%d", threadSeq)
		mu.Lock()
		threads[id] = true
		mu.Unlock()
		reply(req.ID, map[string]any{
			"thread": map[string]any{"id": id},
			"cwd":    params.Cwd,
			"model":  "fake-model", "modelProvider": "fake",
		})
	case "thread/resume":
		var params struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(req.Params, &params)
		mu.Lock()
		threads[params.ThreadID] = true // resume may reconnect after restart
		mu.Unlock()
		reply(req.ID, map[string]any{"thread": map[string]any{"id": params.ThreadID}})
	case "turn/start":
		var params struct {
			ThreadID string `json:"threadId"`
			Input    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"input"`
		}
		_ = json.Unmarshal(req.Params, &params)
		turnSeq++
		turnID := fmt.Sprintf("fake-turn-%d", turnSeq)
		text := ""
		if len(params.Input) > 0 {
			text = params.Input[0].Text
		}
		reply(req.ID, map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress", "items": []any{}}})
		go runTurn(params.ThreadID, turnID, text)
	case "turn/interrupt":
		reply(req.ID, map[string]any{})
	case "account/read":
		reply(req.ID, map[string]any{"auth": map[string]any{"mode": ""}})
	case "model/list":
		reply(req.ID, map[string]any{"models": []any{
			map[string]any{"slug": "fake-model", "displayName": "Fake Model"},
		}})
	case "account/login/start", "account/login/cancel", "account/logout":
		reply(req.ID, map[string]any{})
	default:
		if req.ID != nil {
			send(message{ID: req.ID, Error: map[string]any{"code": -32601, "Message": "method not found: " + req.Method}})
		}
	}
}

func runTurn(threadID, turnID, userText string) {
	notify("turn/started", map[string]any{
		"threadId": threadID, "turn": map[string]any{"id": turnID, "status": "inProgress", "items": []any{}},
	})
	decision := ""
	mode := activeMode()
	if mode == "approve" || mode == "deny" {
		decision = requestApproval(threadID, turnID)
	}
	status := "completed"
	text := "收到：" + userText
	switch {
	case mode == "fail":
		status = "failed"
		text = ""
	case decision == "decline":
		text = "命令被用户拒绝，未执行。"
	}
	if text != "" {
		notify("item/completed", map[string]any{
			"completedAtMs": 1,
			"threadId":      threadID, "turnId": turnID,
			"item": map[string]any{"type": "agentMessage", "id": "fake-item-1", "text": text},
		})
	}
	notify("thread/tokenUsage/updated", map[string]any{
		"threadId": threadID, "turnId": turnID,
		"tokenUsage": map[string]any{
			"last":  map[string]any{"inputTokens": 10, "outputTokens": 5, "cachedInputTokens": 0, "reasoningOutputTokens": 0, "totalTokens": 15},
			"total": map[string]any{"inputTokens": 10, "outputTokens": 5, "cachedInputTokens": 0, "reasoningOutputTokens": 0, "totalTokens": 15},
		},
	})
	notify("turn/completed", map[string]any{
		"threadId": threadID,
		"turn":     map[string]any{"id": turnID, "status": status, "items": []any{}},
	})
}

// requestApproval simulates a native command approval, waits for the product
// decision (routed by request id) and returns the engine decision string.
func requestApproval(threadID, turnID string) string {
	approvalID := fmt.Sprintf("fake-approval-%d", turnSeq)
	key := `"` + approvalID + `"`
	decision := make(chan string, 1)
	pendingMu.Lock()
	pending[key] = decision
	pendingMu.Unlock()
	defer func() {
		pendingMu.Lock()
		delete(pending, key)
		pendingMu.Unlock()
	}()

	params, _ := json.Marshal(map[string]any{
		"threadId": threadID, "turnId": turnID, "itemId": "fake-item-approval",
		"command": `pwsh -Command "Write-Host e2e"`, "cwd": "C:\\", "reason": "e2e scripted approval",
	})
	rawID, _ := json.Marshal(approvalID)
	send(message{ID: rawID, Method: "item/commandExecution/requestApproval", Params: params})

	select {
	case d := <-decision:
		return d
	case <-time.After(2 * time.Minute):
		return "timeout"
	}
}
