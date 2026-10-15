package sqlite

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/domain"
)

type pageCursor struct {
	Scope string `json:"s"`
	Value string `json:"v"`
	ID    string `json:"i"`
}

// PageIDs accepts only application-owned SQL fragments. Values and cursors are
// bound parameters. Close rows before loading entities on the single connection.
func PageIDs(ctx context.Context, q Queryer, from, idColumn, sortColumn, scope, cursor string, args []any, limit int, ascending bool) ([]string, string, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	direction, comparison := "DESC", "<"
	if ascending {
		direction, comparison = "ASC", ">"
	}
	query := "SELECT " + idColumn + ", " + sortColumn + " " + from
	if cursor != "" {
		var anchor pageCursor
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(raw) > 2048 || json.Unmarshal(raw, &anchor) != nil || anchor.Scope != scope || anchor.ID == "" || anchor.Value == "" {
			return nil, "", domain.InvalidRequest("error.cursor.invalid", nil)
		}
		query += " AND (" + sortColumn + comparison + "? OR (" + sortColumn + "=? AND " + idColumn + comparison + "?))"
		args = append(args, anchor.Value, anchor.Value, anchor.ID)
	}
	query += " ORDER BY " + sortColumn + " " + direction + ", " + idColumn + " " + direction + " LIMIT ?"
	args = append(args, limit+1)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	var anchors []pageCursor
	for rows.Next() {
		var anchor pageCursor
		anchor.Scope = scope
		if err := rows.Scan(&anchor.ID, &anchor.Value); err != nil {
			rows.Close()
			return nil, "", err
		}
		anchors = append(anchors, anchor)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(anchors) > limit {
		anchors = anchors[:limit]
		raw, _ := json.Marshal(anchors[len(anchors)-1])
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	ids := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		ids = append(ids, anchor.ID)
	}
	return ids, next, nil
}
