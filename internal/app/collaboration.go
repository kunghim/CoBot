package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// collaborationEtiquette keeps member traffic to what moves the work: every
// message that needs no action still costs the recipient a turn.
const collaborationEtiquette = `
# 协作约定
- 有人交付、被打回、收到需要处理的消息时，系统会唤醒对应的人。派完活、交付完或在等别人时，直接结束这一轮，不要等待或反复查看进度。
- 只处理已经送到你手上的任务。你名下的任务还在等前置任务完成时，不要提前开工；有人催你，简短说明即可，任务送到后系统会唤醒你。
- 同一内容要告诉几个人时，用 targetBotIds 一次发完。只是顺带告知、对方不需要做任何事时才设 notifyOnly。
- 不要发“收到”“好的”“明白”这类没有新信息的消息，也不要为了表示确认而回复。
- 交付说明与任务规模相称：写清怎么运行或查看成果、实际做了哪些验证和结果即可。
`

// SendMessageFromBot delivers a bot-to-bot message. It is stored first, then
// queued: accepted only ever means the fact is persisted, never that the
// target ran. The target's busy queue is respected — nothing interrupts.
func (a *App) SendMessageFromBot(ctx context.Context, principal auth.Principal, targetBotID, text, taskID, requestID string) (map[string]any, error) {
	return a.sendFromBot(ctx, principal, targetBotID, text, taskID, requestID, false)
}

// NotifyFromBot leaves a member a message it reads with its next turn's
// context, without waking it or spending the chain: progress notes and
// "for your information" need no reply.
func (a *App) NotifyFromBot(ctx context.Context, principal auth.Principal, targetBotID, text, taskID, requestID string) (map[string]any, error) {
	return a.sendFromBot(ctx, principal, targetBotID, text, taskID, requestID, true)
}

func (a *App) sendFromBot(ctx context.Context, principal auth.Principal, targetBotID, text, taskID, requestID string, notify bool) (map[string]any, error) {
	cached, unlock, writeErr := beginWrite[map[string]any](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return nil, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindBot {
		return nil, domain.Forbidden("error.forbidden.bot_only", nil)
	}
	body := strings.TrimSpace(text)
	if body == "" {
		return nil, domain.InvalidRequest("error.message.text_required", nil)
	}
	source, err := sqlite.GetBot(ctx, a.DB, principal.BotID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	target, err := sqlite.GetBot(ctx, a.DB, targetBotID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	if source.TeamID != target.TeamID {
		return nil, domain.Forbidden("error.message.other_team", map[string]any{"targetBotId": targetBotID})
	}
	if source.ID == target.ID {
		return nil, domain.InvalidRequest("error.message.to_self", nil)
	}
	if taskID != "" {
		task, err := sqlite.GetTask(ctx, a.DB, taskID)
		if err != nil {
			return nil, sqlite.NormalizeError(err)
		}
		if task.TeamID != source.TeamID {
			return nil, domain.Forbidden("error.message.other_team", nil)
		}
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, source.TeamID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	if team.Status != domain.TeamStatusActive {
		return nil, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}

	// Idempotent replay: the same key and body returns the stored fact.
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: body}})
	if err != nil {
		return nil, err
	}
	if requestID != "" {
		if existing, err := sqlite.FindMessageByRequestID(ctx, a.DB, source.TeamID, requestID); err == nil {
			if existing.BotID != target.ID || existing.SourceBotID != source.ID || existing.Kind != domain.MessageKindCollaboration || existing.RunID != taskID || hashBlocks(existing.Blocks) != hashBlocks(blocks) {
				return nil, domain.IdempotencyConflict("error.message.request_reused", map[string]any{"requestId": requestID})
			}
			queued, _ := sqlite.CountQueuedDeliveries(ctx, a.DB, target.ID)
			return map[string]any{"status": "duplicate", "messageId": existing.ID, "queuedAhead": queued}, nil
		} else if !errors.Is(err, sqlite.ErrNotFound) {
			return nil, err
		}
	}

	status := domain.DeliveryQueued
	if notify {
		status = "" // stored for the recipient's context, never dispatched
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: source.TeamID, BotID: target.ID,
		Kind: domain.MessageKindCollaboration, Blocks: blocks,
		SourceBotID: source.ID, RequestID: requestID,
		DeliveryStatus: status, RunID: taskID,
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		if isUniqueViolation(err) {
			return nil, domain.IdempotencyConflict("error.message.request_reused", map[string]any{"requestId": requestID})
		}
		return nil, err
	}
	if notify {
		result := map[string]any{"status": "notified", "messageId": message.ID}
		if err := a.saveWriteResult(ctx, tx, principal, result); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		_ = a.recordEvent(ctx, domain.Event{Type: domain.EventMessageCreated, TeamID: source.TeamID, BotID: target.ID, ResourceID: message.ID})
		a.Hub.Publish()
		return result, nil
	}
	// The wake this causes belongs to the causal chain the sender is working
	// in. The lookup runs on the caller's transaction: the pool is a single
	// connection, so a query on a.DB from inside an open tx would deadlock.
	chainID := chainOfRunLocked(ctx, tx, source.ID)
	if chainID == "" {
		chain, cerr := a.openChainLocked(ctx, tx, source.TeamID, message.ID, source.ID)
		if cerr != nil {
			return nil, cerr
		}
		chainID = chain.ID
	}
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: domain.NewID(), MessageID: message.ID, TeamID: source.TeamID,
		TargetBotID: target.ID, Status: domain.DeliveryQueued, ChainID: chainID,
		TaskID: taskID, SourceRunID: principal.RunID,
	}); err != nil {
		return nil, err
	}
	// A busy member keeps its working projection: enqueueing must not clear
	// current_run_id of a run that is still live (review B minor13).
	if _, ferr := sqlite.FindActiveRun(ctx, tx, target.ID); ferr != nil && errors.Is(ferr, sqlite.ErrNotFound) {
		if err := sqlite.SetBotRunState(ctx, tx, target.ID, domain.BotStatusQueued, ""); err != nil {
			return nil, err
		}
	}
	queued, err := sqlite.CountQueuedDeliveries(ctx, tx, target.ID)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"status": "accepted", "messageId": message.ID, "queuedAhead": queued}
	if err := a.saveWriteResult(ctx, tx, principal, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventMessageCreated, TeamID: source.TeamID, BotID: target.ID, ResourceID: message.ID})
	a.Hub.Publish()
	if a.Scheduler != nil {
		a.Scheduler.Wake(target.ID)
	}
	return result, nil
}

