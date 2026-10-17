package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Codex sub-agents. A member may ask Codex to spawn sub-agents inside its own
// session; they are not team members. Each runs in its own thread, which
// Codex links to the thread that spawned it. CoBot follows that link to the
// member and attributes the sub-agent's steps, approvals, guard checks, tool
// calls and usage to the member's live run.

// rootThreader is an engine that knows which thread a sub-agent's descends
// from.
type rootThreader interface {
	RootThread(threadID string) string
}

// RootThread is the member thread a (sub-agent) thread descends from, or the
// thread itself.
func (s *Scheduler) RootThread(teamID, threadID string) string {
	rt := s.runtime(teamID)
	if rt == nil {
		return threadID
	}
	return rootOf(rt, threadID)
}

func rootOf(rt *teamRuntime, threadID string) string {
	if rt == nil || rt.server == nil {
		return threadID
	}
	if roots, ok := rt.server.(rootThreader); ok {
		return roots.RootThread(threadID)
	}
	return threadID
}

// subAgentRun finds the live run a sub-agent thread works in: the active run
// of the member whose thread spawned it. It returns the run and the
// sub-agent thread, or "" when the thread is no sub-agent of a live member.
func (s *Scheduler) subAgentRun(ctx context.Context, rt *teamRuntime, threadID string) (string, string) {
	root := rootOf(rt, threadID)
	if root == threadID {
		return "", ""
	}
	botID, err := s.app.botByThread(ctx, rt.server.Epoch(), root)
	if err != nil {
		return "", ""
	}
	run, err := sqlite.FindActiveRun(ctx, s.app.DB, botID)
	if errors.Is(err, sqlite.ErrNotFound) || err != nil {
		return "", ""
	}
	return run.ID, threadID
}

// subAgentStep keeps a sub-agent's step apart from the member's own (item ids
// are only unique inside one thread) and marks whose it is.
func subAgentStep(thread, itemID string, detail json.RawMessage) (string, json.RawMessage) {
	short := thread
	if len(short) > 8 {
		short = short[len(short)-8:]
	}
	if len(detail) == 0 {
		return "sub-" + short + ":" + itemID, detail
	}
	fields := map[string]any{}
	if json.Unmarshal(detail, &fields) == nil {
		fields["subAgentThread"] = thread
		if marked, err := json.Marshal(fields); err == nil {
			detail = marked
		}
	}
	return "sub-" + short + ":" + itemID, detail
}

// runForThread finds the run a thread's turn belongs to: the member's own
// turn, or for a sub-agent the member's live run.
func (s *Scheduler) runForThread(ctx context.Context, rt *teamRuntime, threadID, turnID string) (domain.Run, bool) {
	runID := s.findRunByTurn(ctx, rt.server.Epoch(), threadID, turnID)
	if runID == "" {
		runID, _ = s.subAgentRun(ctx, rt, threadID)
	}
	if runID == "" {
		return domain.Run{}, false
	}
	run, err := sqlite.GetRun(ctx, s.app.DB, runID)
	return run, err == nil
}
