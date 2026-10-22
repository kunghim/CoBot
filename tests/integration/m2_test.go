package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	apihttp "github.com/kunghim/CoBot/internal/api/http"
	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/collaboration"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/secrets"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

type m2Harness struct {
	t       *testing.T
	app     *app.App
	sched   *app.Scheduler
	fakes   map[string]*fakeengine.Server // teamID -> fake
	user    auth.Principal
	teamDir string

	// api serves the real HTTP surface over the same app so the MCP route and
	// the user routes can be exercised together.
	api    *httptest.Server
	tokens *auth.TokenStore
	bearer string

	fakeSeq int
	epochs  map[string]string
	fakeMu  sync.Mutex
	onFake  func(*fakeengine.Server)
}

// ensureFake waits for the scheduler to build the team's engine. The factory
// is the only place a fake is constructed, so a bridge credential is always
// minted against the instance dispatch will actually report — never a twin
// that would carry a different epoch.
func (h *m2Harness) ensureFake(t *testing.T, teamID string) *fakeengine.Server {
	t.Helper()
	h.fakeMu.Lock()
	existing := h.fakes[teamID]
	h.fakeMu.Unlock()
	if existing != nil {
		return existing
	}
	// Bring the team's engine up now: a bridge credential must be minted
	// against the server the scheduler really holds, and nothing else may
	// build a second one for this team.
	if _, err := h.app.Scheduler.EnsureTeamServer(context.Background(), teamID, h.teamDir); err != nil {
		t.Fatalf("ensure team server: %v", err)
	}
	return h.fakeFor(t, teamID)
}

// epochFor returns the engine epoch for the server the scheduler is about to
// build. A restart drops the cached epoch first, so the replacement always
// gets a new one — the core must treat a different epoch as a different
// connection and refuse its stale credentials.
func (h *m2Harness) epochFor(teamID string) string {
	h.fakeMu.Lock()
	defer h.fakeMu.Unlock()
	if e, ok := h.epochs[teamID]; ok {
		return e
	}
	h.fakeSeq++
	e := fmt.Sprintf("epoch-fake-%d", h.fakeSeq)
	h.epochs[teamID] = e
	return e
}

