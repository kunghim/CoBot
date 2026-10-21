package app

import (
	"context"
	"database/sql"
	"errors"
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
// reminded to hand it on.
//
// A reminder alone does not always help. A thread can wear into a groove:
// one MiMo thread, unable to emit the handoff call, ran 41 shell commands
// that only echoed what it meant to do ("I need to call the CoBot handoff
// tool"), and every later turn copied that pattern from its own history;
// the same request with the history cut away called the tool every time. So
// a card still open after its reminder gets one more, on a fresh thread
// handed over from the old one. After that the card stays as it is for the
// user to see.

// cardReminderMarker opens a reminder; finding it again with a card's id
// counts the reminders the card had.
const cardReminderMarker = "这一轮结束了，但卡片还没有交接"

// cardReminders is how many reminders a card gets: one on the member's
// thread, one on a fresh thread.
const cardReminders = 2

// remindUnhandedCardsLocked queues the next reminder for the cards the run
// was working on that are still open, and reports whether it queued one. A
// turn the member spent on something else (the user talking to it) does
// not count: those cards were not what it set out to finish.
func remindUnhandedCardsLocked(ctx context.Context, tx *sql.Tx, teamID, botID, runID string) (bool, error) {
	cards, err := sqlite.ListTasks(ctx, tx, teamID, botID, 50)
	if err != nil {
		return false, err
	}
	var open []domain.Task
	round := 0
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
		if reminded >= cardReminders {
			continue
		}
		// One reminder speaks for one round: the cards already reminded
		// once go together, and so do the ones never reminded.
		if len(open) == 0 {
			round = reminded
		} else if reminded != round {
			continue
		}
		open = append(open, card)
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
	if round > 0 {
		retired, err := retireThreadLocked(ctx, tx, botID)
		if err != nil {
			return false, err
		}
		if retired {
			b.WriteString("上次提醒后还是没有交出。这是一个新会话，之前的完整记录见开头的说明；先确认工作目录里的改动，再交接。\n")
		}
	}
	b.WriteString("做完了就调用 handoff 交出（outcome=done）；做不下去或需要项目经理决定，就用 handoff 上报（outcome=escalate）并写明卡在哪里。" +
		"只在回复里写「已完成」不会交给下一环。")
	return true, enqueueSystemMessageLocked(ctx, tx, systemMessage{
		teamID: teamID, target: botID, text: b.String(), about: open[0].ID, taskID: open[0].ID, kind: "message",
	})
}

// retireThreadLocked lets go of the member's thread so its next turn starts
// a fresh one, handed over from it. It reports false when there is no
// thread to let go of.
func retireThreadLocked(ctx context.Context, tx *sql.Tx, botID string) (bool, error) {
	session, err := sqlite.GetProviderSession(ctx, tx, botID)
	if errors.Is(err, sqlite.ErrNotFound) || (err == nil && session.ProviderThreadID == "") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	handover := domain.ThreadHandover{
		ThreadID: session.ProviderThreadID, TranscriptPath: session.TranscriptPath,
		ModelProvider: session.ModelProvider, Model: session.Model,
	}
	if err := sqlite.RetireSessionThread(ctx, tx, botID, handover); err != nil {
		return false, err
	}
	return true, nil
}
