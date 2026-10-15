package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// ServerFactory starts one team app-server. Tests inject a fake; production
// wires the Codex adapter in bootstrap.
type ServerFactory func(spec ServerSpec) (engine.Server, error)

// ServerSpec describes the team server to start.
type ServerSpec struct {
	TeamID    string
	Cwd       string
	Callbacks engine.Callbacks
}

// teamRuntime owns one team's app-server inside this core lifetime.
type teamRuntime struct {
	server engine.Server
	cwd    string

	mu     sync.Mutex
	buffer map[string]*runBuffer // runID -> accumulated agent text
}

type runBuffer struct {
	texts []string
}

// Scheduler wakes per bot, claims queued deliveries serially and drives the
// engine. Queue and approval handling never wait on the model inside SQL
// transactions.
type Scheduler struct {
	app     *App
	factory ServerFactory
	logger  *slog.Logger

	mu       sync.Mutex
	teams    map[string]*teamRuntime
	wake     chan string
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// NewScheduler creates a scheduler bound to one app.
func NewScheduler(a *App, factory ServerFactory) *Scheduler {
	return &Scheduler{
		app:     a,
		factory: factory,
		logger:  a.Logger,
		teams:   map[string]*teamRuntime{},
		wake:    make(chan string, 256),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Start launches the dispatch loop until Stop.
func (s *Scheduler) Start() {
	go func() {
		defer close(s.done)
		for {
			select {
			case <-s.stop:
				return
			case botID := <-s.wake:
				s.dispatchSafely(botID)
			}
		}
	}()
}

// Stop shuts the loop and every owned team server; repeated calls are safe.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
	s.mu.Lock()
	teams := s.teams
	s.teams = map[string]*teamRuntime{}
	s.mu.Unlock()
	var errs []error
	for teamID, rt := range teams {
		if err := rt.server.Shutdown(ctx); err != nil {
			s.logger.Warn("team server shutdown", "teamId", teamID, "error", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Wake schedules a dispatch attempt for one bot without blocking writers.
func (s *Scheduler) Wake(botID string) {
	select {
	case s.wake <- botID:
	default:
		// Queue full: a later wake or notification retry covers it; never
		// block an API request on the scheduler.
		s.logger.Warn("scheduler wake queue full", "botId", botID)
	}
}

func (s *Scheduler) dispatchSafely(botID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("dispatch panic", "botId", botID, "panic", rec)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.dispatch(ctx, botID); err != nil {
		s.logger.Warn("dispatch failed", "botId", botID, "error", err)
	}
}

// dispatch claims the oldest queued delivery of an idle bot and starts a
// turn. Busy bots keep their queue; nothing interrupts implicitly.
func (s *Scheduler) dispatch(ctx context.Context, botID string) error {
	bot, err := sqlite.GetBot(ctx, s.app.DB, botID)
	if err != nil {
		return err // deleted bot: drop quietly
	}
	team, directory, err := sqlite.GetTeam(ctx, s.app.DB, bot.TeamID)
	if err != nil {
		return err
	}
	if team.Status != domain.TeamStatusActive {
		return nil // archived/missing/error teams pause dispatch
	}
	if _, err := sqlite.FindActiveRun(ctx, s.app.DB, botID); err == nil {
		return nil // one active run per bot; queue stays
	} else if !errors.Is(err, sqlite.ErrNotFound) {
		return err
	}

	queue, err := sqlite.ListQueuedDeliveries(ctx, s.app.DB, botID, 1)
	if err != nil || len(queue) == 0 {
		return err
	}
	record := queue[0]

	settings, err := s.app.GetSettings(ctx)
	if err != nil {
		return err
	}
	// Budget gate: only NEW turns stop. An exhausted budget, or spend the
	// product cannot judge, holds the queue and asks the user through
	// settings; it never interrupts work that already started (G07/G08).
	if pause, perr := s.app.budgetPaused(ctx, team.ID, bot.ID, settings); perr != nil {
		return perr
	} else if pause != nil {
		return s.holdForBudget(ctx, team.ID, bot.ID, pause.Reason)
	}

	// Claim: delivery -> dispatching, run row created, bot -> working.
	run := domain.Run{
		ID: domain.NewID(), TeamID: team.ID, BotID: bot.ID,
		Status: domain.RunStarting, StartedAt: time.Now().UTC(),
	}
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	delivery, err := sqlite.GetDelivery(ctx, tx, record.ID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if delivery.Status != domain.DeliveryQueued {
		tx.Rollback()
		return nil // cancelled meanwhile
	}
	// The chain budget is evaluated in the same transaction as the claim:
	// counting a wake and becoming eligible to run are one decision, so a
	// crash between them can neither lose nor double-count a wake.
	gate, action, chainEvents, err := s.app.EvaluateChainGate(ctx, tx, delivery)
	if err != nil {
		tx.Rollback()
		return err
	}
	if gate != ChainGateAllowed {
		// The claim is abandoned, so anything the gate decided must survive
		// (the confirmation card, the stopped-chain sweep) is written in its
		// own transaction — inside the claim it would be rolled back along
		// with the claim, and the gate would ask again on every wake.
		tx.Rollback()
		if action != ChainActionNone {
			wtx, werr := s.app.DB.BeginTx(ctx, nil)
			if werr != nil {
				return werr
			}
			evs, perr := s.app.PersistChainAction(ctx, wtx, action, delivery)
			if perr != nil {
				wtx.Rollback()
				return perr
			}
			if cerr := wtx.Commit(); cerr != nil {
				return cerr
			}
			chainEvents = append(chainEvents, evs...)
		}
		for _, ev := range chainEvents {
			s.app.recordEvent(ctx, ev)
		}
		s.app.Hub.Publish()
		return nil // waiting on a user decision, or cancelled by the gate
	}
	message, err := sqlite.GetMessage(ctx, tx, record.MessageID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := sqlite.SetMessageDeliveryStatus(ctx, tx, message.ID, domain.DeliveryDispatching); err != nil {
		tx.Rollback()
		return err
	}
	if err := sqlite.InsertRun(ctx, tx, run); err != nil {
		tx.Rollback()
		return err
	}
	if err := sqlite.ClaimDeliveryRun(ctx, tx, record.ID, run.ID); err != nil {
		tx.Rollback()
		return err
	}
	if err := sqlite.SetBotRunState(ctx, tx, bot.ID, domain.BotStatusWorking, run.ID); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Engine interaction happens outside any SQL transaction.
	server, err := s.EnsureTeamServer(ctx, team.ID, directory)
	if err != nil {
		return s.failStart(ctx, run.ID, delivery.ID, message.ID, bot.ID, "error.engine.unavailable")
	}
	threadID, err := s.ensureThread(ctx, team, bot, directory, server)
	if err != nil {
		return s.failStart(ctx, run.ID, delivery.ID, message.ID, bot.ID, "error.engine.thread_failed")
	}
	// The context is rebuilt on every dispatch so the bot sees the current
	// team, member list and task instead of trusting a stale model memory.
	// A task-assignment message carries its task id in run_id.
	taskID := ""
	if message.Kind == domain.MessageKindSystem {
		taskID = message.RunID
	}
	input := s.app.BuildContext(ctx, bot, taskID) + "\n\n" + message.RenderText()
	turnID, err := server.StartTurn(ctx, threadID, input, settings.DefaultModel)
	if err != nil {
		// The engine may or may not have accepted the turn: the run stays
		// unknown until verified, never silently retried (D04).
		return s.unknownStart(ctx, run.ID, delivery.ID, message.ID, bot.ID)
	}

	tx, err = s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunRunning, turnID, server.Epoch(), "", false); err != nil {
		return err
	}
	if err := sqlite.SetDeliveryStatus(ctx, tx, delivery.ID, domain.DeliveryAccepted); err != nil {
		return err
	}
	if err := sqlite.SetMessageDeliveryStatus(ctx, tx, message.ID, domain.DeliveryAccepted); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, ev := range chainEvents {
		s.app.recordEvent(ctx, ev)
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, TeamID: team.ID, BotID: bot.ID, ResourceID: run.ID})
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventMessageUpdated, TeamID: team.ID, BotID: bot.ID, ResourceID: message.ID})
	s.app.Hub.Publish()
	s.logger.Info("run started", "botId", bot.ID, "runId", run.ID, "threadId", threadID)
	return nil
}

func (s *Scheduler) failStart(ctx context.Context, runID, deliveryID, messageID, botID, errorKey string) error {
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.UpdateRunStatus(ctx, tx, runID, domain.RunFailed, "", "", errorKey, true); err != nil {
		return err
	}
	if err := sqlite.SetDeliveryStatus(ctx, tx, deliveryID, domain.DeliveryUnknown); err != nil {
		return err
	}
	if err := sqlite.SetMessageDeliveryStatus(ctx, tx, messageID, domain.DeliveryUnknown); err != nil {
		return err
	}
	if err := sqlite.SetBotRunState(ctx, tx, botID, domain.BotStatusIdle, ""); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, BotID: botID, ResourceID: runID})
	s.app.Hub.Publish()
	return fmt.Errorf("start failed: %s", errorKey)
}

func (s *Scheduler) unknownStart(ctx context.Context, runID, deliveryID, messageID, botID string) error {
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.UpdateRunStatus(ctx, tx, runID, domain.RunUnknown, "", "", "error.engine.turn_unknown", true); err != nil {
		return err
	}
	if err := sqlite.SetDeliveryStatus(ctx, tx, deliveryID, domain.DeliveryUnknown); err != nil {
		return err
	}
	if err := sqlite.SetMessageDeliveryStatus(ctx, tx, messageID, domain.DeliveryUnknown); err != nil {
		return err
	}
	if err := sqlite.SetBotRunState(ctx, tx, botID, domain.BotStatusUnknown, runID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, BotID: botID, ResourceID: runID})
	s.app.Hub.Publish()
	return errors.New("turn/start outcome unknown; bot locked for verification")
}

// holdForBudget parks a bot whose queue is blocked by a budget: the bot
// shows "needs input", the deliveries keep their place, and the reason is a
// durable event the UI can explain. Fixing the budget re-wakes these bots.
func (s *Scheduler) holdForBudget(ctx context.Context, teamID, botID, reason string) error {
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.SetBotRunState(ctx, tx, botID, domain.BotStatusWaitingInput, ""); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventBudgetPaused, TeamID: teamID, BotID: botID, ResourceID: reason})
	s.app.Hub.Publish()
	return nil
}

