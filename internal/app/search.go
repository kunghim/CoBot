package app

import (
	"context"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// searchResultLimit caps each group; the header field shows a short list,
// never the whole history, and selection jumps into the conversation.
// maxSearchRunes drops absurdly long queries rather than scanning for them.
const (
	searchResultLimit = 5
	maxSearchRunes    = 100
)

// SearchConversations finds chats and team members by conversation content:
// titles and message text, newest activity first, archived and deleted
// conversations excluded (see the sqlite queries).
func (a *App) SearchConversations(ctx context.Context, p auth.Principal, query string) (domain.SearchResults, error) {
	if p.Kind != auth.KindUser {
		return domain.SearchResults{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	query = strings.TrimSpace(query)
	out := domain.SearchResults{Query: query, Chats: []domain.ChatSearchResult{}, Members: []domain.MemberSearchResult{}}
	if query == "" || len([]rune(query)) > maxSearchRunes {
		return out, nil
	}
	chats, err := sqlite.SearchChats(ctx, a.DB, query, searchResultLimit)
	if err != nil {
		return out, err
	}
	members, err := sqlite.SearchMembers(ctx, a.DB, query, searchResultLimit)
	if err != nil {
		return out, err
	}
	out.Chats, out.Members = chats, members
	return out, nil
}
