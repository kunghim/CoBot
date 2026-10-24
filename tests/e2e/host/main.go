// Command host is a fake codex app-server used only by browser E2E tests.
// It speaks the same stdio JSON-RPC protocol the product expects, so the
// whole pipeline (scheduler, runs, approvals, events) runs for real; only the
// model is scripted. It is never part of the production binary.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

	once sync.Once
	wd   string
)

// activeMode returns the mode for the current turn. FAKE_MODE sets the start
// default; a control file lets a test switch a running engine per turn without
// restarting the core. The control file wins while it exists.
//
// The path is team-scoped (FAKE_CONTROL plus a hash of the process cwd, which
// is the team directory) because every team gets its own app-server and the
// specs run several teams against one core. A single shared file would leak
// one team's mode into another team's turn.
func activeMode() string {
	cp := controlPath()
	if b, err := os.ReadFile(cp); err == nil {
		if m := strings.TrimSpace(string(b)); m != "" {
			return m
		}
	}
	if mode == "" {
		return "reply"
	}
	return mode
}

// controlPath is where a test drops the next mode. FAKE_CONTROL names a
// directory; the per-team file inside it is keyed by the process cwd, which is
// the team working directory the core gave this app-server. An empty value
// keeps the engine in its default mode so a run can never inherit a stale
// mode from an earlier test.
//
// The key normalizes separators and case so it matches what a spec computes
// from the directory string it typed into the create dialog, even though
// Windows reports the working directory in its own casing.
func controlPath() string {
	base := os.Getenv("FAKE_CONTROL")
	if base == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalizePath(cwd())))
	return filepath.Join(base, fmt.Sprintf("mode-%x", sum[:8]))
}

// normalizePath folds a path to the form both sides hash: forward slashes,
// lowercased on Windows, no trailing separator.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return strings.TrimRight(p, "/")
}

