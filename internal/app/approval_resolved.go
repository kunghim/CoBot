package app

import (
	"context"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
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
	changed, err := sqlite.InvalidatePendingEngineRequest(ctx, a.DB, teamID, epoch, string(requestID))
	if err != nil {
		return
	}
	if changed > 0 {
		a.recordEvent(ctx, domain.Event{Type: domain.EventApprovalUpdated, TeamID: teamID})
		a.Hub.Publish()
	}
}
