package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// A turn can end with the member's card still open and nothing sent on:
// the model wrote the handoff as text instead of calling the tool, or
// stopped thinking it was done. Nothing would wake anyone again, and the
// requirement would sit there with nobody working on it. So a member whose
// turn ends normally while a card that turn was working on is still open is
// reminded once to hand it on. If it still does not, the card stays as it
// is for the user to see.

// cardReminderMarker opens the reminder; finding it again with a card's id
// tells the card was reminded already.
const cardReminderMarker = "这一轮结束了，但卡片还没有交接"

// remindUnhandedCardsLocked queues one reminder for the cards the run was
// working on that are still open and were not reminded before, and reports
// whether it queued one. A turn the member spent on something else (the
// user talking to it directly) does not count: those cards were not what it
// set out to finish.
func remindUnhandedCardsLocked(ctx context.Context, tx *sql.Tx, teamID, botID, runID string) (bool, error) {
	cards, err := sqlite.ListTasks(ctx, tx, teamID, botID, 50)
	if err != nil {
		return false, err
	}
	var open []domain.Task
	for _, card := range cards {
		if card.Status != domain.TaskDoing || card.RequirementID == "" {
			continue
		}
		req, err := sqlite.GetRequirement(ctx, tx, card.RequirementID)
		if err != nil || req.Status != domain.RequirementActive {
			continue
		}
		var carried, reminded int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM deliveries WHERE run_id = ? AND task_id = ?`, runID, card.ID).Scan(&carried); err != nil {
			return false, err
		}
		if carried == 0 {
			continue
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE bot_id = ? AND kind = ? AND blocks LIKE ? AND blocks LIKE ?`,
			botID, domain.MessageKindSystem, "%"+cardReminderMarker+"%", "%cardId="+card.ID+"%").Scan(&reminded); err != nil {
			return false, err
		}
		if reminded == 0 {
			open = append(open, card)
		}
	}
	if len(open) == 0 {
		return false, nil
	}
	var b strings.Builder
	b.WriteString(cardReminderMarker)
	b.WriteString("：\n")
	for _, card := range open {
		fmt.Fprintf(&b, "- 「%s」（cardId=%s）\n", card.Title, card.ID)
	}
	b.WriteString("做完了就调用 handoff 交出（outcome=done）；做不下去或需要项目经理决定，就用 handoff 上报（outcome=escalate）并写明卡在哪里。" +
		"只在回复里写「已完成」不会交给下一环。")
	return true, enqueueSystemMessageLocked(ctx, tx, systemMessage{
		teamID: teamID, target: botID, text: b.String(), about: open[0].ID, taskID: open[0].ID, kind: "message",
	})
}