// resolveUnknownRun closes an unknown run as interrupted, which is what
// recovery reports when the engine cannot be resumed.
func (h *m2Harness) resolveUnknownRun(t *testing.T, botID string) {
	t.Helper()
	ctx := context.Background()
	run, err := sqlite.FindActiveRun(ctx, h.app.DB, botID)
	if errors.Is(err, sqlite.ErrNotFound) {
		// Recovery is idempotent: the run may already be resolved.
		return
	}
	if err != nil {
		t.Fatalf("active run: %v", err)
	}
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunInterrupted, "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SetBotRunState(ctx, tx, botID, domain.BotStatusIdle, ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// restartTeamServer simulates the team's app-server dying and a new one
// starting, which is the only way a team reaches a second epoch: the old
// server is gone, and every credential it minted must stop working.
func (h *m2Harness) restartTeamServer(t *testing.T, teamID string) {
	t.Helper()
	h.fakeMu.Lock()
	delete(h.epochs, teamID)
	delete(h.fakes, teamID)
	h.fakeMu.Unlock()
}

// botToken mints a bridge credential bound to the engine epoch the fake for
// that team reports, which is what the core hands the spawned bridge.
func (h *m2Harness) botToken(t *testing.T, bot domain.Bot) string {
	t.Helper()
	fake := h.ensureFake(t, bot.TeamID)
	token, err := h.tokens.Issue(bot.ID, bot.TeamID, fake.Epoch())
	if err != nil {
		t.Fatalf("issue bot token: %v", err)
	}
	return token
}

// callTool drives one MCP tool through the real HTTP route, exactly as the
// bridge does.
func (h *m2Harness) callTool(t *testing.T, token, tool string, args map[string]any) (int, map[string]any) {
	t.Helper()
	// These tests script a model's product calls rather than a full model
	// turn. Supply an explicit, bounded fake host execution for write scripts.
	// Raw/obsolete credentials are tested through callToolRaw, never repaired.
	if collaboration.IsSideEffectTool(tool) {
		if binding, err := h.tokens.Resolve(token); err == nil && binding.RunID == "" && binding.EngineEpoch == h.app.EngineEpochOfTeam(binding.TeamID) {
			var cleanup func()
			token, cleanup = h.scriptExecution(t, binding)
			defer cleanup()
		}
	}
	return h.callToolRaw(t, token, tool, args)
}

func (h *m2Harness) scriptExecution(t *testing.T, binding auth.BotBinding) (string, func()) {
	t.Helper()
	ctx := context.Background()
	run, err := sqlite.FindActiveRun(ctx, h.app.DB, binding.BotID)
	created := false
	if errors.Is(err, sqlite.ErrNotFound) {
		run = domain.Run{ID: domain.NewID(), TeamID: binding.TeamID, BotID: binding.BotID, EngineEpoch: binding.EngineEpoch, Status: domain.RunRunning, StartedAt: time.Now().UTC()}
		tx, e := h.app.DB.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		if e = sqlite.InsertRun(ctx, tx, run); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		created = true
	} else if err != nil {
		t.Fatal(err)
	}
	token, err := h.tokens.IssueForRun(binding.BotID, binding.TeamID, binding.EngineEpoch, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return token, func() {
		if !created {
			return
		}
		tx, e := h.app.DB.BeginTx(ctx, nil)
		if e != nil {
			t.Error(e)
			return
		}
		if e = sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunCompleted, "", binding.EngineEpoch, "", true); e != nil {
			tx.Rollback()
			t.Error(e)
			return
		}
		if e = tx.Commit(); e != nil {
			t.Error(e)
		}
		h.tokens.RevokeRun(run.ID)
		h.app.Scheduler.Wake(binding.BotID)
	}
}

// callToolRaw sends the exact credential with no simulated execution.
func (h *m2Harness) callToolRaw(t *testing.T, token, tool string, args map[string]any) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"requestId": "req-" + tool, "tool": tool, "arguments": args})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, h.api.URL+"/internal/v1/collaboration", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bot-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call tool %s: %v", tool, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode tool response: %v", err)
	}
	return resp.StatusCode, out
}

// userGet drives a user route with the bearer token issued at startup.
func (h *m2Harness) userGet(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.api.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode GET %s: %v", path, err)
	}
	return resp.StatusCode, out
}

func newM2Harness(t *testing.T) *m2Harness {
	t.Helper()
	root := t.TempDir()
	paths, err := platform.Resolve(platform.EnvDevelopment, filepath.Join(root, "profile"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(paths.DatabaseFile())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	migrator, err := sqlite.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.MigrateToLatest(context.Background(), paths.BackupsDir()); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	a := &app.App{
		DB: db, Paths: paths, Hub: events.NewHub(), Live: events.NewLiveHub(), InstanceID: "test-instance",
		ServiceEpoch: "epoch-0", Version: "test", Logger: logger,
	}
	// The credential store holds the model providers' keys, and the binder
	// stands in for the engine that takes a provider.
	store, err := secrets.New(paths.CredentialsDir())
	if err != nil {
		t.Fatal(err)
	}
	a.Credentials = store
	a.ProviderBinderFactory = func(context.Context) (engine.ProviderBinder, error) { return newProviderBinder(), nil }
	tokens := auth.NewTokenStore(time.Hour)
	a.CollaborationTokens = tokens
	bearer := auth.NewBearerStore(time.Hour)
	tok, err := bearer.Issue(auth.LocalUserID)
	if err != nil {
		t.Fatal(err)
	}
	h := &m2Harness{
		t: t, app: a, tokens: tokens, bearer: tok.Token,
		fakes: map[string]*fakeengine.Server{}, epochs: map[string]string{}, user: auth.NewPrincipalUser("sess-1"),
	}
	apiServer := &apihttp.Server{
		App: a, Paths: paths, Sessions: auth.NewSessionStore(time.Hour), Bearer: bearer,
		Issuer: auth.NewBootstrapIssuer(time.Minute), Hub: a.Hub, Logger: logger,
		CollaborationTokens: tokens,
		Dispatcher:          collaboration.NewDispatcher(a, tokens),
		EngineEpochOfTeam:   a.EngineEpochOfTeam,
	}
	h.api = httptest.NewServer(apiServer.Handler())
	t.Cleanup(h.api.Close)
	sched := app.NewScheduler(a, func(spec app.ServerSpec) (engine.Server, error) {
		fake := fakeengine.New(spec.Callbacks, h.epochFor(spec.TeamID))
		if h.onFake != nil {
			h.onFake(fake)
		}
		h.fakeMu.Lock()
		h.fakes[spec.TeamID] = fake
		h.fakeMu.Unlock()
		return fake, nil
	})
	a.Scheduler = sched
	sched.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sched.Stop(ctx)
	})

	teamDir := filepath.Join(root, "team-workdir")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.teamDir = teamDir
	return h
}

