package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/hookrun"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// ChatRunner owns projectless engine lifetimes. Its locks never span engine RPCs:
// the same connection's read loop delivers callbacks while an RPC is in flight.
type ChatRunner struct {
	a           *App
	factory     ServerFactory
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	runtimes    map[string]*chatRuntime
	locks       sync.Map
	engineLocks sync.Map
	wg          sync.WaitGroup
	closed      bool
}
type chatRuntime struct {
	id         string
	server     engine.Server
	run        domain.ChatRun
	thread     string
	texts      map[string]string
	order      []string
	commentary string
	// loose are the messages the engine gave no phase (a third-party model's),
	// each with the work steps begun before it; see looseReply.
	loose    []loosePiece
	work     int
	workSeen map[string]bool
	// retryOpen is set while the engine reconnects: the one "retry" step is
	// in progress until the turn makes progress again.
	retryOpen  bool
	lastActive time.Time
	bindings   map[string][2]string
}

func NewChatRunner(a *App, factory ServerFactory) *ChatRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &ChatRunner{a: a, factory: factory, ctx: ctx, cancel: cancel, runtimes: map[string]*chatRuntime{}}
}
func (r *ChatRunner) lock(id string) func() {
	v, _ := r.locks.LoadOrStore(id, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
func (r *ChatRunner) spawn(f func()) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.wg.Add(1)
	go func() { defer r.wg.Done(); f() }()
	return true
}
func (r *ChatRunner) runtime(id string) *chatRuntime {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runtimes[id]
}
func (r *ChatRunner) report(err error) {
	if err != nil && r.a.Logger != nil {
		r.a.Logger.Error("chat state persistence failed", "error", err)
	}
}

func (r *ChatRunner) Start() error {
	ctx := r.ctx
	runs, err := sqlite.ListChatRuns(ctx, r.a.DB, "")
	if err != nil {
		return err
	}
	for _, run := range runs {
		if !domain.ChatRunActive(run.Status) {
			continue
		}
		if run.Status == "queued" {
			var data string
			if err = r.a.DB.QueryRowContext(ctx, `SELECT data FROM chat_messages WHERE id=?`, run.MessageID).Scan(&data); err != nil {
				return err
			}
			var m domain.ChatMessage
			if err = json.Unmarshal([]byte(data), &m); err != nil {
				return err
			}
			r.wake(run.ChatID, run.ID, m.Text)
		} else {
			release := r.lock(run.ChatID)
			err = r.markUnknown(ctx, run, "error.chat.restarted")
			release()
			if err != nil {
				return err
			}
		}
	}
	r.spawn(func() {
		tick := time.NewTicker(platform.LeaseInterval)
		defer tick.Stop()
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-tick.C:
				r.maintain()
			}
		}
	})
	return nil
}
func (r *ChatRunner) wake(id, runID, text string) { r.spawn(func() { r.dispatch(id, runID, text) }) }
func (r *ChatRunner) discard(id string) {
	r.mu.Lock()
	rt := r.runtimes[id]
	delete(r.runtimes, id)
	r.mu.Unlock()
	platform.RemoveChatLease(r.a.Paths.Root, id)
	if rt != nil && rt.server != nil {
		r.spawn(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = rt.server.Shutdown(ctx)
		})
	}
}
func (r *ChatRunner) Stop(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	r.cancel()
	all := make([]*chatRuntime, 0, len(r.runtimes))
	for _, rt := range r.runtimes {
		all = append(all, rt)
	}
	r.mu.Unlock()
	var errs []error
	for _, rt := range all {
		platform.RemoveChatLease(r.a.Paths.Root, rt.id)
		if rt.server != nil {
			if err := rt.server.Shutdown(ctx); err != nil {
				errs = append(errs, err)
			}
		}
	}
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}
	return errors.Join(errs...)
}
func (r *ChatRunner) maintain() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.runtimes))
	for id := range r.runtimes {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		release := r.lock(id)
		rt := r.runtime(id)
		if rt != nil && rt.server != nil {
			if domain.ChatRunActive(rt.run.Status) && rt.run.Status != domain.RunUnknown {
				r.report(r.touchLease(rt))
			} else if time.Since(rt.lastActive) > defaultIdleEngineTimeout {
				r.discard(id)
			}
		}
		release()
	}
}
func (r *ChatRunner) touchLease(rt *chatRuntime) error {
	return platform.WriteChatLease(r.a.Paths.Root, platform.ChatLease{ChatID: rt.id, RunID: rt.run.ID, InstanceID: r.a.InstanceID, Epoch: rt.run.Epoch, UpdatedAt: time.Now().UTC()})
}

