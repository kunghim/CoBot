// Package memories is what members remember between turns.
package memories

import (
	"context"
	"unicode/utf8"

	"github.com/kunghim/CoBot/internal/app/core"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Memories hold working methods and preferences per member:
// ownership is filtered in the store, retrieval is bounded, deletion is a
// tombstone so the same fact is never recreated automatically. Models have no
// tool to add one: the rows here come from earlier builds.

// Service answers for the memories on the shared parts it is given.
type Service struct {
	core.Core
}

// SearchResult is the bounded retrieval handed to the caller: at most
// MemorySearchMaxItems items and MemorySearchMaxChars characters in total,
// with the overflow reported instead of silently dropped.
type SearchResult struct {
	Items   []domain.Memory `json:"items"`
	Dropped int             `json:"dropped"`
}

func boundMemories(items []domain.Memory) SearchResult {
	out := SearchResult{Items: []domain.Memory{}}
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

// List exposes one member's live memories for the UI: the user reads
// any member's record, a bot only its own.
func (s Service) List(ctx context.Context, principal auth.Principal, botID string, limit int) ([]domain.Memory, error) {
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
	return sqlite.ListLiveMemories(ctx, s.DB, teamOfMemoryTarget(ctx, s, botID), botID, limit)
}

// teamOfMemoryTarget resolves which team the bot belongs to, so the ownership
// filter always includes the team dimension.
func teamOfMemoryTarget(ctx context.Context, s Service, botID string) string {
	bot, err := sqlite.GetBot(ctx, s.DB, botID)
	if err != nil {
		return ""
	}
	return bot.TeamID
}

// Delete tombstones one memory. The row survives so its dedupe key
// keeps blocking the same fact from being recreated automatically.
func (s Service) Delete(ctx context.Context, principal auth.Principal, memoryID string) error {
	cached, unlock, writeErr := core.Begin[bool](ctx, s.Core, principal)
	defer unlock()
	if writeErr != nil {
		return writeErr
	}
	if cached != nil {
		return nil
	}

	mem, err := sqlite.GetMemory(ctx, s.DB, memoryID)
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
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.TombstoneMemory(ctx, tx, memoryID); err != nil {
		return sqlite.NormalizeError(err)
	}
	if err := s.SaveResult(ctx, tx, principal, true); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.Record(ctx, domain.Event{Type: domain.EventMemoryChanged, TeamID: mem.TeamID, BotID: mem.BotID, ResourceID: mem.ID})
	s.Hub.Publish()
	return nil
}

// ForContext is the per-dispatch slice of the bot's own recent
// memories, bounded like any other retrieval.
func (s Service) ForContext(ctx context.Context, teamID, botID string) SearchResult {
	items, err := sqlite.SearchLiveMemories(ctx, s.DB, teamID, botID, "", 5)
	if err != nil || len(items) == 0 {
		return SearchResult{Items: []domain.Memory{}}
	}
	return boundMemories(items)
}
