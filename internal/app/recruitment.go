package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// RequestMemberInput is the project manager's recruitment request: which
// roles the team lacks and why.
type RequestMemberInput struct {
	RoleIDs []string
	Reason  string
}

// RequestMember turns the project manager's recruitment request into a
// question card the user decides. It never blocks the caller: the leader
// receives a waiting item and ends its turn. Nothing is added to the team
// here — the user's decision is what adds members, always through the same
// transactional checks a manual add goes through.
func (a *App) RequestMember(ctx context.Context, principal auth.Principal, in RequestMemberInput) (domain.Question, error) {
	cached, unlock, writeErr := beginWrite[domain.Question](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Question{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindBot {
		return domain.Question{}, domain.Forbidden("error.forbidden.bot_only", nil)
	}
	bot, err := sqlite.GetBot(ctx, a.DB, principal.BotID)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	if !bot.IsLeader {
		return domain.Question{}, domain.Forbidden("error.recruitment.leader_only", nil)
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return domain.Question{}, domain.InvalidRequest("error.recruitment.reason_required", nil)
	}
	roleIDs, err := a.checkRecruitment(ctx, a.DB, bot.TeamID, in.RoleIDs)
	if err != nil {
		return domain.Question{}, err
	}
	item := domain.Question{
		ID: domain.NewID(), Kind: domain.QuestionKindRecruitment,
		TeamID: bot.TeamID, RequesterBotID: bot.ID, RunID: principal.RunID,
		Question: reason, RoleIDs: roleIDs, Options: []string{},
		Status: domain.QuestionOpen, Revision: 1,
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Question{}, err
	}
	defer tx.Rollback()
	if _, err := sqlite.GetBot(ctx, tx, bot.ID); err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	if err := sqlite.InsertQuestion(ctx, tx, item); err != nil {
		return domain.Question{}, err
	}
	item, err = sqlite.GetQuestion(ctx, tx, item.ID)
	if err != nil {
		return domain.Question{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, item); err != nil {
		return domain.Question{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Question{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventQuestionCreated, TeamID: item.TeamID, BotID: item.RequesterBotID, ResourceID: item.ID})
	a.Hub.Publish()
	return item, nil
}

// checkRecruitment validates the requested roles the way the team sees them:
// every id must be a real non-leader role the team does not have yet, and the
// request must fit the member limit. The user's decision re-runs the same
// checks inside addBotLocked, so a stale request can never bypass them.
func (a *App) checkRecruitment(ctx context.Context, q sqlite.Queryer, teamID string, roleIDs []string) ([]string, error) {
	seen := map[string]bool{}
	requested := make([]string, 0, len(roleIDs))
	for _, id := range roleIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		requested = append(requested, id)
	}
	if len(requested) == 0 {
		return nil, domain.InvalidRequest("error.recruitment.roles_required", nil)
	}
	bots, err := sqlite.ListBots(ctx, q, teamID)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, bot := range bots {
		present[bot.RoleID] = true
	}
	for _, id := range requested {
		role, err := roles.Default(id)
		if err != nil || role.ID == roles.LeaderRoleID {
			return nil, domain.InvalidRequest("error.team.unknown_role", map[string]any{"roleId": id})
		}
		if present[id] {
			return nil, domain.InvalidRequest("error.team.role_present", map[string]any{"roleId": id})
		}
	}
	settings, err := sqlite.GetSettings(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(bots)+len(requested) > settings.MemberLimit {
		return nil, domain.InvalidRequest("error.recruitment.member_limit", map[string]any{
			"requested": len(requested), "limit": settings.MemberLimit,
		})
	}
	return requested, nil
}

// AnswerRecruitment records the user's verdict on a recruitment request and
// delivers it to the project manager as fresh input. Approving adds the
// requested members through addBotLocked, the same transactional path a
// manual add uses; rejecting changes nothing. Both verdicts wake the leader
// with the result on a new causal chain.
func (a *App) AnswerRecruitment(ctx context.Context, principal auth.Principal, questionID, decision, comment string, expectedRevision int64) (domain.Question, error) {
	cached, unlock, writeErr := beginWrite[domain.Question](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Question{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindUser {
		return domain.Question{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	question, err := sqlite.GetQuestion(ctx, a.DB, questionID)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	if question.Kind != domain.QuestionKindRecruitment {
		return domain.Question{}, domain.InvalidRequest("error.question.not_recruitment", nil)
	}
	if question.Status != domain.QuestionOpen {
		return domain.Question{}, domain.Conflict("error.question.already_answered", map[string]any{"status": question.Status})
	}
	if decision != domain.RecruitmentApprove && decision != domain.RecruitmentReject {
		return domain.Question{}, domain.InvalidRequest("error.question.recruitment_decision", nil)
	}
	comment = strings.TrimSpace(comment)

	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Question{}, err
	}
	defer tx.Rollback()
	var effect relayEffect
	var answerText, resultText string
	if decision == domain.RecruitmentApprove {
		names := make([]string, 0, len(question.RoleIDs))
		added := make([]string, 0, len(question.RoleIDs))
		for _, roleID := range question.RoleIDs {
			role, err := roles.Default(roleID)
			if err != nil {
				return domain.Question{}, err
			}
			bot, err := a.addBotLocked(ctx, tx, question.TeamID, roleID)
			if err != nil {
				return domain.Question{}, err
			}
			names = append(names, role.Name)
			added = append(added, fmt.Sprintf("%s（id=%s）", bot.Name, bot.ID))
			effect.events = append(effect.events,
				domain.Event{Type: domain.EventBotAdded, TeamID: question.TeamID, BotID: bot.ID, ResourceID: bot.ID})
		}
		effect.events = append(effect.events, domain.Event{Type: domain.EventTeamUpdated, TeamID: question.TeamID, ResourceID: question.TeamID})
		answerText = "同意：" + strings.Join(names, "、")
		resultText = "你同意了招募申请，已加入成员：" + strings.Join(added, "、") + "。"
	} else {
		names := make([]string, 0, len(question.RoleIDs))
		for _, roleID := range question.RoleIDs {
			if role, err := roles.Default(roleID); err == nil {
				names = append(names, role.Name)
			} else {
				names = append(names, roleID)
			}
		}
		answerText = "拒绝：" + strings.Join(names, "、")
		resultText = "你拒绝了招募申请（" + strings.Join(names, "、") + "）。"
	}
	if comment != "" {
		answerText += "：" + comment
		resultText += "你的留言：" + comment
	}
	messageID, err := a.deliverDecisionInputLocked(ctx, tx, question, resultText)
	if err != nil {
		return domain.Question{}, err
	}
	effect.events = append(effect.events, domain.Event{Type: domain.EventMessageCreated, TeamID: question.TeamID, BotID: question.RequesterBotID, ResourceID: messageID})
	effect.wake = append(effect.wake, question.RequesterBotID)
	answered, err := sqlite.AnswerQuestion(ctx, tx, questionID, answerText, messageID, expectedRevision)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, answered); err != nil {
		return domain.Question{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Question{}, err
	}
	effect.events = append(effect.events, domain.Event{Type: domain.EventQuestionAnswered, TeamID: question.TeamID, BotID: question.RequesterBotID, ResourceID: question.ID})
	a.publishRelay(ctx, effect)
	return answered, nil
}
