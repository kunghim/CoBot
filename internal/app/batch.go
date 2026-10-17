package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// maxBatch bounds how many queued messages one turn takes in.
const maxBatch = 10

// claimBatchLocked takes the messages that queued up behind the head while
// the member was busy into the same turn, so a member answers five pings
// once instead of starting five turns. Only plain messages of the same kind
// and the same causal chain join: user messages with user messages, member
// messages of one chain with each other. Task assignments and approval
// results keep their own turns, their gates are per item. A merged turn is
// one wake: companions are recorded on the chain without spending more.
func (s *Scheduler) claimBatchLocked(ctx context.Context, tx *sql.Tx, botID string, head sqlite.DeliveryRecord, headMessage domain.Message, runID string) ([]domain.Message, error) {
	if head.Kind != "message" || (headMessage.Kind != domain.MessageKindUser && headMessage.Kind != domain.MessageKindCollaboration) {
		return nil, nil
	}
	queue, err := sqlite.ListDispatchableDeliveries(ctx, tx, botID, maxBatch*3)
	if err != nil {
		return nil, err
	}
	var batch []domain.Message
	for _, record := range queue {
		if len(batch) >= maxBatch-1 {
			break
		}
		if record.ID == head.ID || record.Kind != "message" || record.ChainID != head.ChainID {
			continue
		}
		message, err := sqlite.GetMessage(ctx, tx, record.MessageID)
		if err != nil {
			return nil, err
		}
		if message.Kind != headMessage.Kind {
			continue
		}
		if record.TaskID != "" {
			task, err := sqlite.GetTask(ctx, tx, record.TaskID)
			if err != nil || task.Status == domain.TaskCancelled || task.Status == domain.TaskDone {
				continue // its own dispatch will settle it
			}
		}
		if record.ChainID != "" {
			key := record.DispatchKey
			if key == "" {
				key = record.ID
			}
			if _, err := sqlite.CountChainWake(ctx, tx, record.ChainID, key, record.ID, botID); err != nil {
				return nil, err
			}
		}
		if err := sqlite.SetMessageDeliveryStatus(ctx, tx, message.ID, domain.DeliveryDispatching); err != nil {
			return nil, err
		}
		if err := sqlite.ClaimDeliveryRun(ctx, tx, record.ID, runID); err != nil {
			return nil, err
		}
		batch = append(batch, message)
	}
	return batch, nil
}

// batchInput renders a merged turn's messages, each under its sender, so the
// member can tell who asked what.
func (s *Scheduler) batchInput(ctx context.Context, messages []domain.Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你在上一轮工作期间收到了 %d 条消息，请一起处理：\n", len(messages))
	for _, message := range messages {
		sender := "用户"
		if message.SourceBotID != "" {
			sender = "成员"
			if bot, err := sqlite.GetBot(ctx, s.app.DB, message.SourceBotID); err == nil {
				sender = bot.Name
			}
		}
		fmt.Fprintf(&b, "\n【来自 %s】\n%s\n", sender, message.RenderText())
	}
	return b.String()
}
