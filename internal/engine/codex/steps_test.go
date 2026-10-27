package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kunghim/CoBot/internal/engine"
)

func TestProgressNotificationsAreNormalized(t *testing.T) {
	var got []engine.Notification
	server := &Server{pending: map[string]pendingApproval{}, callbacks: engine.Callbacks{OnNotification: func(n engine.Notification) { got = append(got, n) }}}
	server.handleNotify("item/started", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"commandExecution","id":"c1","command":"go test","cwd":"E:\\w","status":"inProgress","commandActions":[]}}`))
	server.handleNotify("item/commandExecution/outputDelta", json.RawMessage(`{"threadId":"th","turnId":"tu","itemId":"c1","delta":"ok\n"}`))
	server.handleNotify("item/completed", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"reasoning","id":"r1","summary":["看一下","再改"],"content":[]}}`))
	server.handleNotify("item/completed", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"fileChange","id":"f1","status":"completed","changes":[{"path":"a.go","kind":{"type":"update","move_path":"b.go"},"diff":"+x"}]}}`))
	server.handleNotify("item/completed", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"agentMessage","id":"m1","text":"好了","phase":"final_answer"}}`))
	server.handleNotify("error", json.RawMessage(`{"threadId":"th","turnId":"tu","willRetry":true,"error":{"message":"Reconnecting... 1/5","codexErrorInfo":{"responseStreamDisconnected":{"httpStatusCode":null}}}}`))
	server.handleNotify("warning", json.RawMessage(`{"threadId":"th","message":"slow"}`))

	if len(got) != 7 {
		t.Fatalf("notifications = %d", len(got))
	}
	if s := got[0].Step; s == nil || s.Kind != engine.StepCommand || s.Status != engine.StepInProgress || !strings.Contains(string(s.Detail), `"command":"go test"`) {
		t.Fatalf("command step = %+v", got[0].Step)
	}
	if got[1].DeltaKind != "output" || got[1].ItemID != "c1" || got[1].Delta != "ok\n" {
		t.Fatalf("output delta = %+v", got[1])
	}
	if s := got[2].Step; s == nil || s.Kind != engine.StepReasoning || s.Status != engine.StepCompleted || !strings.Contains(string(s.Detail), `看一下\n\n再改`) {
		t.Fatalf("reasoning step = %+v", got[2].Step)
	}
	if s := got[3].Step; s == nil || s.Kind != engine.StepFileChange || !strings.Contains(string(s.Detail), `"movePath":"b.go"`) || !strings.Contains(string(s.Detail), `"kind":"update"`) {
		t.Fatalf("file step = %s", got[3].Step.Detail)
	}
	if got[4].Step != nil || got[4].ItemType != "agentMessage" || got[4].Text != "好了" {
		t.Fatalf("a reply must stay a reply: %+v", got[4])
	}
	if p := got[5].Problem; p == nil || !p.WillRetry || p.Code != "responseStreamDisconnected" {
		t.Fatalf("retry problem = %+v", got[5].Problem)
	}
	if p := got[6].Problem; p == nil || p.Level != "warning" || p.Message != "slow" {
		t.Fatalf("warning = %+v", got[6].Problem)
	}
}

func TestStepOutputIsClippedOnRuneBoundaries(t *testing.T) {
	long := strings.Repeat("测", maxStepOutput)
	tail := clipTail(long, maxStepOutput)
	if len(tail) > maxStepOutput+8 || !strings.HasPrefix(tail, "…\n") || !strings.HasSuffix(tail, "测") {
		t.Fatalf("tail clip len=%d", len(tail))
	}
	for _, r := range strings.TrimPrefix(tail, "…\n") {
		if r == '\uFFFD' {
			t.Fatal("clipped inside a rune")
		}
	}
	head := clipHead(long, 10)
	if head != "测测测\n…" {
		t.Fatalf("head clip = %q", head)
	}
}

