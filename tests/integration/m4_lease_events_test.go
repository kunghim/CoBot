package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/collaboration"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/eventsink"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// TestSchedulerKeepsLeasesFresh pins H06's writer: while the core runs, the
// scheduler's ticker keeps every live member's lease fresh for this
// instance, so external helpers can trust it.
func TestSchedulerKeepsLeasesFresh(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"backend-dev"})
	member := bots[1]
	_ = team
	h.ensureFake(t, team.ID)

	waitUntil(t, 5*time.Second, "member lease", func() bool {
		lease, reason, err := platform.ReadLease(h.app.Paths.Root, member.ID, h.app.InstanceID, time.Now())
		return err == nil && reason == platform.LeaseReasonOK && lease.BotID == member.ID
	})
	if reason := h.app.CheckLease(member.ID); reason != platform.LeaseReasonOK {
		t.Fatalf("CheckLease = %q, want ok while the core runs", reason)
	}
}

// TestBridgeSideEffectGateUsesLocalLease pins H06's reader: the spawned
// bridge refuses side effects on a missing or stale lease, and read tools
// keep working so the model can still inspect state.
func TestBridgeSideEffectGateUsesLocalLease(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"backend-dev"})
	member := bots[1]

	leaseRoot := t.TempDir()
	readToken := h.botToken(t, member)
	binding, _ := h.tokens.Resolve(readToken)
	writeToken, finish := h.scriptExecution(t, binding)
	defer finish()
	t.Setenv(collaboration.BridgeEnvEndpoint, h.api.URL)
	t.Setenv(collaboration.BridgeEnvToken, writeToken)
	t.Setenv(collaboration.BridgeEnvBotID, member.ID)
	t.Setenv(collaboration.BridgeEnvLeaseDir, platform.LeaseDir(leaseRoot))

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	bridge := collaboration.NewBridge(h.api.URL, "", slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	go func() { _ = bridge.Serve(context.Background(), serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "lease-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	call := func(tool string, args map[string]any) (*mcp.CallToolResult, error) {
		return session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	}

	// Missing lease: the side effect is refused; the reason is honest.
	res, err := call("remember", map[string]any{"body": "提交前先 gofmt", "idempotencyKey": "key-1"})
	if err != nil {
		t.Fatalf("remember call: %v", err)
	}
	if !res.IsError || !strings.Contains(mustText(t, res), "error.lease.missing") {
		t.Fatalf("missing lease text = %q, want an IsError naming error.lease.missing", mustText(t, res))
	}

	// Fresh lease: the same side effect goes through to the core.
	if err := platform.WriteLease(leaseRoot, platform.Lease{
		BotID: member.ID, TeamID: team.ID, InstanceID: h.app.InstanceID, UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	res, err = call("remember", map[string]any{"body": "提交前先 gofmt", "idempotencyKey": "key-1"})
	if err != nil {
		t.Fatalf("remember with lease: %v", err)
	}
	if res.IsError {
		t.Fatalf("fresh lease remember = %+v, want success", res)
	}

	// Stale lease: side effects are refused again...
	stale := platform.Lease{BotID: member.ID, TeamID: team.ID, InstanceID: h.app.InstanceID, UpdatedAt: time.Now().Add(-platform.LeaseTTL - time.Second)}
	if err := platform.WriteLease(leaseRoot, stale); err != nil {
		t.Fatal(err)
	}
	res, err = call("remember", map[string]any{"body": "又一条", "idempotencyKey": "key-2"})
	if err != nil {
		t.Fatalf("remember stale: %v", err)
	}
	if !res.IsError || !strings.Contains(mustText(t, res), "error.lease.expired") {
		t.Fatalf("stale lease text = %q, want IsError error.lease.expired", mustText(t, res))
	}

	// ...but read tools still work, so the model can inspect state and
	// report honestly instead of being blind.
	res, err = call("get_context", map[string]any{})
	if err != nil {
		t.Fatalf("get_context: %v", err)
	}
	if res.IsError {
		t.Fatalf("read tool under stale lease = %+v, want success", res)
	}
}

func mustText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var out strings.Builder
	for _, block := range res.Content {
		if text, ok := block.(*mcp.TextContent); ok {
			out.WriteString(text.Text)
		}
	}
	return out.String()
}

// TestEventLedgerAppendImportAndGap pins H08: live events mirror to disk,
// import is idempotent by event id, corrupt files are quarantined as
// evidence after their valid lines are imported, an over-capacity ledger
// records one visible gap instead of deleting un-imported evidence, and
// retention never touches a live file.
func TestEventLedgerAppendImportAndGap(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	member := bots[1]
	fake := h.ensureFake(t, team.ID)

	sink, err := eventsink.Open(h.app.Paths.Root, h.app.InstanceID)
	if err != nil {
		t.Fatalf("open sink: %v", err)
	}
	h.app.Events = sink

	// A real product action records a real event, mirrored to disk. The turn
	// is completed before importing so the dispatch's own events settle
	// instead of racing the drain.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: member.ID, Text: "触发事件"}); err != nil {
		t.Fatalf("msg: %v", err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "completed", "好的")
	waitUntil(t, 5*time.Second, "run ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, member.ID)
		return err != nil
	})
	live, err := sink.LiveFiles()
	if err != nil || len(live) != 1 {
		t.Fatalf("live files = %d (err %v), want the instance file", len(live), err)
	}
	lines, corrupt, err := eventsink.ReadLines(live[0])
	if err != nil || corrupt != 0 || len(lines) == 0 {
		t.Fatalf("live lines = %d corrupt %d err %v", len(lines), corrupt, err)
	}

	// The mirror holds events the database already has: importing drains the
	// file and inserts nothing new — the same event arriving twice is one
	// row (H08's idempotency). A foreign event later proves new rows land.
	var before int
	if err := h.app.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&before); err != nil {
		t.Fatalf("count: %v", err)
	}
	if _, err := h.app.ImportEventFiles(ctx); err != nil {
		t.Fatalf("import: %v", err)
	}
	after, err := sink.LiveFiles()
	if err != nil || len(after) != 0 {
		t.Fatalf("live files after import = %d, want drained", len(after))
	}
	var stable int
	if err := h.app.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&stable); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if stable != before {
		t.Fatalf("events grew by %d on re-import, want 0 (idempotent by id)", stable-before)
	}
	// A corrupt file: valid lines import, the file is quarantined as
	// evidence rather than deleted.
	valid, _ := json.Marshal(domain.Event{ID: "ev-imported-1", InstanceID: "foreign", Type: "message.created"})
	corruptFile := filepath.Join(filepath.Dir(live[0]), "live-"+h.app.InstanceID+".jsonl")
	if err := os.WriteFile(corruptFile, append(valid, []byte("\n{broken json\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	imported, err := h.app.ImportEventFiles(ctx)
	if err != nil || imported != 1 {
		t.Fatalf("corrupt-file import = %d (err %v), want the one valid line", imported, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(corruptFile))
	quarantined := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "quarantine-") {
			quarantined++
		}
	}
	if quarantined != 1 {
		t.Fatalf("quarantined files = %d, want the corrupt file kept as evidence", quarantined)
	}

	// Over capacity: appending refuses and the gap is recorded once in the
	// database; nothing on disk is deleted to make room.
	small, err := eventsink.OpenWithLimit(t.TempDir(), "gap-instance", 40)
	if err != nil {
		t.Fatal(err)
	}
	gapApp := &app.App{DB: h.app.DB, Paths: h.app.Paths, Hub: h.app.Hub, InstanceID: "gap-instance", Logger: h.app.Logger, Events: small}
	// The first line fits the budget; the next one is refused and the gap is
	// recorded once.
	if err := gapApp.RecordEventForTest(ctx, domain.Event{ID: "ev-small", Type: "message.created"}); err != nil {
		t.Fatalf("record small: %v", err)
	}
	big := domain.Event{ID: "ev-big", Type: "message.created", Payload: json.RawMessage(`{"pad":"` + strings.Repeat("x", 200) + `"}`)}
	if err := gapApp.RecordEventForTest(ctx, big); err != nil {
		t.Fatalf("record over capacity: %v", err)
	}
	var gaps int
	if err := h.app.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE type = ?`, domain.EventLogGap).Scan(&gaps); err != nil {
		t.Fatalf("count gaps: %v", err)
	}
	if gaps != 1 {
		t.Fatalf("log.gap events = %d, want exactly one", gaps)
	}

	// Retention prunes old imported files only; live evidence stays.
	oldSink := sink
	oldPath := filepath.Join(filepath.Dir(live[0]), "imported-old.jsonl")
	if err := os.WriteFile(oldPath, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().AddDate(0, 0, -(eventsink.RetainDays + 1))
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(live[0]), "live-keep.jsonl"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pruned, over, err := oldSink.PruneImported(time.Now())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 || over {
		t.Fatalf("prune = %d over=%v, want the old imported file gone and capacity fine", pruned, over)
	}
	if _, err := os.Stat(oldPath); err == nil {
		t.Fatal("old imported file must be pruned")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(live[0]), "live-keep.jsonl")); err != nil {
		t.Fatal("retention must never delete a live (un-imported) file")
	}
}