// BotOf resolves a live member row; the dispatcher uses it to prove a bridge
// token still maps to a real bot.
func (a *App) BotOf(ctx context.Context, botID string) (domain.Bot, error) {
	bot, err := sqlite.GetBot(ctx, a.DB, botID)
	if err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	return bot, nil
}

// CaptainOf returns the captain bot id of a team.
func (a *App) CaptainOf(ctx context.Context, teamID string) (string, error) {
	team, _, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return "", sqlite.NormalizeError(err)
	}
	return team.LeaderID, nil
}

// ListCollaboration projects a read-only collaboration record. A bot reads
// the exchange with one same-team peer; the user reads a named member's whole
// record (everything the member sent or received).
func (a *App) ListCollaboration(ctx context.Context, principal auth.Principal, peerBotID string, limit int) ([]domain.Message, error) {
	var botID, peer string
	switch principal.Kind {
	case auth.KindUser:
		// Passing an empty peer asks the store for the member's whole record;
		// a (member, member) pair would only ever match a self-addressed
		// message, which the product never creates.
		botID, peer = peerBotID, ""
	case auth.KindBot:
		botID, peer = principal.BotID, peerBotID
		peerRow, err := sqlite.GetBot(ctx, a.DB, peerBotID)
		if err != nil {
			return nil, sqlite.NormalizeError(err)
		}
		self, err := sqlite.GetBot(ctx, a.DB, botID)
		if err != nil {
			return nil, sqlite.NormalizeError(err)
		}
		if peerRow.TeamID != self.TeamID {
			return nil, domain.Forbidden("error.message.other_team", nil)
		}
	default:
		return nil, domain.Unauthorized("error.unauthorized.principal_required")
	}
	return sqlite.ListCollaborationMessages(ctx, a.DB, botID, peer, limit)
}

