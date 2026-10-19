package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/collaboration"
)

// memberTools is the surface a member's bridge advertises; keeping it in one
// place makes a missing or extra tool a test failure rather than a drift.
var memberTools = []string{"handoff", "view_tasks", "send_message", "ask_user"}

// TestMCPBridgeProtocol drives the real stdio bridge over an in-memory MCP
// transport pair against the live HTTP collaboration route. The bridge holds
// no database handle and no delivery queue, so every answer it returns came
// back through the core.
func TestMCPBridgeProtocol(t *testing.T) {
	h := newM2Harness(t)
	_, bots := h.createTeam(t, []string{"fullstack-engineer"})
	memberID, captainID := bots[1].ID, bots[0].ID
	readToken := h.botToken(t, bots[1])
	binding, _ := h.tokens.Resolve(readToken)
	writeToken, finish := h.scriptExecution(t, binding)
	defer finish()

	t.Setenv(collaboration.BridgeEnvEndpoint, h.api.URL)
	t.Setenv(collaboration.BridgeEnvToken, writeToken)
	t.Setenv(collaboration.BridgeEnvRole, collaboration.RoleMember)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	bridge := collaboration.NewBridge(h.api.URL, "", slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	go func() { _ = bridge.Serve(context.Background(), serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// The handshake advertises exactly the member's tools, no more.
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	advertised := map[string]bool{}
	for _, tool := range tools.Tools {
		advertised[tool.Name] = true
	}
	for _, want := range memberTools {
		if !advertised[want] {
			t.Errorf("tool %q is missing from the advertised list", want)
		}
	}
	if len(advertised) != len(memberTools) {
		t.Errorf("advertised %d tools, want %d", len(advertised), len(memberTools))
	}

	// A read tool round-trips through the bridge into the core and back.
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "view_tasks",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call view_tasks: %v", err)
	}
	if res.IsError {
		t.Fatalf("view_tasks failed: %+v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("view_tasks returned no content")
	}

	// A write with neither an idempotency key nor the engine's call id is
	// refused by the core and reported as a readable failure.
	bad, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "send_message",
		Arguments: map[string]any{"targetBotId": captainID, "text": "hi"},
	})
	if err != nil {
		t.Fatalf("call send_message: %v", err)
	}
	if !bad.IsError {
		t.Fatal("send_message without idempotencyKey must be reported as an error result")
	}

	// Codex names every call; the core derives the key from that, so the
	// model never invents one. "accepted" means the fact is persisted in the
	// core, never that a turn ran.
	okRes, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "send_message",
		Meta:      mcp.Meta{"callId": "exec-bridge-1"},
		Arguments: map[string]any{"targetBotId": captainID, "text": "开始吧"},
	})
	if err != nil {
		t.Fatalf("call send_message: %v", err)
	}
	if okRes.IsError {
		t.Fatalf("send_message failed: %+v", okRes.Content)
	}

	// The message is visible to either side of the exchange, read-only: the
	// sending bot reads its own record without naming itself.
	messages, err := h.app.ListCollaboration(context.Background(), auth.NewPrincipalBot(memberID, ""), captainID, 10)
	if err != nil {
		t.Fatalf("list collaboration: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("send_message must persist a collaboration message")
	}

	// An unknown tool is refused rather than silently accepted. The SDK may
	// reject it before the bridge sees it, which is also a refusal.
	unknown, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "not_a_tool",
		Arguments: map[string]any{},
	})
	if err != nil {
		return
	}
	if !unknown.IsError {
		t.Fatal("an unknown tool must be refused")
	}
}