func (r *ChatRunner) ensure(chat domain.Chat) (*chatRuntime, error) {
	v, _ := r.engineLocks.LoadOrStore(chat.ID, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	defer m.Unlock()
	if rt := r.runtime(chat.ID); rt != nil && rt.server != nil {
		return rt, nil
	}
	if err := os.MkdirAll(chat.Workspace, 0700); err != nil {
		return nil, err
	}
	rt := &chatRuntime{id: chat.ID, texts: map[string]string{}, bindings: map[string][2]string{}, lastActive: time.Now()}
	srv, err := r.factory(ServerSpec{Context: r.ctx, Cwd: chat.Workspace, Callbacks: engine.Callbacks{
		OnNotification: func(n engine.Notification) { r.notification(rt, n) },
		OnApproval:     func(req engine.ApprovalRequest) { r.approval(rt, req) },
		OnInput:        func(req engine.ApprovalRequest) { r.approval(rt, req) },
		OnExit:         func(err error) { r.exited(rt) },
	}})
	if err != nil {
		return nil, err
	}
	rt.server = srv
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = srv.Shutdown(context.Background())
		return nil, context.Canceled
	}
	r.runtimes[chat.ID] = rt
	r.mu.Unlock()
	return rt, nil
}

// thread is the chat's engine thread for its next turn. mayStartFresh lets it
// start a new one when the model chosen now cannot take the old thread's
// history (another vendor's, as for a team member, see needsFreshThread); the
// note it returns then opens that turn, saying where the old record is.
func (r *ChatRunner) thread(rt *chatRuntime, chat domain.Chat, mayStartFresh bool) (domain.ChatSession, string, error) {
	choice, err := r.a.chatChoice(r.ctx, chat.ProviderID, chat.Model)
	if err != nil {
		return domain.ChatSession{}, "", err
	}
	config := map[string]any{}
	if choice.EngineProvider != officialEngineProvider && !r.a.webSearchAllowed(r.ctx, choice) {
		config["web_search"] = "disabled"
	}
	// The window the model has, which the engine cannot know of a model it
	// has no entry for (see the engine's model catalog).
	if spec, ok := r.a.specOfChoice(r.ctx, choice, chat.Model); ok && spec.ContextWindow > 0 {
		config["model_context_window"] = spec.ContextWindow
	}
	instructions := ""
	if choice.EngineProvider != officialEngineProvider {
		// What a model without the engine's own tools is told (see threadInstructions).
		instructions = strings.TrimSpace(fileEditGuide(runtime.GOOS) + toolCallGuide)
	}
	spec := engine.ThreadSpec{Cwd: chat.Workspace, Permission: chat.PermissionMode, ApprovalsReviewer: "user", Model: chat.Model, ModelProvider: choice.EngineProvider, DeveloperInstructions: instructions, Config: codex.ThreadConfig(config)}
	session, err := sqlite.GetChatSession(r.ctx, r.a.DB, chat.ID)
	var info engine.ThreadInfo
	note := ""
	switching := false
	if errors.Is(err, sqlite.ErrNotFound) {
		info, err = rt.server.StartThread(r.ctx, spec)
	} else if err == nil {
		old := domain.ProviderSession{ProviderThreadID: session.ThreadID, ModelProvider: session.Provider, Model: session.Model}
		if mayStartFresh && r.a.needsFreshThread(r.ctx, old, choice) {
			handover := handoverFrom(r.ctx, rt.server, old)
			note = r.a.handoverIntro(r.ctx, *handover, domain.ProviderSession{ModelProvider: choice.EngineProvider, Model: chat.Model}) +
				"\n先用一两句话说清楚之前聊到哪，再回答用户下面的新消息。\n\n用户的新消息：\n"
			info, err = rt.server.StartThread(r.ctx, spec)
		} else {
			switching = session.Model != "" && chat.Model != "" && session.Model != chat.Model
			info, err = rt.server.ResumeThread(r.ctx, session.ThreadID, spec)
		}
	}
	if err != nil {
		return session, "", err
	}
	// Moving to another model of the same vendor, the thread keeps the one it
	// began with in its own record; the turn carries the model asked for.
	if !switching && chat.Model != "" && info.Model != "" && chat.Model != info.Model {
		return session, "", domain.Conflict("error.chat.model_mismatch", nil)
	}
	if info.ModelProvider != "" && info.ModelProvider != choice.EngineProvider {
		return session, "", domain.Conflict("error.chat.model_mismatch", nil)
	}
	if switching {
		info.Model = chat.Model
	}
	session = domain.ChatSession{ChatID: chat.ID, ThreadID: info.ThreadID, Epoch: rt.server.Epoch(), Model: info.Model, Provider: info.ModelProvider}
	if session.ThreadID == "" {
		return session, "", errors.New("missing chat thread identity")
	}
	err = sqlite.SaveChatSession(r.ctx, r.a.DB, session)
	if err != nil {
		return session, "", err
	}
	release := r.lock(chat.ID)
	rt.thread = session.ThreadID
	if chat.Model == "" && info.Model != "" {
		fresh, e := sqlite.GetChat(r.ctx, r.a.DB, chat.ID)
		if e == nil {
			fresh.Model = info.Model
			fresh.Revision++
			e = sqlite.SaveChat(r.ctx, r.a.DB, fresh, false)
		}
		if e != nil {
			release()
			return session, "", e
		}
	}
	release()
	return session, note, nil
}

