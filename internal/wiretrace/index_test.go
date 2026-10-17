package wiretrace

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSession lays out a small recording the way the recorders would: the
// core's engine traffic, a bridge, a hook and a Codex rollout.
func writeSession(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	base := time.Date(2026, 10, 17, 22, 0, 0, 0, time.Local)
	write := func(name string, entries ...Entry) {
		var lines []string
		for i, e := range entries {
			e.Seq = int64(i + 1)
			line, _ := json.Marshal(e)
			lines = append(lines, string(line))
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	at := func(seconds float64) string {
		return base.Add(time.Duration(seconds * float64(time.Second))).Format(time.RFC3339Nano)
	}
	core := func(s float64, channel, dir, raw string) Entry {
		return Entry{Time: at(s), Proc: "core", PID: 1, Channel: channel, Dir: dir, Peer: "codex#9", Raw: raw}
	}
	const thread = "01a14a39-6d10-71f0-b21c-26762ebd8fa6"
	write("core-1.jsonl",
		core(0, "codex.stdio", "cobot→codex", `{"id":1,"method":"thread/start","params":{"developerInstructions":"你的名字：队长（角色：队长）"}}`),
		core(1, "codex.stdio", "codex→cobot", `{"id":1,"result":{"thread":{"id":"`+thread+`"}}}`),
		core(2, "codex.stdio", "cobot→codex", `{"id":2,"method":"turn/start","params":{"threadId":"`+thread+`","input":[{"type":"text","text":"做一个页面"}]}}`),
		core(3, "codex.stdio", "codex→cobot", `{"method":"turn/started","params":{"threadId":"`+thread+`","turn":{"id":"t1"}}}`),
		core(30, "codex.stdio", "codex→cobot", `{"method":"error","params":{"threadId":"`+thread+`","willRetry":true,"error":{"message":"Reconnecting... 2/5"}}}`),
		core(60, "codex.stdio", "codex→cobot", `{"method":"error","params":{"threadId":"`+thread+`","willRetry":true,"error":{"message":"Reconnecting... 3/5"}}}`),
		core(90, "codex.stdio", "codex→cobot", `{"method":"warning","params":{"threadId":"`+thread+`","message":"Falling back from WebSockets to HTTPS transport."}}`),
		core(91, "codex.stdio", "codex→cobot", `{"method":"item/agentMessage/delta","params":{"threadId":"`+thread+`","itemId":"m1","delta":"好"}}`),
		core(91.1, "codex.stdio", "codex→cobot", `{"method":"item/agentMessage/delta","params":{"threadId":"`+thread+`","itemId":"m1","delta":"的"}}`),
		core(92, "codex.stdio", "codex→cobot", `{"method":"thread/goal/cleared","params":{"threadId":"`+thread+`"}}`),
		core(93, "codex.stdio", "codex→cobot", `{"method":"item/started","params":{"threadId":"`+thread+`","item":{"type":"sleep","id":"i9"}}}`),
		core(94, "codex.stderr", "codex→cobot", "\x1b[2m2026-10-17T14:00:00Z\x1b[0m ERROR codex_models_manager::manager: failed to refresh available models: request timed out"),
		core(95, "codex.stderr", "codex→cobot", "\x1b[2m2026-10-17T14:05:00Z\x1b[0m ERROR codex_models_manager::manager: failed to refresh available models: request timed out"),
	)
	write("mcp-7.jsonl",
		Entry{Time: at(4), Proc: "mcp", PID: 7, Channel: "mcp.process", Dir: "start", Raw: `{"args":["cobot","mcp"],"botId":"bot-1"}`},
		Entry{Time: at(5), Proc: "mcp", PID: 7, Channel: "mcp.stdio", Dir: "codex→bridge", Raw: `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"send_message"}}`},
		Entry{Time: at(6), Proc: "mcp", PID: 7, Channel: "mcp.stdio", Dir: "bridge→codex", Raw: `{"jsonrpc":"2.0","id":3,"result":{}}`},
	)
	write("hook-8.jsonl",
		Entry{Time: at(7), Proc: "hook", PID: 8, Channel: "hook.stdin", Dir: "codex→hook", Raw: `{"session_id":"` + thread + `","tool_name":"shell","tool_input":{"command":"rm -rf x"}}`},
		Entry{Time: at(7.1), Proc: "hook", PID: 8, Channel: "hook.stdout", Dir: "hook→codex", Raw: `{"hookSpecificOutput":{"permissionDecision":"deny"}}`},
	)
	// The bridge's bot is named by the core's thread binding.
	write("core-2.jsonl", Entry{Time: at(1.5), Proc: "core", PID: 2, Channel: "cobot.meta", Dir: "thread-bound", Raw: `{"threadId":"` + thread + `","botId":"bot-1","botName":"队长"}`})
	rollout := filepath.Join(dir, "codex-home", "home", "sessions", "rollout-2026-10-17T22-00-00-"+thread+".jsonl")
	_ = os.MkdirAll(filepath.Dir(rollout), 0o700)
	_ = os.WriteFile(rollout, []byte(fmt.Sprintf(`{"timestamp":"%s","type":"response_item","payload":{"type":"message","role":"assistant"}}`+"\n", base.Add(91*time.Second).UTC().Format(time.RFC3339Nano))), 0o600)
	return dir
}

func TestIndexNamesBothEndsAndKeepsRawBytes(t *testing.T) {
	idx, err := Load(writeSession(t))
	if err != nil {
		t.Fatal(err)
	}
	find := func(kind string) Row {
		for _, row := range idx.Rows {
			if row.Kind == kind {
				return row
			}
		}
		t.Fatalf("no row %q", kind)
		return Row{}
	}
	if r := find("turn/start"); r.From != "CoBot" || r.To != "队长(Codex)" || r.Summary != "做一个页面" {
		t.Fatalf("turn/start = %+v", r)
	}
	if r := find("↩ thread/start"); r.From != "队长(Codex)" {
		t.Fatalf("response not paired: %+v", r)
	}
	if r := find("↩ tools/call"); r.From != "队长的协作工具桥" || r.Who != "队长" {
		t.Fatalf("bridge row = %+v", r)
	}
	if r := find("拒绝"); r.Anomaly != "安全检查拒绝" || r.Who != "队长" {
		t.Fatalf("hook answer = %+v", r)
	}
	if r := find("response_item/message assistant"); r.To != "Codex 本地记录" {
		t.Fatalf("rollout row = %+v", r)
	}
	// Rows are in time order across files, and raw bytes come back as sent.
	for i := 1; i < len(idx.Rows); i++ {
		if idx.Rows[i].at.Before(idx.Rows[i-1].at) {
			t.Fatal("rows out of time order")
		}
	}
	raw, err := idx.Raw(find("ERROR").ID)
	if err != nil || !strings.HasPrefix(string(raw), "\x1b[2m2026-10-17T14:00:00Z") {
		t.Fatalf("raw = %q %v", raw, err)
	}
}

func TestSummaryFindsUnhandledMessagesReconnectsAndTrouble(t *testing.T) {
	idx, err := Load(writeSession(t))
	if err != nil {
		t.Fatal(err)
	}
	s := idx.Summarize([]string{"turn/started", "error", "warning", "item/agentMessage/delta", "item/started"}, []string{"agentMessage"})
	if len(s.Unhandled) != 1 || s.Unhandled[0].Name != "thread/goal/cleared" {
		t.Fatalf("unhandled = %+v", s.Unhandled)
	}
	if len(s.UnknownItems) != 1 || s.UnknownItems[0].Name != "sleep" {
		t.Fatalf("unknown items = %+v", s.UnknownItems)
	}
	// The retries began before the turn did anything, so the wait counts
	// from the turn's start (3s) to the fallback warning (90s).
	if len(s.Reconnects) != 1 || s.Reconnects[0].Attempts != 2 || s.Reconnects[0].Seconds != 87 || s.Reconnects[0].Who != "队长" {
		t.Fatalf("reconnects = %+v", s.Reconnects)
	}
	var logGroup *AnomalyGroup
	for i, a := range s.Anomalies {
		if a.Kind == "Codex 日志 ERROR" {
			logGroup = &s.Anomalies[i]
		}
	}
	if logGroup == nil || logGroup.Count != 2 || logGroup.Text != "failed to refresh available models: request timed out" {
		t.Fatalf("log anomalies = %+v", s.Anomalies)
	}
	if !strings.Contains(s.Markdown(), "thread/goal/cleared") {
		t.Fatal("report lacks the unhandled message")
	}
}

func TestViewerFoldsDeltasAndServesRaw(t *testing.T) {
	idx, err := Load(writeSession(t))
	if err != nil {
		t.Fatal(err)
	}
	viewer := NewViewer(idx, idx.Summarize(nil, nil))
	server := httptest.NewServer(nil)
	server.Config.Handler = viewer.Handler(strings.TrimPrefix(server.URL, "http://"))
	defer server.Close()
	get := func(path string, out any) {
		t.Helper()
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	var page struct {
		Total int       `json:"total"`
		Rows  []listRow `json:"rows"`
	}
	get("/api/rows?groups=codex&fold=1&q=agentMessage", &page)
	if page.Total != 1 || page.Rows[0].Folded != 2 || page.Rows[0].Summary != "好的" {
		t.Fatalf("folded page = %+v", page)
	}
	get("/api/rows?groups=codex&fold=0&q=agentMessage", &page)
	if page.Total != 2 {
		t.Fatalf("unfolded total = %d", page.Total)
	}
	var raw struct {
		Raw string `json:"raw"`
	}
	get(fmt.Sprintf("/api/raw?id=%d", page.Rows[0].ID), &raw)
	if !strings.Contains(raw.Raw, `"delta":"好"`) {
		t.Fatalf("raw = %q", raw.Raw)
	}
	// Another host name is refused (DNS rebinding).
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/summary", nil)
	req.Host = "evil.example"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host status = %d", resp.StatusCode)
	}
}
