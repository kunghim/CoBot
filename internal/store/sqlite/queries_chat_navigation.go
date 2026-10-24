package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kunghim/CoBot/internal/domain"
)

func ChatNavigation(ctx context.Context, q Queryer) (domain.ChatNavigation, error) {
	return readChatJSON[domain.ChatNavigation](q.QueryRowContext(ctx, `SELECT data FROM chat_navigation WHERE id=1`))
}
func SaveChatNavigation(ctx context.Context, q Queryer, n domain.ChatNavigation) error {
	_, err := q.ExecContext(ctx, `UPDATE chat_navigation SET data=? WHERE id=1`, chatJSON(n))
	return err
}

// The cursor binds all filters and the stable sort tuple, never a page offset.
func ListOrganizedChats(ctx context.Context, q Queryer, cursor string, f domain.ChatListFilter) (domain.ChatList, error) {
	out := domain.ChatList{Items: []domain.Chat{}}
	if f.Sort == "" {
		f.Sort = "recent"
	}
	f.Search = strings.TrimSpace(f.Search)
	key, direction := "activity_at", "DESC"
	switch f.Sort {
	case "recent":
	case "manual":
		key, direction = "printf('%020d',COALESCE(json_extract(data,'$.manualOrder'),rowid))", "ASC"
	case "name":
		key, direction = "lower(json_extract(data,'$.title'))", "ASC"
	case "created":
		key = "json_extract(data,'$.createdAt')"
	case "archived":
		key = "COALESCE(json_extract(data,'$.archivedAt'),'')"
	default:
		return out, domain.InvalidRequest("error.invalid_request", nil)
	}
	raw, _ := json.Marshal(f)
	scope := fmt.Sprintf("%x", sha256.Sum256(raw))
	var anchor struct {
		Scope string
		Pin   int
		Key   string
		ID    string
	}
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(b, &anchor) != nil || anchor.Scope != scope || anchor.ID == "" {
			return out, domain.InvalidRequest("error.invalid_request", nil)
		}
	}
	cmp := "<"
	if direction == "ASC" {
		cmp = ">"
	}
	// Pinned rows form their own first bucket; archived rows are ordered only by the chosen key.
	query := fmt.Sprintf(`WITH selected AS (
 SELECT data,id,EXISTS(SELECT 1 FROM chat_runs WHERE chat_id=chats.id AND status IN ('queued','starting','running','waiting_approval','waiting_input','unknown')) AS busy,%s AS sort_key,CASE WHEN ? THEN 0 ELSE -COALESCE(json_extract(data,'$.pinned'),0) END AS pin
 FROM chats WHERE (COALESCE(json_extract(data,'$.archivedAt'),'')<>'')=?
 AND (?='' OR instr(lower(json_extract(data,'$.title')),lower(?))>0)
 AND (?='*' OR COALESCE(json_extract(data,'$.sectionId'),'')=?)
 AND (?='' OR (?='only' AND COALESCE(json_extract(data,'$.pinned'),0)=1) OR (?='exclude' AND COALESCE(json_extract(data,'$.pinned'),0)=0))
 ) SELECT data,id,sort_key,pin,busy FROM selected
 WHERE ?='' OR pin>? OR (pin=? AND (sort_key %s ? OR (sort_key=? AND id %s ?)))
 ORDER BY pin ASC,sort_key %s,id %s LIMIT 51`, key, cmp, cmp, direction, direction)
	section := f.SectionID
	if section == "" {
		section = "*"
	}
	if section == "_default" {
		section = ""
	}
	rows, err := q.QueryContext(ctx, query, f.Archived, f.Archived, f.Search, f.Search, section, section, f.Pinned, f.Pinned, f.Pinned, cursor, anchor.Pin, anchor.Pin, anchor.Key, anchor.Key, anchor.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var data, id, k string
		var pin int
		var busy bool
		if err = rows.Scan(&data, &id, &k, &pin, &busy); err != nil {
			return out, err
		}
		if len(out.Items) == 50 {
			return out, nil
		}
		var c domain.Chat
		if err = json.Unmarshal([]byte(data), &c); err != nil {
			return out, err
		}
		c.Busy = busy
		out.Items = append(out.Items, c)
		anchor.Scope, anchor.Pin, anchor.Key, anchor.ID = scope, pin, k, id
		b, _ := json.Marshal(anchor)
		out.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	out.NextCursor = ""
	return out, rows.Err()
}

// ReorderChat inserts before an anchor in the same section/pin bucket, including unloaded pages.
func ReorderChat(ctx context.Context, q Queryer, c domain.Chat, before string) error {
	n, err := ChatNavigation(ctx, q)
	if err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, `SELECT id,COALESCE(json_extract(data,'$.sectionId'),'') FROM chats WHERE COALESCE(json_extract(data,'$.archivedAt'),'')='' AND COALESCE(json_extract(data,'$.pinned'),0)=? ORDER BY COALESCE(json_extract(data,'$.manualOrder'),rowid),id`, c.Pinned)
	if err != nil {
		return err
	}
	all := []string{}
	bucket := []string{}
	for rows.Next() {
		var id, section string
		if err = rows.Scan(&id, &section); err != nil {
			rows.Close()
			return err
		}
		all = append(all, id)
		if n.Grouping == "merged" || c.Pinned || section == c.SectionID {
			bucket = append(bucket, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if before == ":up" || before == ":down" {
		at := -1
		for i, id := range bucket {
			if id == c.ID {
				at = i
			}
		}
		if at < 0 {
			return domain.Conflict("error.chat.order_conflict", nil)
		}
		if before == ":up" {
			if at == 0 {
				return nil
			}
			before = bucket[at-1]
		} else {
			if at == len(bucket)-1 {
				return nil
			}
			before = ""
			if at+2 < len(bucket) {
				before = bucket[at+2]
			}
		}
	}
	ids := []string{}
	found := before == ""
	for _, id := range all {
		if id == c.ID {
			continue
		}
		if id == before {
			ids = append(ids, c.ID)
			found = true
		}
		ids = append(ids, id)
	}
	if !found {
		return domain.Conflict("error.chat.order_conflict", nil)
	}
	if before == "" {
		ids = append(ids, c.ID)
	}
	for i, id := range ids {
		if _, err = q.ExecContext(ctx, `UPDATE chats SET data=json_set(data,'$.manualOrder',?,'$.revision',json_extract(data,'$.revision')+1) WHERE id=?`, i+1, id); err != nil {
			return err
		}
	}
	return nil
}