func (h *m2Harness) createTeam(t *testing.T, roles []string) (domain.Team, []domain.Bot) {
	t.Helper()
	return h.createTeamWithDir(t, "测试团队", filepath.Base(h.teamDir), roles)
}

// createTeamWithDir creates a team in its own working directory; one profile
// binds each directory to exactly one team, so a second team needs its own.
func (h *m2Harness) createTeamWithDir(t *testing.T, name, dirName string, roles []string) (domain.Team, []domain.Bot) {
	t.Helper()
	dir := filepath.Join(filepath.Dir(h.teamDir), dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	team, bots, err := h.app.CreateTeam(context.Background(), h.user, app.CreateTeamInput{
		Name: name, Directory: dir, InitialRoleIDs: roles,
	})
	if err != nil {
		t.Fatalf("create team %s: %v", name, err)
	}
	return team, bots
}

// queuedForBot counts the queued deliveries a bot has yet to run.
func (h *m2Harness) queuedForBot(t *testing.T, botID string) int {
	t.Helper()
	n, err := sqlite.CountQueuedDeliveries(context.Background(), h.app.DB, botID)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// deliveryStatusOf reads the status of a task's assignment delivery.
func (h *m2Harness) deliveryStatusOf(t *testing.T, taskID string) string {
	t.Helper()
	var status string
	err := h.app.DB.QueryRow(`SELECT d.status FROM deliveries d
		JOIN messages m ON m.id = d.message_id WHERE m.run_id = ?`, taskID).Scan(&status)
	if err != nil {
		return ""
	}
	return status
}

// runStatusID reports the id of a bot's most recent run.
func (h *m2Harness) runStatusID(t *testing.T, botID string) string {
	t.Helper()
	var id string
	err := h.app.DB.QueryRow(`SELECT id FROM runs WHERE bot_id = ? ORDER BY started_at DESC LIMIT 1`, botID).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

// runStatus reports the status of a bot's most recent run, or "none".
func (h *m2Harness) runStatus(t *testing.T, botID string) string {
	t.Helper()
	var status string
	err := h.app.DB.QueryRow(`SELECT status FROM runs WHERE bot_id = ? ORDER BY started_at DESC LIMIT 1`, botID).Scan(&status)
	if err != nil {
		return "none"
	}
	return status
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *m2Harness) fakeFor(t *testing.T, teamID string) *fakeengine.Server {
	t.Helper()
	var fake *fakeengine.Server
	waitUntil(t, 5*time.Second, "team fake server", func() bool {
		h.fakeMu.Lock()
		defer h.fakeMu.Unlock()
		fake = h.fakes[teamID]
		return fake != nil
	})
	return fake
}

// fakeGeneration waits for a server the scheduler built after the previous
// one was dropped, which is what a restart produces.
func (h *m2Harness) fakeGeneration(t *testing.T, teamID string, after *fakeengine.Server) *fakeengine.Server {
	t.Helper()
	var fake *fakeengine.Server
	waitUntil(t, 5*time.Second, "next engine generation", func() bool {
		h.fakeMu.Lock()
		defer h.fakeMu.Unlock()
		fake = h.fakes[teamID]
		return fake != nil && fake != after
	})
	return fake
}

func TestTeamCreationValidation(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	// Same directory twice is refused even after status changes.
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	if team.Status != domain.TeamStatusActive {
		t.Fatalf("team status = %s, want active", team.Status)
	}
	if len(bots) != 2 || !bots[0].IsLeader {
		t.Fatalf("expected leader + fullstack-engineer, got %+v", bots)
	}
	// The marker and the Git-exclude path behaviour: plain dir has no Git,
	// but the marker must exist.
	if _, err := os.Stat(filepath.Join(h.teamDir, ".cobot", "team.json")); err != nil {
		t.Fatalf("team marker missing: %v", err)
	}
	_, _, err := h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "重复", Directory: h.teamDir})
	if err == nil {
		t.Fatal("duplicate directory must be refused")
	}

	// Nesting is refused in both directions.
	nested := filepath.Join(h.teamDir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "嵌套", Directory: nested})
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("nested directory must conflict, got %v", err)
	}

	// Reserved profile directories are refused.
	_, _, err = h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "保留", Directory: h.app.Paths.Root})
	if err == nil {
		t.Fatal("reserved directory must be refused")
	}

	// Member limit includes the leader.
	limit, _ := h.app.GetSettings(ctx)
	members := make([]string, 0, limit.MemberLimit)
	for i := 1; i < limit.MemberLimit+2; i++ {
		members = append(members, "fullstack-engineer")
	}
	dir2 := filepath.Join(filepath.Dir(h.teamDir), "team-2")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "超限", Directory: dir2, InitialRoleIDs: members})
	if err == nil {
		t.Fatal("member limit must be enforced")
	}
}

