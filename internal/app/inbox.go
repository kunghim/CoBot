package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The inbox is what a member has been told and what is waiting for it: its
// messages and their deliveries. Every write to them goes through here, except
// deleting a team or a member, which the store keeps in one place.
//
// Each operation runs on the caller's transaction. None of them changes a
// member's state, records an event or wakes anyone: that is the caller's call,
// and it differs from one caller to the next.

// inboxPostTx writes a message. A message that opens a causal chain is written
// first, because the chain names it as its root; inboxQueueTx then delivers it.
func inboxPostTx(ctx context.Context, tx *sql.Tx, message domain.Message) error {
	return sqlite.InsertMessage(ctx, tx, message)
}

// inboxQueueTx puts a written message in line for its member.
func inboxQueueTx(ctx context.Context, tx *sql.Tx, record sqlite.DeliveryRecord) error {
	return sqlite.InsertDeliveryRow(ctx, tx, record)
}

// inboxEnqueueTx writes a message and puts it in line.
func inboxEnqueueTx(ctx context.Context, tx *sql.Tx, message domain.Message, record sqlite.DeliveryRecord) error {
	if err := inboxPostTx(ctx, tx, message); err != nil {
		return err
	}
	return inboxQueueTx(ctx, tx, record)
}

// inboxNoteTx leaves a member a note it reads with its next turn; it wakes
// nobody.
func inboxNoteTx(ctx context.Context, tx *sql.Tx, teamID, target, source, about, text string) error {
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return err
	}
	return inboxPostTx(ctx, tx, domain.Message{
		ID: domain.NewID(), TeamID: teamID, BotID: target,
		Kind: domain.MessageKindCollaboration, Blocks: blocks, SourceBotID: source, RunID: about,
	})
}

// inboxRecordTx leaves the user a note in a member's conversation, about a
// decision made from a card there. It wakes nobody and the engine never reads
// it: it is a record of what happened, not an instruction.
func inboxRecordTx(ctx context.Context, tx *sql.Tx, teamID, botID, text string) error {
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return err
	}
	return inboxPostTx(ctx, tx, domain.Message{
		ID: domain.NewID(), TeamID: teamID, BotID: botID,
		Kind: domain.MessageKindSystem, Blocks: blocks, CreatedAt: time.Now().UTC(),
	})
}

// inboxAssistantReplyTx writes what a turn answered and returns its id.
func inboxAssistantReplyTx(ctx context.Context, tx *sql.Tx, teamID, botID, runID, text string) (string, error) {
	blocks, _ := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	message := domain.Message{
		ID: domain.NewID(), TeamID: teamID, BotID: botID,
		Kind: domain.MessageKindAssistant, Blocks: blocks, RunID: runID,
		CreatedAt: time.Now().UTC(),
	}
	if err := inboxPostTx(ctx, tx, message); err != nil {
		return "", err
	}
	return message.ID, nil
}

// inboxEditTx replaces the words of a message nobody has acted on yet.
func inboxEditTx(ctx context.Context, tx *sql.Tx, messageID string, blocks []byte) error {
	return sqlite.UpdateMessageBlocks(ctx, tx, messageID, blocks)
}

// inboxReplaceTurnTx drops an edited message and everything its turn left, so
// the new words can go out as a message of their own.
func inboxReplaceTurnTx(ctx context.Context, tx *sql.Tx, teamID, botID, runID, messageID string) error {
	if err := sqlite.DeleteMessageDeliveries(ctx, tx, messageID); err != nil {
		return err
	}
	return sqlite.DeleteRunMessages(ctx, tx, teamID, botID, runID, messageID)
}

// inboxSetTx moves one delivery and its message to a status together.
func inboxSetTx(ctx context.Context, tx *sql.Tx, deliveryID, messageID, status string) error {
	if err := sqlite.SetDeliveryStatus(ctx, tx, deliveryID, status); err != nil {
		return err
	}
	return sqlite.SetMessageDeliveryStatus(ctx, tx, messageID, status)
}

// inboxCancelTx takes one delivery and its message out of line.
func inboxCancelTx(ctx context.Context, tx *sql.Tx, deliveryID, messageID string) error {
	return inboxSetTx(ctx, tx, deliveryID, messageID, domain.DeliveryCancelled)
}

// inboxRequeueTx puts one delivery and its message back in line.
func inboxRequeueTx(ctx context.Context, tx *sql.Tx, deliveryID, messageID string) error {
	return inboxSetTx(ctx, tx, deliveryID, messageID, domain.DeliveryQueued)
}

// inboxAcceptTx records that the engine took one delivery.
func inboxAcceptTx(ctx context.Context, tx *sql.Tx, deliveryID, messageID string) error {
	return inboxSetTx(ctx, tx, deliveryID, messageID, domain.DeliveryAccepted)
}

// inboxCancelDeliveryTx takes one of a message's deliveries out of line. A
// caller going through all of them ends with inboxCancelMessageTx.
func inboxCancelDeliveryTx(ctx context.Context, tx *sql.Tx, deliveryID string) error {
	return sqlite.SetDeliveryStatus(ctx, tx, deliveryID, domain.DeliveryCancelled)
}

// inboxCancelMessageTx marks a message whose deliveries are all out of line.
func inboxCancelMessageTx(ctx context.Context, tx *sql.Tx, messageID string) error {
	return sqlite.SetMessageDeliveryStatus(ctx, tx, messageID, domain.DeliveryCancelled)
}

// inboxCancelChainTx cancels what a stopped chain still has in line.
func inboxCancelChainTx(ctx context.Context, tx *sql.Tx, chainID string) (int64, error) {
	return sqlite.CancelChainDeliveries(ctx, tx, chainID)
}

// inboxCancelForCardTx cancels what a cancelled card still has in line.
func inboxCancelForCardTx(ctx context.Context, tx *sql.Tx, cardID string) error {
	return sqlite.CancelQueuedForTask(ctx, tx, cardID)
}

// inboxClaimTx hands a delivery and its message to the run that will carry them.
func inboxClaimTx(ctx context.Context, tx *sql.Tx, deliveryID, messageID, runID string) error {
	if err := sqlite.SetMessageDeliveryStatus(ctx, tx, messageID, domain.DeliveryDispatching); err != nil {
		return err
	}
	return sqlite.ClaimDeliveryRun(ctx, tx, deliveryID, runID)
}

// inboxWithdrawForSteerTx puts a delivery the running turn refused back in
// line, no longer tied to that run.
func inboxWithdrawForSteerTx(ctx context.Context, tx *sql.Tx, deliveryID, messageID string) error {
	if err := sqlite.WithdrawDelivery(ctx, tx, deliveryID); err != nil {
		return err
	}
	return sqlite.SetMessageDeliveryStatus(ctx, tx, messageID, domain.DeliveryQueued)
}

// inboxMarkRunTx moves a run's deliveries that are in one of the from statuses
// to a status together.
func inboxMarkRunTx(ctx context.Context, tx *sql.Tx, runID, status string, from ...string) ([]string, error) {
	return sqlite.SetRunDeliveriesStatus(ctx, tx, runID, status, from...)
}

// inboxMarkRunUnknownTx records that nobody knows how far a run's delivery got,
// and returns that delivery.
func inboxMarkRunUnknownTx(ctx context.Context, tx *sql.Tx, runID string) (string, error) {
	return sqlite.MarkDeliveryUnknownByRun(ctx, tx, runID)
}
