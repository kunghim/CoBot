package app

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Session statistics are read off what Codex already reports (turn and step
// timings, token usage, the account's rate limits); computing them never
// calls the model.

// SessionStats is one member's conversation in numbers.
type SessionStats struct {
	Turns int `json:"turns"`
	Steps int `json:"steps"`
	// ModelMs is time the model spent (turn time less tools and approvals);
	// ToolMs is time spent running commands and tools.
	ModelMs int64 `json:"modelMs"`
	ToolMs  int64 `json:"toolMs"`
	// FirstStepMs estimates the time to first token: from a turn's start to
	// its first visible step, averaged over turns.
	FirstStepMs int64 `json:"firstStepMs,omitempty"`
	// OutputTokensPerSecond estimates output speed over the model time.
	OutputTokensPerSecond float64 `json:"outputTokensPerSecond,omitempty"`
	Tokens                struct {
		Input      int64 `json:"input"`
		CachedRead int64 `json:"cachedRead"`
		CacheWrite int64 `json:"cacheWrite"`
		Output     int64 `json:"output"`
		Reasoning  int64 `json:"reasoning"`
	} `json:"tokens"`
	// Context is the current thread: what the last model call sent and the
	// model's window.
	Context struct {
		Used   int64 `json:"used"`
		Window int64 `json:"window,omitempty"`
	} `json:"context"`
	RateLimits json.RawMessage `json:"rateLimits,omitempty"`
}

// rateLimitStore keeps the account's latest rate limits from Codex.
type rateLimitStore struct {
	mu  sync.Mutex
	raw json.RawMessage
}

var accountRateLimits rateLimitStore

// recordRateLimits keeps the newest account/rateLimits/updated payload.
func (a *App) recordRateLimits(raw json.RawMessage) {
	var p struct {
		RateLimits json.RawMessage `json:"rateLimits"`
	}
	if json.Unmarshal(raw, &p) != nil || len(p.RateLimits) == 0 {
		return
	}
	accountRateLimits.mu.Lock()
	accountRateLimits.raw = p.RateLimits
	accountRateLimits.mu.Unlock()
}

// BotStats computes one member's session statistics.
func (a *App) BotStats(ctx context.Context, principal auth.Principal, botID string) (SessionStats, error) {
	var stats SessionStats
	if principal.Kind != auth.KindUser {
		return stats, domain.Unauthorized("error.unauthorized.user_required")
	}
	bot, err := sqlite.GetBot(ctx, a.DB, botID)
	if err != nil {
		return stats, sqlite.NormalizeError(err)
	}
	parse := func(s string) time.Time {
		t, _ := time.Parse(time.RFC3339Nano, s)
		return t
	}

	var runMs int64
	firstStep := map[string]time.Time{}
	runStart := map[string]time.Time{}
	rows, err := a.DB.QueryContext(ctx, `SELECT id, started_at, ended_at FROM runs WHERE bot_id = ? AND ended_at != ''`, botID)
	if err != nil {
		return stats, err
	}
	for rows.Next() {
		var id, started, ended string
		if rows.Scan(&id, &started, &ended) != nil {
			continue
		}
		s, e := parse(started), parse(ended)
		if s.IsZero() || e.Before(s) {
			continue
		}
		stats.Turns++
		runMs += e.Sub(s).Milliseconds()
		runStart[id] = s
	}
	rows.Close()

	rows, err = a.DB.QueryContext(ctx, `SELECT run_id, kind, status, started_at, updated_at FROM run_steps WHERE bot_id = ?`, botID)
	if err != nil {
		return stats, err
	}
	for rows.Next() {
		var runID, kind, status, started, updated string
		if rows.Scan(&runID, &kind, &status, &started, &updated) != nil {
			continue
		}
		stats.Steps++
		s, u := parse(started), parse(updated)
		if first, ok := firstStep[runID]; !ok || s.Before(first) {
			firstStep[runID] = s
		}
		switch kind {
		case engine.StepCommand, engine.StepTool, engine.StepWebSearch:
			if status != "in_progress" && !s.IsZero() && u.After(s) {
				stats.ToolMs += u.Sub(s).Milliseconds()
			}
		}
	}
	rows.Close()
	var firstTotal int64
	var firstCount int64
	for runID, first := range firstStep {
		if start, ok := runStart[runID]; ok && first.After(start) {
			firstTotal += first.Sub(start).Milliseconds()
			firstCount++
		}
	}
	if firstCount > 0 {
		stats.FirstStepMs = firstTotal / firstCount
	}

	var approvalMs int64
	rows, err = a.DB.QueryContext(ctx, `SELECT a.created_at, d.decided_at FROM approvals a JOIN approval_decisions d ON d.approval_id = a.id WHERE a.requester_bot_id = ?`, botID)
	if err == nil {
		for rows.Next() {
			var created, decided string
			if rows.Scan(&created, &decided) == nil {
				if c, d := parse(created), parse(decided); !c.IsZero() && d.After(c) {
					approvalMs += d.Sub(c).Milliseconds()
				}
			}
		}
		rows.Close()
	}
	stats.ModelMs = max(0, runMs-stats.ToolMs-approvalMs)

	_ = a.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(input_tokens),0), COALESCE(SUM(cached_input_tokens),0), COALESCE(SUM(cache_write_input_tokens),0),
		COALESCE(SUM(output_tokens),0), COALESCE(SUM(reasoning_output_tokens),0) FROM usage_ledger WHERE bot_id = ? AND status = ?`, botID, domain.UsageStatusRecorded).
		Scan(&stats.Tokens.Input, &stats.Tokens.CachedRead, &stats.Tokens.CacheWrite, &stats.Tokens.Output, &stats.Tokens.Reasoning)
	if stats.ModelMs > 0 && stats.Tokens.Output > 0 {
		stats.OutputTokensPerSecond = float64(stats.Tokens.Output) / (float64(stats.ModelMs) / 1000)
	}

	var snapshot usageSnapshot
	if json.Unmarshal([]byte(sqlite.SessionUsageSnapshot(ctx, a.DB, botID)), &snapshot) == nil {
		stats.Context.Used = snapshot.Last.InputTokens + snapshot.Last.OutputTokens
		stats.Context.Window = snapshot.ContextWindow
	}
	// The account's rate limits are the ChatGPT account's quota. A member
	// resolved to another vendor's model — by its own choice, the default
	// model, or the first enabled provider — spends none of it, so it is
	// shown none.
	// The same resolution a turn gets: the member's own choice, then the
	// team's provider, then the default model, then the first enabled
	// provider.
	if choice, err := a.MemberModel(ctx, bot.TeamID, botID); err == nil && choice.EngineProvider == officialEngineProvider {
		accountRateLimits.mu.Lock()
		stats.RateLimits = accountRateLimits.raw
		accountRateLimits.mu.Unlock()
	}
	return stats, nil
}
