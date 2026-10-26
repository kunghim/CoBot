package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/failure"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

type liveTurnKey struct{ threadID, turnID string }

// liveRun is the run a provider turn belongs to, plus the reconnect attempts
// seen so far in it.
type liveRun struct {
	teamID, botID, runID string
	// subAgent is set when the turn is a sub-agent's, working inside the
	// member's run; its steps are kept apart from the member's own.
	subAgent string
	retries  int
	// retryOpen is true while the retry step still says "reconnecting".
	retryOpen bool
}

// attemptPattern reads the engine's own count from "Reconnecting... 3/5".
var attemptPattern = regexp.MustCompile(`(\d+)\s*/\s*\d+`)

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
	subAgent := ""
	if runID == "" {
		// A sub-agent's turn has its own ids: it works inside the live run
		// of the member whose thread spawned it.
		runID, subAgent = s.subAgentRun(ctx, rt, threadID)
	}
	if runID == "" {
		return liveRun{}, false
	}
	run, err := sqlite.GetRun(ctx, s.app.DB, runID)
	if err != nil {
		return liveRun{}, false
	}
	ref = liveRun{teamID: teamID, botID: run.BotID, runID: run.ID, subAgent: subAgent}
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

// providerSecrets returns every provider key CoBot holds. The engine runs
// with them in its environment, so a vendor's error body can echo one back.
func (s *Scheduler) providerSecrets(ctx context.Context) []string {
	if s.app == nil {
		return nil
	}
	env, err := s.app.ProviderEnv(ctx)
	if err != nil {
		return nil
	}
	secrets := make([]string, 0, len(env))
	for _, secret := range env {
		if secret != "" {
			secrets = append(secrets, secret)
		}
	}
	return secrets
}

// scrub takes provider keys out of text that is about to be stored or logged.
// A vendor routinely repeats the key that was rejected in its error body, and
// the engine passes that body on in its own message.
func scrub(text string, secrets []string) string {
	for _, secret := range secrets {
		if strings.Contains(text, secret) {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return text
}

// onProgress records and relays what a running turn is doing: its steps,
// streamed text and engine problems. It returns false for events that carry
// no progress, which the caller handles as before.
func (s *Scheduler) onProgress(ctx context.Context, teamID string, rt *teamRuntime, n engine.Notification) bool {
	if n.Step == nil && n.DeltaKind == "" && n.Problem == nil {
		return false
	}
	// What the user gets to read about a failure: the root cause when the
	// vendor's status says what it was, and otherwise the engine's own
	// words with every provider key taken out of them.
	message := ""
	if n.Problem != nil {
		message = scrub(n.Problem.Message, s.providerSecrets(ctx))
		if n.Problem.FailureCode != "" {
			message = failure.Text(n.Problem.FailureCode)
		}
	}
	ref, ok := s.resolveLiveRun(ctx, teamID, rt, n.ThreadID, n.TurnID)
	if !ok {
		if n.Problem != nil {
			// A notice outside any turn of ours is only logged.
			s.logger.Info("engine notice", "teamId", teamID, "level", n.Problem.Level, "message", message)
		}
		return true
	}
	switch {
	case n.Problem != nil && n.Problem.WillRetry:
		attempt := ref.retries + 1
		if m := attemptPattern.FindStringSubmatch(message); m != nil {
			if reported, err := strconv.Atoi(m[1]); err == nil && reported > 0 {
				attempt = reported
			}
		}
		ref.retries, ref.retryOpen = attempt, true
		rt.storeLiveRun(n.ThreadID, n.TurnID, ref)
		detail, _ := json.Marshal(map[string]any{"attempt": attempt, "message": message, "code": n.Problem.Code})
		s.saveStep(ctx, ref, domain.RunStep{ID: retryStepID, Kind: domain.StepKindRetry, Status: engine.StepInProgress, Detail: detail})
	case n.Problem != nil:
		s.settleRetry(ctx, rt, n, &ref)
		detail, _ := json.Marshal(map[string]any{"level": n.Problem.Level, "message": message, "code": n.Problem.Code, "failureCode": n.Problem.FailureCode})
		s.saveStep(ctx, ref, domain.RunStep{ID: "cobot-notice-" + domain.NewID(), Kind: domain.StepKindNotice, Status: engine.StepCompleted, Detail: detail})
	case n.Step != nil:
		s.settleRetry(ctx, rt, n, &ref)
		step := domain.RunStep{ID: n.Step.ID, Kind: n.Step.Kind, Status: n.Step.Status, Detail: n.Step.Detail}
		if ref.subAgent != "" {
			step.ID, step.Detail = subAgentStep(ref.subAgent, step.ID, step.Detail)
		}
		s.saveStep(ctx, ref, step)
		// A step notification may carry streamed text of its own (a message
		// item the turn never announced is announced by its first delta).
		s.publishDelta(n, &ref)
	default:
		if n.Delta == "" {
			return true
		}
		s.settleRetry(ctx, rt, n, &ref)
		s.publishDelta(n, &ref)
	}
	return true
}

// publishDelta streams one piece of text into the conversation. A sub-agent's
// answer is its result, shown with its steps, never streamed as the member's
// reply.
func (s *Scheduler) publishDelta(n engine.Notification, ref *liveRun) {
	if n.Delta == "" {
		return
	}
	itemID, kind := n.ItemID, n.DeltaKind
	if ref.subAgent != "" {
		itemID, _ = subAgentStep(ref.subAgent, itemID, nil)
		if kind == "message" {
			kind = "commentary"
		}
	}
	s.app.Live.Publish(events.Live{Type: "delta", TeamID: ref.teamID, BotID: ref.botID, RunID: ref.runID, ItemID: itemID, Kind: kind, Text: n.Delta})
}

func (rt *teamRuntime) storeLiveRun(threadID, turnID string, ref liveRun) {
	rt.mu.Lock()
	if rt.live == nil {
		rt.live = map[liveTurnKey]liveRun{}
	}
	rt.live[liveTurnKey{threadID, turnID}] = ref
	rt.mu.Unlock()
}

// saveStep stores a step and relays its stored form.
func (s *Scheduler) saveStep(ctx context.Context, ref liveRun, step domain.RunStep) {
	step.RunID, step.TeamID, step.BotID = ref.runID, ref.teamID, ref.botID
	stored, err := sqlite.UpsertRunStep(ctx, s.app.DB, step)
	if err != nil {
		s.logger.Warn("record run step", "runId", ref.runID, "error", err)
		return
	}
	s.app.Live.Publish(events.Live{Type: "step", TeamID: ref.teamID, BotID: ref.botID, RunID: ref.runID, Step: stored})
}

// settleRetry closes an open "reconnecting" step: any real progress (a step,
// streamed text, another notice) means the connection is back.
func (s *Scheduler) settleRetry(ctx context.Context, rt *teamRuntime, n engine.Notification, ref *liveRun) {
	if !ref.retryOpen {
		return
	}
	ref.retryOpen = false
	rt.storeLiveRun(n.ThreadID, n.TurnID, *ref)
	current, err := sqlite.GetRunStep(ctx, s.app.DB, ref.runID, retryStepID)
	if err != nil || current.Status != engine.StepInProgress {
		return
	}
	current.Status = engine.StepCompleted
	s.saveStep(ctx, *ref, current)
}