// cwd is resolved once so the key stays stable even if the process later
// changes its working directory.
func cwd() string {
	once.Do(func() {
		resolved, err := os.Getwd()
		if err != nil {
			resolved = ""
		}
		wd = resolved
	})
	return wd
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
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("codex-cli 0.161.0")
		return
	}
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
			Cwd      string `json:"cwd"`
			Model    string `json:"model"`
			Provider string `json:"modelProvider"`
		}
		_ = json.Unmarshal(req.Params, &params)
		threadSeq++
		id := fmt.Sprintf("fake-thread-%d", threadSeq)
		mu.Lock()
		threads[id] = true
		mu.Unlock()
		if params.Model == "" {
			params.Model = "fake-model"
		}
		if params.Provider == "" {
			params.Provider = "openai"
		}
		reply(req.ID, map[string]any{
			"thread": map[string]any{"id": id},
			"cwd":    params.Cwd,
			"model":  params.Model, "modelProvider": params.Provider,
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
	case "thread/archive", "thread/unarchive", "thread/read":
		var p struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		cwd, _ := os.Getwd()
		file := filepath.Join(cwd, ".fake-archive-"+p.ThreadID)
		if req.Method == "thread/archive" {
			_ = os.WriteFile(file, []byte("archived"), 0600)
			reply(req.ID, map[string]any{})
			return
		}
		if req.Method == "thread/unarchive" {
			_ = os.Remove(file)
			reply(req.ID, map[string]any{"thread": map[string]any{"id": p.ThreadID}})
			return
		}
		dir := "sessions"
		if _, err := os.Stat(file); err == nil {
			dir = "archived_sessions"
		}
		reply(req.ID, map[string]any{"thread": map[string]any{"id": p.ThreadID, "path": "C:/fake-codex/" + dir + "/rollout-" + p.ThreadID + ".jsonl", "turns": []any{}}})
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
	case "turn/steer":
		// The scripted turns finish on their own; extra input is accepted.
		reply(req.ID, map[string]any{})
	case "turn/interrupt":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ThreadID == "" || p.TurnID == "" {
			send(message{ID: req.ID, Error: map[string]any{"code": -32600, "message": "threadId and turnId required"}})
			return
		}
		reply(req.ID, map[string]any{})
	case "account/read":
		reply(req.ID, map[string]any{"account": nil, "requiresOpenaiAuth": true})
	case "model/list":
		reply(req.ID, map[string]any{"data": []any{
			map[string]any{"id": "fake-model", "model": "fake-model", "displayName": "Fake Model"},
		}})
	case "account/login/start":
		reply(req.ID, map[string]any{"type": "chatgpt", "loginId": "fake-login", "authUrl": "https://example.invalid/login"})
	case "account/login/cancel":
		var p struct {
			LoginID string `json:"loginId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.LoginID == "" {
			send(message{ID: req.ID, Error: map[string]any{"code": -32600, "message": "loginId required"}})
			return
		}
		reply(req.ID, map[string]any{})
	case "account/logout":
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
	if mode == "process" {
		runProcess(threadID, turnID, userText)
		return
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
	// Named after its own turn: the shared counter has moved on by the time
	// this goroutine runs when several members start at once, and two
	// requests under one id leave one of them waiting forever.
	approvalID := "fake-approval-" + turnID
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

// runProcess scripts a turn the way a real one looks from the outside: the
// model connection drops and is retried, the member thinks, runs a command
// whose output streams, edits a file and streams a Markdown reply that links
// the file it wrote. Pauses keep each phase visible to the browser.
func runProcess(threadID, turnID, userText string) {
	pause := func() { time.Sleep(700 * time.Millisecond) }
	ids := map[string]any{"threadId": threadID, "turnId": turnID}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range ids {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	notify("error", with(map[string]any{"willRetry": true, "error": map[string]any{"message": "Reconnecting... 1/5", "codexErrorInfo": "other"}}))
	pause()
	notify("item/started", with(map[string]any{"item": map[string]any{"type": "reasoning", "id": "r1", "summary": []any{}, "content": []any{}}}))
	notify("item/reasoning/summaryTextDelta", with(map[string]any{"itemId": "r1", "delta": "先看看目录结构", "summaryIndex": 0}))
	pause()
	notify("item/completed", with(map[string]any{"item": map[string]any{"type": "reasoning", "id": "r1", "summary": []any{"先看看目录结构"}, "content": []any{}}}))
	command := map[string]any{"type": "commandExecution", "id": "c1", "command": "go test ./...", "cwd": cwd(), "status": "inProgress", "commandActions": []any{}}
	notify("item/started", with(map[string]any{"item": command}))
	notify("item/commandExecution/outputDelta", with(map[string]any{"itemId": "c1", "delta": "ok  \texample/pkg\t0.01s\n"}))
	pause()
	command["status"], command["exitCode"], command["durationMs"], command["aggregatedOutput"] = "completed", 0, 640, "ok  \texample/pkg\t0.01s\n"
	notify("item/completed", with(map[string]any{"item": command}))
	notify("item/completed", with(map[string]any{"item": map[string]any{
		"type": "fileChange", "id": "f1", "status": "completed",
		"changes": []any{map[string]any{"path": "notes.md", "kind": map[string]any{"type": "add"}, "diff": "+# 笔记\n+完成"}},
	}}))
	reply := "## 已完成\n\n- 测试全部通过\n- 写了 [notes.md](notes.md)\n\n收到：" + userText
	_ = os.WriteFile(filepath.Join(cwd(), "notes.md"), []byte("# 笔记\n完成\n"), 0o644)
	split := strings.Index(reply, "\n\n")
	for _, part := range []string{reply[:split], reply[split:]} {
		notify("item/agentMessage/delta", with(map[string]any{"itemId": "m1", "delta": part}))
		pause()
	}
	notify("item/completed", with(map[string]any{"completedAtMs": 1, "item": map[string]any{"type": "agentMessage", "id": "m1", "text": reply}}))
	notify("turn/completed", map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": "completed", "items": []any{}}})
}
