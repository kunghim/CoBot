package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// ServerFactory starts one team app-server. Tests inject a fake; production
// wires the Codex adapter in bootstrap.
type ServerFactory func(spec ServerSpec) (engine.Server, error)

// ServerSpec describes the team server to start.
type ServerSpec struct {
	Context   context.Context
	TeamID    string
	Cwd       string
	Callbacks engine.Callbacks
}

// teamRuntime owns one team's app-server inside this core lifetime.
type teamRuntime struct {
	server engine.Server
	cwd    string

	// lastActive is when the engine last did or was asked for anything
	// (unix nanoseconds); an engine idle long enough is shut down.
	lastActive atomic.Int64

	mu     sync.Mutex
	buffer map[string]*runBuffer // runID -> accumulated agent text
	// live resolves a turn's progress events to its run without a database
	// read per streamed delta; entries end with the turn.
	live map[liveTurnKey]liveRun
}

type runBuffer struct {
	texts []string
	items map[string]bool
	// commentary is the last thing the model said while working; it stands
	// in as the reply only when a turn ends without an answer.
	commentary string
}

// Scheduler wakes per bot, claims queued deliveries serially and drives the
// engine. Queue and approval handling never wait on the model inside SQL
// transactions.
type Scheduler struct {
	app     *App
	factory ServerFactory
	logger  *slog.Logger

	mu        sync.Mutex
	teams     map[string]*teamRuntime
	wake      chan string
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	workers   sync.WaitGroup
	busy      map[string]bool
	pending   map[string]bool
	teamLocks sync.Map // team id -> *sync.Mutex; serializes runtime start/stop

	// leaseDone is closed when the lease loop exits, so Stop joins it.
	leaseDone chan struct{}

	// IdleEngineTimeout is how long a team's engine may sit with nothing to
	// do before it is shut down; the next turn starts it again and resumes
	// the members' threads. Zero keeps engines running.
	IdleEngineTimeout time.Duration
}

// defaultIdleEngineTimeout frees an unused team's engine process.
const defaultIdleEngineTimeout = 15 * time.Minute

// NewScheduler creates a scheduler bound to one app.
func NewScheduler(a *App, factory ServerFactory) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		ctx: ctx, cancel: cancel, busy: map[string]bool{}, pending: map[string]bool{},
		app:       a,
		factory:   factory,
		logger:    a.Logger,
		teams:     map[string]*teamRuntime{},
		leaseDone: make(chan struct{}),
		wake:      make(chan string, 256),

		IdleEngineTimeout: defaultIdleEngineTimeout,
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
	}
}

// Start launches the dispatch loop and the lease refresh loop until Stop.
// Leases tick on their own goroutine (review B9): a slow engine call inside
// dispatch must never starve the 2-second refresh, or every member's bridge
// would start refusing side effects while the core is healthy (H06).
func (s *Scheduler) Start() {
	go func() {
		defer close(s.done)
		for {
			select {
			case <-s.stop:
				return
			case botID := <-s.wake:
				s.launch(botID)
			}
		}
	}()
	go func() {
		defer close(s.leaseDone)
		leaseTicker := time.NewTicker(platform.LeaseInterval)
		defer leaseTicker.Stop()
		s.app.CheckTeamDirectories(s.ctx)
		for {
			select {
			case <-s.stop:
				return
			case <-leaseTicker.C:
				s.app.RefreshLeases(s.ctx)
				s.stopIdleEngines(s.ctx)
			}
		}
	}()
}

