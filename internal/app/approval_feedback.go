package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// decideProductApproval commits the decision, member effect, notification
// and replay record in one transaction. It never calls a model or Git.
func (a *App) decideProductApproval(ctx context.Context, p auth.Principal, id, option, comment string, expected int64) (domain.Approval, error) {
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
	if approval.Type == domain.ApprovalMerge && option == "approve" {
		if _, err := a.validateMergeDecision(ctx, tx, approval); err != nil {
			return domain.Approval{}, err
		}
	}
	approval, err = sqlite.DecideApprovalTx(ctx, tx, id, option, comment, p.UserID, expected)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}
	var member domain.Bot
	if approval.Type == domain.ApprovalRecruitment && option == "approve" {
		var roleID string
		if json.Unmarshal(approval.EngineRequestID, &roleID) != nil || roleID == "" {
			return domain.Approval{}, domain.InvalidRequest("error.team.unknown_role", nil)
		}
		member, err = a.addBotLocked(ctx, tx, approval.TeamID, roleID)
		if err != nil {
			return domain.Approval{}, err
		}
	}
	if err := a.enqueueApprovalFeedbackLocked(ctx, tx, approval, member.ID); err != nil {
		return domain.Approval{}, err
	}
	approval, err = sqlite.GetApproval(ctx, tx, id)
	if err != nil {
		return domain.Approval{}, err
	}
	if err := a.saveWriteResult(ctx, tx, p, approval); err != nil {
		return domain.Approval{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Approval{}, err
	}
	if member.ID != "" {
		_ = a.recordEvent(ctx, domain.Event{Type: domain.EventBotAdded, TeamID: approval.TeamID, BotID: member.ID, ResourceID: member.ID})
	}
	a.publishApprovalUpdated(ctx, approval)
	if a.Scheduler != nil {
		a.Scheduler.Wake(approval.RequesterBotID)
	}
	return sqlite.GetApproval(ctx, a.DB, id)
}

func (a *App) enqueueApprovalFeedbackLocked(ctx context.Context, tx *sql.Tx, approval domain.Approval, memberID string) error {
	if _, err := sqlite.GetBot(ctx, tx, approval.RequesterBotID); err != nil {
		if !errors.Is(err, sqlite.ErrNotFound) {
			return err
		}
		return sqlite.SetApprovalResponseState(ctx, tx, approval.ID, domain.ResponseExpired, "requester_unavailable")
	}
	taskID, chainID := "", ""
	if approval.RunID != "" {
		var err error
		chainID, err = sqlite.ChainOfRun(ctx, tx, approval.RunID)
		if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT task_id FROM deliveries WHERE run_id = ? LIMIT 1`, approval.RunID).Scan(&taskID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	payload := map[string]any{"type": "approval_result", "approvalId": approval.ID, "approvalType": approval.Type, "decision": approval.Decision.OptionID, "comment": approval.Decision.Comment, "requesterBotId": approval.RequesterBotID}
	if approval.Type == domain.ApprovalMerge {
		var binding mergeBinding
		if err := json.Unmarshal(approval.EngineRequestID, &binding); err != nil {
			return err
		}
		taskID = binding.TaskID
		payload["details"] = binding.MergeApprovalDetails
		payload["authorizedCandidate"] = approval.Decision.OptionID == "approve"
	}
	if memberID != "" {
		payload["memberId"] = memberID
	}
	payload["taskId"] = taskID
	text, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "approval_result", Text: string(text)}})
	if err != nil {
		return err
	}
	message := domain.Message{ID: domain.NewID(), TeamID: approval.TeamID, BotID: approval.RequesterBotID, Kind: domain.MessageKindSystem, Blocks: blocks, RunID: approval.ID, DeliveryStatus: domain.DeliveryQueued, CreatedAt: time.Now().UTC()}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return err
	}
	deliveryID := domain.NewID()
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{ID: deliveryID, MessageID: message.ID, TeamID: approval.TeamID, TargetBotID: approval.RequesterBotID, TaskID: taskID, SourceRunID: approval.RunID, Kind: "approval_result", Status: domain.DeliveryQueued, ChainID: chainID}); err != nil {
		return err
	}
	return sqlite.BindApprovalDelivery(ctx, tx, approval.ID, deliveryID)
}
