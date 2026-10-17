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
	server.handleNotify("item/completed", json.RawMessage(`{"threadId":"th","turnId":"tu","item":{"type":"agentMessage","id":"m1","text":"好了"}}`))
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
