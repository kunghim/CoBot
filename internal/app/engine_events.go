package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/hookrun"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// onNotification translates engine events into product facts: assistant
// messages are buffered during the turn and persisted at completion; run and
// bot status always come from these facts, never from UI optimism.
func (s *Scheduler) onNotification(teamID string, source *teamRuntime, n engine.Notification) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Every agent message counts toward the reply bookkeeping, also the
	// working remarks that onProgress takes as steps.
	if n.Method == "item/completed" && n.ItemType == "agentMessage" {
		s.bufferReply(ctx, source, n)
	}
	if s.onProgress(ctx, teamID, source, n) {
		return
	}

	switch n.Method {
	case "turn/started":
		if run, ok := s.runForThread(ctx, source, n.ThreadID, n.TurnID); ok && s.app.Paths.Root != "" {
			if run.Status == domain.RunStarting || run.Status == domain.RunRunning || run.Status == domain.RunWaitingApproval {
				// A sub-agent's tool calls reach the hook under the member's
				// session with the sub-agent's turn, so its turn is bound there too.
				threads := []string{n.ThreadID}
				if root := rootOf(source, n.ThreadID); root != n.ThreadID {
					threads = append(threads, root)
				}
				for _, thread := range threads {
					err := hookrun.WriteNativeBinding(s.app.Paths.Root, hookrun.NativeBinding{
						ThreadID: thread, TurnID: n.TurnID, BotID: run.BotID, RunID: run.ID,
						EngineEpoch: source.server.Epoch(), InstanceID: s.app.InstanceID, ServiceEpoch: s.app.ServiceEpoch,
					})
					if err != nil {
						s.logger.Warn("native hook turn binding unavailable", "error", err)
					}
				}
			}
		}
	case "item/completed":
		// An agent message's text was taken by bufferReply above.
	case "thread/tokenUsage/updated":
		if uerr := s.app.ingestTokenUsage(ctx, teamID, source.server.Epoch(), n.TokenUsage); uerr != nil {
			s.logger.Warn("record usage snapshot", "teamId", teamID, "error", uerr)
		}
	case "turn/completed":
		source.mu.Lock()
		ref, tracked := source.live[liveTurnKey{n.ThreadID, n.TurnID}]
		source.mu.Unlock()
		if tracked && ref.subAgent != "" {
			source.forgetLiveRun(n.ThreadID, n.TurnID)
			if s.app.Paths.Root != "" {
				hookrun.RemoveNativeBinding(s.app.Paths.Root, n.ThreadID, n.TurnID)
				hookrun.RemoveNativeBinding(s.app.Paths.Root, rootOf(source, n.ThreadID), n.TurnID)
			}
			return
		}
		// Steps settle before the run does, so the reload the completion
		// triggers never shows a finished run with a step still running.
		if tracked {
			source.forgetLiveRun(n.ThreadID, n.TurnID)
			status := engine.StepInterrupted
			if n.TurnStatus == "completed" {
				status = engine.StepCompleted
			}
			if err := sqlite.CloseOpenRunSteps(ctx, s.app.DB, ref.runID, status); err != nil {
				s.logger.Warn("settle run steps", "runId", ref.runID, "error", err)
			}
		}
		s.finishRun(ctx, teamID, source, n)
	case "account/updated", "account/login/completed":
		s.app.invalidateEngineStatus()
	case "account/rateLimits/updated":
		s.app.recordRateLimits(n.Raw)
	case "serverRequest/resolved":
		s.app.settleEngineRequest(ctx, teamID, source.server.Epoch(), n.RequestID)
	case "hook/completed":
		var result struct {
			Run struct {
				EventName  string `json:"eventName"`
				Status     string `json:"status"`
				ID         string `json:"id"`
				SourcePath string `json:"sourcePath"`
				DurationMs int64  `json:"durationMs"`
			} `json:"run"`
		}
		if json.Unmarshal(n.Raw, &result) == nil {
			s.logger.Info("native hook completed", "teamId", teamID, "threadId", n.ThreadID, "turnId", n.TurnID, "hookId", result.Run.ID, "event", result.Run.EventName, "status", result.Run.Status, "durationMs", result.Run.DurationMs)
			if filepath.Clean(result.Run.SourcePath) == filepath.Join(s.app.Paths.CodexHome(), "hooks.json") {
				if result.Run.Status == "failed" {
					s.app.hookFailures.Store(teamID, true)
				} else if result.Run.Status == "completed" || result.Run.Status == "blocked" {
					s.app.hookFailures.Delete(teamID)
				}
			}
		}
	default:
		// Other notifications (deltas, progress) are transient by contract.
	}
}

