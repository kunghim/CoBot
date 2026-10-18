package app

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// activityTypes are the events the 动态 page shows: what the team did that a
// user cares about. Run, usage, message bookkeeping and internal records stay
// out; a member-to-member message is the only message shown.
var activityTypes = []string{
	domain.EventTaskCreated, domain.EventTaskUpdated,
	domain.EventTransferOffered, domain.EventTransferAccepted,
	domain.EventQuestionCreated, domain.EventQuestionAnswered,
	domain.EventApprovalCreated,
	domain.EventBotAdded, domain.EventBotDeleted,
	domain.EventTeamCreated, domain.EventTeamArchived, domain.EventTeamRestored,
	domain.EventBudgetPaused, domain.EventMemoryChanged,
	domain.EventMessageCreated,
}

// activityFilter is the SQL condition (on events e) for activityTypes.
func activityFilter() (string, []any) {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(activityTypes)), ",")
	args := make([]any, 0, len(activityTypes)+2)
	for _, t := range activityTypes {
		args = append(args, t)
	}
	args = append(args, domain.EventMessageCreated, domain.MessageKindCollaboration)
	return " AND e.type IN (" + marks + ") AND (e.type != ? OR e.resource_id IN (SELECT id FROM messages WHERE kind = ?))", args
}

// ActivityItem is one 动态 entry: the event plus the facts needed to say it
// in plain words (titles, who, a short excerpt). Detail is best effort: a
// record deleted since reads as the bare event.
type ActivityItem struct {
	domain.Event
	Detail map[string]any `json:"detail,omitempty"`
}

func excerpt(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}

func (a *App) describeActivity(ctx context.Context, q sqlite.Queryer, event domain.Event) map[string]any {
	detail := map[string]any{}
	if len(event.Payload) > 0 {
		_ = json.Unmarshal(event.Payload, &detail)
	}
	switch event.Type {
	case domain.EventTaskCreated, domain.EventTaskUpdated:
		if task, err := sqlite.GetTask(ctx, q, event.ResourceID); err == nil {
			detail["title"] = task.Title
			if _, ok := detail["ownerId"]; !ok {
				detail["ownerId"] = task.OwnerID
			}
			if _, ok := detail["status"]; !ok && event.Type == domain.EventTaskCreated {
				detail["status"] = task.Status
			}
		}
	case domain.EventTransferOffered, domain.EventTransferAccepted:
		var taskID, from, to string
		if err := q.QueryRowContext(ctx, `SELECT task_id, from_bot_id, to_bot_id FROM transfers WHERE id = ?`, event.ResourceID).Scan(&taskID, &from, &to); err == nil {
			detail["fromBotId"], detail["toBotId"] = from, to
			if task, err := sqlite.GetTask(ctx, q, taskID); err == nil {
				detail["title"] = task.Title
			}
		}
	case domain.EventQuestionCreated, domain.EventQuestionAnswered:
		if question, err := sqlite.GetQuestion(ctx, q, event.ResourceID); err == nil {
			detail["question"] = excerpt(question.Question, 120)
		}
	case domain.EventApprovalCreated:
		if approval, err := sqlite.GetApproval(ctx, q, event.ResourceID); err == nil {
			detail["approvalType"] = approval.Type
			detail["state"] = approval.RequestState
			if approval.Decision != nil {
				detail["decision"] = approval.Decision.OptionID
			}
			if approval.Type == domain.ApprovalNativeTool {
				detail["summary"] = approval.Summary
			}
		}
	case domain.EventBotAdded:
		if bot, err := sqlite.GetBot(ctx, q, event.ResourceID); err == nil {
			detail["name"] = bot.Name
		}
	case domain.EventMessageCreated:
		if message, err := sqlite.GetMessage(ctx, q, event.ResourceID); err == nil {
			detail["fromBotId"], detail["toBotId"] = message.SourceBotID, message.BotID
			detail["text"] = excerpt(message.RenderText(), 160)
		}
	case domain.EventMemoryChanged:
		var body, deleted string
		if err := q.QueryRowContext(ctx, `SELECT body, deleted_at FROM memories WHERE id = ?`, event.ResourceID).Scan(&body, &deleted); err == nil {
			detail["text"] = excerpt(body, 120)
			detail["deleted"] = deleted != ""
		}
	}
	if len(detail) == 0 {
		return nil
	}
	return detail
}
