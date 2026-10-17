package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Usage ledger and budget gate (G06/G07/G08). The ledger stores one row per
// engine turn, updated in place; the gate blocks only NEW turns once a
// budget is exhausted or usage cannot be judged, and it never pretends the
// limit is a precise hard cap.

// tokenBreakdown is one of Codex's usage figures.
type tokenBreakdown struct {
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	CacheWriteInputTokens int64 `json:"cacheWriteInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
	TotalTokens           int64 `json:"totalTokens"`
}

func (b tokenBreakdown) minus(o tokenBreakdown) tokenBreakdown {
	return tokenBreakdown{
		InputTokens: b.InputTokens - o.InputTokens, CachedInputTokens: b.CachedInputTokens - o.CachedInputTokens,
		CacheWriteInputTokens: b.CacheWriteInputTokens - o.CacheWriteInputTokens, OutputTokens: b.OutputTokens - o.OutputTokens,
		ReasoningOutputTokens: b.ReasoningOutputTokens - o.ReasoningOutputTokens, TotalTokens: b.TotalTokens - o.TotalTokens,
	}
}

// tokenUsagePayload mirrors thread/tokenUsage/updated, sent after every model
// call: last is that call, total the whole thread so far.
type tokenUsagePayload struct {
	ThreadID   string `json:"threadId"`
	TurnID     string `json:"turnId"`
	TokenUsage struct {
		Last               tokenBreakdown `json:"last"`
		Total              tokenBreakdown `json:"total"`
		ModelContextWindow *int64         `json:"modelContextWindow"`
	} `json:"tokenUsage"`
}

// usageSnapshot is a member's latest engine usage, kept with its session:
// the thread totals, the last call (whose input is the context in use) and
// the model's context window.
type usageSnapshot struct {
	ThreadID      string         `json:"threadId"`
	Total         tokenBreakdown `json:"total"`
	Last          tokenBreakdown `json:"last"`
	ContextWindow int64          `json:"contextWindow,omitempty"`
}

// ingestTokenUsage adds one model call's usage to its turn. A turn may call
// the model many times, and each call reports only itself plus the thread
// total; the turn's row is the sum of its calls, measured as the growth of
// the thread total since the previous report (stored with the session, so a
// restart neither loses nor repeats a call). The bot is resolved from the
// live thread binding so system threads count to the team without
// pretending to be a member.
func (a *App) ingestTokenUsage(ctx context.Context, teamID, epoch string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var p tokenUsagePayload
	if err := json.Unmarshal(raw, &p); err != nil || p.TurnID == "" {
		return nil // malformed usage never crashes the notification loop
	}
	botID, _ := a.botByThread(ctx, epoch, p.ThreadID)
	call := p.TokenUsage.Last
	var previous usageSnapshot
	if botID != "" {
		_ = json.Unmarshal([]byte(sqlite.SessionUsageSnapshot(ctx, a.DB, botID)), &previous)
		if previous.ThreadID == p.ThreadID && previous.Total.TotalTokens > 0 && p.TokenUsage.Total.TotalTokens >= previous.Total.TotalTokens {
			call = p.TokenUsage.Total.minus(previous.Total)
		}
	}
	entry := domain.UsageEntry{
		ID: domain.NewID(), TeamID: teamID, BotID: botID, Purpose: domain.UsagePurposeRun,
		EngineEpoch: epoch, ThreadID: p.ThreadID, TurnID: p.TurnID, Status: domain.UsageStatusRecorded,
		CreatedAt: time.Now().UTC(),
	}
	if existing, err := sqlite.GetUsage(ctx, a.DB, epoch, p.ThreadID, p.TurnID); err == nil && existing.Status == domain.UsageStatusRecorded {
		entry.ID, entry.CreatedAt = existing.ID, existing.CreatedAt
		entry.InputTokens, entry.OutputTokens, entry.CachedInputTokens = existing.InputTokens, existing.OutputTokens, existing.CachedInputTokens
		entry.CacheWriteInputTokens, entry.ReasoningOutputTokens = existing.CacheWriteInputTokens, existing.ReasoningOutputTokens
	}
	entry.InputTokens += call.InputTokens
	entry.OutputTokens += call.OutputTokens
	entry.CachedInputTokens += call.CachedInputTokens
	entry.CacheWriteInputTokens += call.CacheWriteInputTokens
	entry.ReasoningOutputTokens += call.ReasoningOutputTokens
	if botID != "" {
		snapshot := usageSnapshot{ThreadID: p.ThreadID, Total: p.TokenUsage.Total, Last: p.TokenUsage.Last}
		if p.TokenUsage.ModelContextWindow != nil {
			snapshot.ContextWindow = *p.TokenUsage.ModelContextWindow
		}
		data, _ := json.Marshal(snapshot)
		if err := sqlite.SetSessionUsageSnapshot(ctx, a.DB, botID, string(data)); err != nil {
			return err
		}
	}
	return a.RecordUsage(ctx, entry)
}

// RecordUsage stores a usage snapshot and publishes the change. Repeated
// snapshots for one turn replace the row; they never accumulate (G06).
func (a *App) RecordUsage(ctx context.Context, entry domain.UsageEntry) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.UpsertUsage(ctx, tx, entry); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventUsageUpdated, TeamID: entry.TeamID, BotID: entry.BotID, ResourceID: entry.ThreadID})
	a.Hub.Publish()
	// Fresh numbers may make a paused budget determinable again (the unknown
	// row they replace was what held the queue), so held work is re-woken.
	a.ResumeBudgetPausedBots(ctx)
	return nil
}

// MarkUsageUnknownIfMissing records that a finished turn never reported
// tokens. It runs on the caller's transaction (single-connection pool).
func (a *App) MarkUsageUnknownIfMissing(ctx context.Context, tx *sql.Tx, teamID, botID, epoch, threadID, turnID string) error {
	if turnID == "" {
		return nil
	}
	return sqlite.MarkUnknownUsageIfMissing(ctx, tx, domain.UsageEntry{
		TeamID: teamID, BotID: botID, Purpose: domain.UsagePurposeRun,
		EngineEpoch: epoch, ThreadID: threadID, TurnID: turnID,
	})
}

// BudgetPause describes why new turns for a bot are held.
type BudgetPause struct {
	Reason string // exceeded | usage_unknown
}

// budgetPaused evaluates the configured budget against the ledger. With no
// budget configured nothing is paused (the default). An unknown usage row in
// the scope pauses too: a budget cannot be trusted without knowing the spend,
// and the product says so instead of guessing (G08). The sum may overshoot
// under concurrency; no precise hard cap is promised (G07).
func (a *App) budgetPaused(ctx context.Context, teamID, botID string, settings domain.Settings) (*BudgetPause, error) {
	if settings.BudgetScope == domain.BudgetScopeNone || settings.BudgetTokens <= 0 {
		return nil, nil
	}
	scopeBot := ""
	if settings.BudgetScope == domain.BudgetScopeBot {
		scopeBot = botID
	}
	billable, unknown, err := sqlite.SumUsage(ctx, a.DB, teamID, scopeBot)
	if err != nil {
		return nil, err
	}
	if unknown > 0 {
		return &BudgetPause{Reason: "usage_unknown"}, nil
	}
	if billable >= settings.BudgetTokens {
		return &BudgetPause{Reason: "exceeded"}, nil
	}
	return nil, nil
}

// ResumeBudgetPausedBots re-wakes every bot that still has queued work after
// a settings change, so fixing or removing a budget continues exactly the
// deliveries it held back.
func (a *App) ResumeBudgetPausedBots(ctx context.Context) {
	if a.Scheduler == nil {
		return
	}
	bots, err := sqlite.QueuedDeliveryBots(ctx, a.DB)
	if err != nil {
		return
	}
	for _, botID := range bots {
		a.Scheduler.Wake(botID)
	}
}
