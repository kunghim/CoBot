package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// onNotification translates engine events into product facts: assistant
// messages are buffered during the turn and persisted at completion; run and
// bot status always come from these facts, never from UI optimism.
func (s *Scheduler) onNotification(teamID string, n engine.Notification) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch n.Method {
	case "item/completed":
		if n.ItemType == "agentMessage" && n.TurnID != "" {
			epoch := s.engineEpoch(teamID)
			if runID := s.findRunByTurn(ctx, epoch, n.ThreadID, n.TurnID); runID != "" {
				if rt := s.runtime(teamID); rt != nil {
					rt.mu.Lock()
					buf := rt.buffer[runID]
					if buf == nil {
						buf = &runBuffer{}
						rt.buffer[runID] = buf
					}
					buf.texts = append(buf.texts, n.Text)
					rt.mu.Unlock()
				}
			}
		}
	case "turn/completed":
		s.finishRun(ctx, teamID, n)
	case "account/updated", "account/login/completed":
		s.app.invalidateEngineStatus()
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
	run, err := sqlite.FindActiveRun(ctx, s.app.DB, botID)
	if err != nil {
		return ""
	}
	if run.ProviderTurnID != "" && run.ProviderTurnID != turnID {
		return ""
	}
	return run.ID
}

// finishRun maps turn/completed onto run status, persists the merged
// assistant text and re-opens the bot for the next queued delivery.
func (s *Scheduler) finishRun(ctx context.Context, teamID string, n engine.Notification) {
	botID, err := s.app.botByThread(ctx, s.engineEpoch(teamID), n.ThreadID)
	if err != nil {
		s.logger.Warn("turn completion for unknown thread", "threadId", n.ThreadID)
		return
	}
	run, err := sqlite.FindActiveRun(ctx, s.app.DB, botID)
	if err != nil {
		s.logger.Warn("turn completion without active run", "threadId", n.ThreadID, "turnId", n.TurnID)
		return
	}
	if n.TurnID != "" && run.ProviderTurnID != "" && run.ProviderTurnID != n.TurnID {
		s.logger.Warn("ignoring stale turn completion", "threadId", n.ThreadID, "turnId", n.TurnID, "runTurnId", run.ProviderTurnID)
		return // stale completion of an interrupted predecessor
	}

	status := domain.RunCompleted
	switch n.TurnStatus {
	case "failed":
		status = domain.RunFailed
	case "interrupted":
		status = domain.RunInterrupted
	case "inProgress":
		return
	}

	var text string
	if rt := s.runtime(teamID); rt != nil {
		rt.mu.Lock()
		if buf := rt.buffer[run.ID]; buf != nil {
			text = joinTexts(buf.texts)
			delete(rt.buffer, run.ID)
		}
		rt.mu.Unlock()
	}

	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, status, n.TurnID, "", true); err != nil {
		return
	}
	if text != "" {
		blocks, _ := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
		assistant := domain.Message{
			ID: domain.NewID(), TeamID: teamID, BotID: botID,
			Kind: domain.MessageKindAssistant, Blocks: blocks, RunID: run.ID,
			CreatedAt: time.Now().UTC(),
		}
		if err := sqlite.InsertMessage(ctx, tx, assistant); err != nil {
			return
		}
	}
	nextStatus := domain.BotStatusIdle
	if queued, err := sqlite.CountQueuedDeliveries(ctx, tx, botID); err == nil && queued > 0 {
		nextStatus = domain.BotStatusQueued
	}
	if err := sqlite.SetBotRunState(ctx, tx, botID, nextStatus, ""); err != nil {
		return
	}
	if err := tx.Commit(); err != nil {
		return
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, TeamID: teamID, BotID: botID, ResourceID: run.ID})
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventMessageUpdated, TeamID: teamID, BotID: botID, ResourceID: botID})
	s.app.Hub.Publish()
	s.Wake(botID) // pick up the next queued delivery without model help
}

func joinTexts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ""
		}
		out += p
	}
	return out
}

