package app

import (
	"context"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/domain"
)

// settleEngineRequest closes an approval card Codex no longer waits for: the
// engine reports every server request it settled, and one CoBot did not
// answer (a turn interrupted, a sub-agent closed) must not stay on screen as
// if a decision were still needed. A card the user already decided is no
// longer pending and is left as it is.
func (a *App) settleEngineRequest(ctx context.Context, teamID, epoch string, requestID json.RawMessage) {
	if len(requestID) == 0 {
		return
	}
	result, err := a.DB.ExecContext(ctx, `UPDATE approvals SET request_state = ?, revision = revision + 1
		WHERE team_id = ? AND engine_epoch = ? AND engine_request_id = ? AND request_state = ?`,
		domain.ApprovalStateInvalidated, teamID, epoch, string(requestID), domain.ApprovalStatePending)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed > 0 {
		a.recordEvent(ctx, domain.Event{Type: domain.EventApprovalUpdated, TeamID: teamID})
		a.Hub.Publish()
	}
}
