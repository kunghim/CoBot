package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

// What a member's thread is started with: the collaboration bridge, tied to
// this member and this engine. The configuration's names are Codex's own, and
// which of them are sent is pinned here while they move behind the engine.
func TestAThreadIsStartedWithTheMembersBridge(t *testing.T) {
	h := newM2Harness(t)
	h.app.MCPBinary = "cobot-under-test"
	h.app.MCPEndpoint = "http://127.0.0.1:1/internal"
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	pm, dev := bots[0], bots[1]
	startTurn(t, h, fake, dev, "做点事")
	startTurn(t, h, fake, pm, "安排一下")
	if len(fake.StartedConfigs()) != 2 {
		t.Fatalf("%d threads, want one per member", len(fake.StartedConfigs()))
	}
	for i, want := range []struct {
		bot  domain.Bot
		role string
	}{{dev, "member"}, {pm, "leader"}} {
		config := fake.StartedConfigs()[i]
		if config["mcp_servers.cobot.enabled"] != true || config["mcp_servers.cobot.command"] != "cobot-under-test" ||
			config["mcp_servers.cobot.required"] != true || config["mcp_servers.cobot.default_tools_approval_mode"] != "approve" {
			t.Fatalf("bridge settings of %s = %+v", want.bot.Name, config)
		}
		if args := config["mcp_servers.cobot.args"]; !reflect.DeepEqual(args, []string{"mcp", "--data-root", h.app.Paths.Root}) {
			t.Fatalf("bridge args = %v", args)
		}
		env, _ := config["mcp_servers.cobot.env"].(map[string]string)
		if env[auth.BridgeEnvEndpoint] != h.app.MCPEndpoint || env[auth.BridgeEnvToken] == "" || env[auth.BridgeEnvBotID] != want.bot.ID ||
			env[auth.BridgeEnvLeaseDir] != h.app.LeaseDir() || env[auth.BridgeEnvRole] != want.role {
			t.Fatalf("bridge environment of %s: botId=%s role=%s token set=%v", want.bot.Name, env[auth.BridgeEnvBotID], env[auth.BridgeEnvRole], env[auth.BridgeEnvToken] != "")
		}
	}
}

// Without a bridge to spawn, the tools are simply not offered.
func TestAThreadWithoutABridgeIsOfferedNoTools(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	startTurn(t, h, fake, bots[1], "做点事")
	for key := range fake.StartedConfigs()[0] {
		if strings.HasPrefix(key, "mcp_servers") {
			t.Fatalf("offered %s without a bridge", key)
		}
	}
}

// startedChat sends a first chat message and waits for its running turn.
func startedChat(t *testing.T, h *m2Harness, made <-chan *chatFake, key string) (*chatFake, domain.Chat, domain.ChatRun) {
	t.Helper()
	ctx := context.Background()
	sent, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: key, Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	f := chatCreated(t, made)
	var run domain.ChatRun
	waitUntil(t, 5*time.Second, "a running chat turn", func() bool {
		page, err := h.app.ChatPage(ctx, h.user, sent.Chat.ID, "")
		if err != nil {
			return false
		}
		for _, r := range page.Runs {
			if r.Status == domain.RunRunning {
				run = r
				return true
			}
		}
		return false
	})
	return f, sent.Chat, run
}

