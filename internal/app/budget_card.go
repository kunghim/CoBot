package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// When a daily budget holds a member, the user is asked once whether to start
// today's count again. The card is a question about the user's own limit, so
// only the user's answer moves it: "reset" counts from now, "wait" leaves the
// team held until the day turns over.

// budgetApprovalBinding says which limit the card is about.
type budgetApprovalBinding struct {
	// Scope is whose limit held the member: team or bot.
	Scope  string `json:"scope"`
	TeamID string `json:"teamId"`
	// BotID is the member the hold was seen on, and for a member's own limit
	// the member the card is about.
	BotID string `json:"botId"`
}

// requestBudgetDecision opens the card for a hold, unless one is already open
// for the same limit or the user already chose to wait today. A team's limit
// holds every member at once; one card covers them all.
func (a *App) requestBudgetDecision(ctx context.Context, tx *sql.Tx, teamID, botID string, pause *BudgetPause) (*domain.Approval, error) {
	binding := budgetApprovalBinding{Scope: pause.Scope, TeamID: teamID, BotID: botID}
	// Which member a team card names is only the first one held; what makes
	// two cards the same is the team, or for a member's own limit, the member.
	identity, args := `a.team_id = ? AND json_extract(a.engine_request_id,'$.scope') = ?`, []any{teamID, pause.Scope}
	if pause.Scope == "bot" {
		identity += ` AND json_extract(a.engine_request_id,'$.botId') = ?`
		args = append(args, botID)
	}
	var open int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM approvals a WHERE a.type = ? AND a.request_state = ? AND json_valid(a.engine_request_id) AND `+identity,
		append([]any{domain.ApprovalBudget, domain.ApprovalStatePending}, args...)...).Scan(&open); err != nil {
		return nil, err
	}
	if open > 0 {
		return nil, nil
	}
	var waived int
	since := dayStart(budgetClock())
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM approvals a JOIN approval_decisions d ON d.approval_id = a.id
		WHERE a.type = ? AND json_valid(a.engine_request_id) AND d.option_id = ? AND strftime('%Y-%m-%d %H:%M:%f', d.decided_at) >= ? AND `+identity,
		append([]any{domain.ApprovalBudget, domain.BudgetOptionWait, since}, args...)...).Scan(&waived); err != nil {
		return nil, err
	}
	if waived > 0 {
		return nil, nil
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		return nil, err
	}
	card := domain.Approval{
		ID: domain.NewID(), TeamID: teamID, RequesterBotID: botID,
		EngineRequestID: raw, Type: domain.ApprovalBudget,
		Summary: "approval.card.budget_" + pause.Scope, Reason: "budget_" + pause.Reason,
		Options: []domain.ApprovalOption{
			{ID: domain.BudgetOptionReset, MessageKey: "approval.option.budget_reset", Text: "重置今日用量并继续"},
			{ID: domain.BudgetOptionWait, MessageKey: "approval.option.budget_wait", Text: "保持暂停，明天自动恢复"},
		},
		RequestState: domain.ApprovalStatePending, CreatedAt: time.Now().UTC(),
	}
	if err := sqlite.InsertApproval(ctx, tx, card); err != nil {
		return nil, err
	}
	return &card, nil
}

// decideBudgetApproval records the user's answer. Resetting starts the count
// for the limit that held the member again from now, in the decision's own
// transaction; waiting changes nothing but remembers the answer for the day.
func (a *App) decideBudgetApproval(ctx context.Context, p auth.Principal, card domain.Approval, option, comment string, expected int64) (domain.Approval, error) {
	cached, unlock, err := beginWrite[domain.Approval](ctx, a, p)
	defer unlock()
	if err != nil {
		return domain.Approval{}, err
	}
	if cached != nil {
		return sqlite.GetApproval(ctx, a.DB, cached.ID)
	}
	var binding budgetApprovalBinding
	if json.Unmarshal(card.EngineRequestID, &binding) != nil || binding.TeamID == "" {
		return domain.Approval{}, domain.InvalidRequest("error.budget.binding_missing", nil)
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
	if option == domain.BudgetOptionReset {
		kind, id := sqlite.BudgetKindTeam, binding.TeamID
		if binding.Scope == "bot" {
			kind, id = sqlite.BudgetKindBot, binding.BotID
		}
		if err := sqlite.SetBudgetReset(ctx, tx, kind, id, ledgerStamp(budgetClock())); err != nil {
			return domain.Approval{}, err
		}
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
	a.publishApprovalUpdated(ctx, card)
	if option == domain.BudgetOptionReset {
		a.ResumeBudgetPausedBots(ctx)
	}
	return card, nil
}

// closeFreedBudgetCards invalidates the open cards whose limit no longer holds
// anyone: it was reset, raised or removed, or the day turned over. A card the
// user never answered would otherwise keep asking about something already
// settled.
func (a *App) closeFreedBudgetCards(ctx context.Context) {
	rows, err := a.DB.QueryContext(ctx, `SELECT id FROM approvals WHERE type = ? AND request_state = ?`, domain.ApprovalBudget, domain.ApprovalStatePending)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return
	}
	settings, err := sqlite.GetSettings(ctx, a.DB)
	if err != nil {
		return
	}
	for _, id := range ids {
		card, err := sqlite.GetApproval(ctx, a.DB, id)
		if err != nil {
			continue
		}
		var binding budgetApprovalBinding
		if json.Unmarshal(card.EngineRequestID, &binding) != nil {
			continue
		}
		// With nothing queued in the team there is nothing held, whatever the
		// limit says: the held message was cancelled or went elsewhere.
		var queued int
		if err := a.DB.QueryRowContext(ctx, `SELECT count(*) FROM deliveries WHERE status = ? AND target_bot_id IN (SELECT id FROM bots WHERE team_id = ?)`,
			domain.DeliveryQueued, binding.TeamID).Scan(&queued); err != nil {
			continue
		}
		pause, err := a.budgetPaused(ctx, binding.TeamID, binding.BotID, settings)
		if err != nil || (queued > 0 && pause != nil && pause.Scope == binding.Scope) {
			continue
		}
		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			continue
		}
		if sqlite.SetApprovalRequestState(ctx, tx, id, domain.ApprovalStateInvalidated) == nil && tx.Commit() == nil {
			if updated, err := sqlite.GetApproval(ctx, a.DB, id); err == nil {
				a.publishApprovalUpdated(ctx, updated)
			}
			continue
		}
		tx.Rollback()
	}
}