// engineEpoch identifies the app-server lifetime that emitted an event.
// Provider thread ids are only unique inside one engine, so every lookup that
// resolves an event back to a bot must be scoped by it.
func (s *Scheduler) engineEpoch(teamID string) string {
	if rt := s.runtime(teamID); rt != nil && rt.server != nil {
		return rt.server.Epoch()
	}
	return ""
}

func (s *Scheduler) findRunByTurn(ctx context.Context, epoch, threadID, turnID string) string {
	botID, err := s.app.botByThread(ctx, epoch, threadID)
	if err != nil {
		return ""
	}
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return ""
	}
	defer tx.Rollback()
	run, err := sqlite.FindActiveRun(ctx, tx, botID)
	if err != nil {
		return ""
	}
	if run.ProviderTurnID != "" && run.ProviderTurnID != turnID {
		return ""
	}
	if run.EngineEpoch != epoch || turnID == "" {
		return ""
	}
	if run.ProviderTurnID == "" {
		var previous int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE bot_id = ? AND engine_epoch = ? AND provider_turn_id = ? AND id != ?`, botID, epoch, turnID, run.ID).Scan(&previous); err != nil || previous != 0 {
			return ""
		}
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET provider_turn_id = ? WHERE id = ? AND provider_turn_id = ''`, turnID, run.ID); err != nil {
			return ""
		}
	}
	if err := tx.Commit(); err != nil {
		return ""
	}
	return run.ID
}

// finishRun maps turn/completed onto run status, persists the merged
// assistant text and re-opens the bot for the next queued delivery.
func (s *Scheduler) finishRun(ctx context.Context, teamID string, source *teamRuntime, n engine.Notification) {
	botID, err := s.app.botByThread(ctx, source.server.Epoch(), n.ThreadID)
	if err != nil {
		s.logger.Warn("turn completion for unknown thread", "threadId", n.ThreadID, "epoch", source.server.Epoch(), "err", err)
		return
	}
	runID := s.findRunByTurn(ctx, source.server.Epoch(), n.ThreadID, n.TurnID)
	run, err := sqlite.GetRun(ctx, s.app.DB, runID)
	if err != nil {
		s.logger.Warn("turn completion without active run", "threadId", n.ThreadID, "turnId", n.TurnID)
		return
	}
	// Turn ids are only unique inside one engine epoch: a restarted app-server
	// numbers from 1 again, so the stored id of a previous connection must not
	// be used to judge this completion. A stale completion is one whose turn
	// id differs from the stored one *within the same epoch*; anything from a
	// different connection has already been invalidated as unknown, so the
	// completion of the run that is live now is authoritative.
	if n.TurnID != "" && run.ProviderTurnID != "" && run.EngineEpoch == source.server.Epoch() && run.ProviderTurnID != n.TurnID {
		s.logger.Warn("ignoring stale turn completion", "threadId", n.ThreadID, "turnId", n.TurnID, "runTurnId", run.ProviderTurnID)
		return // stale completion of an interrupted predecessor
	}

	status := domain.RunCompleted
	switch n.TurnStatus {
	case "failed":
		status = domain.RunFailed
	case "interrupted":
		status = domain.RunInterrupted
	case "completed":
	default:
		return
	}

	settlement := runSettlement{
		teamID: teamID, botID: botID, run: run, status: status,
		threadID: n.ThreadID, turnID: n.TurnID, epoch: source.server.Epoch(),
		text: takeReply(source, run.ID),
	}
	if err := s.settleRun(ctx, settlement); err != nil {
		// turn/completed arrives once: a lost settlement would leave the
		// member working forever, so it is retried until it lands.
		s.logger.Error("settle finished turn; retrying", "runId", run.ID, "botId", botID, "error", err)
		go s.retrySettle(settlement)
	}
}