// An engine that cannot start fails the turn; it is not left waiting.
func TestAChatWhoseEngineCannotStartFails(t *testing.T) {
	h := newM2Harness(t)
	h.app.Chats = app.NewChatRunner(h.app, func(app.ServerSpec) (engine.Server, error) {
		return nil, errors.New("no engine here")
	})
	if err := h.app.Chats.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.app.Chats.Stop(ctx)
	})
	sent, err := h.app.SendChat(context.Background(), h.user, "", app.ChatSendInput{RequestID: "no-engine", Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	page := chatWait(t, h, sent.Chat.ID, domain.RunFailed)
	if got := page.Runs[len(page.Runs)-1].ErrorKey; got != "error.chat.failed" {
		t.Fatalf("errorKey = %q", got)
	}
}

// A reconnect ends the moment the turn makes progress again.
func TestAChatReconnectStepClosesWhenProgressResumes(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	f, chat, run := startedChat(t, h, made, "reconnect")
	f.Emit(engine.Notification{Method: "error", ThreadID: run.ThreadID, TurnID: run.TurnID,
		Problem: &engine.Problem{Level: "error", Message: "Reconnecting... 1/5", Code: "responseStreamDisconnected", WillRetry: true}})
	retryStatus := func() string {
		page, err := h.app.ChatPage(ctx, h.user, chat.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page.Runs {
			for _, s := range r.Steps {
				if s.Kind == domain.StepKindRetry {
					return s.Status
				}
			}
		}
		return ""
	}
	waitUntil(t, 5*time.Second, "the reconnecting step", func() bool { return retryStatus() == engine.StepInProgress })
	f.Emit(engine.Notification{Method: "item/agentMessage/delta", ThreadID: run.ThreadID, TurnID: run.TurnID, ItemID: "m1", DeltaKind: "message", Delta: "好"})
	waitUntil(t, 5*time.Second, "the step closed", func() bool { return retryStatus() == engine.StepCompleted })
}

// Usage is the newest snapshot per turn: a repeated report replaces it.
func TestAChatRecordsUsagePerTurn(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	f, chat, run := startedChat(t, h, made, "usage")
	f.EmitTokenUsage(run.ThreadID, run.TurnID, 10, 5, 1)
	f.EmitTokenUsage(run.ThreadID, run.TurnID, 20, 6, 2)
	row := func() (int, string) {
		var n int
		var data string
		if err := h.app.DB.QueryRow(`SELECT count(*), coalesce(max(data), '') FROM chat_usage WHERE chat_id = ?`, chat.ID).Scan(&n, &data); err != nil {
			t.Fatal(err)
		}
		return n, data
	}
	waitUntil(t, 5*time.Second, "the newest snapshot", func() bool {
		n, data := row()
		return n == 1 && strings.Contains(data, `"inputTokens":20`)
	})
}

// A native request the engine stopped waiting for closes its card.
func TestAChatRequestTheEngineSettledCloses(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	ctx := context.Background()
	f, chat, run := startedChat(t, h, made, "resolved")
	f.EmitApproval("42", run.ThreadID, run.TurnID, "go test")
	requestState := func() string {
		page, err := h.app.ChatPage(ctx, h.user, chat.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Requests) == 0 {
			return ""
		}
		return page.Requests[0].State
	}
	waitUntil(t, 5*time.Second, "the card", func() bool { return requestState() == domain.ApprovalStatePending })
	id, _ := json.Marshal("42")
	f.Emit(engine.Notification{Method: "serverRequest/resolved", ThreadID: run.ThreadID, TurnID: run.TurnID, RequestID: id})
	waitUntil(t, 5*time.Second, "the card closed", func() bool { return requestState() == domain.ApprovalStateInvalidated })
}

// A running chat keeps its lease fresh: the guard refuses a stale one.
func TestARunningChatKeepsItsLeaseFresh(t *testing.T) {
	h := newM2Harness(t)
	made := attachChats(t, h, nil)
	_, chat, _ := startedChat(t, h, made, "lease")
	read := func() time.Time {
		lease, _, err := platform.ReadChatLease(h.app.Paths.Root, chat.ID, h.app.InstanceID, time.Now())
		if err != nil {
			return time.Time{}
		}
		return lease.UpdatedAt
	}
	waitUntil(t, 5*time.Second, "a lease", func() bool { return !read().IsZero() })
	first := read()
	waitUntil(t, 3*platform.LeaseInterval, "the lease refreshed", func() bool { return read().After(first) })
}

// Asking for changes at acceptance goes back to the project manager as the
// user's own words.
func TestAcceptanceReviseGoesBackAsTheUsersMessage(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]
	d := relayDriver{t: t, h: h, team: team}
	req := d.start(pm, []domain.Stage{
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Kind: domain.StageAccept},
	})
	d.confirm(req)
	d.handoff(d.open(req), domain.HandoffDone, "")
	d.handoff(d.open(req), domain.HandoffDone, "")
	ctx := context.Background()
	questions, err := sqlite.ListQuestions(ctx, h.app.DB, team.ID, domain.QuestionOpen, 20)
	if err != nil {
		t.Fatal(err)
	}
	var acceptance domain.Question
	for _, q := range questions {
		if q.RequirementID == req.ID && q.Kind == domain.QuestionKindAcceptance {
			acceptance = q
		}
	}
	if acceptance.ID == "" {
		t.Fatal("no acceptance question")
	}
	answered, err := h.app.AnswerDecision(ctx, h.user, acceptance.ID, domain.AcceptanceRevise, "", "按钮太小", acceptance.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if answered.Status != domain.QuestionAnswered || answered.AnswerMessageID == "" {
		t.Fatalf("answered = %+v", answered)
	}
	message, err := sqlite.GetMessage(ctx, h.app.DB, answered.AnswerMessageID)
	if err != nil {
		t.Fatal(err)
	}
	if message.Kind != domain.MessageKindUser || message.BotID != pm.ID || !strings.Contains(message.RenderText(), "验收未通过，需要修改：按钮太小") {
		t.Fatalf("message = %+v", message)
	}
	if h.deliveriesForBot(t, pm.ID) == 0 {
		t.Fatal("the project manager was not given the changes")
	}
}

// A recovery decision must be one of the offered options, and only work whose
// fate is still open can run again.
func TestRecoveryRefusesWhatItCannotDo(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	member := bots[1]
	h.ensureFake(t, team.ID)
	runID, deliveryID := seedUnresolvedRun(t, h, team, member, "要恢复的派发", "turn-x", "epoch-dead")
	proveSeededRunEnded(t, h, runID, domain.RunInterrupted)
	if err := h.app.ReconcileAfterRestart(ctx); err != nil {
		t.Fatal(err)
	}
	cards, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var card domain.Approval
	for _, c := range cards {
		if c.Type == domain.ApprovalRecovery {
			card = c
		}
	}
	if card.ID == "" {
		t.Fatal("no recovery card")
	}
	if _, err := h.app.DecideApproval(ctx, h.user, card.ID, "nonsense", "", card.Revision, nil); err == nil {
		t.Fatal("an option that was not offered must be refused")
	}
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SetDeliveryStatus(ctx, tx, deliveryID, domain.DeliveryCancelled); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var de *domain.Error
	if _, err := h.app.DecideApproval(ctx, h.user, card.ID, domain.RecoveryOptionRerun, "", card.Revision, nil); !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("rerunning cancelled work = %v, want a conflict", err)
	}
}