func TestSendMessageDispatchAndAssistantReply(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]

	result, err := h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{
		BotID: bot.ID, Text: "你好，队长", RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if result.DeliveryStatus != domain.DeliveryQueued {
		t.Fatalf("delivery status = %s, want queued", result.DeliveryStatus)
	}

	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn started", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	// A turn may carry what changed for the member before the user's text,
	// so the text is the tail of the input rather than the whole of it.
	if !strings.HasSuffix(turn.Text, "你好，队长") {
		t.Fatalf("engine received %q", turn.Text)
	}

	// Same requestId replays; different text conflicts.
	replay, err := h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{
		BotID: bot.ID, Text: "你好，队长", RequestID: "req-1",
	})
	if err != nil || replay.Message.ID != result.Message.ID {
		t.Fatalf("idempotent replay failed: %v %+v", err, replay)
	}
	_, err = h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{
		BotID: bot.ID, Text: "换个内容", RequestID: "req-1",
	})
	if err == nil {
		t.Fatal("same requestId with different text must conflict")
	}

	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "你好！有什么可以帮你？")
	waitUntil(t, 5*time.Second, "assistant message", func() bool {
		detail, err := h.app.ListConversation(context.Background(), h.user, bot.ID, "", 50)
		if err != nil {
			return false
		}
		messages := detail["messages"].([]domain.Message)
		for _, m := range messages {
			if m.Kind == domain.MessageKindAssistant {
				return true
			}
		}
		return false
	})
	detail, _ := h.app.ListConversation(context.Background(), h.user, bot.ID, "", 50)
	if detail["queuedCount"].(int) != 0 {
		t.Fatal("queue must be drained")
	}
	botsAfter, _ := h.app.GetTeamDetail(context.Background(), h.user, team.ID)
	if botsAfter.Bots[0].Status != domain.BotStatusIdle {
		t.Fatalf("bot status = %s, want idle", botsAfter.Bots[0].Status)
	}
}

