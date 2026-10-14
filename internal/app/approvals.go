package app

import (
	"context"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// PendingApprovals returns the undecided cards visible to the user. The
// leader sees the whole team; the member projection filters by requester and
// is applied server-side (MCP identities get their own scope later).
func (a *App) PendingApprovals(ctx context.Context, principal auth.Principal, teamID string, requesterBotID string) ([]domain.Approval, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	approvals, err := sqlite.ListApprovals(ctx, a.DB, teamID, domain.ApprovalStatePending, 100)
	if err != nil {
		return nil, err
	}
	if requesterBotID != "" {
		filtered := approvals[:0]
		for _, approval := range approvals {
			if approval.RequesterBotID == requesterBotID {
				filtered = append(filtered, approval)
			}
		}
		approvals = filtered
	}
	// Oldest first so the pager is stable.
	for i, j := 0, len(approvals)-1; i < j; i, j = i+1, j-1 {
		approvals[i], approvals[j] = approvals[j], approvals[i]
	}
	return approvals, nil
}

// ApprovalHistory returns decided/invalidated records, newest first.
func (a *App) ApprovalHistory(ctx context.Context, principal auth.Principal, teamID string, limit int) ([]domain.Approval, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	decided, err := sqlite.ListApprovals(ctx, a.DB, teamID, domain.ApprovalStateDecided, limit)
	if err != nil {
		return nil, err
	}
	invalidated, err := sqlite.ListApprovals(ctx, a.DB, teamID, domain.ApprovalStateInvalidated, limit)
	if err != nil {
		return nil, err
	}
	out := append(decided, invalidated...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// DecideApproval records the user's decision with CAS and delivers the
// engine response. The decision survives a failed delivery; the response
// state truthfully reports queued/sent/unknown/expired.
func (a *App) DecideApproval(ctx context.Context, principal auth.Principal, approvalID, optionID, comment string, expectedRevision int64) (domain.Approval, error) {
	if principal.Kind != auth.KindUser {
		return domain.Approval{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	approval, err := sqlite.DecideApproval(ctx, a.DB, approvalID, optionID, comment, principal.UserID, expectedRevision)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}

	response, err := engineResponseFor(approval.Type, approval.Method, optionID)
	if err != nil {
		// Recorded decision stands; nothing engine-bound to deliver.
		_ = sqlite.SetApprovalResponseState(ctx, a.DB, approvalID, domain.ResponseExpired, "not_engine_bound")
		a.publishApprovalUpdated(ctx, approval)
		return sqlite.GetApproval(ctx, a.DB, approvalID)
	}

	rt := a.Scheduler.runtime(approval.TeamID)
	if rt == nil {
		_ = sqlite.SetApprovalResponseState(ctx, a.DB, approvalID, domain.ResponseUnknown, "team_server_not_running")
		a.publishApprovalUpdated(ctx, approval)
		return sqlite.GetApproval(ctx, a.DB, approvalID)
	}
	// A restarted connection must never answer old request ids (F04).
	if approval.EngineEpoch != "" && rt.server.Epoch() != approval.EngineEpoch {
		_ = sqlite.SetApprovalResponseState(ctx, a.DB, approvalID, domain.ResponseExpired, "engine_epoch_changed")
		a.publishApprovalUpdated(ctx, approval)
		return sqlite.GetApproval(ctx, a.DB, approval.ID)
	}

	replyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := rt.server.ReplyApproval(replyCtx, approval.EngineRequestID, response); err != nil {
		_ = sqlite.SetApprovalResponseState(ctx, a.DB, approvalID, domain.ResponseUnknown, "reply_failed")
	} else {
		_ = sqlite.SetApprovalResponseState(ctx, a.DB, approvalID, domain.ResponseSent, "")
	}
	a.publishApprovalUpdated(ctx, approval)

	// The engine resumes the run by itself after a reply; bot status returns
	// to working until turn/completed says otherwise.
	if approval.RunID != "" {
		if run, err := sqlite.GetRun(ctx, a.DB, approval.RunID); err == nil && run.Status == domain.RunWaitingApproval {
			tx, txErr := a.DB.BeginTx(ctx, nil)
			if txErr == nil {
				if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunRunning, "", "", false); err == nil {
					if err := sqlite.SetBotRunState(ctx, tx, approval.RequesterBotID, domain.BotStatusWorking, run.ID); err == nil {
						_ = tx.Commit()
					} else {
						tx.Rollback()
					}
				} else {
					tx.Rollback()
				}
			}
		}
	}
	return sqlite.GetApproval(ctx, a.DB, approvalID)
}

func (a *App) publishApprovalUpdated(ctx context.Context, approval domain.Approval) {
	_ = a.recordEvent(ctx, domain.Event{
		Type: domain.EventApprovalUpdated, TeamID: approval.TeamID,
		BotID: approval.RequesterBotID, ResourceID: approval.ID, Revision: approval.Revision,
	})
	a.Hub.Publish()
}

// engineResponseFor maps the stored option onto the exact native response.
func engineResponseFor(approvalType, method, optionID string) (map[string]any, error) {
	if approvalType != domain.ApprovalNativeTool {
		return nil, nil // non-engine cards have no protocol reply
	}
	switch method {
	case "item/permissions/requestApproval":
		// Only the deny shape is verified on the target version; a grant
		// response cannot be expressed accurately, so it is not offered.
		return map[string]any{"permissions": map[string]any{}}, nil
	default:
		switch optionID {
		case "accept", "acceptForSession", "decline":
			return map[string]any{"decision": optionID}, nil
		default:
			return nil, nil
		}
	}
}