func (r *ChatRunner) dispatch(chatID, runID, text string) {
	release := r.lock(chatID)
	run, err := sqlite.GetChatRun(r.ctx, r.a.DB, runID)
	if err != nil || run.ChatID != chatID || run.Status != "queued" {
		release()
		return
	}
	chat, err := sqlite.GetChat(r.ctx, r.a.DB, chatID)
	if err != nil {
		release()
		return
	}
	// Claim durably before the first engine operation; restart never replays it.
	run.Status = domain.RunStarting
	err = sqlite.SaveChatRun(r.ctx, r.a.DB, run)
	release()
	if err != nil {
		r.report(err)
		return
	}
	rt, err := r.ensure(chat)
	if err != nil {
		r.preDispatchFailed(run)
		return
	}
	session, note, err := r.thread(rt, chat, true)
	if err != nil {
		r.preDispatchFailed(run)
		return
	}
	release = r.lock(chatID)
	fresh, e := sqlite.GetChatRun(r.ctx, r.a.DB, runID)
	if e != nil || fresh.Status != domain.RunStarting {
		release()
		return
	}
	run = fresh
	rt.texts = map[string]string{}
	rt.order = nil
	rt.commentary = ""
	rt.loose, rt.work, rt.workSeen, rt.retryOpen = nil, 0, nil, false
	if run.StopRequested {
		rt.run = run
		err = r.finish(rt, domain.RunInterrupted)
		release()
		r.report(err)
		r.a.chatChanged(r.ctx, chatID, "chat.run.updated")
		return
	}
	run.ThreadID = session.ThreadID
	run.Epoch = session.Epoch
	rt.run = run
	rt.texts = map[string]string{}
	rt.order = nil
	rt.commentary = ""
	rt.loose, rt.work, rt.workSeen = nil, 0, nil
	rt.lastActive = time.Now()
	err = sqlite.SaveChatRun(r.ctx, r.a.DB, run)
	if err == nil {
		err = r.touchLease(rt)
	}
	release()
	if err != nil {
		r.report(err)
		r.preDispatchFailed(run)
		return
	}
	r.a.chatChanged(r.ctx, chatID, "chat.run.updated")
	if note != "" {
		text = note + text
	}
	// The level asked for goes only if this model offers it, as for a team.
	effort := chat.ReasoningEffort
	if choice, e := r.a.chatChoice(r.ctx, chat.ProviderID, chat.Model); e == nil {
		effort = r.a.effortFor(r.ctx, choice, chat.Model, chat.ReasoningEffort)
	}
	turnID, err := rt.server.StartTurn(r.ctx, session.ThreadID, text, engine.TurnOptions{Model: chat.Model, Effort: effort, Permission: chat.PermissionMode})
	release = r.lock(chatID)
	defer release()
	if r.runtime(chatID) != rt {
		return
	}
	fresh, e = sqlite.GetChatRun(r.ctx, r.a.DB, runID)
	if e != nil {
		r.report(e)
		return
	}
	if !domain.ChatRunActive(fresh.Status) {
		return
	}
	if err != nil {
		if errors.Is(err, engine.ErrRejected) && fresh.TurnID == "" {
			fresh.Status = domain.RunFailed
			fresh.ErrorKey = "error.chat.failed"
			fresh.EndedAt = time.Now().UTC()
			r.report(sqlite.SaveChatRun(r.ctx, r.a.DB, fresh))
			rt.run = fresh
			platform.RemoveChatLease(r.a.Paths.Root, chatID)
		} else {
			r.report(r.markUnknown(r.ctx, fresh, "error.chat.unknown"))
			rt.run.Status = domain.RunUnknown
		}
	} else if fresh.Status == domain.RunStarting {
		fresh.TurnID = turnID
		fresh.Status = domain.RunRunning
		rt.run = fresh
		r.report(sqlite.SaveChatRun(r.ctx, r.a.DB, fresh))
	}
	r.a.chatChanged(r.ctx, chatID, "chat.run.updated")
}
func (r *ChatRunner) preDispatchFailed(run domain.ChatRun) {
	release := r.lock(run.ChatID)
	defer release()
	fresh, err := sqlite.GetChatRun(context.Background(), r.a.DB, run.ID)
	if err != nil {
		return
	}
	if fresh.Status != domain.RunStarting {
		return
	}
	fresh.Status = domain.RunFailed
	fresh.ErrorKey = "error.chat.failed"
	fresh.EndedAt = time.Now().UTC()
	r.report(sqlite.SaveChatRun(context.Background(), r.a.DB, fresh))
	platform.RemoveChatLease(r.a.Paths.Root, run.ChatID)
	r.a.chatChanged(context.Background(), run.ChatID, "chat.run.updated")
}

