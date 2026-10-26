package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// MessageEditText is one edit of a member's last user message.
type MessageEditText struct {
	RequestID string `json:"requestId"`
	Text      string `json:"text"`
}

// EditMessage replaces the words of a member's last user message and sends
// them again. The turn that message started is rolled back in the engine's
// thread history and its local record goes away; file changes it made stay.
// A turn that left collaboration behind (a requirement, a hand-off, a note to
// a teammate, an approval) cannot be rolled back, so its message refuses the
// edit.
func (a *App) EditMessage(ctx context.Context, principal auth.Principal, messageID string, in MessageEditText) (SendMessageResult, error) {
	if principal.Write == nil && in.RequestID != "" {
		var err error
		principal, err = WithWriteRequest(principal, "message.edit", in.RequestID, struct {
			MessageID string
			Input     MessageEditText
		}{messageID, in})
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
	if text == "" || len(text) > 512*1024 {
		return SendMessageResult{}, domain.InvalidRequest("error.invalid_request", nil)
	}
	message, err := sqlite.GetMessage(ctx, a.DB, messageID)
	if err != nil {
		return SendMessageResult{}, sqlite.NormalizeError(err)
	}
	if message.Kind != domain.MessageKindUser {
		return SendMessageResult{}, domain.InvalidRequest("error.message.edit_not_user_message", nil)
	}
	bot, err := sqlite.GetBot(ctx, a.DB, message.BotID)
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
	default:
		return SendMessageResult{}, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}
	if later, err := sqlite.CountUserMessagesAfter(ctx, a.DB, team.ID, bot.ID, message.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return SendMessageResult{}, err
	} else if later > 0 {
		return SendMessageResult{}, domain.Conflict("error.message.edit_not_last", nil)
	}

	runIDs, err := sqlite.ListDeliveryRunsByMessage(ctx, a.DB, message.ID)
	if err != nil {
		return SendMessageResult{}, err
	}
	runID := ""
	for _, id := range runIDs {
		if id != "" {
			runID = id
			break
		}
	}
	if runID == "" {
		// The message never reached the engine: new words are all it takes.
		blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
		if err != nil {
			return SendMessageResult{}, err
		}
		message.Blocks = blocks
		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return SendMessageResult{}, err
		}
		defer tx.Rollback()
		if err := sqlite.UpdateMessageBlocks(ctx, tx, message.ID, blocks); err != nil {
			return SendMessageResult{}, err
		}
		queued, err := sqlite.CountQueuedDeliveries(ctx, tx, bot.ID)
		if err != nil {
			return SendMessageResult{}, err
		}
		result := SendMessageResult{Message: message, DeliveryStatus: message.DeliveryStatus, QueuedAhead: queued}
		if err := a.saveWriteResult(ctx, tx, principal, result); err != nil {
			return SendMessageResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return SendMessageResult{}, err
		}
		a.recordEvent(ctx, domain.Event{Type: domain.EventMessageCreated, TeamID: team.ID, BotID: bot.ID, ResourceID: message.ID,
			Payload: mustJSON(map[string]any{"messageId": message.ID, "kind": message.Kind})})
		a.Hub.Publish()
		return result, nil
	}
	run, err := sqlite.GetRun(ctx, a.DB, runID)
	if err != nil {
		return SendMessageResult{}, sqlite.NormalizeError(err)
	}
	switch run.Status {
	case domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval, domain.RunWaitingInput, domain.RunUnknown:
		return SendMessageResult{}, domain.Conflict("error.message.edit_running", map[string]any{"status": run.Status})
	}
	if sideEffects, err := sqlite.RunHasCollaborationSideEffects(ctx, a.DB, run.ID); err != nil {
		return SendMessageResult{}, err
	} else if sideEffects {
		return SendMessageResult{}, domain.Conflict("error.message.edit_side_effects", nil)
	}
	// The engine's thread history rolls back on the team's engine: resuming
	// it loads the same thread the turn ran on, so the revert lands even after
	// a restart.
	if a.Scheduler == nil {
		return SendMessageResult{}, domain.Conflict("error.engine.not_running", nil)
	}
	if run.ProviderTurnID != "" {
		rt := a.Scheduler.runtime(team.ID)
		var server engine.Server
		if rt != nil {
			server = rt.server
		} else if team.Directory != "" {
			if started, err := a.Scheduler.EnsureTeamServer(ctx, team.ID, team.Directory); err == nil {
				server = started
			}
		}
		if server == nil {
			return SendMessageResult{}, domain.Conflict("error.engine.not_running", nil)
		}
		session, err := sqlite.GetProviderSession(ctx, a.DB, bot.ID)
		if err != nil {
			return SendMessageResult{}, err
		}
		if err := server.RevertTurn(ctx, session.ProviderThreadID, run.ProviderTurnID); err != nil {
			return SendMessageResult{}, domain.EngineUnavailable("error.message.revert_failed", nil, err)
		}
	}
	// The turn's local record goes with its history.
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return SendMessageResult{}, err
	}
	defer tx.Rollback()
	if err := sqlite.DeleteRunSteps(ctx, tx, run.ID); err != nil {
		return SendMessageResult{}, err
	}
	if err := sqlite.DeleteRun(ctx, tx, run.ID); err != nil {
		return SendMessageResult{}, err
	}
	if err := sqlite.DeleteMessageDeliveries(ctx, tx, message.ID); err != nil {
		return SendMessageResult{}, err
	}
	if err := sqlite.DeleteRunMessages(ctx, tx, team.ID, bot.ID, run.ID, message.ID); err != nil {
		return SendMessageResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return SendMessageResult{}, err
	}
	a.Hub.Publish()
	// The new words go out as a message of their own, with the chain and the
	// delivery any user message gets. The enqueue runs inside this request's
	// write scope: SendMessage's own would deadlock on it.
	return a.enqueueUserMessage(ctx, principal, team, bot, text, in.RequestID+":resend")
}
