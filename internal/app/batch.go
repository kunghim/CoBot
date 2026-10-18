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

// batchedKinds are the deliveries that may share a turn with others of their
// kind: plain messages, task assignments, delivery notices to the leader and
// review requests to a reviewer. Each item still gets its own answer (a
// delivery per task, a verdict per review); merging only saves the turn.
// Approval and question results, rejections, escalations, requirement
// reports and cancellation notices keep their own turns.
var batchedKinds = map[string]bool{"message": true, "task_assignment": true, "review_notice": true, "review_request": true}

// claimBatchLocked takes the deliveries that queued up behind the head while
// the member was busy into the same turn, so a member answers five pings
// once, starts three assigned tasks together, or reviews two deliveries in
// one turn instead of starting a turn for each. Only deliveries of the head's
// kind join: user messages with user messages (across chains, each user
// message opens its own), everything else only within the head's causal
// chain. A merged turn is one wake: companions are recorded on the chain
// without spending more.
func (s *Scheduler) claimBatchLocked(ctx context.Context, tx *sql.Tx, botID string, head sqlite.DeliveryRecord, headMessage domain.Message, runID string) ([]domain.Message, error) {
	if !batchedKinds[head.Kind] {
		return nil, nil
	}
	if head.Kind == "message" && headMessage.Kind != domain.MessageKindUser && headMessage.Kind != domain.MessageKindCollaboration {
		return nil, nil
	}
	queue, err := sqlite.ListDispatchableDeliveries(ctx, tx, botID, maxBatch*3)
	if err != nil {
		return nil, err
	}
	acrossChains := head.Kind == "message" && headMessage.Kind == domain.MessageKindUser
	var batch []domain.Message
	for _, record := range queue {
		if len(batch) >= maxBatch-1 {
			break
		}
		if record.ID == head.ID || record.Kind != head.Kind || (!acrossChains && record.ChainID != head.ChainID) {
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
			if record.Kind == "task_assignment" && (task.OwnerID != botID || task.Status == domain.TaskReview) {
				continue // stale: its own dispatch cancels it
			}
		}
		if !acrossChains && record.ChainID != "" {
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

// batchHeaders open a merged turn by what it holds.
var batchHeaders = map[string]string{
	"message":         "你在上一轮工作期间收到了 %d 条消息，请一起处理：\n",
	"task_assignment": "你被指派了 %d 个任务，请一起处理，每个任务分别用 deliver_task 交付：\n",
	"review_notice":   "有 %d 个任务交付了，等你验收，请对每个任务分别用 review_task 给出结论：\n",
	"review_request":  "有 %d 个交付等你评审，请对每个任务分别用 review_task 给出结论：\n",
}

// batchInput renders a merged turn's messages, each under its sender, so the
// member can tell who asked what.
func (s *Scheduler) batchInput(ctx context.Context, kind string, messages []domain.Message) string {
	var b strings.Builder
	header, ok := batchHeaders[kind]
	if !ok {
		header = batchHeaders["message"]
	}
	fmt.Fprintf(&b, header, len(messages))
	for _, message := range messages {
		sender := "用户"
		if message.SourceBotID != "" {
			sender = "成员"
			if bot, err := sqlite.GetBot(ctx, s.app.DB, message.SourceBotID); err == nil {
				sender = bot.Name
			}
		} else if message.Kind == domain.MessageKindSystem {
			sender = "系统"
		}
		fmt.Fprintf(&b, "\n【来自 %s】\n%s\n", sender, message.RenderText())
	}
	return b.String()
}