// A model the engine does not know is told its context window, and a provider
// that does not run web search has it turned off, for a member and a chat alike.
// With the model's advanced settings saying it can search, it stays on.
func TestThreadConfigFollowsTheModel(t *testing.T) {
	const window = 123456
	save := func(t *testing.T, h *m2Harness, search bool) {
		t.Helper()
		if _, err := h.app.SaveProvider(context.Background(), domain.Provider{
			ID: "gateway", Name: "gateway", Kind: domain.ProviderKindCustom, AuthMode: domain.ProviderAuthAPIKey,
			Endpoints:    []domain.ProviderEndpoint{{Dialect: domain.DialectOpenAIResponses, BaseURL: "https://gateway.example/v1"}},
			Models:       []domain.ProviderModel{{ID: "gateway-model", ContextWindow: window, WebSearch: search}},
			DefaultModel: "gateway-model",
		}, 0); err != nil {
			t.Fatal(err)
		}
	}
	check := func(t *testing.T, config map[string]any, search bool) {
		t.Helper()
		if got := config["model_context_window"]; got != window {
			t.Fatalf("context window = %v, want %d", got, window)
		}
		if got, off := config["web_search"]; off == search {
			t.Fatalf("web search setting = %v with search allowed %v", got, search)
		}
	}
	for _, search := range []bool{false, true} {
		t.Run(fmt.Sprintf("member search=%v", search), func(t *testing.T) {
			h := newM2Harness(t)
			ctx := context.Background()
			save(t, h, search)
			team, bots := h.createTeam(t, []string{"fullstack-engineer"})
			fake := h.ensureFake(t, team.ID)
			dev := bots[1]
			if _, err := h.app.SetBotModel(ctx, h.user, dev.ID, "gateway", "gateway-model", dev.Revision); err != nil {
				t.Fatal(err)
			}
			startTurn(t, h, fake, dev, "做点事")
			check(t, fake.StartedConfigs()[0], search)
		})
		t.Run(fmt.Sprintf("chat search=%v", search), func(t *testing.T) {
			h := newM2Harness(t)
			save(t, h, search)
			made := attachChats(t, h, nil)
			if _, err := h.app.SendChat(context.Background(), h.user, "", app.ChatSendInput{RequestID: "config", Text: "你好", ProviderID: "gateway", Model: "gateway-model"}); err != nil {
				t.Fatal(err)
			}
			f := chatCreated(t, made)
			waitUntil(t, 5*time.Second, "the chat's thread", func() bool { return len(f.StartedConfigs()) > 0 })
			check(t, f.StartedConfigs()[0], search)
		})
	}
}