// Stop shuts the loop and every owned team server; repeated calls are safe.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() { s.cancel(); close(s.stop) })
	select {
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	// Both loops must be gone before the caller closes the database: a late
	// lease write racing DB.Close would be swallowed or worse (review B). The
	// lease loop goes first: it may still hand an idle engine's shutdown to a
	// worker, which the wait below then joins.
	select {
	case <-s.leaseDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	joined := make(chan struct{})
	go func() { s.workers.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	teams := make(map[string]*teamRuntime, len(s.teams))
	for id, rt := range s.teams {
		teams[id] = rt
	}
	s.mu.Unlock()
	var errs []error
	for teamID, rt := range teams {
		// H01: request interruption of every in-flight turn before the engine
		// goes down. Whatever cannot finish in the bounded window dies with
		// its own process; the next startup's reconciliation classifies it as
		// unknown instead of this shutdown guessing an outcome.
		if runs, rerr := sqlite.ListActiveRunsByTeam(ctx, s.app.DB, teamID); rerr == nil {
			for _, run := range runs {
				session, serr := sqlite.GetProviderSession(ctx, s.app.DB, run.BotID)
				if serr != nil || session.ProviderThreadID == "" {
					continue
				}
				if run.ProviderTurnID == "" {
					continue
				}
				if ierr := rt.server.InterruptTurn(ctx, session.ProviderThreadID, run.ProviderTurnID); ierr != nil {
					s.logger.Warn("interrupt in-flight run on shutdown", "botId", run.BotID, "error", ierr)
				}
			}
		}
		if err := rt.server.Shutdown(ctx); err != nil {
			s.logger.Warn("team server shutdown", "teamId", teamID, "error", err)
			errs = append(errs, err)
		}
		s.mu.Lock()
		if s.teams[teamID] == rt {
			delete(s.teams, teamID)
		}
		s.mu.Unlock()
	}
	return errors.Join(errs...)
}

// StopTeam shuts down one team's engine when the team is archived or
// deleted, so an idle team holds no process. The team has no live run by
// then; a later dispatch (after a restore) starts a fresh engine.
func (s *Scheduler) StopTeam(ctx context.Context, teamID string) error {
	lock := s.teamLock(teamID)
	lock.Lock()
	defer lock.Unlock()
	s.mu.Lock()
	rt := s.teams[teamID]
	delete(s.teams, teamID)
	s.mu.Unlock()
	if rt == nil {
		return nil
	}
	return rt.server.Shutdown(ctx)
}

// Wake schedules a dispatch attempt for one bot without blocking writers.
func (s *Scheduler) Wake(botID string) {
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	s.pending[botID] = true
	s.mu.Unlock()
	select {
	case s.wake <- botID:
	default:
		// The pending map retains the wake even when the bounded signal queue
		// fills. A worker completion drains it; no business request blocks.
	}
}

func (s *Scheduler) launch(botID string) {
	s.mu.Lock()
	if s.ctx.Err() != nil || s.busy[botID] || !s.pending[botID] {
		s.mu.Unlock()
		return
	}
	delete(s.pending, botID)
	s.busy[botID] = true
	s.workers.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.workers.Done()
		s.dispatchSafely(botID)
		s.mu.Lock()
		delete(s.busy, botID)
		ids := make([]string, 0, len(s.pending))
		for id := range s.pending {
			ids = append(ids, id)
		}
		s.mu.Unlock()
		for _, id := range ids {
			s.launch(id)
		}
	}()
}

// dispatchTimeout bounds one dispatch, which on a team's first turn also
// starts the engine and the member's thread. turnStartTimeout bounds only
// turn/start: past it the turn's outcome is unknown and needs verifying, so a
// slow engine start-up must not eat into it.
const (
	dispatchTimeout  = 2 * time.Minute
	turnStartTimeout = 30 * time.Second
)

func (s *Scheduler) dispatchSafely(botID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("dispatch panic", "botId", botID, "panic", rec)
		}
	}()
	ctx, cancel := context.WithTimeout(s.ctx, dispatchTimeout)
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
	teamLock := s.teamLock(bot.TeamID)
	teamLock.Lock()
	locked := true
	unlock := func() {
		if locked {
			teamLock.Unlock()
			locked = false
		}
	}
	defer unlock()
	team, directory, err := sqlite.GetTeam(ctx, s.app.DB, bot.TeamID)
	if err != nil {
		return err
	}
	// The folder is checked before every turn: a team whose folder is gone
	// becomes directory_missing here instead of failing inside the engine.
	team = s.app.checkTeamDirectory(ctx, team)
	if team.Status != domain.TeamStatusActive {
		return nil // archived/missing/error teams pause dispatch
	}
	if _, err := sqlite.FindActiveRun(ctx, s.app.DB, botID); err == nil {
		return nil // one active run per bot; queue stays
	} else if !errors.Is(err, sqlite.ErrNotFound) {
		return err
	}

	queue, err := sqlite.ListDispatchableDeliveries(ctx, s.app.DB, botID, 1)
	if err != nil || len(queue) == 0 {
		if err == nil {
			var questions int
			if e := s.app.DB.QueryRowContext(ctx, `SELECT count(*) FROM questions WHERE requester_bot_id = ? AND status = 'open' AND kind = ''`, botID).Scan(&questions); e != nil {
				return e
			}
			queued, e := sqlite.CountQueuedDeliveries(ctx, s.app.DB, botID)
			if e != nil {
				return e
			}
			state := domain.BotStatusIdle
			if questions > 0 || queued > 0 {
				state = domain.BotStatusWaitingInput
			}
			if bot.Status != state {
				tx, e := s.app.DB.BeginTx(ctx, nil)
				if e != nil {
					return e
				}
				defer tx.Rollback()
				if e := sqlite.SetBotRunState(ctx, tx, botID, state, ""); e != nil {
					return e
				}
				return tx.Commit()
			}
		}
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
	currentTeam, _, err := sqlite.GetTeam(ctx, tx, team.ID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if currentTeam.Status != domain.TeamStatusActive {
		tx.Rollback()
		return nil
	}
	if _, err := sqlite.GetBot(ctx, tx, botID); err != nil {
		tx.Rollback()
		if errors.Is(err, sqlite.ErrNotFound) {
			return nil
		}
		return err
	}
	message, err := sqlite.GetMessage(ctx, tx, record.MessageID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if delivery.TaskID != "" {
		task, err := sqlite.GetTask(ctx, tx, delivery.TaskID)
		if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			tx.Rollback()
			return err
		}
		// A card is stale once it was handed off in another turn (the member
		// saw it among its cards and did it) or its requirement was
		// cancelled: the turn it would cost would hear nothing new.
		if errors.Is(err, sqlite.ErrNotFound) || task.IsTerminal() {
			if err := sqlite.SetDeliveryStatus(ctx, tx, delivery.ID, domain.DeliveryCancelled); err != nil {
				tx.Rollback()
				return err
			}
			if err := sqlite.SetMessageDeliveryStatus(ctx, tx, message.ID, domain.DeliveryCancelled); err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			s.Wake(botID)
			return nil
		}
	}
	if message.Kind != domain.MessageKindUser {
		blocked, err := sqlite.HasBlockingQuestion(ctx, tx, botID, delivery.TaskID)
		if err != nil {
			tx.Rollback()
			return err
		}
		if blocked {
			tx.Rollback()
			return nil
		}
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
		s.wakeAfterSkippedDelivery(ctx, botID, record.ID)
		return nil // waiting on a user decision, or cancelled by the gate
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
	batch, err := s.claimBatchLocked(ctx, tx, botID, delivery, message, run.ID)
	if err != nil {
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
	unlock()
	// The counted wake committed with the claim: emit its events now, before
	// any engine interaction can fail, so the fact is never persisted without
	// its trail (review B18).
	for _, ev := range chainEvents {
		s.app.recordEvent(ctx, ev)
	}

	// Engine interaction happens outside any SQL transaction.
	server, err := s.EnsureTeamServer(ctx, team.ID, directory)
	if err != nil {
		return s.failStart(ctx, run.ID, bot.ID, "error.engine.unavailable")
	}
	// Publish the host execution binding before the bridge can call back,
	// including while turn/start has not yet returned a provider turn id.
	if _, err := s.app.DB.ExecContext(ctx, `UPDATE runs SET engine_epoch = ? WHERE id = ? AND status = ?`, server.Epoch(), run.ID, domain.RunStarting); err != nil {
		return err
	}
	threadID, err := s.ensureThread(ctx, team, bot, directory, server, run.ID)
	if err != nil {
		return s.failStart(ctx, run.ID, bot.ID, "error.engine.thread_failed")
	}
	// The thread keeps everything it was told; a turn adds only what changed
	// since (see turnContext), then the incoming messages.
	text := message.RenderText()
	if len(batch) > 0 {
		text = s.batchInput(ctx, delivery.Kind, append([]domain.Message{message}, batch...))
	}
	update, told := s.app.turnContext(ctx, bot, run.ID, parseDigest(sqlite.SessionContextDigest(ctx, s.app.DB, bot.ID)))
	input := text
	if update != "" {
		input = update + "\n\n" + text
	}
	s.app.TouchLease(ctx, bot.ID)
	turnCtx, cancelTurn := context.WithTimeout(ctx, turnStartTimeout)
	turnID, err := server.StartTurn(turnCtx, threadID, input, engine.TurnOptions{Model: settings.DefaultModel, Permission: permissionMode(team), Effort: team.ReasoningEffort})
	cancelTurn()
	if errors.Is(err, engine.ErrRejected) {
		// The engine answered with a refusal, so no turn exists: the run
		// failed, and there is nothing to verify or recover.
		s.logger.Warn("turn/start rejected", "botId", bot.ID, "runId", run.ID, "error", err)
		return s.failStart(ctx, run.ID, bot.ID, "error.engine.turn_rejected")
	}
	if err != nil {
		// The engine may or may not have accepted the turn: the run stays
		// unknown until verified, never silently retried (D04).
		return s.unknownStart(ctx, run.ID, delivery.ID, bot.ID)
	}

	tx, err = s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.AcknowledgeRunStart(ctx, tx, run.ID, turnID, server.Epoch()); err != nil {
		return err
	}
	if _, err := sqlite.SetRunDeliveriesStatus(ctx, tx, run.ID, domain.DeliveryAccepted, domain.DeliveryDispatching); err != nil {
		return err
	}
	// The thread has now heard this turn's update.
	if err := sqlite.SetSessionContextDigest(ctx, tx, bot.ID, told.String()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, TeamID: team.ID, BotID: bot.ID, ResourceID: run.ID})
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventMessageUpdated, TeamID: team.ID, BotID: bot.ID, ResourceID: message.ID})
	s.app.Hub.Publish()
	s.logger.Info("run started", "botId", bot.ID, "runId", run.ID, "threadId", threadID)
	s.app.startAssignedTasks(ctx, run.ID, bot.ID)

	// The engine may spawn this bot's MCP bridge at any moment inside the
	// turn; the lease must already be fresh when it asks to act (H06).
	s.app.TouchLease(ctx, bot.ID)
	return nil
}