// runSettlement is everything needed to record a turn's end; it is kept
// whole so a failed write can be retried after the notification is gone.
type runSettlement struct {
	teamID, botID string
	run           domain.Run
	status        string
	threadID      string
	turnID        string
	epoch         string // the engine connection the turn ran on
	text          string // the turn's reply, empty when it said nothing
}

// takeReply removes and returns the reply buffered for a run.
func takeReply(rt *teamRuntime, runID string) string {
	if rt == nil {
		return ""
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	buf := rt.buffer[runID]
	if buf == nil {
		return ""
	}
	delete(rt.buffer, runID)
	if text := joinTexts(buf.texts); text != "" {
		return text
	}
	return buf.commentary
}

// settleRetryDelays space the retries of a settlement whose write failed.
var settleRetryDelays = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}

func (s *Scheduler) retrySettle(settlement runSettlement) {
	for _, delay := range settleRetryDelays {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(delay):
		}
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		err := s.settleRun(ctx, settlement)
		cancel()
		if err == nil {
			return
		}
		s.logger.Warn("settle finished turn", "runId", settlement.run.ID, "error", err)
	}
	// Still recorded as running: interrupting it from the conversation asks
	// the engine for the turn's real end and settles it then.
	s.logger.Error("finished turn could not be recorded", "runId", settlement.run.ID, "botId", settlement.botID)
}

// settleRun records a turn's end: the run's final status, its reply, the
// member's next state. It is idempotent: a run that is no longer the
// member's active one has been settled already.
func (s *Scheduler) settleRun(ctx context.Context, st runSettlement) error {
	run, teamID, botID := st.run, st.teamID, st.botID
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	fresh, err := sqlite.FindActiveRun(ctx, tx, botID)
	if errors.Is(err, sqlite.ErrNotFound) || (err == nil && fresh.ID != run.ID) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, st.status, st.turnID, st.epoch, "", true); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE approvals SET request_state = ?, revision = revision + 1 WHERE run_id = ? AND type = ? AND request_state = ?`, domain.ApprovalStateInvalidated, run.ID, domain.ApprovalNativeTool, domain.ApprovalStatePending); err != nil {
		return err
	}
	// A finished turn that never reported tokens is an unknown spend, not a
	// zero one: a budget must pause rather than trust it. A usage row
	// that already arrived keeps its real numbers.
	epoch := run.EngineEpoch
	if epoch == "" {
		epoch = st.epoch
	}
	turnID := st.turnID
	if turnID == "" {
		turnID = run.ProviderTurnID
	}
	if err := s.app.MarkUsageUnknownIfMissing(ctx, tx, teamID, botID, epoch, st.threadID, turnID); err != nil {
		return err
	}
	assistantID := ""
	if st.text != "" {
		blocks, _ := json.Marshal([]domain.TextBlock{{Type: "text", Text: st.text}})
		assistant := domain.Message{
			ID: domain.NewID(), TeamID: teamID, BotID: botID,
			Kind: domain.MessageKindAssistant, Blocks: blocks, RunID: run.ID,
			CreatedAt: time.Now().UTC(),
		}
		if err := sqlite.InsertMessage(ctx, tx, assistant); err != nil {
			return err
		}
		assistantID = assistant.ID
	}
	nextStatus := domain.BotStatusIdle
	if queued, err := sqlite.CountQueuedDeliveries(ctx, tx, botID); err == nil && queued > 0 {
		nextStatus = domain.BotStatusQueued
		if eligible, err := sqlite.ListDispatchableDeliveries(ctx, tx, botID, 1); err == nil && len(eligible) == 0 {
			nextStatus = domain.BotStatusWaitingInput
		}
	} else {
		var questions int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM questions WHERE requester_bot_id = ? AND status = 'open' AND kind = ''`, botID).Scan(&questions); err == nil && questions > 0 {
			nextStatus = domain.BotStatusWaitingInput
		}
	}
	// A turn that ended with the member's card still open, and nothing else
	// coming, gets one reminder to hand it on.
	if st.status == domain.RunCompleted && nextStatus == domain.BotStatusIdle {
		reminded, err := remindUnhandedCardsLocked(ctx, tx, teamID, botID, run.ID)
		if err != nil {
			return err
		}
		if reminded {
			nextStatus = domain.BotStatusQueued
		}
	}
	if err := sqlite.SetBotRunState(ctx, tx, botID, nextStatus, ""); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.app.CollaborationTokens != nil {
		s.app.CollaborationTokens.RevokeRun(run.ID)
	}
	if s.app.Paths.Root != "" {
		hookrun.RemoveNativeBinding(s.app.Paths.Root, st.threadID, turnID)
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, TeamID: teamID, BotID: botID, ResourceID: run.ID})
	messageResource := assistantID
	if messageResource == "" {
		messageResource = botID
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventMessageUpdated, TeamID: teamID, BotID: botID, ResourceID: messageResource})
	s.app.Hub.Publish()
	s.Wake(botID) // pick up the next queued delivery without model help
	return nil
}

