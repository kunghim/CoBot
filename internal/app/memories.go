package app

import (
	"context"
	"regexp"
	"unicode/utf8"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Memories hold working methods and preferences per member (G04/G05):
// ownership is filtered in the store, retrieval is bounded, deletion is a
// tombstone so the same fact is never recreated automatically, and the write
// path refuses secret-shaped bodies and oversized entries.

// memorySecretGuards match high-signal secret shapes. They are a guard
// rail, not a guarantee: the product never claims to have caught every
// secret, and the tool description tells the model to store methods only.
var memorySecretGuards = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`(?i)\b(?:password|passwd|api[_-]?key|secret)\s*[:=]\s*\S{6,}`),
}

// memorySearchResult is the bounded retrieval handed to the caller: at most
// MemorySearchMaxItems items and MemorySearchMaxChars characters in total,
// with the overflow reported instead of silently dropped (G05).
type memorySearchResult struct {
	Items   []domain.Memory `json:"items"`
	Dropped int             `json:"dropped"`
}

func boundMemories(items []domain.Memory) memorySearchResult {
	out := memorySearchResult{Items: []domain.Memory{}}
	used := 0
	for i, m := range items {
		size := utf8.RuneCountInString(m.Body)
		if used+size > domain.MemorySearchMaxChars {
			remaining := domain.MemorySearchMaxChars - used
			if remaining > 0 {
				runes := []rune(m.Body)
				// One rune of the budget is reserved for the ellipsis, so the
				// truncated item still fits the character cap exactly.
				cut := remaining - 1
				if cut > len(runes) {
					cut = len(runes)
				}
				if cut < 0 {
					cut = 0
				}
				m.Body = string(runes[:cut]) + "…"
				out.Items = append(out.Items, m)
			}
			out.Dropped = len(items) - i
			return out
		}
		used += size
		out.Items = append(out.Items, m)
	}
	return out
}

// ListMemories exposes one member's live memories for the UI: the user reads
// any member's record, a bot only its own.
func (a *App) ListMemories(ctx context.Context, principal auth.Principal, botID string, limit int) ([]domain.Memory, error) {
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		if principal.BotID != botID {
			return nil, domain.Forbidden("error.forbidden.bot_only", nil)
		}
	default:
		return nil, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return sqlite.ListLiveMemories(ctx, a.DB, teamOfMemoryTarget(ctx, a, botID), botID, limit)
}

// teamOfMemoryTarget resolves which team the bot belongs to, so the ownership
// filter always includes the team dimension.
func teamOfMemoryTarget(ctx context.Context, a *App, botID string) string {
	bot, err := sqlite.GetBot(ctx, a.DB, botID)
	if err != nil {
		return ""
	}
	return bot.TeamID
}

// DeleteMemory tombstones one memory. The row survives so its dedupe key
// keeps blocking the same fact from being recreated automatically (G04).
func (a *App) DeleteMemory(ctx context.Context, principal auth.Principal, memoryID string) error {
	cached, unlock, writeErr := beginWrite[bool](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return writeErr
	}
	if cached != nil {
		return nil
	}

	mem, err := sqlite.GetMemory(ctx, a.DB, memoryID)
	if err != nil {
		return sqlite.NormalizeError(err)
	}
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		if principal.BotID != mem.BotID {
			return domain.Forbidden("error.forbidden.bot_only", nil)
		}
	default:
		return domain.Unauthorized("error.unauthorized.principal_required")
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.TombstoneMemory(ctx, tx, memoryID); err != nil {
		return sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, true); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventMemoryChanged, TeamID: mem.TeamID, BotID: mem.BotID, ResourceID: mem.ID})
	a.Hub.Publish()
	return nil
}

// memoriesForContext is the per-dispatch slice of the bot's own recent
// memories, bounded like any other retrieval.
func (a *App) memoriesForContext(ctx context.Context, teamID, botID string) memorySearchResult {
	items, err := sqlite.SearchLiveMemories(ctx, a.DB, teamID, botID, "", 5)
	if err != nil || len(items) == 0 {
		return memorySearchResult{Items: []domain.Memory{}}
	}
	return boundMemories(items)
}
