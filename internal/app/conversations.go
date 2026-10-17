package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// SendMessageInput is the user chat send request.
type SendMessageInput struct {
	BotID     string
	Text      string
	RequestID string
}

// SendMessageResult reports what actually happened; accepted is never
// claimed before the fact.
type SendMessageResult struct {
	Message        domain.Message `json:"message"`
	DeliveryStatus string         `json:"deliveryStatus"`
	QueuedAhead    int            `json:"queuedAhead"`
}

// SendMessage stores the user message and a queued delivery in one
// transaction, then wakes the scheduler. A repeated requestId replays the
// stored message; the same id with different text is a conflict.
func (a *App) SendMessage(ctx context.Context, principal auth.Principal, in SendMessageInput) (SendMessageResult, error) {
	if principal.Write == nil && in.RequestID != "" {
		var err error
		principal, err = WithWriteRequest(principal, "message.send", in.RequestID, in)
		if err != nil {
			return SendMessageResult{}, err
		}
	}
	cached, unlock, writeErr := beginWrite[SendMessageResult](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return SendMessageResult{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindUser {
		return SendMessageResult{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return SendMessageResult{}, domain.InvalidRequest("error.message.text_required", nil)
	}
	bot, err := sqlite.GetBot(ctx, a.DB, in.BotID)
	if err != nil {
		return SendMessageResult{}, sqlite.NormalizeError(err)
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, bot.TeamID)
	if err != nil {
		return SendMessageResult{}, sqlite.NormalizeError(err)
	}
	switch team.Status {
	case domain.TeamStatusActive:
	case domain.TeamStatusArchived:
		return SendMessageResult{}, domain.Conflict("error.team.archived", nil)
	case domain.TeamStatusDirectoryMissing:
		return SendMessageResult{}, domain.Conflict("error.team.directory_missing", map[string]any{"directory": team.Directory})
	default:
		return SendMessageResult{}, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}

	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return SendMessageResult{}, err
	}
	if in.RequestID != "" {
		if existing, err := sqlite.FindMessageByRequestID(ctx, a.DB, team.ID, in.RequestID); err == nil {
			if existing.BotID != bot.ID || existing.Kind != domain.MessageKindUser || hashBlocks(existing.Blocks) != hashBlocks(blocks) {
				return SendMessageResult{}, domain.IdempotencyConflict("error.message.request_reused", map[string]any{"requestId": in.RequestID})
			}
			queued, _ := sqlite.CountQueuedDeliveries(ctx, a.DB, bot.ID)
			return SendMessageResult{Message: existing, DeliveryStatus: existing.DeliveryStatus, QueuedAhead: queued}, nil
		} else if !errors.Is(err, sqlite.ErrNotFound) {
			return SendMessageResult{}, err
		}
	}

	message := domain.Message{
		ID: domain.NewID(), TeamID: team.ID, BotID: bot.ID,
		Kind: domain.MessageKindUser, Blocks: blocks,
		RequestID: in.RequestID, DeliveryStatus: domain.DeliveryQueued,
		CreatedAt: time.Now().UTC(),
	}
	deliveryID := domain.NewID()
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return SendMessageResult{}, err
	}
	defer tx.Rollback()
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		if isUniqueViolation(err) {
			return SendMessageResult{}, domain.IdempotencyConflict("error.message.request_reused", map[string]any{"requestId": in.RequestID})
		}
		return SendMessageResult{}, err
	}
	// A user message opens a new causal chain (G02). It never clears a chain
	// that is still running; that one keeps its own budget.
	chain, err := a.openChainLocked(ctx, tx, team.ID, message.ID, "")
	if err != nil {
		return SendMessageResult{}, err
	}
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: deliveryID, MessageID: message.ID, TeamID: team.ID,
		TargetBotID: bot.ID, Status: domain.DeliveryQueued, ChainID: chain.ID,
	}); err != nil {
		return SendMessageResult{}, err
	}
	// Enqueueing a user message never clears a live run's projection
	// (review B minor13).
	if _, ferr := sqlite.FindActiveRun(ctx, tx, bot.ID); ferr != nil && errors.Is(ferr, sqlite.ErrNotFound) {
		if err := sqlite.SetBotRunState(ctx, tx, bot.ID, domain.BotStatusQueued, ""); err != nil {
			return SendMessageResult{}, err
		}
	}
	queued, err := sqlite.CountQueuedDeliveries(ctx, tx, bot.ID)
	if err != nil {
		return SendMessageResult{}, err
	}
	result := SendMessageResult{Message: message, DeliveryStatus: domain.DeliveryQueued, QueuedAhead: queued}
	if err := a.saveWriteResult(ctx, tx, principal, result); err != nil {
		return SendMessageResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return SendMessageResult{}, err
	}
	if err := a.recordEvent(ctx, domain.Event{
		Type: domain.EventMessageCreated, TeamID: team.ID, BotID: bot.ID, ResourceID: message.ID,
		Payload: mustJSON(map[string]any{"messageId": message.ID, "kind": message.Kind}),
	}); err != nil {
		return SendMessageResult{}, err
	}
	a.Hub.Publish()
	if a.Scheduler != nil {
		// A busy member takes the user's message into the turn it is working
		// on; otherwise the message waits for the next turn.
		if a.Scheduler.trySteer(ctx, bot.ID, deliveryID, message) {
			result.DeliveryStatus = domain.DeliveryAccepted
			result.QueuedAhead = 0
		} else {
			a.Scheduler.Wake(bot.ID)
		}
	}
	return result, nil
}