func TestQueuedFIFOAndCancel(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	first, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "第一条"})
	if err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "first turn", func() bool { return len(fake.Turns()) == 1 })
	// A busy member normally takes user messages into its running turn; when
	// the engine refuses that, they queue in order and can be retracted.
	fake.SteerErr = errors.New("no active turn")

	// Bot is busy; further messages queue without starting turns.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "第二条"}); err != nil {
		t.Fatal(err)
	}
	third, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "第三条"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Turns()) != 1 {
		t.Fatal("busy bot must not start new turns")
	}

	// Cancelling the not-yet-sent third message must keep it from executing.
	if _, err := h.app.CancelQueuedMessage(ctx, h.user, third.Message.ID); err != nil {
		t.Fatalf("cancel queued: %v", err)
	}
	// The in-flight first message cannot be cancelled.
	if _, err := h.app.CancelQueuedMessage(ctx, h.user, first.Message.ID); err == nil {
		t.Fatal("dispatched message must not be cancellable")
	}

	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "completed", "已处理第一条")
	waitUntil(t, 5*time.Second, "second turn", func() bool { return len(fake.Turns()) == 2 })
	if !strings.HasSuffix(fake.Turns()[1].Text, "第二条") {
		t.Fatalf("FIFO violated: got %q", fake.Turns()[1].Text)
	}
	fake.EmitTurnCompleted(fake.Turns()[1].ThreadID, fake.Turns()[1].TurnID, "completed", "已处理第二条")
	// The cancelled third message must never reach the engine.
	time.Sleep(200 * time.Millisecond)
	if len(fake.Turns()) != 2 {
		t.Fatalf("cancelled message reached the engine: %+v", fake.Turns())
	}
}

func TestInterruptRequestsEngineStopAndFinalizes(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "跑个长任务"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })

	detail, _ := h.app.ListConversation(ctx, h.user, bot.ID, "", 10)
	run := detail["activeRun"].(domain.Run)
	runID, err := h.app.InterruptRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if runID != run.ID {
		t.Fatal("interrupt must target the active run")
	}
	if len(fake.Interrupts()) != 1 {
		t.Fatal("engine interrupt not requested")
	}
	// Only that bot: another message to the same bot stays queued.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "后续"}); err != nil {
		t.Fatal(err)
	}
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "interrupted", "")
	waitUntil(t, 5*time.Second, "interrupted run", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, run.ID)
		return err == nil && r.Status == domain.RunInterrupted
	})
}

func TestNativeApprovalRoundTrip(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "写个文件"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]

	fake.EmitApproval("req-appr-1", turn.ThreadID, turn.TurnID, `pwsh -Command "Write-Host hi"`)
	waitUntil(t, 5*time.Second, "pending approval", func() bool {
		items, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return err == nil && len(items) == 1
	})
	pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	approval := pending[0]
	if approval.Type != domain.ApprovalNativeTool || approval.Summary == "" {
		t.Fatalf("unexpected approval: %+v", approval)
	}
	// Both entry points see the same single request: member scope filter works.
	memberScoped, _ := h.app.PendingApprovals(ctx, h.user, team.ID, bot.ID)
	if len(memberScoped) != 1 {
		t.Fatal("requester-scoped projection must see its own card")
	}
	otherScoped, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "some-other-bot")
	if len(otherScoped) != 0 {
		t.Fatal("unrelated scope must see nothing")
	}
	// While waiting, the bot reports waiting_approval.
	detail, _ := h.app.GetTeamDetail(ctx, h.user, team.ID)
	if detail.Bots[0].Status != domain.BotStatusWaitingApproval {
		t.Fatalf("bot status = %s, want waiting_approval", detail.Bots[0].Status)
	}

	decided, err := h.app.DecideApproval(ctx, h.user, approval.ID, "accept", "", approval.Revision)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if decided.RequestState != domain.ApprovalStateDecided || decided.ResponseState != domain.ResponseSent {
		t.Fatalf("unexpected decided state: %+v", decided)
	}
	replies := fake.Replies()
	if len(replies) != 1 || replies[0].Response["decision"] != "accept" {
		t.Fatalf("engine reply missing/wrong: %+v", replies)
	}
	// Double decide is refused.
	if _, err := h.app.DecideApproval(ctx, h.user, approval.ID, "decline", "", decided.Revision); err == nil {
		t.Fatal("second decision must conflict")
	}
	// History keeps the original options and choice.
	history, _ := h.app.ApprovalHistory(ctx, h.user, team.ID, 50)
	if len(history) != 1 || history[0].Decision == nil || history[0].Decision.OptionID != "accept" {
		t.Fatalf("history incomplete: %+v", history)
	}
	// The turn continues and completes normally.
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "文件已写入")
	waitUntil(t, 5*time.Second, "completion", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, approval.RunID)
		return err == nil && r.Status == domain.RunCompleted
	})
}