// joinTexts keeps each reply segment of a turn (a note before the work, the
// final answer) its own paragraph.
func joinTexts(parts []string) string {
	return strings.Join(parts, "\n\n")
}

// onApproval persists a native tool approval and projects it to the member
// and leader sessions through the shared pending list.
func (s *Scheduler) onApproval(teamID string, source *teamRuntime, req engine.ApprovalRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// A sub-agent asks on behalf of the member whose thread spawned it.
	botID, err := s.app.botByThread(ctx, source.server.Epoch(), rootOf(source, req.ThreadID))
	if err != nil {
		s.logger.Error("approval for unknown thread", "threadId", req.ThreadID, "method", req.Method)
		// Cannot route: decline explicitly rather than hang the engine.
		_ = source.server.ReplyApproval(ctx, req.RequestID, map[string]any{"decision": domain.DecisionDeclineEngine})
		return
	}
	run, found := s.runForThread(ctx, source, req.ThreadID, req.TurnID)
	if !found || run.BotID != botID {
		_ = source.server.ReplyApproval(ctx, req.RequestID, map[string]any{"decision": domain.DecisionDeclineEngine})
		return
	}

	approval := domain.Approval{
		ID: domain.NewID(), TeamID: teamID, RequesterBotID: botID,
		RunID: runIDOrEmpty(run), EngineRequestID: req.RequestID, EngineEpoch: "",
		Type: domain.ApprovalNativeTool, Method: req.Method,
		Summary: approvalSummary(req), Reason: req.Reason,
		Options:  nativeToolOptions(req.Method),
		Revision: 1, CreatedAt: time.Now().UTC(),
	}
	projectNativeFileOptions(&approval)
	if rt := source; rt != nil {
		approval.EngineEpoch = rt.server.Epoch()
	}
	stored, err := s.storeApproval(ctx, botID, run, approval, req)
	if err != nil {
		// The engine waits for an answer to every request: one that cannot
		// become a card is declined, never left hanging with the turn.
		s.logger.Error("approval could not be recorded; declined", "botId", botID, "method", req.Method, "error", err)
		_ = source.server.ReplyApproval(ctx, req.RequestID, map[string]any{"decision": domain.DecisionDeclineEngine})
		return
	}
	if !stored {
		return // the card exists already
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventApprovalCreated, TeamID: teamID, BotID: botID, ResourceID: approval.ID, Revision: approval.Revision})
	s.app.Hub.Publish()
}

