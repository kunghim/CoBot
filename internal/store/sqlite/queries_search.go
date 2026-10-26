package sqlite

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kunghim/CoBot/internal/domain"
)

// likePattern wraps a plain-text query for LIKE with \ as the escape
// character, so user input never acts as a wildcard.
func likePattern(query string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
}

// excerptRunes bounds a search snippet; excerptAround runes of lead-in keep
// the match off the left edge.
const (
	excerptRunes  = 48
	excerptAround = 12
)

// excerpt narrows matched text to one line around the first occurrence of
// the query, case-insensitively, so each result stays readable.
func excerpt(text, query string) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) <= excerptRunes {
		return string(runes)
	}
	lower := strings.ToLower(string(runes))
	at := strings.Index(lower, strings.ToLower(query))
	start := 0
	if at > 0 {
		// strings.Index counts bytes; the window counts runes. Lowercasing
		// can also expand runes, so the derived offset is clamped below.
		start = utf8.RuneCountInString(lower[:at]) - excerptAround
		if start < 0 {
			start = 0
		} else {
			start-- // room for the leading ellipsis inside the window
		}
	}
	if max := len(runes) - excerptRunes; start > max {
		start = max
	}
	end := start + excerptRunes
	window := string(runes[start:end])
	if start > 0 {
		window = "…" + window
	}
	if end < len(runes) {
		window += "…"
	}
	return window
}

// SearchChats returns unarchived chats whose title or any message text
// contains the query, most recently active first. The snippet is the excerpt
// of the latest matching message; title-only matches leave it empty. The
// snippet comes from a correlated subquery because the pool holds a single
// connection: a second query while these rows are open would wait forever.
func SearchChats(ctx context.Context, q Queryer, query string, limit int) ([]domain.ChatSearchResult, error) {
	pattern := likePattern(query)
	rows, err := q.QueryContext(ctx, `SELECT id,COALESCE(json_extract(data,'$.title'),''),activity_at,
		COALESCE((SELECT json_extract(m.data,'$.text') FROM chat_messages m
			WHERE m.chat_id=chats.id AND json_extract(m.data,'$.text') LIKE ? ESCAPE '\'
			ORDER BY m.created_at DESC,m.id DESC LIMIT 1),'')
		FROM chats
		WHERE COALESCE(json_extract(data,'$.archivedAt'),'')=''
		AND (instr(lower(json_extract(data,'$.title')),lower(?))>0
			OR EXISTS(SELECT 1 FROM chat_messages m WHERE m.chat_id=chats.id AND json_extract(m.data,'$.text') LIKE ? ESCAPE '\'))
		ORDER BY activity_at DESC,id DESC LIMIT ?`, pattern, query, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ChatSearchResult{}
	for rows.Next() {
		var r domain.ChatSearchResult
		var at, text string
		if err = rows.Scan(&r.ChatID, &r.Title, &at, &text); err != nil {
			return nil, err
		}
		if r.ActivityAt, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		r.Snippet = excerpt(text, query)
		out = append(out, r)
	}
	return out, rows.Err()
}

// SearchMembers returns live members of live teams whose user or assistant
// message text contains the query, latest match first. Collaboration and
// system messages are record-keeping, not conversation content, and stay out.
func SearchMembers(ctx context.Context, q Queryer, query string, limit int) ([]domain.MemberSearchResult, error) {
	pattern := likePattern(query)
	rows, err := q.QueryContext(ctx, `SELECT bot_id,bot_name,team_id,team_name,blocks,created_at FROM (
		SELECT m.bot_id AS bot_id,b.name AS bot_name,m.team_id AS team_id,t.name AS team_name,m.blocks AS blocks,m.created_at AS created_at,
			ROW_NUMBER() OVER (PARTITION BY m.bot_id ORDER BY m.created_at DESC,m.id DESC) AS rn
		FROM messages m
		JOIN bots b ON b.id=m.bot_id AND b.deleted_at=''
		JOIN teams t ON t.id=m.team_id AND t.status NOT IN ('archived','deleted')
		WHERE m.kind IN (?,?) AND EXISTS (
			SELECT 1 FROM json_each(m.blocks) je
			WHERE json_extract(je.value,'$.type')='text' AND json_extract(je.value,'$.text') LIKE ? ESCAPE '\')
	) WHERE rn=1 ORDER BY created_at DESC,bot_id DESC LIMIT ?`,
		domain.MessageKindUser, domain.MessageKindAssistant, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MemberSearchResult{}
	for rows.Next() {
		var r domain.MemberSearchResult
		var blocks, at string
		if err = rows.Scan(&r.BotID, &r.BotName, &r.TeamID, &r.TeamName, &blocks, &at); err != nil {
			return nil, err
		}
		if r.ActivityAt, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		r.Snippet = excerpt(blockText(blocks), query)
		out = append(out, r)
	}
	return out, rows.Err()
}