// CancelQueuedMessage cancels a not-yet-dispatched user message. Dispatched
// or completed work is never claimed to be retractable.
func (a *App) CancelQueuedMessage(ctx context.Context, principal auth.Principal, messageID string) (domain.Message, error) {
	cached, unlock, writeErr := beginWrite[domain.Message](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Message{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}

	if principal.Kind != auth.KindUser {
		return domain.Message{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	message, err := sqlite.GetMessage(ctx, a.DB, messageID)
	if err != nil {
		return domain.Message{}, sqlite.NormalizeError(err)
	}
	if message.Kind != domain.MessageKindUser {
		return domain.Message{}, domain.InvalidRequest("error.message.not_user_message", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Message{}, err
	}
	defer tx.Rollback()
	deliveries, err := sqlite.ListDeliveriesByMessage(ctx, tx, messageID)
	if err != nil {
		return domain.Message{}, err
	}
	for _, delivery := range deliveries {
		switch delivery.Status {
		case domain.DeliveryQueued:
			if err := sqlite.SetDeliveryStatus(ctx, tx, delivery.ID, domain.DeliveryCancelled); err != nil {
				return domain.Message{}, err
			}
		case domain.DeliveryDispatching, domain.DeliveryAccepted:
			return domain.Message{}, domain.Conflict("error.message.already_dispatched", map[string]any{"status": delivery.Status})
		}
	}
	if err := sqlite.SetMessageDeliveryStatus(ctx, tx, messageID, domain.DeliveryCancelled); err != nil {
		return domain.Message{}, err
	}
	updated, err := sqlite.GetMessage(ctx, tx, messageID)
	if err != nil {
		return domain.Message{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, updated); err != nil {
		return domain.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Message{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventMessageUpdated, TeamID: message.TeamID, BotID: message.BotID, ResourceID: message.ID})
	a.Hub.Publish()
	return updated, nil
}

// ListConversation returns the paged message history of one bot plus the
// current queue depth for the composer state.
func (a *App) ListConversation(ctx context.Context, principal auth.Principal, botID, before string, limit int) (map[string]any, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	bot, err := sqlite.GetBot(ctx, a.DB, botID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	messages, err := sqlite.ListMessages(ctx, a.DB, botID, before, limit)
	if err != nil {
		return nil, err
	}
	queued, err := sqlite.CountQueuedDeliveries(ctx, a.DB, botID)
	if err != nil {
		return nil, err
	}
	var activeRun any
	if run, err := sqlite.FindActiveRun(ctx, a.DB, botID); err == nil {
		activeRun = run
	}
	if messages == nil {
		messages = []domain.Message{}
	}
	return map[string]any{
		"messages":    messages,
		"queuedCount": queued,
		"bot":         bot,
		"activeRun":   activeRun,
	}, nil
}

// GetRun returns one run's product facts.
func (a *App) GetRun(ctx context.Context, principal auth.Principal, runID string) (domain.Run, error) {
	if principal.Kind != auth.KindUser {
		return domain.Run{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	run, err := sqlite.GetRun(ctx, a.DB, runID)
	return run, sqlite.NormalizeError(err)
}

func hashBlocks(blocks json.RawMessage) string {
	sum := sha256.Sum256(blocks)
	return hex.EncodeToString(sum[:])
}