// errRunEnded reports an approval that arrived after its run stopped being
// the member's live one.
var errRunEnded = errors.New("run is no longer active")

// storeApproval writes a native approval card and pauses its run. It reports
// false when the same request already has a card.
func (s *Scheduler) storeApproval(ctx context.Context, botID string, run domain.Run, approval domain.Approval, req engine.ApprovalRequest) (bool, error) {
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var duplicate int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM approvals WHERE engine_epoch = ? AND engine_request_id = ?`, approval.EngineEpoch, string(req.RequestID)).Scan(&duplicate); err != nil {
		return false, err
	}
	if duplicate != 0 {
		return false, nil
	}
	if fresh, err := sqlite.FindActiveRun(ctx, tx, botID); err != nil {
		return false, err
	} else if fresh.ID != run.ID {
		return false, errRunEnded
	}
	if err := sqlite.InsertApproval(ctx, tx, approval); err != nil {
		return false, err
	}
	if run.ID != "" {
		// Pausing for approval keeps the engine's own turn id: only the status
		// changes, so the eventual turn/completed still matches this run.
		if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunWaitingApproval, run.ProviderTurnID, run.EngineEpoch, "", false); err != nil {
			return false, err
		}
	}
	if err := sqlite.SetBotRunState(ctx, tx, botID, domain.BotStatusWaitingApproval, runIDOrEmpty(run)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func runIDOrEmpty(run domain.Run) string {
	if run.ID == "" {
		return ""
	}
	return run.ID
}

func approvalSummary(req engine.ApprovalRequest) string {
	// The complete requested operation is needed for a meaningful decision.
	// Keep only relevant public fields; never persist the transport envelope.
	details := map[string]any{}
	if req.Command != "" {
		details["command"] = req.Command
	}
	if req.Cwd != "" {
		details["cwd"] = req.Cwd
	}
	if len(req.Paths) > 0 {
		details["paths"] = req.Paths
	}
	if len(req.Changes) > 0 {
		details["changes"] = req.Changes
	}
	if len(req.Permissions) > 0 {
		details["permissions"] = req.Permissions
	}
	if req.Network {
		details["network"] = true
	}
	if len(details) == 0 {
		return req.Method
	}
	value, _ := json.MarshalIndent(details, "", "  ")
	return string(value)
}

// nativeToolOptions are the choices Codex itself offers. "For this session"
// lasts for the member's engine session: Codex keeps the grant until its
// thread is restarted, and nothing in CoBot can widen it further.
func nativeToolOptions(method string) []domain.ApprovalOption {
	if method == "item/permissions/requestApproval" {
		return []domain.ApprovalOption{
			{ID: domain.DecisionGrantTurn, MessageKey: "approval.option.grant_turn"},
			{ID: domain.DecisionGrantSession, MessageKey: "approval.option.grant_session"},
			{ID: "deny", MessageKey: "approval.option.permissions.deny"},
		}
	}
	return []domain.ApprovalOption{
		{ID: domain.DecisionAcceptEngine, MessageKey: "approval.option.allow_once"},
		{ID: domain.DecisionAcceptSession, MessageKey: "approval.option.allow_session"},
		{ID: domain.DecisionDeclineEngine, MessageKey: "approval.option.decline"},
		{ID: domain.DecisionCancelEngine, MessageKey: "approval.option.decline_stop"},
	}
}

// onServerExitRuntime marks the team degraded when THIS runtime dies: pending
// approvals of its epoch are invalidated, active runs become unknown (not
// assumed stopped), and no automatic restart happens. The runtime identity
// check keeps a stale loser's exit (a raced EnsureTeamServer that lost, then
// got shut down) from deleting the winner or invalidating the live
// connection's cards (review B).
func (s *Scheduler) onServerExitRuntime(teamID string, rt *teamRuntime, err error) {
	s.mu.Lock()
	if rt == nil || rt.server == nil || s.teams[teamID] != rt {
		s.mu.Unlock()
		s.logger.Warn("ignoring engine exit from a superseded runtime", "teamId", teamID, "error", err)
		return
	}
	delete(s.teams, teamID)
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s.logger.Warn("team app-server exited", "teamId", teamID, "error", err)

	runs, rerr := sqlite.ListActiveRunsByTeam(ctx, s.app.DB, teamID)
	if rerr != nil {
		return
	}
	for _, run := range runs {
		if run.EngineEpoch != rt.server.Epoch() {
			continue
		}
		tx, txErr := s.app.DB.BeginTx(ctx, nil)
		if txErr != nil {
			continue
		}
		fresh, readErr := sqlite.GetRun(ctx, tx, run.ID)
		if readErr != nil || fresh.Status == domain.RunCompleted || fresh.Status == domain.RunFailed || fresh.Status == domain.RunInterrupted {
			tx.Rollback()
			continue
		}
		run = fresh
		if uerr := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunUnknown, run.ProviderTurnID, run.EngineEpoch, "error.engine.connection_lost", false); uerr != nil {
			tx.Rollback()
			continue
		}
		if uerr := sqlite.SetBotRunState(ctx, tx, run.BotID, domain.BotStatusUnknown, run.ID); uerr != nil {
			tx.Rollback()
			continue
		}
		deliveryID, uerr := sqlite.MarkDeliveryUnknownByRun(ctx, tx, run.ID)
		if uerr != nil {
			tx.Rollback()
			continue
		}
		cardID, uerr := ensureRecoveryCardLocked(ctx, tx, run, deliveryID)
		if uerr != nil {
			tx.Rollback()
			continue
		}
		if cerr := tx.Commit(); cerr != nil {
			continue
		}
		if cardID != "" {
			if card, err := sqlite.GetApproval(ctx, s.app.DB, cardID); err == nil {
				s.app.publishApprovalUpdated(ctx, card)
			}
		}
		if s.app.CollaborationTokens != nil {
			s.app.CollaborationTokens.RevokeRun(run.ID)
		}
		s.app.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, TeamID: teamID, BotID: run.BotID, ResourceID: run.ID})
	}
	// Pending approvals bound to the dead connection can never be answered
	// from it; invalidate them now instead of leaving cards that only fail
	// later.
	if rt != nil && rt.server != nil {
		epoch := rt.server.Epoch()
		// Collect this connection's pending cards first — the invalidate step
		// removes them from the pending set — then expire their responses and
		// publish each one so history shows why the card stopped.
		if pending, lerr := sqlite.ListPendingApprovalsOfType(ctx, s.app.DB, domain.ApprovalNativeTool); lerr == nil {
			var mine []domain.Approval
			for _, approval := range pending {
				if approval.EngineEpoch == epoch {
					mine = append(mine, approval)
				}
			}
			if n, ierr := sqlite.InvalidateApprovalsOfEpoch(ctx, s.app.DB, epoch); ierr == nil && n > 0 {
				for _, approval := range mine {
					_ = sqlite.EnsureApprovalResponseState(ctx, s.app.DB, approval.ID, domain.ResponseExpired, "engine_exited")
					s.app.publishApprovalUpdated(ctx, approval)
				}
			}
		}
	}
	s.app.Hub.Publish()
}

// InterruptRun requests an engine-side stop for one run. Only that bot's
// turn is affected; the rest of the team keeps running.
func (a *App) InterruptRun(ctx context.Context, runID string) (string, error) {
	run, err := sqlite.GetRun(ctx, a.DB, runID)
	if err != nil {
		return "", sqlite.NormalizeError(err)
	}
	switch run.Status {
	case domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval, domain.RunWaitingInput:
	default:
		return "", domain.Conflict("error.run.not_active", map[string]any{"status": run.Status})
	}
	botID := run.BotID
	team, _, err := sqlite.GetTeam(ctx, a.DB, run.TeamID)
	if err != nil {
		return "", err
	}
	rt := a.Scheduler.runtime(team.ID)
	if rt == nil {
		return "", domain.Conflict("error.engine.not_running", nil)
	}
	session, err := sqlite.GetProviderSession(ctx, a.DB, botID)
	if err != nil {
		return "", err
	}
	if run.ProviderTurnID == "" {
		return "", domain.Conflict("error.run.not_active", map[string]any{"status": "starting"})
	}
	if err := rt.server.InterruptTurn(ctx, session.ProviderThreadID, run.ProviderTurnID); err != nil {
		// The engine may refuse because the turn already ended and its end was
		// never recorded here. Its own record of the turn settles the run.
		if a.Scheduler.settleEndedTurn(ctx, rt, run, session.ProviderThreadID) {
			return run.ID, nil
		}
		return "", domain.EngineUnavailable("error.engine.interrupt_failed", nil, err)
	}
	// The authoritative terminal state arrives with turn/completed.
	return run.ID, nil
}

// settleEndedTurn records the end of a run whose turn the engine reports as
// finished. It reports whether the run was settled.
func (s *Scheduler) settleEndedTurn(ctx context.Context, rt *teamRuntime, run domain.Run, threadID string) bool {
	inspector, ok := rt.server.(engine.TurnInspector)
	if !ok || run.EngineEpoch != rt.server.Epoch() {
		return false
	}
	state, err := inspector.InspectTurn(ctx, threadID, run.ProviderTurnID)
	if err != nil {
		return false
	}
	status := ""
	switch state {
	case "completed":
		status = domain.RunCompleted
	case "failed":
		status = domain.RunFailed
	case "interrupted":
		status = domain.RunInterrupted
	default:
		return false // still running: the interrupt failed for another reason
	}
	rt.forgetLiveRun(threadID, run.ProviderTurnID)
	stepStatus := engine.StepInterrupted
	if status == domain.RunCompleted {
		stepStatus = engine.StepCompleted
	}
	if err := sqlite.CloseOpenRunSteps(ctx, s.app.DB, run.ID, stepStatus); err != nil {
		s.logger.Warn("settle run steps", "runId", run.ID, "error", err)
	}
	err = s.settleRun(ctx, runSettlement{
		teamID: run.TeamID, botID: run.BotID, run: run, status: status,
		threadID: threadID, turnID: run.ProviderTurnID, epoch: rt.server.Epoch(),
		text: takeReply(rt, run.ID),
	})
	if err != nil {
		s.logger.Warn("settle ended turn", "runId", run.ID, "error", err)
		return false
	}
	s.logger.Info("settled a turn the engine had already ended", "runId", run.ID, "status", status)
	return true
}

func (a *App) botByThread(ctx context.Context, epoch, threadID string) (string, error) {
	return sqlite.FindBotByThread(ctx, a.DB, epoch, threadID)
}

// EngineStatusInvalidated drops the cached engine projection (login changes).
func (a *App) EngineStatusInvalidated() { a.invalidateEngineStatus() }

func (a *App) invalidateEngineStatus() {
	a.engineStatusMu.Lock()
	a.engineStatusCache = nil
	a.engineStatusMu.Unlock()
}

// bufferReply keeps a turn's answer as it arrives: final-answer text becomes
// the reply; a working remark is only remembered as the last thing said, the
// reply of a turn that ends without an answer.
func (s *Scheduler) bufferReply(ctx context.Context, source *teamRuntime, n engine.Notification) {
	if n.TurnID == "" || source == nil {
		return
	}
	runID := s.findRunByTurn(ctx, source.server.Epoch(), n.ThreadID, n.TurnID)
	if runID == "" {
		return
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	buf := source.buffer[runID]
	if buf == nil {
		buf = &runBuffer{items: map[string]bool{}}
		source.buffer[runID] = buf
	}
	if n.ItemID != "" && buf.items[n.ItemID] {
		return
	}
	if n.Phase == "commentary" {
		buf.commentary = n.Text
	} else {
		buf.texts = append(buf.texts, n.Text)
	}
	buf.items[n.ItemID] = true
}
