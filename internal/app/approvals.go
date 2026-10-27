package app

import (
	"context"
	"encoding/json"

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
		projectNativeFileOptions(&approvals[i])
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
	page, err := a.ListPage(ctx, principal, PageRequest{Kind: "approvals", TeamID: teamID, State: "history", Limit: limit})
	if err != nil {
		return nil, err
	}
	out := []domain.Approval{}
	for _, item := range page["items"].([]any) {
		out = append(out, item.(domain.Approval))
	}
	return out, nil
}

// DecideApproval records the user's decision with CAS and delivers the
// engine response. The decision survives a failed delivery; the response
// state truthfully reports queued/sent/unknown/expired.
func (a *App) DecideApproval(ctx context.Context, principal auth.Principal, approvalID, optionID, comment string, expectedRevision int64, content map[string]any) (domain.Approval, error) {
	if principal.Kind != auth.KindUser {
		return domain.Approval{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	card, err := sqlite.GetApproval(ctx, a.DB, approvalID)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}
	switch card.Type {
	case domain.ApprovalNativeTool:
		return a.decideNativeApproval(ctx, principal, approvalID, optionID, comment, expectedRevision, content)
	case domain.ApprovalChain, domain.ApprovalRecovery:
		return a.decideControlApproval(ctx, principal, card, optionID, comment, expectedRevision)
	case domain.ApprovalBudget:
		return a.decideBudgetApproval(ctx, principal, card, optionID, comment, expectedRevision)
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
// An elicitation's accepted reply carries the user's form content.
func engineResponseFor(approval domain.Approval, optionID string, content map[string]any) (map[string]any, error) {
	if approval.Type != domain.ApprovalNativeTool {
		return nil, nil // non-engine cards have no protocol reply
	}
	if approval.Method == "mcpServer/elicitation/request" {
		switch optionID {
		case domain.DecisionAcceptEngine:
			return map[string]any{"action": "accept", "content": content}, nil
		default:
			return map[string]any{"action": "decline"}, nil
		}
	}
	switch approval.Method {
	case "item/permissions/requestApproval":
		switch optionID {
		case "deny":
			return map[string]any{"permissions": map[string]any{}}, nil
		case domain.DecisionGrantTurn, domain.DecisionGrantSession:
			// A grant is exactly what was asked for, never more.
			var asked struct {
				Permissions json.RawMessage `json:"permissions"`
			}
			if json.Unmarshal([]byte(approval.Summary), &asked) != nil || len(asked.Permissions) == 0 {
				return nil, domain.Conflict("error.approval.option_unavailable", nil)
			}
			scope := "turn"
			if optionID == domain.DecisionGrantSession {
				scope = "session"
			}
			return map[string]any{"permissions": asked.Permissions, "scope": scope}, nil
		default:
			return nil, domain.Conflict("error.approval.option_unavailable", nil)
		}
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		switch optionID {
		case domain.DecisionAcceptEngine, domain.DecisionAcceptSession, domain.DecisionDeclineEngine, domain.DecisionCancelEngine:
			return map[string]any{"decision": optionID}, nil
		default:
			return nil, domain.Conflict("error.approval.option_unavailable", nil)
		}
	default:
		return nil, domain.Conflict("error.approval.option_unavailable", nil)
	}
}
