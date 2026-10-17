package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/events"
)

// A running turn's steps, streamed text and reconnects reach the live
// stream, the steps are stored with the run, and a finished turn leaves no
// step running.
func TestTurnProgressIsRelayedAndStored(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	live, unsubscribe := h.app.Live.Subscribe()
	defer unsubscribe()

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "跑一下测试", RequestID: "req-progress"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn started", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	emit := func(n engine.Notification) {
		n.ThreadID, n.TurnID = turn.ThreadID, turn.TurnID
		fake.Emit(n)
	}

	emit(engine.Notification{Method: "error", Problem: &engine.Problem{Level: "error", Message: "Reconnecting... 1/5", WillRetry: true}})
	// The engine's own count wins over ours: a first notice may arrive late.
	emit(engine.Notification{Method: "error", Problem: &engine.Problem{Level: "error", Message: "Reconnecting... 4/5", WillRetry: true}})
	emit(engine.Notification{Method: "item/started", Step: &engine.Step{ID: "cmd-1", Kind: engine.StepCommand, Status: engine.StepInProgress, Detail: json.RawMessage(`{"command":"go test ./...","cwd":"."}`)}})
	emit(engine.Notification{Method: "item/commandExecution/outputDelta", ItemID: "cmd-1", DeltaKind: "output", Delta: "ok  pkg\n"})
	emit(engine.Notification{Method: "item/completed", Step: &engine.Step{ID: "cmd-1", Kind: engine.StepCommand, Status: engine.StepCompleted, Detail: json.RawMessage(`{"command":"go test ./...","exitCode":0,"output":"ok  pkg\n"}`)}})
	emit(engine.Notification{Method: "item/started", Step: &engine.Step{ID: "think-1", Kind: engine.StepReasoning, Status: engine.StepInProgress, Detail: json.RawMessage(`{"text":""}`)}})
	emit(engine.Notification{Method: "item/agentMessage/delta", DeltaKind: "message", Delta: "测试"})

	var steps, deltas int
	var sawRetry bool
	deadline := time.After(5 * time.Second)
	for steps < 4 || deltas < 2 {
		select {
		case signal := <-live:
			if signal.BotID != bot.ID || signal.TeamID != team.ID || signal.RunID == "" {
				t.Fatalf("signal not attributed to the run: %+v", signal)
			}
			switch signal.Type {
			case "step":
				steps++
				if step, ok := signal.Step.(domain.RunStep); ok && step.Kind == domain.StepKindRetry {
					sawRetry = true
				}
			case "delta":
				deltas++
			}
		case <-deadline:
			t.Fatalf("live signals: %d steps, %d deltas", steps, deltas)
		}
	}
	if !sawRetry {
		t.Fatal("reconnects were not relayed")
	}

	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "测试通过")
	var page map[string]any
	waitUntil(t, 5*time.Second, "run settled", func() bool {
		var err error
		page, err = h.app.ListPage(ctx, h.user, app.PageRequest{Kind: "messages", BotID: bot.ID})
		return err == nil && page["activeRun"] == nil
	})
	runs := page["runs"].([]domain.RunWithSteps)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	byID := map[string]domain.RunStep{}
	for _, step := range runs[0].Steps {
		byID[step.ID] = step
		if step.Status == engine.StepInProgress {
			t.Fatalf("step %s still running after its turn ended", step.ID)
		}
	}
	retry, ok := byID["cobot-retry"]
	if !ok {
		t.Fatal("retry step not stored")
	}
	var retryDetail struct {
		Attempt int `json:"attempt"`
	}
	if err := json.Unmarshal(retry.Detail, &retryDetail); err != nil || retryDetail.Attempt != 4 || retry.Status != engine.StepCompleted {
		t.Fatalf("retry detail = %s", retry.Detail)
	}
	command := byID["cmd-1"]
	if command.Status != engine.StepCompleted || command.Seq <= retry.Seq {
		t.Fatalf("command step = %+v (retry seq %d)", command, retry.Seq)
	}
	if byID["think-1"].Status != engine.StepCompleted {
		t.Fatalf("open step was not settled: %+v", byID["think-1"])
	}
}

// The stream handler's live hub drops signals for a slow reader instead of
// blocking the engine.
func TestLiveHubNeverBlocks(t *testing.T) {
	hub := events.NewLiveHub()
	_, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			hub.Publish(events.Live{Type: "delta", Text: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publishing blocked on a slow subscriber")
	}
}
