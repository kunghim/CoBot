package app

import (
	"context"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Product control effects share the decision's transaction. Provider inspection
// and interruption are deliberately outside it; neither is inferred from a PID.
func (a *App) decideControlApproval(ctx context.Context, p auth.Principal, card domain.Approval, option, comment string, expected int64) (domain.Approval, error) {
	cached, unlock, err := beginWrite[domain.Approval](ctx, a, p)
	defer unlock()
	if err != nil {
		return domain.Approval{}, err
	}
	if cached != nil {
		return sqlite.GetApproval(ctx, a.DB, cached.ID)
	}
	terminal := ""
	if card.Type == domain.ApprovalRecovery {
		terminal, err = a.verifyRecoveryTurn(ctx, card)
		if err != nil {
			return domain.Approval{}, err
		}
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Approval{}, err
	}
	defer tx.Rollback()
	card, err = sqlite.DecideApprovalTx(ctx, tx, card.ID, option, comment, p.UserID, expected)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}
	var chainEffect chainDecisionEffect
	var run domain.Run
	if card.Type == domain.ApprovalChain {
		chainEffect, err = a.applyChainDecisionLocked(ctx, tx, card, option)
	} else {
		run, err = a.applyVerifiedRecoveryDecisionLocked(ctx, tx, card, option, terminal)
	}
	if err != nil {
		return domain.Approval{}, err
	}
	if err := sqlite.SetApprovalResponseState(ctx, tx, card.ID, domain.ResponseSent, "product_effect_committed"); err != nil {
		return domain.Approval{}, err
	}
	card, err = sqlite.GetApproval(ctx, tx, card.ID)
	if err != nil {
		return domain.Approval{}, err
	}
	if err := a.saveWriteResult(ctx, tx, p, card); err != nil {
		return domain.Approval{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Approval{}, err
	}
	if card.Type == domain.ApprovalChain {
		a.finishChainDecision(ctx, chainEffect)
	} else {
		a.finishRecoveryDecision(ctx, run)
	}
	a.publishApprovalUpdated(ctx, card)
	return card, nil
}
