package app

import (
	"context"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

type liveTurnKey struct{ threadID, turnID string }

// liveRun is the run a provider turn belongs to, plus the reconnect attempts
// seen so far in it.
type liveRun struct {
	teamID, botID, runID string
	retries              int
}

// retryStepID is the one step that tracks a run's reconnect attempts.
const retryStepID = "cobot-retry"

// resolveLiveRun maps a turn's event onto its run, caching the answer for
// the rest of the turn.
func (s *Scheduler) resolveLiveRun(ctx context.Context, teamID string, rt *teamRuntime, threadID, turnID string) (liveRun, bool) {
	if threadID == "" || turnID == "" {
		return liveRun{}, false
	}
	key := liveTurnKey{threadID, turnID}
	rt.mu.Lock()
	ref, ok := rt.live[key]
	rt.mu.Unlock()
	if ok {
		return ref, true
	}
	runID := s.findRunByTurn(ctx, rt.server.Epoch(), threadID, turnID)
	if runID == "" {
		return liveRun{}, false
	}
	run, err := sqlite.GetRun(ctx, s.app.DB, runID)
	if err != nil {
		return liveRun{}, false
	}
	ref = liveRun{teamID: teamID, botID: run.BotID, runID: run.ID}
	rt.mu.Lock()
	if rt.live == nil {
		rt.live = map[liveTurnKey]liveRun{}
	}
	if cached, ok := rt.live[key]; ok {
		ref = cached
	} else {
		rt.live[key] = ref
	}
	rt.mu.Unlock()
	return ref, true
}

// forgetLiveRun drops a finished turn from the cache.
func (rt *teamRuntime) forgetLiveRun(threadID, turnID string) {
	rt.mu.Lock()
	delete(rt.live, liveTurnKey{threadID, turnID})
	rt.mu.Unlock()
}

// onProgress records and relays what a running turn is doing: its steps,
// streamed text and engine problems. It returns false for events that carry
// no progress, which the caller handles as before.
func (s *Scheduler) onProgress(ctx context.Context, teamID string, rt *teamRuntime, n engine.Notification) bool {
	switch {
	case n.Step != nil:
		ref, ok := s.resolveLiveRun(ctx, teamID, rt, n.ThreadID, n.TurnID)
		if !ok {
			return true
		}
		s.saveStep(ctx, ref, domain.RunStep{ID: n.Step.ID, Kind: n.Step.Kind, Status: n.Step.Status, Detail: n.Step.Detail})
		return true
	case n.DeltaKind != "":
		ref, ok := s.resolveLiveRun(ctx, teamID, rt, n.ThreadID, n.TurnID)
		if !ok || n.Delta == "" {
			return true
		}
		s.app.Live.Publish(events.Live{Type: "delta", TeamID: ref.teamID, BotID: ref.botID, RunID: ref.runID, ItemID: n.ItemID, Kind: n.DeltaKind, Text: n.Delta})
		return true
	case n.Problem != nil:
		ref, ok := s.resolveLiveRun(ctx, teamID, rt, n.ThreadID, n.TurnID)
		if !ok {
			// A warning outside any turn of ours is only logged.
			s.logger.Info("engine notice", "teamId", teamID, "level", n.Problem.Level, "message", n.Problem.Message)
			return true
		}
		if n.Problem.WillRetry {
			rt.mu.Lock()
			ref.retries++
			rt.live[liveTurnKey{n.ThreadID, n.TurnID}] = ref
			rt.mu.Unlock()
			detail, _ := json.Marshal(map[string]any{"attempt": ref.retries, "message": n.Problem.Message, "code": n.Problem.Code})
			s.saveStep(ctx, ref, domain.RunStep{ID: retryStepID, Kind: domain.StepKindRetry, Status: engine.StepInProgress, Detail: detail})
			return true
		}
		detail, _ := json.Marshal(map[string]any{"level": n.Problem.Level, "message": n.Problem.Message, "code": n.Problem.Code})
		s.saveStep(ctx, ref, domain.RunStep{ID: "cobot-notice-" + domain.NewID(), Kind: domain.StepKindNotice, Status: engine.StepCompleted, Detail: detail})
		return true
	}
	return false
}

// saveStep stores a step and relays its stored form. Any real progress after
// a reconnect means the connection is back, so the retry step settles.
func (s *Scheduler) saveStep(ctx context.Context, ref liveRun, step domain.RunStep) {
	step.RunID, step.TeamID, step.BotID = ref.runID, ref.teamID, ref.botID
	if step.Kind != domain.StepKindRetry && ref.retries > 0 {
		s.settleRetry(ctx, ref)
	}
	stored, err := sqlite.UpsertRunStep(ctx, s.app.DB, step)
	if err != nil {
		s.logger.Warn("record run step", "runId", ref.runID, "error", err)
		return
	}
	s.app.Live.Publish(events.Live{Type: "step", TeamID: ref.teamID, BotID: ref.botID, RunID: ref.runID, Step: stored})
}

func (s *Scheduler) settleRetry(ctx context.Context, ref liveRun) {
	current, err := sqlite.GetRunStep(ctx, s.app.DB, ref.runID, retryStepID)
	if err != nil || current.Status != engine.StepInProgress {
		return
	}
	current.Status = engine.StepCompleted
	if stored, err := sqlite.UpsertRunStep(ctx, s.app.DB, current); err == nil {
		s.app.Live.Publish(events.Live{Type: "step", TeamID: ref.teamID, BotID: ref.botID, RunID: ref.runID, Step: stored})
	}
}
