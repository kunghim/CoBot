package app

import (
	"context"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// trySteer hands a user message to the member's running turn, the way a
// Codex user adds an instruction while the agent works, instead of queueing
// it behind the whole turn. The delivery is claimed into the run before the
// engine is asked, so the dispatcher can never take it as well; if the turn
// has already ended the claim is undone and the message queues as usual.
// It reports whether the running turn took the message.
func (s *Scheduler) trySteer(ctx context.Context, botID, deliveryID string, message domain.Message) bool {
	if message.Kind != domain.MessageKindUser {
		return false
	}
	run, err := sqlite.FindActiveRun(ctx, s.app.DB, botID)
	if err != nil || run.Status != domain.RunRunning || run.ProviderTurnID == "" {
		return false
	}
	rt := s.runtime(run.TeamID)
	if rt == nil || rt.server == nil || rt.server.Epoch() != run.EngineEpoch {
		return false
	}
	session, err := sqlite.GetProviderSession(ctx, s.app.DB, botID)
	if err != nil || session.ProviderThreadID == "" || session.EngineEpoch != run.EngineEpoch {
		return false
	}

	claim := func(status, runID string) bool {
		tx, err := s.app.DB.BeginTx(ctx, nil)
		if err != nil {
			return false
		}
		defer tx.Rollback()
		delivery, err := sqlite.GetDelivery(ctx, tx, deliveryID)
		if err != nil {
			return false
		}
		switch status {
		case domain.DeliveryDispatching:
			if delivery.Status != domain.DeliveryQueued {
				return false
			}
			if err := sqlite.ClaimDeliveryRun(ctx, tx, deliveryID, runID); err != nil {
				return false
			}
		case domain.DeliveryQueued:
			if delivery.Status != domain.DeliveryDispatching {
				return false
			}
			if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET status = ?, run_id = '', updated_at = ? WHERE id = ?`,
				domain.DeliveryQueued, time.Now().UTC().Format(time.RFC3339Nano), deliveryID); err != nil {
				return false
			}
		default:
			if err := sqlite.SetDeliveryStatus(ctx, tx, deliveryID, status); err != nil {
				return false
			}
		}
		if err := sqlite.SetMessageDeliveryStatus(ctx, tx, message.ID, status); err != nil {
			return false
		}
		return tx.Commit() == nil
	}

	if !claim(domain.DeliveryDispatching, run.ID) {
		return false
	}
	steerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = rt.server.SteerTurn(steerCtx, session.ProviderThreadID, run.ProviderTurnID, message.RenderText())
	cancel()
	if err != nil {
		// The turn ended (or the engine refused): queue it normally.
		s.logger.Info("steer refused; message queued", "botId", botID, "runId", run.ID, "error", err)
		claim(domain.DeliveryQueued, "")
		return false
	}
	claim(domain.DeliveryAccepted, run.ID)
	s.logger.Info("message steered into running turn", "botId", botID, "runId", run.ID)
	_ = s.app.recordEvent(ctx, domain.Event{Type: domain.EventMessageUpdated, TeamID: run.TeamID, BotID: botID, ResourceID: message.ID})
	s.app.Hub.Publish()
	return true
}

// steerNoteToRunningTurn hands a system notice to a member's running turn —
// the same channel a user's mid-turn message takes — so a fact the member
// must know now (a requirement the user just cancelled) reaches the turn it
// changes, not only the next one. Best effort: a turn that cannot take it
// still reads the note from its next turn's context.
func (s *Scheduler) steerNoteToRunningTurn(ctx context.Context, botID, text string) {
	run, err := sqlite.FindActiveRun(ctx, s.app.DB, botID)
	if err != nil || run.Status != domain.RunRunning || run.ProviderTurnID == "" {
		return
	}
	rt := s.runtime(run.TeamID)
	if rt == nil || rt.server == nil || rt.server.Epoch() != run.EngineEpoch {
		return
	}
	session, err := sqlite.GetProviderSession(ctx, s.app.DB, botID)
	if err != nil || session.ProviderThreadID == "" || session.EngineEpoch != run.EngineEpoch {
		return
	}
	steerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := rt.server.SteerTurn(steerCtx, session.ProviderThreadID, run.ProviderTurnID, text); err != nil {
		return
	}
	s.logger.Info("note steered into running turn", "botId", botID, "runId", run.ID)
}
