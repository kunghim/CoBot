package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/collaboration"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The same MCP process/transport survives both turns, just as real Codex
// retains it. A fresh config does not imply it adopted fresh environment.
func TestRetainedMCPBridgeBindsEveryCallToNativeTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	fake := h.ensureFake(t, team.ID)
	activate := func(text string, n int) (string, fakeengine.StartedTurn) {
		t.Helper()
		if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[1].ID, Text: text, RequestID: text}); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, 3*time.Second, "native turn", func() bool {
			if len(fake.Turns()) != n {
				return false
			}
			run, e := sqlite.FindActiveRun(ctx, h.app.DB, bots[1].ID)
			return e == nil && run.Status == domain.RunRunning
		})
		run, _ := sqlite.FindActiveRun(ctx, h.app.DB, bots[1].ID)
		key, err := h.tokens.ActivateBridgeRun(bots[1].ID, team.ID, fake.Epoch(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		return key, fake.Turns()[n-1]
	}
	key, first := activate("first-native", 1)
	t.Setenv(collaboration.BridgeEnvToken, key)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	bridge := collaboration.NewBridge(h.api.URL, "", slog.Default())
	go bridge.Serve(ctx, serverTransport)
	client := mcp.NewClient(&mcp.Implementation{Name: "retained-bridge-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	write := func(turn fakeengine.StartedTurn, text string, wantError bool) {
		t.Helper()
		native, _ := json.Marshal(map[string]any{"thread_id": turn.ThreadID, "turn_id": turn.TurnID})
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "remember", Meta: mcp.Meta{"x-codex-turn-metadata": string(native)}, Arguments: map[string]any{"body": text, "idempotencyKey": text}})
		if err != nil || result.IsError != wantError {
			t.Fatalf("native bridge result: %+v / %v", result, err)
		}
	}
	write(first, "first fact", false)
	fake.EmitTurnCompleted(first.ThreadID, first.TurnID, "completed", "first done")
	write(first, "idle write", true)
	secondKey, second := activate("second-native", 2)
	if secondKey != key {
		t.Fatal("test must retain the exact bridge credential")
	}
	write(first, "stale turn write", true)
	write(fakeengine.StartedTurn{ThreadID: "wrong-thread", TurnID: second.TurnID}, "wrong thread", true)
	write(second, "second fact", false)
	h.tokens.RevokeOfBot(bots[1].ID)
	write(second, "revoked write", true)
}