// What the model says while working is a step with its own stream; only the
// final answer streams as the reply.
func TestCommentaryIsAStepAndOnlyTheAnswerIsTheReply(t *testing.T) {
	var got []engine.Notification
	server := &Server{pending: map[string]pendingApproval{}, callbacks: engine.Callbacks{OnNotification: func(n engine.Notification) { got = append(got, n) }}}
	server.handleNotify("item/started", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"agentMessage","id":"c1","text":"","phase":"commentary"}}`))
	server.handleNotify("item/agentMessage/delta", json.RawMessage(`{"threadId":"th","turnId":"tu","itemId":"c1","delta":"我先看看"}`))
	server.handleNotify("item/completed", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"agentMessage","id":"c1","text":"我先看看目录","phase":"commentary"}}`))
	server.handleNotify("item/started", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"agentMessage","id":"a1","text":"","phase":"final_answer"}}`))
	server.handleNotify("item/agentMessage/delta", json.RawMessage(`{"threadId":"th","turnId":"tu","itemId":"a1","delta":"做好了"}`))
	server.handleNotify("item/agentMessage/delta", json.RawMessage(`{"threadId":"th","turnId":"tu","itemId":"x9","delta":"旧引擎"}`))

	if s := got[0].Step; s == nil || s.Kind != engine.StepMessage || s.Status != engine.StepInProgress {
		t.Fatalf("commentary start = %+v", got[0])
	}
	if got[1].DeltaKind != "commentary" || got[1].ItemID != "c1" {
		t.Fatalf("commentary delta = %+v", got[1])
	}
	if s := got[2].Step; s == nil || s.Kind != engine.StepMessage || s.Status != engine.StepCompleted || !strings.Contains(string(s.Detail), "我先看看目录") || got[2].Phase != "commentary" {
		t.Fatalf("commentary done = %+v", got[2])
	}
	if got[3].Step != nil || got[3].Phase != "final_answer" {
		t.Fatalf("answer start must not be a step: %+v", got[3])
	}
	if got[4].DeltaKind != "message" {
		t.Fatalf("answer delta = %+v", got[4])
	}
	// An item the turn never announced streams where it sits, with a step to
	// stream into; it is not part of the reply.
	if got[5].DeltaKind != "commentary" {
		t.Fatalf("unannounced-item delta = %+v", got[5])
	}
	if s := got[5].Step; s == nil || s.Kind != engine.StepMessage || s.ID != "x9" || s.Status != engine.StepInProgress {
		t.Fatalf("unannounced item got no step: %+v", got[5])
	}
}

// A sub-agent's thread is traced back to the member thread that spawned it,
// from the thread itself and from the member's sub-agent items, which show
// as agent steps.
func TestSubAgentThreadsLeadBackToTheMember(t *testing.T) {
	var got []engine.Notification
	server := &Server{pending: map[string]pendingApproval{}, callbacks: engine.Callbacks{OnNotification: func(n engine.Notification) { got = append(got, n) }}}
	server.handleNotify("item/started", json.RawMessage(`{"threadId":"member","turnId":"t1","item":{"type":"collabAgentToolCall","id":"c1","tool":"spawnAgent","senderThreadId":"member","receiverThreadIds":["child"],"prompt":"查一下测试","agentsStates":{},"status":"inProgress"}}`))
	server.handleNotify("thread/started", json.RawMessage(`{"thread":{"id":"grandchild","parentThreadId":"child"}}`))
	server.handleNotify("item/completed", json.RawMessage(`{"threadId":"member","turnId":"t1","item":{"type":"subAgentActivity","id":"a1","kind":"completed","agentPath":"/root/tester","agentThreadId":"other"}}`))
	// A sub-agent messaging its parent names the parent; that is no new link.
	server.handleNotify("item/started", json.RawMessage(`{"threadId":"child","turnId":"t2","item":{"type":"subAgentActivity","id":"a2","kind":"interacted","agentPath":"/root","agentThreadId":"member"}}`))

	for thread, want := range map[string]string{"child": "member", "grandchild": "member", "other": "member", "member": "member", "stranger": "stranger"} {
		if root := server.RootThread(thread); root != want {
			t.Fatalf("root of %s = %s, want %s", thread, root, want)
		}
	}
	if s := got[0].Step; s == nil || s.Kind != engine.StepAgent || !strings.Contains(string(s.Detail), `"tool":"spawnAgent"`) || !strings.Contains(string(s.Detail), "查一下测试") {
		t.Fatalf("spawn step = %+v", got[0].Step)
	}
	if s := got[2].Step; s == nil || s.Kind != engine.StepAgent || !strings.Contains(string(s.Detail), `"activity":"completed"`) {
		t.Fatalf("activity step = %+v", got[2].Step)
	}
}

// The turn's diff is one step holding the newest diff; a settled server
// request names its id.
func TestTurnDiffAndSettledRequests(t *testing.T) {
	var got []engine.Notification
	server := &Server{pending: map[string]pendingApproval{}, callbacks: engine.Callbacks{OnNotification: func(n engine.Notification) { got = append(got, n) }}}
	server.handleNotify("turn/diff/updated", json.RawMessage(`{"threadId":"th","turnId":"tu","diff":"diff --git a/x b/x\n+1\ndiff --git a/y b/y\n+2\n"}`))
	server.handleNotify("serverRequest/resolved", json.RawMessage(`{"threadId":"th","requestId":7}`))
	if s := got[0].Step; s == nil || s.ID != "turn-diff" || s.Kind != engine.StepDiff || !strings.Contains(string(s.Detail), `"files":2`) {
		t.Fatalf("diff step = %+v", got[0].Step)
	}
	if string(got[1].RequestID) != "7" || got[1].ThreadID != "th" {
		t.Fatalf("settled request = %+v", got[1])
	}
}

// The engine folding the conversation into a summary shows in the flow, with
// its start and its end.
func TestContextCompactionIsAStep(t *testing.T) {
	var got []engine.Notification
	server := &Server{pending: map[string]pendingApproval{}, callbacks: engine.Callbacks{OnNotification: func(n engine.Notification) { got = append(got, n) }}}
	server.handleNotify("item/started", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"contextCompaction","id":"cc1"}}`))
	server.handleNotify("item/completed", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"contextCompaction","id":"cc1"}}`))
	if len(got) != 2 {
		t.Fatalf("notifications = %+v", got)
	}
	if s := got[0].Step; s == nil || s.Kind != engine.StepCompaction || s.Status != engine.StepInProgress {
		t.Fatalf("start = %+v", got[0])
	}
	if s := got[1].Step; s == nil || s.Kind != engine.StepCompaction || s.Status != engine.StepCompleted {
		t.Fatalf("done = %+v", got[1])
	}
}

// A model that marks no phase (a third-party one) still says things on the
// way: each message is a step in the place it was said, and streams there.
// Which of them is the reply is for the end of the turn to say.
func TestAMessageWithNoPhaseIsAStepAndStreamsAsOne(t *testing.T) {
	var got []engine.Notification
	server := &Server{pending: map[string]pendingApproval{}, callbacks: engine.Callbacks{OnNotification: func(n engine.Notification) { got = append(got, n) }}}
	server.handleNotify("item/started", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"agentMessage","id":"p1","text":""}}`))
	server.handleNotify("item/agentMessage/delta", json.RawMessage(`{"threadId":"th","turnId":"tu","itemId":"p1","delta":"我先看看"}`))
	server.handleNotify("item/completed", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"agentMessage","id":"p1","text":"我先看看目录"}}`))
	if len(got) != 3 {
		t.Fatalf("notifications = %d", len(got))
	}
	if s := got[0].Step; s == nil || s.Kind != engine.StepMessage || s.Status != engine.StepInProgress {
		t.Fatalf("start = %+v", got[0])
	}
	if got[1].DeltaKind != "commentary" || got[1].ItemID != "p1" {
		t.Fatalf("delta = %+v", got[1])
	}
	if s := got[2].Step; s == nil || s.Kind != engine.StepMessage || s.Status != engine.StepCompleted || !strings.Contains(string(s.Detail), "我先看看目录") {
		t.Fatalf("done = %+v", got[2])
	}
}

func TestDiffStatsAreSavedBeforeClipping(t *testing.T) {
	body := strings.Repeat("+line\n", 60000)
	diff := "diff --git a/big.txt b/big.txt\n--- a/big.txt\n+++ b/big.txt\n@@ -0,0 +1,60000 @@\n" + body
	detail := turnDiffDetail(diff)
	if detail["statsComplete"] != true || detail["diffTruncated"] != true || len(detail["diff"].(string)) > maxTurnDiff+len("\n…") {
		t.Fatalf("unexpected completeness: stats=%v truncated=%v", detail["statsComplete"], detail["diffTruncated"])
	}
	encoded, _ := json.Marshal(detail)
	var saved struct {
		Changes []struct {
			Additions     int  `json:"additions"`
			DiffTruncated bool `json:"diffTruncated"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(encoded, &saved); err != nil || len(saved.Changes) != 1 || saved.Changes[0].Additions != 60000 || !saved.Changes[0].DiffTruncated {
		t.Fatalf("saved metadata: %+v err=%v", saved, err)
	}
	raw, _ := json.Marshal(map[string]any{"id": "large", "type": "fileChange", "status": "completed", "changes": []any{map[string]any{"path": "big.txt", "kind": map[string]any{"type": "add"}, "diff": diff}}})
	step := normalizeStep(raw)
	var fileChange struct {
		Changes []struct {
			Additions     int    `json:"additions"`
			StatsComplete bool   `json:"statsComplete"`
			DiffTruncated bool   `json:"diffTruncated"`
			Diff          string `json:"diff"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(step.Detail, &fileChange); err != nil || len(fileChange.Changes) != 1 {
		t.Fatalf("file change detail: %s err=%v", step.Detail, err)
	}
	change := fileChange.Changes[0]
	if change.Additions != 60000 || !change.StatsComplete || !change.DiffTruncated || len(change.Diff) > maxStepDiff+len("\n…") {
		t.Fatalf("file change stats lost before clipping: additions=%d complete=%v truncated=%v", change.Additions, change.StatsComplete, change.DiffTruncated)
	}
}