func TestApprovalExpiresAfterEngineRestart(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "干活"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitApproval("req-appr-2", turn.ThreadID, turn.TurnID, "rm something")
	waitUntil(t, 5*time.Second, "pending", func() bool {
		items, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return len(items) == 1
	})
	pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")

	// The connection dies while the approval is pending.
	fake.EmitExit(errors.New("process died"))
	waitUntil(t, 5*time.Second, "run unknown after exit", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, pending[0].RunID)
		return err == nil && r.Status == domain.RunUnknown
	})

	// The bot is locked by the unknown run: dispatch must not restart
	// it, so the decision records truthfully that no reply was delivered.
	if len(fake.Turns()) != 1 {
		t.Fatal("locked bot must not start new turns after connection loss")
	}
	// Connection death invalidates the undecided card immediately and marks its response honestly expired — the dead engine can
	// never answer its request id. A later decision is therefore refused
	// instead of recording a decision that could not be delivered.
	invalidated, err := sqlite.GetApproval(ctx, h.app.DB, pending[0].ID)
	if err != nil {
		t.Fatalf("read card after exit: %v", err)
	}
	if invalidated.RequestState != domain.ApprovalStateInvalidated {
		t.Fatalf("card of the dead connection = %s, want invalidated at exit", invalidated.RequestState)
	}
	if invalidated.ResponseState != domain.ResponseExpired {
		t.Fatalf("card response = %q, want expired", invalidated.ResponseState)
	}
	if _, derr := h.app.DecideApproval(ctx, h.user, pending[0].ID, "accept", "", 0); derr == nil {
		t.Fatal("an invalidated card must refuse a new decision")
	}
	// Decided history rules still hold for cards decided before the crash:
	// nothing else to assert here beyond the refusal above.

	// Once the run is resolved (recovery does this in M4; here we
	// simulate the resolved state) and a NEW server generation exists, the
	// old request id must expire instead of being answered on the new
	// connection.
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Resolving a run without an engine clears the turn pair: no connection
	// owns it any more, which is what recovery reports.
	if err := sqlite.UpdateRunStatus(ctx, tx, pending[0].RunID, domain.RunInterrupted, "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SetBotRunState(ctx, tx, bot.ID, domain.BotStatusIdle, ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Drop the dead server before waking the bot: a message queued first
	// would be dispatched into the crashed generation and drain the queue
	// into a server that cannot answer.
	h.fakeMu.Lock()
	prior := h.fakes[team.ID]
	h.fakeMu.Unlock()
	h.restartTeamServer(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "重启后的新任务"}); err != nil {
		t.Fatal(err)
	}
	// The app-server died. The unknown run keeps the bot locked until a human
	// verifies it, so resolve it first — this is what recovery does.
	h.resolveUnknownRun(t, bot.ID)
	decided2, err := h.app.DecideApproval(ctx, h.user, pending[0].ID, "accept", "", 0)
	if err == nil {
		t.Fatal("second decision on the same approval must conflict")
	}
	_ = decided2

	// The next turn brings up a NEW server generation, the only way a team
	// reaches a second epoch.
	fake2 := h.fakeGeneration(t, team.ID, prior)
	waitUntil(t, 5*time.Second, "second-generation turn", func() bool {
		return len(fake2.Turns()) >= 1
	})
	turn2 := fake2.Turns()[len(fake2.Turns())-1]
	// Emit the approval while the turn is still live: completing the turn first
	// would make the run terminal, and an exit then marks nothing unknown.
	fake2.EmitApproval("req-appr-4", turn2.ThreadID, turn2.TurnID, "old-epoch command")
	waitUntil(t, 5*time.Second, "second pending", func() bool {
		items, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		for _, item := range items {
			if item.Type == domain.ApprovalNativeTool {
				return true
			}
		}
		return false
	})
	allPending2, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	var pending2 []domain.Approval
	for _, item := range allPending2 {
		if item.Type == domain.ApprovalNativeTool {
			pending2 = append(pending2, item)
		}
	}
	// Crash while the approval is still pending: the run is live, so the exit
	// marks it unknown and the later decision must expire.
	fake2.EmitExit(errors.New("second crash"))
	waitUntil(t, 5*time.Second, "second unknown run", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, pending2[0].RunID)
		return err == nil && r.Status == domain.RunUnknown
	})
	// The same rule as the first crash: the card is invalidated when its
	// connection dies, so no decision can be recorded against it.
	invalidated2, err := sqlite.GetApproval(ctx, h.app.DB, pending2[0].ID)
	if err != nil {
		t.Fatalf("read card after second crash: %v", err)
	}
	if invalidated2.RequestState != domain.ApprovalStateInvalidated || invalidated2.ResponseState != domain.ResponseExpired {
		t.Fatalf("card after second crash = %s/%s, want invalidated/expired", invalidated2.RequestState, invalidated2.ResponseState)
	}
	if _, derr := h.app.DecideApproval(ctx, h.user, pending2[0].ID, "decline", "", 0); derr == nil {
		t.Fatal("an invalidated card must refuse a decision after the second crash")
	}
}