// RequestMember asks the user to add a member of a given role. It creates a
// recruitment approval; no runnable bot exists before the user approves.
func (a *App) RequestMember(ctx context.Context, principal auth.Principal, roleID, reason string) (domain.Approval, error) {
	cached, unlock, writeErr := beginWrite[domain.Approval](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Approval{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindBot {
		return domain.Approval{}, domain.Forbidden("error.forbidden.bot_only", nil)
	}
	bot, err := sqlite.GetBot(ctx, a.DB, principal.BotID)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}
	isLeader, err := a.isLeaderOf(ctx, bot.TeamID, principal.BotID)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}
	if !isLeader {
		return domain.Approval{}, domain.Forbidden("error.approval.captain_only", nil)
	}
	role, err := roles.Default(roleID)
	if err != nil || role.ID == roles.LeaderRoleID {
		return domain.Approval{}, domain.InvalidRequest("error.team.unknown_role", map[string]any{"roleId": roleID})
	}
	teamID := bot.TeamID
	approval := domain.Approval{
		ID: domain.NewID(), TeamID: teamID, RequesterBotID: principal.BotID,
		RunID: principal.RunID,
		Type:  domain.ApprovalRecruitment, Summary: role.Name,
		Reason: strings.TrimSpace(reason),
		Options: []domain.ApprovalOption{
			{ID: "approve", MessageKey: "approval.option.recruitment.approve"},
			{ID: "reject", MessageKey: "approval.option.recruitment.reject"},
		},
		Revision:        1,
		EngineRequestID: json.RawMessage(`"` + roleID + `"`),
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Approval{}, err
	}
	defer tx.Rollback()
	if err := sqlite.InsertApproval(ctx, tx, approval); err != nil {
		return domain.Approval{}, err
	}
	approval, err = sqlite.GetApproval(ctx, tx, approval.ID)
	if err != nil {
		return domain.Approval{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, approval); err != nil {
		return domain.Approval{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Approval{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventApprovalCreated, TeamID: teamID, BotID: principal.BotID, ResourceID: approval.ID})
	a.Hub.Publish()
	return approval, nil
}

// BuildBotContext resolves the caller's own bot and returns its fresh context
// block for the get_context tool.
func (a *App) BuildBotContext(ctx context.Context, principal auth.Principal, taskID string) (string, error) {
	if principal.Kind != auth.KindBot {
		return "", domain.Forbidden("error.forbidden.bot_only", nil)
	}
	bot, err := sqlite.GetBot(ctx, a.DB, principal.BotID)
	if err != nil {
		return "", sqlite.NormalizeError(err)
	}
	return a.BuildContext(ctx, bot, taskID), nil
}

// BuildContext assembles the fresh per-dispatch context block. It is
// prepended to the turn text so a bot never relies on a stale model memory of
// the team, the member list or its task.
func (a *App) BuildContext(ctx context.Context, bot domain.Bot, taskID string) string {
	var b strings.Builder
	team, directory, err := sqlite.GetTeam(ctx, a.DB, bot.TeamID)
	if err == nil {
		fmt.Fprintf(&b, "# 团队\n名称：%s\n工作目录：%s\n", team.Name, directory)
		if members, err := sqlite.ListBots(ctx, a.DB, bot.TeamID); err == nil {
			b.WriteString("成员：\n")
			for _, m := range members {
				role := "成员"
				if m.IsLeader {
					role = "队长"
				}
				fmt.Fprintf(&b, "- %s（%s，id=%s）\n", m.Name, role, m.ID)
			}
		}
	}
	// The member's own recent memories ride along bounded like any other
	// retrieval (G05): methods and preferences, newest first.
	if mems := a.memoriesForContext(ctx, bot.TeamID, bot.ID); len(mems.Items) > 0 {
		b.WriteString("\n# 你的记忆（工作方法与偏好）\n")
		for _, m := range mems.Items {
			fmt.Fprintf(&b, "- %s\n", m.Body)
		}
	}
	if taskID != "" {
		// Only this member's own team's task may enter the context (review
		// B major6); a foreign id is treated as absent, never rendered.
		if task, err := sqlite.GetTask(ctx, a.DB, taskID); err == nil && task.TeamID == bot.TeamID {
			fmt.Fprintf(&b, "\n# 当前任务\nID：%s\n版本：%d\n负责人：%s\n标题：%s\n详细要求：%s\n完成标准：%s\n状态：%s\n",
				task.ID, task.Revision, task.OwnerID, task.Title, task.Description, task.AcceptanceCriteria, task.Status)
			if evidence, err := sqlite.ListEvidence(ctx, a.DB, task.ID); err == nil && len(evidence) > 0 {
				b.WriteString("已有证据：\n")
				for _, e := range evidence {
					fmt.Fprintf(&b, "- %s\n", e.Summary)
				}
			}
		}
	}
	// Recent unread collaboration with peers keeps the bot current without a
	// full history dump.
	if peers, err := sqlite.ListBots(ctx, a.DB, bot.TeamID); err == nil {
		for _, peer := range peers {
			if peer.ID == bot.ID {
				continue
			}
			messages, err := sqlite.ListCollaborationMessages(ctx, a.DB, bot.ID, peer.ID, 5)
			if err != nil || len(messages) == 0 {
				continue
			}
			fmt.Fprintf(&b, "\n# 与%s的最近沟通\n", peer.Name)
			for _, m := range messages {
				from := peer.Name
				if m.SourceBotID == bot.ID {
					from = "你"
				}
				fmt.Fprintf(&b, "- %s：%s\n", from, m.RenderText())
			}
		}
	}
	if role, err := roles.Default(bot.RoleID); err == nil {
		fmt.Fprintf(&b, "\n# 你的角色\n%s\n", role.Instructions)
	}
	b.WriteString(collaborationEtiquette)
	return b.String()
}