// onApproval persists a native tool approval and projects it to the member
// and leader sessions through the shared pending list.
func (s *Scheduler) onApproval(teamID string, req engine.ApprovalRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	botID, err := s.app.botByThread(ctx, s.engineEpoch(teamID), req.ThreadID)
	if err != nil {
		s.logger.Error("approval for unknown thread", "threadId", req.ThreadID, "method", req.Method)
		// Cannot route: decline explicitly rather than hang the engine.
		_ = s.replyApproval(ctx, teamID, req, map[string]any{"decision": domain.DecisionDeclineEngine})
		return
	}
	run, _ := sqlite.FindActiveRun(ctx, s.app.DB, botID)

	approval := domain.Approval{
		ID: domain.NewID(), TeamID: teamID, RequesterBotID: botID,
		RunID: runIDOrEmpty(run), EngineRequestID: req.RequestID, EngineEpoch: "",
		Type: domain.ApprovalNativeTool, Method: req.Method,
		Summary: approvalSummary(req), Reason: req.Reason,
		Options:  nativeToolOptions(req.Method),
		Revision: 1, CreatedAt: time.Now().UTC(),
	}
	if rt := s.runtime(teamID); rt != nil {
		approval.EngineEpoch = rt.server.Epoch()
	}
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if err := sqlite.InsertApproval(ctx, tx, approval); err != nil {
		return
	}
	if run.ID != "" {
		if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunWaitingApproval, "", "", false); err != nil {
			return
		}
	}
	if err := sqlite.SetBotRunState(ctx, tx, botID, domain.BotStatusWaitingApproval, runIDOrEmpty(run)); err != nil {
		return
	}
	if err := tx.Commit(); err != nil {
		return
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventApprovalCreated, TeamID: teamID, BotID: botID, ResourceID: approval.ID, Revision: approval.Revision})
	s.app.Hub.Publish()
}

func runIDOrEmpty(run domain.Run) string {
	if run.ID == "" {
		return ""
	}
	return run.ID
}

func approvalSummary(req engine.ApprovalRequest) string {
	switch {
	case req.Command != "":
		return truncate(req.Command, 300)
	case len(req.Paths) > 0:
		out := ""
		for i, p := range req.Paths {
			if i > 0 {
				out += ", "
			}
			out += p
			if i >= 4 {
				out += ", …"
				break
			}
		}
		return truncate(out, 300)
	default:
		return req.Method
	}
}

func nativeToolOptions(method string) []domain.ApprovalOption {
	if method == "item/permissions/requestApproval" {
		// The grant response shape is not verified on the target engine
		// version, so only the verified deny shape is offered (F07 rule:
		// an option that cannot be expressed accurately stays disabled).
		return []domain.ApprovalOption{
			{ID: "deny", MessageKey: "approval.option.permissions.deny"},
		}
	}
	return []domain.ApprovalOption{
		{ID: domain.DecisionAcceptEngine, MessageKey: "approval.option.allow_once"},
		{ID: "acceptForSession", MessageKey: "approval.option.allow_thread_session"},
		{ID: domain.DecisionDeclineEngine, MessageKey: "approval.option.decline"},
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// onServerExit marks the team degraded: pending approvals of this epoch are
// invalidated, active runs become unknown (not assumed stopped), and no
// automatic restart happens.
func (s *Scheduler) onServerExit(teamID string, err error) {
	s.mu.Lock()
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
		tx, txErr := s.app.DB.BeginTx(ctx, nil)
		if txErr != nil {
			continue
		}
		if uerr := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunUnknown, "", "error.engine.connection_lost", true); uerr != nil {
			tx.Rollback()
			continue
		}
		if uerr := sqlite.SetBotRunState(ctx, tx, run.BotID, domain.BotStatusUnknown, run.ID); uerr != nil {
			tx.Rollback()
			continue
		}
		if cerr := tx.Commit(); cerr != nil {
			continue
		}
		s.app.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, TeamID: teamID, BotID: run.BotID, ResourceID: run.ID})
	}
	s.app.Hub.Publish()
}

// ReplyApproval delivers the engine response for a decided approval and
// tracks queued/sent/unknown truthfully.
func (s *Scheduler) replyApproval(ctx context.Context, teamID string, req engine.ApprovalRequest, response map[string]any) error {
	rt := s.runtime(teamID)
	if rt == nil {
		return errors.New("team server is not running")
	}
	return rt.server.ReplyApproval(ctx, req.RequestID, response)
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
	if err := rt.server.InterruptTurn(ctx, session.ProviderThreadID); err != nil {
		return "", domain.EngineUnavailable("error.engine.interrupt_failed", nil, err)
	}
	// The authoritative terminal state arrives with turn/completed.
	return run.ID, nil
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
