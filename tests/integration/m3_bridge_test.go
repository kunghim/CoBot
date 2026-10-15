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

// collaborationTools is the advertised surface; keeping it in one place makes
// a missing or extra tool a test failure rather than a silent drift.
var collaborationTools = []string{
	"get_context", "list_tasks", "get_task", "list_roles", "send_message",
	"create_task", "propose_task", "assign_task", "update_task",
	"submit_evidence", "report_completion", "confirm_task",
	"offer_transfer", "accept_transfer", "ask_user", "request_member", "request_merge",
	"remember", "search_memory",
}

// TestMCPBridgeProtocol drives the real stdio bridge over an in-memory MCP
// transport pair against the live HTTP collaboration route. The bridge holds
// no database handle and no delivery queue, so every answer it returns came
// back through the core.
func TestMCPBridgeProtocol(t *testing.T) {
	h := newM2Harness(t)
	_, bots := h.createTeam(t, []string{"backend-dev"})
	memberID, captainID := bots[1].ID, bots[0].ID

	t.Setenv(collaboration.BridgeEnvEndpoint, h.api.URL)
	t.Setenv(collaboration.BridgeEnvToken, h.botToken(t, bots[1]))

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	bridge := collaboration.NewBridge(h.api.URL, "", slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	go func() { _ = bridge.Serve(context.Background(), serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// The handshake advertises exactly the collaboration tools, no more.
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	advertised := map[string]bool{}
	for _, tool := range tools.Tools {
		advertised[tool.Name] = true
	}
	for _, want := range collaborationTools {
		if !advertised[want] {
			t.Errorf("tool %q is missing from the advertised list", want)
		}
	}
	if len(advertised) != len(collaborationTools) {
		t.Errorf("advertised %d tools, want %d", len(advertised), len(collaborationTools))
	}

	// A read tool round-trips through the bridge into the core and back.
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_context",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call get_context: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_context failed: %+v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("get_context returned no content")
	}

	// A write tool without an idempotency key is refused by the core and
	// reported as a readable failure, not a transport error.
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

	// With the key the same call is accepted; "accepted" means the fact is
	// persisted in the core, never that a turn ran.
	okRes, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "send_message",
		Arguments: map[string]any{
			"targetBotId": captainID, "text": "开始吧", "idempotencyKey": "bridge-1",
		},
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