func (r *ChatRunner) belongs(rt *chatRuntime, threadID string) bool {
	if threadID == rt.thread {
		return true
	}
	if root, ok := rt.server.(rootThreader); ok {
		return root.RootThread(threadID) == rt.thread
	}
	return false
}
func (r *ChatRunner) notification(rt *chatRuntime, n engine.Notification) {
	release := r.lock(rt.id)
	defer release()
	if r.runtime(rt.id) != rt || rt.server == nil || rt.run.ID == "" || !domain.ChatRunActive(rt.run.Status) || rt.run.Status == domain.RunUnknown {
		return
	}
	if !r.belongs(rt, n.ThreadID) {
		return
	}
	ctx := r.ctx
	rt.lastActive = time.Now()
	sub := n.ThreadID != rt.thread
	if n.Method == "turn/started" {
		if !sub {
			if rt.run.TurnID != "" && rt.run.TurnID != n.TurnID {
				return
			}
			rt.run.TurnID = n.TurnID
			rt.run.Status = domain.RunRunning
			r.report(sqlite.SaveChatRun(ctx, r.a.DB, rt.run))
			if rt.run.StopRequested {
				r.interrupt(rt, n.ThreadID, n.TurnID)
			}
		}
		if err := r.touchLease(rt); err != nil {
			r.report(err)
			return
		}
		threads := []string{n.ThreadID}
		if sub {
			threads = append(threads, rt.thread)
		}
		for _, thread := range threads {
			key := thread + "/" + n.TurnID
			rt.bindings[key] = [2]string{thread, n.TurnID}
			r.report(hookrun.WriteNativeBinding(r.a.Paths.Root, hookrun.NativeBinding{ChatID: rt.id, ThreadID: thread, TurnID: n.TurnID, RunID: rt.run.ID, EngineEpoch: rt.run.Epoch, InstanceID: r.a.InstanceID, ServiceEpoch: r.a.ServiceEpoch}))
		}
	}
	if !sub && n.TurnID != "" && rt.run.TurnID != "" && n.TurnID != rt.run.TurnID {
		return
	}
	// Any progress after a reconnect means the connection is back.
	if !sub && rt.retryOpen && (n.Delta != "" || n.Step != nil || (n.Problem != nil && !n.Problem.WillRetry)) {
		rt.retryOpen = false
		r.report(r.settleRetry(ctx, rt))
	}
	if n.Delta != "" {
		if !sub && n.DeltaKind == "message" {
			if _, ok := rt.texts[n.ItemID]; !ok {
				rt.order = append(rt.order, n.ItemID)
			}
			rt.texts[n.ItemID] += n.Delta
		}
		r.a.Live.Publish(events.Live{Type: "delta", ChatID: rt.id, RunID: rt.run.ID, ItemID: n.ItemID, Kind: n.DeltaKind, Text: n.Delta})
	}
	if n.Step != nil {
		s := domain.ChatStep{ID: n.Step.ID, Kind: n.Step.Kind, Status: n.Step.Status, Detail: n.Step.Detail}
		if sub {
			s.ID = n.ThreadID + "/" + s.ID
		}
		r.report(sqlite.SaveChatStep(ctx, r.a.DB, rt.run.ID, s))
		r.a.chatChanged(ctx, rt.id, "chat.run.updated")
		if !sub && isWorkStep(n.Step.Kind) {
			if rt.workSeen == nil {
				rt.workSeen = map[string]bool{}
			}
			if !rt.workSeen[n.Step.ID] {
				rt.workSeen[n.Step.ID] = true
				rt.work++
			}
		}
	}
	if !sub && n.Method == "item/completed" && n.ItemType == "agentMessage" {
		if n.Phase == "commentary" {
			rt.commentary = n.Text
		} else if n.Phase == "" {
			// Said on the way or as the answer: the end of the turn decides which.
			rt.loose = append(rt.loose, loosePiece{text: n.Text, work: rt.work})
		} else {
			if _, ok := rt.texts[n.ItemID]; !ok {
				rt.order = append(rt.order, n.ItemID)
			}
			rt.texts[n.ItemID] = n.Text
		}
	}
	if n.Problem != nil && n.Problem.WillRetry {
		// The engine is reconnecting and the turn may still finish: it is shown
		// as a reconnect, not as the failure a notice would claim.
		attempt := 1
		if m := attemptPattern.FindStringSubmatch(n.Problem.Message); m != nil {
			if reported, err := strconv.Atoi(m[1]); err == nil && reported > 0 {
				attempt = reported
			}
		}
		rt.retryOpen = true
		b, _ := json.Marshal(map[string]any{"attempt": attempt, "code": n.Problem.Code})
		r.report(sqlite.SaveChatStep(ctx, r.a.DB, rt.run.ID, domain.ChatStep{ID: retryStepID, Kind: domain.StepKindRetry, Status: engine.StepInProgress, Detail: b}))
		r.a.chatChanged(ctx, rt.id, "chat.run.updated")
	} else if n.Problem != nil {
		// Vendor bodies can echo credentials. Keep only stable discriminators.
		b, _ := json.Marshal(map[string]string{"level": n.Problem.Level, "code": n.Problem.Code, "failureCode": n.Problem.FailureCode})
		r.report(sqlite.SaveChatStep(ctx, r.a.DB, rt.run.ID, domain.ChatStep{ID: domain.NewID(), Kind: "notice", Status: engine.StepCompleted, Detail: b}))
	}
	if n.Method == "serverRequest/resolved" {
		r.resolveNativeRequest(rt, n.RequestID)
	}
	if n.Method == "thread/tokenUsage/updated" {
		r.report(r.recordUsage(rt, n))
	}
	if n.Method == "turn/completed" {
		if sub {
			return
		}
		status := n.TurnStatus
		switch status {
		case domain.RunCompleted, domain.RunFailed, domain.RunInterrupted:
		default:
			status = domain.RunUnknown
		}
		r.report(r.finish(rt, status))
		r.a.chatChanged(ctx, rt.id, "chat.run.updated")
	}
}