// installation is an engine the machine can find, at a path a test chooses.
type installation struct{ located []string }

func (i *installation) Locate(path string) (string, error) {
	i.located = append(i.located, path)
	return "the-engine", nil
}
func (i *installation) ProbeVersion(context.Context, string) (string, error) { return "1.2.3", nil }
func (i *installation) Probe(context.Context, string, string) engine.Status {
	return engine.Status{Protocol: "probed", Capabilities: []string{"app-server"}}
}
func (i *installation) BuildCatalog(_ context.Context, _ string, models []engine.ClientModel) (engine.Catalog, error) {
	if len(models) == 0 {
		return engine.Catalog{}, nil
	}
	return engine.Catalog{Path: "catalog.json", Fingerprint: models[0].Slug}, nil
}

// A deleted team's threads are removed through the control engine, once the
// team's own engine has been stopped.
func TestDeletingATeamRemovesItsThreadsThroughTheControlEngine(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	control := fakeengine.New(engine.Callbacks{}, "control")
	h.app.ControlFactory = func(context.Context, string) (engine.Control, error) { return control, nil }
	team, bots := h.createTeam(t, nil)
	fake := h.ensureFake(t, team.ID)
	turn := startTurn(t, h, fake, bots[0], "做点事")
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunCompleted, "好")
	settlesAt(t, h, bots[0].ID, domain.BotStatusIdle)
	if _, err := h.app.ArchiveTeam(ctx, h.user, team.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.DeleteTeam(ctx, h.user, team.ID, 0); err != nil {
		t.Fatal(err)
	}
	if got := control.DeletedThreads(); len(got) != 1 || got[0] != turn.ThreadID {
		t.Fatalf("deleted threads = %v, want %s", got, turn.ThreadID)
	}
}

// The status and the catalog come from the installation; without one there is
// no engine, whatever the machine has.
func TestEngineStatusAndCatalogComeFromTheInstallation(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	if info := h.app.EngineStatus(ctx); info.Status != "not_configured" {
		t.Fatalf("status without an installation = %+v", info)
	}
	if catalog, err := h.app.EngineCatalog(ctx); err != nil || catalog.Path != "" {
		t.Fatalf("catalog without an installation = %+v %v", catalog, err)
	}

	h.app.Install = &installation{}
	control := fakeengine.New(engine.Callbacks{}, "control")
	h.app.ControlFactory = func(context.Context, string) (engine.Control, error) { return control, nil }
	h.app.EngineStatusInvalidated()
	info := h.app.EngineStatus(ctx)
	if info.Status != "ready" || info.BinaryPath != "the-engine" || info.Version != "1.2.3" || info.Protocol != "probed" {
		t.Fatalf("status = %+v", info)
	}
	if catalog, err := h.app.EngineCatalog(ctx); err != nil || catalog.Path != "" {
		t.Fatalf("catalog with no third-party model = %+v %v", catalog, err)
	}
	saveProvider(t, h, "gateway", "gateway-model")
	if _, err := h.app.SaveProvider(ctx, domain.Provider{
		ID: "wide", Name: "wide", Kind: domain.ProviderKindCustom, AuthMode: domain.ProviderAuthAPIKey,
		Endpoints:    []domain.ProviderEndpoint{{Dialect: domain.DialectOpenAIResponses, BaseURL: "https://wide.example/v1"}},
		Models:       []domain.ProviderModel{{ID: "wide-model", ContextWindow: 500000}},
		DefaultModel: "wide-model",
	}, 0); err != nil {
		t.Fatal(err)
	}
	if catalog, err := h.app.EngineCatalog(ctx); err != nil || catalog.Fingerprint != "wide-model" {
		t.Fatalf("catalog = %+v %v, want the wide model", catalog, err)
	}
	if err := h.app.EngineLogout(ctx); err != nil {
		t.Fatal(err)
	}
}