func TestTurnStartUnknownLocksBot(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	// Arm the failure before dispatch spins the team server.
	settingsBefore, _ := h.app.GetSettings(ctx)
	h.onFake = func(fake *fakeengine.Server) {
		fake.BeforeStartTurn = func() error { return errors.New("connection reset") }
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "试试"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "unknown run", func() bool {
		detail, err := h.app.ListConversation(ctx, h.user, bot.ID, "", 10)
		if err != nil {
			return false
		}
		run, ok := detail["activeRun"].(domain.Run)
		return ok && run.Status == domain.RunUnknown
	})
	detail, _ := h.app.ListConversation(ctx, h.user, bot.ID, "", 10)
	if detail["queuedCount"].(int) != 0 {
		t.Fatal("queue must reflect the failed dispatch")
	}
	// The failure lands as an actionable reason on the run, and the
	// engine error never rewrites the user's configuration.
	if run, ok := detail["activeRun"].(domain.Run); !ok || run.ErrorKey != "error.engine.turn_unknown" {
		t.Fatalf("unknown run must carry error.engine.turn_unknown, got %+v", detail["activeRun"])
	}
	settingsAfter, _ := h.app.GetSettings(ctx)
	if settingsAfter.DefaultModel != settingsBefore.DefaultModel || settingsAfter.EnginePath != settingsBefore.EnginePath {
		t.Fatal("an engine failure must not modify settings")
	}
	// Bot is locked: a new message queues but must not start a turn.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "再试"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if len(fake.Turns()) != 0 {
		t.Fatal("locked bot must not start new turns")
	}
	// The first message's delivery is unknown, not silently re-sent.
	_ = team
}