// EnsureTeamServer returns the team's app-server, starting it on first use.
// Exported so a test can bring a team up without dispatching a turn.
func (s *Scheduler) EnsureTeamServer(ctx context.Context, teamID, cwd string) (engine.Server, error) {
	s.mu.Lock()
	if rt, ok := s.teams[teamID]; ok {
		s.mu.Unlock()
		return rt.server, nil
	}
	s.mu.Unlock()

	rt := &teamRuntime{cwd: cwd, buffer: map[string]*runBuffer{}}
	var err error
	rt.server, err = s.factory(ServerSpec{
		TeamID: teamID,
		Cwd:    cwd,
		Callbacks: engine.Callbacks{
			OnApproval:     func(req engine.ApprovalRequest) { s.onApproval(teamID, req) },
			OnNotification: func(n engine.Notification) { s.onNotification(teamID, n) },
			OnExit: func(err error) {
				s.onServerExit(teamID, err)
			},
		},
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if existing, ok := s.teams[teamID]; ok {
		s.mu.Unlock()
		_ = rt.server.Shutdown(ctx)
		return existing.server, nil
	}
	s.teams[teamID] = rt
	s.mu.Unlock()
	return rt.server, nil
}

// EngineEpochOfTeam reports the live engine epoch for a team, or "" when no
// server is running. The HTTP layer uses it to refuse a bridge credential
// minted by a previous connection.
func (a *App) EngineEpochOfTeam(teamID string) string {
	if a.Scheduler == nil {
		return ""
	}
	rt := a.Scheduler.runtime(teamID)
	if rt == nil || rt.server == nil {
		return ""
	}
	return rt.server.Epoch()
}

func (s *Scheduler) runtime(teamID string) *teamRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.teams[teamID]
}

func (s *Scheduler) ensureThread(ctx context.Context, team domain.Team, bot domain.Bot, cwd string, server engine.Server) (string, error) {
	if session, err := sqlite.GetProviderSession(ctx, s.app.DB, bot.ID); err == nil && session.ProviderThreadID != "" {
		if err := server.ResumeThread(ctx, session.ProviderThreadID); err == nil {
			// The thread now belongs to this connection, so rebind it: thread
			// ids are only unique inside one engine, and every later lookup
			// (approval routing, turn completion) resolves the bot by
			// (thread, epoch). A resume on a new connection without this
			// update would silently drop that connection's events.
			if session.EngineEpoch != server.Epoch() {
				tx, txErr := s.app.DB.BeginTx(ctx, nil)
				if txErr != nil {
					return "", txErr
				}
				if uerr := sqlite.UpsertProviderSession(ctx, tx, domain.ProviderSession{
					BotID: bot.ID, ProviderThreadID: session.ProviderThreadID,
					EngineEpoch: server.Epoch(), Model: session.Model, ModelProvider: session.ModelProvider,
				}); uerr != nil {
					tx.Rollback()
					return "", uerr
				}
				if cerr := tx.Commit(); cerr != nil {
					return "", cerr
				}
			}
			return session.ProviderThreadID, nil
		}
		// A resume failure falls through to a fresh thread; the old thread is
		// never deleted, so product messages stay intact.
	}
	role, err := roles.Default(bot.RoleID)
	if err != nil {
		return "", err
	}
	if bot.RoleVersion != "" {
		// Prefer the user's edited copy from the profile when present.
		if profile, perr := roles.LoadProfile(s.app.Paths.RolesDir()); perr == nil {
			for _, r := range profile {
				if r.ID == bot.RoleID {
					role = r
					break
				}
			}
		}
	}
	settings, _ := s.app.GetSettings(ctx)
	info, err := server.StartThread(ctx, engine.ThreadSpec{
		Cwd:                   cwd,
		Sandbox:               "workspace-write",
		ApprovalPolicy:        "untrusted",
		ApprovalsReviewer:     "user",
		Model:                 settings.DefaultModel,
		DeveloperInstructions: buildInstructions(team, bot, role),
		Config:                s.threadConfig(ctx, team, bot, server.Epoch()),
	})
	if err != nil {
		return "", err
	}
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := sqlite.UpsertProviderSession(ctx, tx, domain.ProviderSession{
		BotID: bot.ID, ProviderThreadID: info.ThreadID, EngineEpoch: server.Epoch(),
		Model: info.Model, ModelProvider: info.ModelProvider,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return info.ThreadID, nil
}

// buildInstructions assembles the first-turn role context. Later BuildContext
// (tasks/memories/collaboration) extends this per dispatch.
// threadConfig is the per-thread engine configuration. It merges the
// platform defaults with this bot's collaboration bridge.
//
// The bridge is spawned by the engine as `cobot mcp` with the bot token in
// its environment. That token is minted per bot and bound to this engine
// epoch, so a bridge left behind by a restarted connection can no longer act:
// the core refuses it rather than trusting a stale credential.
func (s *Scheduler) threadConfig(ctx context.Context, team domain.Team, bot domain.Bot, engineEpoch string) map[string]any {
	extra := map[string]any{}
	if s.app.MCPBinary == "" || s.app.MCPEndpoint == "" || s.app.CollaborationTokens == nil {
		// No bridge available (for example a test factory); the tools simply
		// are not offered rather than being offered and always failing.
		return codex.ThreadConfig(extra)
	}
	token, err := s.app.CollaborationTokens.Issue(bot.ID, team.ID, engineEpoch)
	if err != nil {
		s.logger.Error("mint bot token failed; collaboration tools disabled", "botId", bot.ID, "error", err)
		return codex.ThreadConfig(extra)
	}
	prefix := "mcp_servers.cobot"
	extra[prefix+".enabled"] = true
	extra[prefix+".command"] = s.app.MCPBinary
	extra[prefix+".args"] = []string{"mcp"}
	extra[prefix+".env"] = map[string]string{
		auth.BridgeEnvEndpoint: s.app.MCPEndpoint,
		auth.BridgeEnvToken:    token,
	}
	// The tools are CoBot's own structured collaboration surface, so they
	// never need a user approval round trip; the core enforces permissions.
	extra[prefix+".required"] = true
	extra[prefix+".default_tools_approval_mode"] = "auto"
	return codex.ThreadConfig(extra)
}

func buildInstructions(team domain.Team, bot domain.Bot, role roles.Role) string {
	var b strings.Builder
	b.WriteString(role.Instructions)
	b.WriteString("\n\n# 团队\n")
	fmt.Fprintf(&b, "团队名称：%s\n", team.Name)
	fmt.Fprintf(&b, "团队工作目录：%s\n", team.Directory)
	fmt.Fprintf(&b, "你的名字：%s（角色：%s）\n", bot.Name, role.Name)
	b.WriteString("与同团队成员的结构化协作由宿主工具提供；没有工具时直接用文字回复。用用户的语言交流。\n")
	return b.String()
}
