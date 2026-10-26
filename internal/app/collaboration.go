package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// platformLayer is what every member is told first, whatever its role: how
// the relay works, how a card is handed off, what messages are for and what
// to do when the user talks to it directly. Business moves only on cards;
// a message never wakes anyone.
const platformLayer = `# 你在一个 CoBot 团队中工作
团队按“接力”协作。每次需求都有一份需求文档和一条链路，都由项目经理拟定、用户确认；链路由若干环节组成，每个环节是一张卡片，属于一个人。每张卡片开头写着需求、链路和你所在的位置，后面附着需求文档和前面各环的交接与执行记录。

# 处理卡片
- 卡片送到你手上就是你的工作，直接开始，不需要回复“收到”。
- 只处理送到你手上的卡片。链路里排在你后面的工作，等卡片送到再做。做完交出后直接结束这一轮，不要等待或反复查看进度，需要你时系统会把卡片送来。
- 做完用 handoff 交出：
  - 完成：系统按链路交给下一环，你不用指定交给谁。
  - 退回（你负责评审或测试时）：系统会退回给产出这项工作的那一环。写清缺什么、在哪里、怎样算改好，并选一种方式：“重走”（改完重新经过后面的环节）或“只回我”（改完直接回到你这里）。只有风格偏好或可选改进时不要退回，完成时把建议写在 suggestions 里（带建议通过），建议会记为遗留事项。
  - 上报：遇到阻塞、需要项目经理决定，或者工作超出这张卡的范围时，交回项目经理并说明原因。
- 交出时写清做了什么、产出在哪里、怎么查看或运行、实际做了哪些验证。你实际执行的命令和改动的文件，系统会自动附上。

# 沟通
- 业务只走卡片：让别人做事、交付成果、指出问题，都用 handoff。
- send_message 只用于反馈和告知，对方不会被唤醒，下次开始工作时才会看到。不要用它催人、派活或代替交付。
- 不发“收到”“好的”这类没有新信息的消息。
`

// directRequestLayer tells a member (not the project manager, whose job is
// talking to the user) which of the user's direct requests it takes on.
const directRequestLayer = `
# 用户直接找你时
用户可以绕过项目经理直接和你对话。
- 小事直接做：解释代码、回答问题、改文案、修一个明确的小问题。判断标准：只涉及少量文件、不改变需求范围、不需要别人配合。
- 大一点的工作（新功能、跨模块改动、需要设计或测试的修改）：先向用户说明这类工作建议交给项目经理安排，经过评审和测试更稳妥，请用户决定。
- 用户坚持让你直接做：照做，做完用 send_message 告知项目经理你做了什么、改了哪些文件。
- 你手上有卡片在进行时：用户的新要求如果会影响这张卡，先说明影响，再按用户的决定处理。
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

// ListCollaboration projects a read-only collaboration record. A bot reads
// the exchange with one same-team peer; the user reads a named member's whole
// record (everything the member sent or received).
func (a *App) ListCollaboration(ctx context.Context, principal auth.Principal, peerBotID string, limit int) ([]domain.Message, error) {
	var botID, peer string
	switch principal.Kind {
	case auth.KindUser:
		// Passing an empty peer asks the store for the member's whole record;
		// a (member, member) pair would only ever match a self-addressed
		// message, which the product creates exactly once of: the project
		// manager's own record of a cancelled requirement.
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