// settleRetry closes the open "reconnecting" step of the running turn.
func (r *ChatRunner) settleRetry(ctx context.Context, rt *chatRuntime) error {
	_, err := r.a.DB.ExecContext(ctx, `UPDATE chat_run_steps SET data=json_set(data,'$.status',?) WHERE run_id=? AND item_id=? AND json_extract(data,'$.status')=?`,
		engine.StepCompleted, rt.run.ID, retryStepID, engine.StepInProgress)
	return err
}

func (r *ChatRunner) finish(rt *chatRuntime, status string) error {
	ctx := context.Background()
	run := rt.run
	run.Status = status
	run.EndedAt = time.Now().UTC()
	if status == domain.RunFailed {
		run.ErrorKey = "error.chat.failed"
	}
	parts := []string{}
	for _, id := range rt.order {
		if text := rt.texts[id]; text != "" {
			parts = append(parts, text)
		}
	}
	text := strings.Join(parts, "\n\n")
	if text == "" && len(rt.loose) > 0 {
		text = joinTexts(looseReply(rt.loose, rt.work))
	}
	if text == "" {
		text = rt.commentary
	}
	tx, err := r.a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stepStatus := status
	if _, err = tx.ExecContext(ctx, `UPDATE chat_run_steps SET data=json_set(data,'$.status',?) WHERE run_id=? AND json_extract(data,'$.status')='in_progress'`, stepStatus, run.ID); err != nil {
		return err
	}
	if text != "" {
		err = sqlite.SaveChatMessage(ctx, tx, domain.ChatMessage{ID: run.ID + "-reply", ChatID: rt.id, RunID: run.ID, Kind: domain.MessageKindAssistant, Text: text, CreatedAt: run.EndedAt})
		if err != nil {
			return err
		}
	}
	if err = sqlite.SaveChatRun(ctx, tx, run); err != nil {
		return err
	}
	requests, err := sqlite.ListChatRequests(ctx, tx, rt.id)
	if err != nil {
		return err
	}
	for _, req := range requests {
		if req.RunID == run.ID && req.State == domain.ApprovalStatePending {
			req.State = domain.ApprovalStateInvalidated
			req.ResponseState = domain.ResponseExpired
			req.Revision++
			if err = sqlite.SaveChatRequest(ctx, tx, req); err != nil {
				return err
			}
		}
	}
	chat, err := sqlite.GetChat(ctx, tx, rt.id)
	if err != nil {
		return err
	}
	chat.UpdatedAt = run.EndedAt
	chat.Revision++
	if err = sqlite.SaveChat(ctx, tx, chat, true); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	rt.run = run
	for _, b := range rt.bindings {
		hookrun.RemoveNativeBinding(r.a.Paths.Root, b[0], b[1])
	}
	rt.bindings = map[string][2]string{}
	platform.RemoveChatLease(r.a.Paths.Root, rt.id)
	return nil
}
func (r *ChatRunner) markUnknown(ctx context.Context, run domain.ChatRun, key string) error {
	run.Status = domain.RunUnknown
	run.ErrorKey = key
	tx, err := r.a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = sqlite.SaveChatRun(ctx, tx, run); err != nil {
		return err
	}
	requests, err := sqlite.ListChatRequests(ctx, tx, run.ChatID)
	if err != nil {
		return err
	}
	for _, req := range requests {
		if req.State == domain.ApprovalStatePending {
			req.State = domain.ApprovalStateInvalidated
			req.ResponseState = domain.ResponseExpired
			req.Revision++
			if err = sqlite.SaveChatRequest(ctx, tx, req); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	platform.RemoveChatLease(r.a.Paths.Root, run.ChatID)
	r.a.chatChanged(ctx, run.ChatID, "chat.run.updated")
	return nil
}
func (r *ChatRunner) exited(rt *chatRuntime) {
	release := r.lock(rt.id)
	defer release()
	if r.runtime(rt.id) != rt {
		return
	}
	if rt.run.ID != "" && domain.ChatRunActive(rt.run.Status) {
		r.report(r.markUnknown(context.Background(), rt.run, "error.chat.unknown"))
	}
	r.mu.Lock()
	if r.runtimes[rt.id] == rt {
		delete(r.runtimes, rt.id)
	}
	r.mu.Unlock()
}

func (r *ChatRunner) approval(rt *chatRuntime, req engine.ApprovalRequest) {
	release := r.lock(rt.id)
	defer release()
	if r.runtime(rt.id) != rt || rt.run.ID == "" || !domain.ChatRunActive(rt.run.Status) || rt.run.Status == domain.RunUnknown || !r.belongs(rt, req.ThreadID) {
		return
	}
	if req.ThreadID == rt.thread && rt.run.TurnID != "" && req.TurnID != rt.run.TurnID {
		return
	}
	requests, err := sqlite.ListChatRequests(r.ctx, r.a.DB, rt.id)
	if err != nil {
		r.report(err)
		return
	}
	for _, v := range requests {
		if v.Epoch == rt.run.Epoch && string(v.EngineID) == string(req.RequestID) {
			return
		}
	}
	card := domain.Approval{Type: domain.ApprovalNativeTool, Method: req.Method, Summary: approvalSummary(req), Options: nativeToolOptions(req.Method)}
	projectNativeFileOptions(&card)
	request := domain.ChatRequest{ID: domain.NewID(), ChatID: rt.id, RunID: rt.run.ID, Type: domain.ApprovalNativeTool, Method: req.Method, Summary: card.Summary, Reason: req.Reason, Options: card.Options, State: domain.ApprovalStatePending, Revision: 1, EngineID: req.RequestID, Epoch: rt.run.Epoch}
	if req.Method == "item/tool/requestUserInput" {
		request.Params = req.RawParams
		request.Options = []domain.ApprovalOption{}
		rt.run.Status = domain.RunWaitingInput
	} else {
		rt.run.Status = domain.RunWaitingApproval
	}
	tx, err := r.a.DB.BeginTx(r.ctx, nil)
	if err != nil {
		r.report(err)
		return
	}
	defer tx.Rollback()
	if err = sqlite.SaveChatRequest(r.ctx, tx, request); err == nil {
		err = sqlite.SaveChatRun(r.ctx, tx, rt.run)
	}
	if err == nil {
		err = tx.Commit()
	}
	r.report(err)
	r.a.chatChanged(r.ctx, rt.id, "chat.request.updated")
}
func (r *ChatRunner) resolveNativeRequest(rt *chatRuntime, id json.RawMessage) {
	requests, err := sqlite.ListChatRequests(r.ctx, r.a.DB, rt.id)
	if err != nil {
		r.report(err)
		return
	}
	for _, req := range requests {
		if req.Epoch == rt.run.Epoch && string(req.EngineID) == string(id) && req.State == domain.ApprovalStatePending {
			req.State = domain.ApprovalStateInvalidated
			req.ResponseState = domain.ResponseExpired
			req.Revision++
			r.report(sqlite.SaveChatRequest(r.ctx, r.a.DB, req))
		}
	}
	r.a.chatChanged(r.ctx, rt.id, "chat.request.updated")
}

func (r *ChatRunner) recordUsage(rt *chatRuntime, n engine.Notification) error {
	var p tokenUsagePayload
	if json.Unmarshal(n.TokenUsage, &p) != nil || p.ThreadID == "" || p.TurnID == "" {
		return nil
	}
	// Keep cumulative thread snapshots per turn. Duplicate notifications replace
	// the same evidence; they never add the same model call twice.
	_, err := r.a.DB.ExecContext(r.ctx, `INSERT INTO chat_usage(chat_id,epoch,thread_id,turn_id,data) VALUES(?,?,?,?,?) ON CONFLICT(epoch,thread_id,turn_id) DO UPDATE SET data=excluded.data`, rt.id, rt.run.Epoch, p.ThreadID, p.TurnID, string(n.TokenUsage))
	return err
}

func (r *ChatRunner) runAction(ctx context.Context, p auth.Principal, id, runID, action string, in ChatActionInput) (domain.ChatRun, error) {
	p, err := WithWriteRequest(p, "chat.run."+action, in.RequestID, struct {
		ChatID, RunID string
		Input         ChatActionInput
	}{id, runID, in})
	if err != nil {
		return domain.ChatRun{}, err
	}
	cached, unlock, err := beginWrite[domain.ChatRun](ctx, r.a, p)
	defer unlock()
	if err != nil {
		return domain.ChatRun{}, err
	}
	if cached != nil {
		return *cached, nil
	}
	release := r.lock(id)
	run, err := sqlite.GetChatRun(ctx, r.a.DB, runID)
	if err != nil {
		release()
		return run, sqlite.NormalizeError(err)
	}
	if run.ChatID != id {
		release()
		return run, domain.Forbidden("error.forbidden.user_only", nil)
	}
	rt := r.runtime(id)
	if action == "interrupt" {
		if !domain.ChatRunActive(run.Status) || run.Status == domain.RunUnknown {
			release()
			return run, domain.Conflict("error.chat.not_running", nil)
		}
		run.StopRequested = true
		if run.Status == "queued" {
			run.Status = domain.RunInterrupted
			run.EndedAt = time.Now().UTC()
		}
		tx, e := r.a.DB.BeginTx(ctx, nil)
		if e != nil {
			release()
			return run, e
		}
		if e = sqlite.SaveChatRun(ctx, tx, run); e == nil {
			e = r.a.saveWriteResult(ctx, tx, p, run)
		}
		if e == nil {
			e = tx.Commit()
		}
		_ = tx.Rollback()
		if e == nil && rt != nil && rt.run.ID == run.ID {
			rt.run = run
		}
		release()
		if e != nil {
			return run, e
		}
		if rt != nil && run.TurnID != "" && rt.server.Epoch() == run.Epoch {
			r.interrupt(rt, run.ThreadID, run.TurnID)
		}
		r.a.chatChanged(ctx, id, "chat.run.updated")
		return run, nil
	}
	if action != "verify" || run.Status != domain.RunUnknown {
		release()
		return run, domain.Conflict("error.chat.not_unknown", nil)
	}
	chat, err := sqlite.GetChat(ctx, r.a.DB, id)
	release()
	if err != nil {
		return run, err
	}
	if rt == nil {
		rt, err = r.ensure(chat)
		if err != nil {
			return run, err
		}
		if _, _, err = r.thread(rt, chat, false); err != nil {
			return run, err
		}
	}
	inspector, ok := rt.server.(engine.TurnInspector)
	if !ok || run.TurnID == "" {
		return run, domain.Conflict("error.recovery.end_unverified", nil)
	}
	status, err := inspector.InspectTurn(ctx, run.ThreadID, run.TurnID)
	if err != nil {
		return run, err
	}
	if status != domain.RunCompleted && status != domain.RunFailed && status != domain.RunInterrupted {
		return run, domain.Conflict("error.recovery.end_unverified", nil)
	}
	release = r.lock(id)
	defer release()
	fresh, err := sqlite.GetChatRun(ctx, r.a.DB, runID)
	if err != nil {
		return run, err
	}
	if fresh.Status != domain.RunUnknown {
		return fresh, nil
	}
	fresh.Status = status
	fresh.EndedAt = time.Now().UTC()
	fresh.ErrorKey = ""
	tx, err := r.a.DB.BeginTx(ctx, nil)
	if err != nil {
		return fresh, err
	}
	defer tx.Rollback()
	if err = sqlite.SaveChatRun(ctx, tx, fresh); err != nil {
		return fresh, err
	}
	if err = r.a.saveWriteResult(ctx, tx, p, fresh); err != nil {
		return fresh, err
	}
	if err = tx.Commit(); err != nil {
		return fresh, err
	}
	rt.run = fresh
	r.a.chatChanged(ctx, id, "chat.run.updated")
	return fresh, nil
}

func (r *ChatRunner) interrupt(rt *chatRuntime, threadID, turnID string) {
	r.spawn(func() {
		ctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
		defer cancel()
		if err := rt.server.InterruptTurn(ctx, threadID, turnID); err != nil {
			release := r.lock(rt.id)
			defer release()
			if r.runtime(rt.id) == rt && domain.ChatRunActive(rt.run.Status) && rt.run.TurnID == turnID {
				r.report(r.markUnknown(context.Background(), rt.run, "error.chat.unknown"))
				rt.run.Status = domain.RunUnknown
			}
		}
	})
}