// A gate may have parked the previous head after all earlier wake signals
// were consumed. Give the next eligible input a turn without polling a held
// chain or repeatedly scheduling the same skipped delivery.
func (s *Scheduler) wakeAfterSkippedDelivery(ctx context.Context, botID, skippedID string) {
	items, err := sqlite.ListDispatchableDeliveries(ctx, s.app.DB, botID, 1)
	if err == nil && len(items) > 0 && items[0].ID != skippedID {
		s.Wake(botID)
	}
}

func (s *Scheduler) failStart(ctx context.Context, runID, botID, errorKey string) error {
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.UpdateRunStatus(ctx, tx, runID, domain.RunFailed, "", "", errorKey, true); err != nil {
		return err
	}
	if _, err := sqlite.SetRunDeliveriesStatus(ctx, tx, runID, domain.DeliveryUnknown, domain.DeliveryDispatching); err != nil {
		return err
	}
	if err := sqlite.SetBotRunState(ctx, tx, botID, domain.BotStatusIdle, ""); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, BotID: botID, ResourceID: runID})
	if s.app.CollaborationTokens != nil {
		s.app.CollaborationTokens.RevokeRun(runID)
	}
	s.app.Hub.Publish()
	return fmt.Errorf("start failed: %s", errorKey)
}

func (s *Scheduler) unknownStart(ctx context.Context, runID, deliveryID, botID string) error {
	// A cancelled transport does not prove the model rejected the turn. Keep
	// the uncertainty durable even when Stop cancelled the dispatch context.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	run, err := sqlite.GetRun(ctx, tx, runID)
	if err != nil {
		return err
	}
	if run.Status != domain.RunStarting {
		return nil
	}
	if err := sqlite.UpdateRunStatus(ctx, tx, runID, domain.RunUnknown, run.ProviderTurnID, run.EngineEpoch, "error.engine.turn_unknown", false); err != nil {
		return err
	}
	if _, err := sqlite.SetRunDeliveriesStatus(ctx, tx, runID, domain.DeliveryUnknown, domain.DeliveryDispatching); err != nil {
		return err
	}
	if err := sqlite.SetBotRunState(ctx, tx, botID, domain.BotStatusUnknown, runID); err != nil {
		return err
	}
	cardID, err := ensureRecoveryCardLocked(ctx, tx, run, deliveryID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if cardID != "" {
		if card, err := sqlite.GetApproval(ctx, s.app.DB, cardID); err == nil {
			s.app.publishApprovalUpdated(ctx, card)
		}
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
	lock := s.teamLock(teamID)
	lock.Lock()
	defer lock.Unlock()
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if rt, ok := s.teams[teamID]; ok {
		s.mu.Unlock()
		rt.touch()
		return rt.server, nil
	}
	s.mu.Unlock()

	rt := &teamRuntime{cwd: cwd, buffer: map[string]*runBuffer{}}
	rt.touch()
	var err error
	rt.server, err = s.factory(ServerSpec{
		Context: ctx,
		TeamID:  teamID,
		Cwd:     cwd,
		Callbacks: engine.Callbacks{
			OnApproval: func(req engine.ApprovalRequest) {
				rt.touch()
				if s.runtime(teamID) == rt {
					s.onApproval(teamID, rt, req)
				}
			},
			OnNotification: func(n engine.Notification) {
				rt.touch()
				if s.runtime(teamID) == rt {
					s.onNotification(teamID, rt, n)
				}
			},
			OnExit: func(err error) {
				s.onServerExitRuntime(teamID, rt, err)
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

func (rt *teamRuntime) touch() { rt.lastActive.Store(time.Now().UnixNano()) }

// stopIdleEngines shuts down the engines of teams that have had nothing to do
// for IdleEngineTimeout: no turn running, starting, waiting or unverified.
// Holding the team lock keeps a dispatch from claiming a turn between the
// check and the shutdown; a later turn simply starts a fresh engine.
func (s *Scheduler) stopIdleEngines(ctx context.Context) {
	if s.IdleEngineTimeout <= 0 || ctx.Err() != nil {
		return
	}
	cutoff := time.Now().Add(-s.IdleEngineTimeout).UnixNano()
	s.mu.Lock()
	var idle []string
	for teamID, rt := range s.teams {
		if rt.lastActive.Load() < cutoff {
			idle = append(idle, teamID)
		}
	}
	s.mu.Unlock()
	for _, teamID := range idle {
		s.stopIfIdle(ctx, teamID, cutoff)
	}
}

func (s *Scheduler) stopIfIdle(ctx context.Context, teamID string, cutoff int64) {
	lock := s.teamLock(teamID)
	lock.Lock()
	s.mu.Lock()
	rt := s.teams[teamID]
	if rt == nil || rt.lastActive.Load() >= cutoff {
		s.mu.Unlock()
		lock.Unlock()
		return
	}
	s.mu.Unlock()
	runs, err := sqlite.ListActiveRunsByTeam(ctx, s.app.DB, teamID)
	if err != nil || len(runs) > 0 {
		lock.Unlock()
		return
	}
	s.mu.Lock()
	if s.teams[teamID] == rt {
		delete(s.teams, teamID)
	}
	s.mu.Unlock()
	lock.Unlock()
	s.logger.Info("idle team engine stopped", "teamId", teamID, "idleFor", s.IdleEngineTimeout.String())
	// The shutdown waits for the process; off the lease loop, so leases keep
	// their 2-second refresh (H06). Stop joins it like any worker.
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := rt.server.Shutdown(shutdownCtx); err != nil {
			s.logger.Warn("idle team engine shutdown", "teamId", teamID, "error", err)
		}
	}()
}

func (s *Scheduler) teamLock(teamID string) *sync.Mutex {
	value, _ := s.teamLocks.LoadOrStore(teamID, &sync.Mutex{})
	return value.(*sync.Mutex)
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

// threadDeleter is the engine capability to delete a stored thread.
type threadDeleter interface {
	DeleteThread(ctx context.Context, threadID string) error
}

// DeleteThreads removes threads through the team's running engine, which
// holds them while it runs. It reports false when the team has no running
// engine (or it cannot delete), so the caller uses the control engine.
func (s *Scheduler) DeleteThreads(ctx context.Context, teamID string, threads []string) (bool, error) {
	rt := s.runtime(teamID)
	if rt == nil {
		return false, nil
	}
	deleter, ok := rt.server.(threadDeleter)
	if !ok {
		return false, nil
	}
	var errs []error
	for _, thread := range threads {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if err := deleter.DeleteThread(callCtx, thread); err != nil {
			errs = append(errs, err)
		}
		cancel()
	}
	return true, errors.Join(errs...)
}

func (s *Scheduler) runtime(teamID string) *teamRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.teams[teamID]
}

// threadLoadedChecker is implemented by engines that know which threads they
// have loaded; others are resumed before every turn.
type threadLoadedChecker interface {
	ThreadLoaded(threadID string) bool
}

func (s *Scheduler) ensureThread(ctx context.Context, team domain.Team, bot domain.Bot, cwd string, server engine.Server, runID string) (threadID string, err error) {
	defer func() {
		// A recording names the member behind each engine thread.
		if err == nil && s.app.Trace != nil {
			meta, _ := json.Marshal(map[string]string{"threadId": threadID, "botId": bot.ID, "botName": bot.Name, "roleId": bot.RoleID, "teamId": team.ID, "teamName": team.Name})
			s.app.Trace.Record("cobot.meta", "thread-bound", "", meta)
		}
	}()
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
	settings, err := s.app.GetSettings(ctx)
	if err != nil {
		return "", err
	}
	config, err := s.threadConfig(ctx, team, bot, server.Epoch(), runID)
	if err != nil {
		return "", err
	}
	instructions, told := s.app.threadInstructions(ctx, team, bot, role)
	spec := engine.ThreadSpec{
		Cwd:                   cwd,
		Permission:            permissionMode(team),
		ApprovalsReviewer:     "user",
		Model:                 settings.DefaultModel,
		DeveloperInstructions: instructions,
		Config:                config,
	}
	if session, err := sqlite.GetProviderSession(ctx, s.app.DB, bot.ID); err == nil && session.ProviderThreadID != "" {
		// Resuming is for a thread this app-server does not have, after an
		// engine restart or once Codex unloaded it. A thread that is still
		// loaded here takes the turn directly.
		if loaded, ok := server.(threadLoadedChecker); ok && session.EngineEpoch == server.Epoch() && loaded.ThreadLoaded(session.ProviderThreadID) {
			return session.ProviderThreadID, nil
		}
		if err := server.ResumeThread(ctx, session.ProviderThreadID, spec); err == nil {
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
	info, err := server.StartThread(ctx, spec)
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
	// A new thread knows exactly what its instructions told it.
	if err := sqlite.SetSessionContextDigest(ctx, tx, bot.ID, told.String()); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return info.ThreadID, nil
}

// threadConfig is the per-thread engine configuration. It merges the
// platform defaults with this bot's collaboration bridge.
//
// The bridge is spawned by the engine as `cobot mcp` with the bot token in
// its environment. That token is minted per bot and bound to this engine
// epoch, so a bridge left behind by a restarted connection can no longer act:
// the core refuses it rather than trusting a stale credential.
func (s *Scheduler) threadConfig(ctx context.Context, team domain.Team, bot domain.Bot, engineEpoch, runID string) (map[string]any, error) {
	extra := map[string]any{}
	if s.app.MCPBinary == "" || s.app.MCPEndpoint == "" || s.app.CollaborationTokens == nil {
		// No bridge available (for example a test factory); the tools simply
		// are not offered rather than being offered and always failing.
		return codex.ThreadConfig(extra), nil
	}
	token, err := s.app.CollaborationTokens.ActivateBridgeRun(bot.ID, team.ID, engineEpoch, runID)
	if err != nil {
		s.logger.Error("mint bot token failed; collaboration tools disabled", "botId", bot.ID, "error", err)
		return nil, err
	}
	prefix := "mcp_servers.cobot"
	extra[prefix+".enabled"] = true
	extra[prefix+".command"] = s.app.MCPBinary
	// The data root only tells the bridge where a developer recording
	// (`cobot trace on`) would go.
	extra[prefix+".args"] = []string{"mcp", "--data-root", s.app.Paths.Root}
	extra[prefix+".env"] = map[string]string{
		auth.BridgeEnvEndpoint: s.app.MCPEndpoint,
		auth.BridgeEnvToken:    token,
		// The bridge needs to know which member it acts for and where the
		// local leases live, so a stale executor refuses side effects even
		// without HTTP (H06).
		auth.BridgeEnvBotID:    bot.ID,
		auth.BridgeEnvLeaseDir: s.app.LeaseDir(),
		// The bridge offers only the tools of this member's role.
		auth.BridgeEnvRole: bridgeRole(team, bot),
	}
	// The tools are CoBot's own structured collaboration surface, so they
	// never need a user approval round trip; the core enforces permissions.
	extra[prefix+".required"] = true
	extra[prefix+".default_tools_approval_mode"] = "approve"
	return codex.ThreadConfig(extra), nil
}

// permissionMode is the team's mode, the default for a row that predates it.
func permissionMode(team domain.Team) string {
	if domain.ValidPermissionMode(team.PermissionMode) {
		return team.PermissionMode
	}
	return domain.PermissionDefault
}

// Warm starts a team's engine ahead of its first turn: opening a team in the
// UI is the moment a message is likely to follow, and engine start-up then
// overlaps with the user's typing instead of delaying the first reply. It
// never blocks the caller; a failure is left for the real dispatch to report.
func (s *Scheduler) Warm(teamID, cwd string) {
	if s.runtime(teamID) != nil {
		return
	}
	go func() {
		// EnsureTeamServer serializes with dispatch on the team lock.
		if _, err := s.EnsureTeamServer(s.ctx, teamID, cwd); err != nil {
			s.logger.Info("engine warm-up failed", "teamId", teamID, "error", err)
		}
	}()
}

// bridgeRole is the tool set a member's bridge offers: "leader" for the
// team's leader, "member" for everyone else. A reviewer is a member like
// any other: what it reviews comes to it on cards.
func bridgeRole(team domain.Team, bot domain.Bot) string {
	if bot.IsLeader || team.LeaderID == bot.ID {
		return "leader"
	}
	return "member"
}
