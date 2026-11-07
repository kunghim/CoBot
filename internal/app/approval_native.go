package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

func (a *App) decideNativeApproval(ctx context.Context, p auth.Principal, id, option, comment string, expected int64, content map[string]any) (domain.Approval, error) {
	cached, unlock, err := beginWrite[domain.Approval](ctx, a, p)
	defer unlock()
	if err != nil {
		return domain.Approval{}, err
	}
	if cached != nil {
		return sqlite.GetApproval(ctx, a.DB, cached.ID)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Approval{}, err
	}
	defer tx.Rollback()
	approval, err := sqlite.GetApproval(ctx, tx, id)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}
	projectNativeFileOptions(&approval)
	if (option == domain.DecisionAcceptEngine || option == domain.DecisionAcceptSession) && approval.Method == "item/fileChange/requestApproval" && !hasFilePreview(approval) {
		return domain.Approval{}, domain.Conflict("error.approval.file_preview_unavailable", nil)
	}
	response, err := engineResponseFor(approval, option, content)
	if err != nil {
		return domain.Approval{}, err
	}
	approval, err = sqlite.DecideApprovalTx(ctx, tx, id, option, comment, p.UserID, expected)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, p, approval); err != nil {
		return domain.Approval{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Approval{}, err
	}
	var rt *teamRuntime
	if a.Scheduler != nil {
		rt = a.Scheduler.runtime(approval.TeamID)
	}
	if rt == nil {
		_ = sqlite.SetApprovalResponseState(ctx, a.DB, id, domain.ResponseUnknown, "team_server_not_running")
	} else if rt.server.Epoch() != approval.EngineEpoch {
		_ = sqlite.SetApprovalResponseState(ctx, a.DB, id, domain.ResponseExpired, "engine_epoch_changed")
	} else {
		replyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = rt.server.ReplyApproval(replyCtx, approval.EngineRequestID, response)
		cancel()
		if err != nil {
			_ = sqlite.SetApprovalResponseState(ctx, a.DB, id, domain.ResponseUnknown, "reply_failed")
		} else {
			_ = sqlite.SetApprovalResponseState(ctx, a.DB, id, domain.ResponseSent, "")
			a.resumeAfterNativeReply(ctx, approval, rt.server.Epoch())
		}
	}
	a.publishApprovalUpdated(ctx, approval)
	return sqlite.GetApproval(ctx, a.DB, id)
}

func hasFilePreview(card domain.Approval) bool {
	var details struct {
		Paths   []string `json:"paths"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	}
	if json.Unmarshal([]byte(card.Summary), &details) != nil {
		return false
	}
	return len(details.Paths) > 0 || len(details.Changes) > 0
}

func projectNativeFileOptions(card *domain.Approval) {
	if card.Type != domain.ApprovalNativeTool || card.Method != "item/fileChange/requestApproval" || hasFilePreview(*card) {
		return
	}
	for i := range card.Options {
		if card.Options[i].ID == domain.DecisionAcceptEngine || card.Options[i].ID == domain.DecisionAcceptSession {
			card.Options[i].Disabled = true
			card.Options[i].DisabledReasonKey = "error.approval.file_preview_unavailable"
		}
	}
}

// A completion or a second approval can arrive before ReplyApproval returns.
// Re-read in the transaction and never undo either newer fact.
func (a *App) resumeAfterNativeReply(ctx context.Context, approval domain.Approval, epoch string) {
	if approval.RunID == "" {
		return
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	run, err := sqlite.GetRun(ctx, tx, approval.RunID)
	if err != nil || run.Status != domain.RunWaitingApproval || run.EngineEpoch != epoch {
		return
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM approvals WHERE run_id = ? AND type = ? AND request_state = ?`, run.ID, domain.ApprovalNativeTool, domain.ApprovalStatePending).Scan(&pending); err != nil || pending > 0 {
		return
	}
	if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunRunning, run.ProviderTurnID, run.EngineEpoch, "", false); err != nil {
		return
	}
	if err := setMemberStateTx(ctx, tx, run.BotID, domain.BotStatusWorking, run.ID); err != nil {
		return
	}
	_ = tx.Commit()
}

func approvalSummary(req engine.ApprovalRequest) string {
	// The complete requested operation is needed for a meaningful decision.
	// Keep only relevant public fields; never persist the transport envelope.
	if req.Method == "mcpServer/elicitation/request" {
		// What the MCP server asks, verbatim: its words, and the form or the
		// address it wants acted on.
		var payload struct {
			ServerName      string          `json:"serverName"`
			Message         string          `json:"message"`
			Mode            string          `json:"mode"`
			RequestedSchema json.RawMessage `json:"requestedSchema"`
			URL             string          `json:"url"`
		}
		if json.Unmarshal(req.RawParams, &payload) == nil {
			details := map[string]any{"server": payload.ServerName, "mode": payload.Mode, "message": payload.Message}
			if len(payload.RequestedSchema) > 0 && string(payload.RequestedSchema) != "null" {
				details["requestedSchema"] = payload.RequestedSchema
			}
			if payload.URL != "" {
				details["url"] = payload.URL
			}
			value, _ := json.MarshalIndent(details, "", "  ")
			return string(value)
		}
	}
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
	if method == "mcpServer/elicitation/request" {
		return []domain.ApprovalOption{
			{ID: domain.DecisionAcceptEngine, MessageKey: "approval.option.elicit_submit"},
			{ID: domain.DecisionDeclineEngine, MessageKey: "approval.option.elicit_decline"},
		}
	}
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
