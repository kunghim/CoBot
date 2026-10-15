package app

import (
	"context"

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
	for i := range approvals {
		if approvals[i].Type == domain.ApprovalRecovery {
			a.projectRecoveryOptions(ctx, &approvals[i])
		}
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
	card, err := sqlite.GetApproval(ctx, a.DB, approvalID)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}
	switch card.Type {
	case domain.ApprovalMerge, domain.ApprovalRecruitment:
		return a.decideProductApproval(ctx, principal, approvalID, optionID, comment, expectedRevision)
	case domain.ApprovalNativeTool:
		return a.decideNativeApproval(ctx, principal, approvalID, optionID, comment, expectedRevision)
	case domain.ApprovalChain, domain.ApprovalRecovery:
		return a.decideControlApproval(ctx, principal, card, optionID, comment, expectedRevision)
	default:
		return domain.Approval{}, domain.Conflict("error.approval.option_unavailable", nil)
	}
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
		if optionID != "deny" {
			return nil, domain.Conflict("error.approval.option_unavailable", nil)
		}
		return map[string]any{"permissions": map[string]any{}}, nil
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		switch optionID {
		case "accept", "decline":
			return map[string]any{"decision": optionID}, nil
		default:
			return nil, domain.Conflict("error.approval.option_unavailable", nil)
		}
	default:
		return nil, domain.Conflict("error.approval.option_unavailable", nil)
	}
}
