package sqlite

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

func chatJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func SaveChat(ctx context.Context, q Queryer, c domain.Chat, activity bool) error {
	_, err := q.ExecContext(ctx, `INSERT INTO chats(id,activity_at,data) VALUES(?,?,?)
	ON CONFLICT(id) DO UPDATE SET data=excluded.data, activity_at=CASE WHEN ? THEN excluded.activity_at ELSE chats.activity_at END`, c.ID, c.UpdatedAt.UTC().Format(time.RFC3339Nano), chatJSON(c), activity)
	return err
}

func readChatJSON[T any](row *sql.Row) (T, error) {
	var out T
	var data string
	err := row.Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(data), &out)
	return out, err
}

func GetChat(ctx context.Context, q Queryer, id string) (domain.Chat, error) {
	return readChatJSON[domain.Chat](q.QueryRowContext(ctx, `SELECT data FROM chats WHERE id=?`, id))
}

func chatCursor(at, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at + "\n" + id))
}
func parseChatCursor(cursor string) (string, string, error) {
	if cursor == "" {
		return "", "", nil
	}
	b, e := base64.RawURLEncoding.DecodeString(cursor)
	p := strings.Split(string(b), "\n")
	if e != nil || len(p) != 2 || p[0] == "" || p[1] == "" {
		return "", "", domain.InvalidRequest("error.invalid_request", nil)
	}
	return p[0], p[1], nil
}

func ListChats(ctx context.Context, q Queryer, cursor string) (domain.ChatList, error) {
	out := domain.ChatList{Items: []domain.Chat{}}
	at, id, err := parseChatCursor(cursor)
	if err != nil {
		return out, err
	}
	rows, err := q.QueryContext(ctx, `SELECT data,activity_at,id FROM chats WHERE (?='' OR (activity_at,id)<(?,?)) ORDER BY activity_at DESC,id DESC LIMIT 51`, cursor, at, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	more := false
	for rows.Next() {
		var data, a, i string
		if err = rows.Scan(&data, &a, &i); err != nil {
			return out, err
		}
		if len(out.Items) == 50 {
			more = true
			break
		}
		var c domain.Chat
		if err = json.Unmarshal([]byte(data), &c); err != nil {
			return out, err
		}
		out.Items = append(out.Items, c)
		out.NextCursor = chatCursor(a, i)
	}
	if !more {
		out.NextCursor = ""
	}
	return out, rows.Err()
}

func SaveChatMessage(ctx context.Context, q Queryer, m domain.ChatMessage) error {
	_, err := q.ExecContext(ctx, `INSERT INTO chat_messages(id,chat_id,run_id,created_at,data) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, m.ID, m.ChatID, m.RunID, m.CreatedAt.UTC().Format(time.RFC3339Nano), chatJSON(m))
	return err
}
func SaveChatRun(ctx context.Context, q Queryer, r domain.ChatRun) error {
	r.Steps = nil
	_, err := q.ExecContext(ctx, `INSERT INTO chat_runs(id,chat_id,status,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,data=excluded.data`, r.ID, r.ChatID, r.Status, chatJSON(r))
	return err
}
func GetChatRun(ctx context.Context, q Queryer, id string) (domain.ChatRun, error) {
	return readChatJSON[domain.ChatRun](q.QueryRowContext(ctx, `SELECT data FROM chat_runs WHERE id=?`, id))
}
func ListChatRuns(ctx context.Context, q Queryer, chatID string) ([]domain.ChatRun, error) {
	rows, err := q.QueryContext(ctx, `SELECT data FROM chat_runs WHERE (?='' OR chat_id=?) ORDER BY rowid`, chatID, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ChatRun{}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		var r domain.ChatRun
		if err = json.Unmarshal([]byte(b), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func SaveChatSession(ctx context.Context, q Queryer, s domain.ChatSession) error {
	_, err := q.ExecContext(ctx, `INSERT INTO chat_sessions(chat_id,data) VALUES(?,?) ON CONFLICT(chat_id) DO UPDATE SET data=excluded.data`, s.ChatID, chatJSON(s))
	return err
}
func GetChatSession(ctx context.Context, q Queryer, id string) (domain.ChatSession, error) {
	return readChatJSON[domain.ChatSession](q.QueryRowContext(ctx, `SELECT data FROM chat_sessions WHERE chat_id=?`, id))
}

func SaveChatStep(ctx context.Context, q Queryer, runID string, s domain.ChatStep) error {
	_, err := q.ExecContext(ctx, `INSERT INTO chat_run_steps(run_id,item_id,seq,data) VALUES(?,?,COALESCE((SELECT MAX(seq)+1 FROM chat_run_steps WHERE run_id=?),1),?) ON CONFLICT(run_id,item_id) DO UPDATE SET data=excluded.data`, runID, s.ID, runID, chatJSON(s))
	return err
}
func SaveChatRequest(ctx context.Context, q Queryer, r domain.ChatRequest) error {
	_, err := q.ExecContext(ctx, `INSERT INTO chat_requests(id,chat_id,run_id,state,engine_id,epoch,data) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,data=excluded.data`, r.ID, r.ChatID, r.RunID, r.State, string(r.EngineID), r.Epoch, chatJSON(r))
	return err
}
func GetChatRequest(ctx context.Context, q Queryer, id string) (domain.ChatRequest, error) {
	var r domain.ChatRequest
	var data, engineID string
	err := q.QueryRowContext(ctx, `SELECT data,engine_id,epoch FROM chat_requests WHERE id=?`, id).Scan(&data, &engineID, &r.Epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	epoch := r.Epoch
	err = json.Unmarshal([]byte(data), &r)
	r.EngineID = json.RawMessage(engineID)
	r.Epoch = epoch
	return r, err
}
func ListChatRequests(ctx context.Context, q Queryer, chatID string) ([]domain.ChatRequest, error) {
	rows, err := q.QueryContext(ctx, `SELECT data,engine_id,epoch FROM chat_requests WHERE chat_id=? ORDER BY rowid`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ChatRequest{}
	for rows.Next() {
		var b, id, epoch string
		if err = rows.Scan(&b, &id, &epoch); err != nil {
			return nil, err
		}
		var r domain.ChatRequest
		if err = json.Unmarshal([]byte(b), &r); err != nil {
			return nil, err
		}
		r.EngineID = json.RawMessage(id)
		r.Epoch = epoch
		out = append(out, r)
	}
	return out, rows.Err()
}

func GetChatPage(ctx context.Context, q Queryer, id, cursor string) (domain.ChatPage, error) {
	out := domain.ChatPage{Messages: []domain.ChatMessage{}, Runs: []domain.ChatRun{}}
	var err error
	out.Chat, err = GetChat(ctx, q, id)
	if err != nil {
		return out, err
	}
	at, mid, err := parseChatCursor(cursor)
	if err != nil {
		return out, err
	}
	rows, err := q.QueryContext(ctx, `SELECT data,created_at,id FROM chat_messages WHERE chat_id=? AND (?='' OR (created_at,id)<(?,?)) ORDER BY created_at DESC,id DESC LIMIT 51`, id, cursor, at, mid)
	if err != nil {
		return out, err
	}
	more := false
	for rows.Next() {
		var b, a, i string
		if err = rows.Scan(&b, &a, &i); err != nil {
			rows.Close()
			return out, err
		}
		if len(out.Messages) == 50 {
			more = true
			break
		}
		var m domain.ChatMessage
		if err = json.Unmarshal([]byte(b), &m); err != nil {
			rows.Close()
			return out, err
		}
		out.Messages = append(out.Messages, m)
		out.NextCursor = chatCursor(a, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if !more {
		out.NextCursor = ""
	}
	for i, j := 0, len(out.Messages)-1; i < j; i, j = i+1, j-1 {
		out.Messages[i], out.Messages[j] = out.Messages[j], out.Messages[i]
	}
	runs, err := ListChatRuns(ctx, q, id)
	if err != nil {
		return out, err
	}
	visible := map[string]bool{}
	for _, m := range out.Messages {
		visible[m.RunID] = true
	}
	for _, r := range runs {
		if !visible[r.ID] && !domain.ChatRunActive(r.Status) {
			continue
		}
		r.Steps = []domain.ChatStep{}
		steps, e := q.QueryContext(ctx, `SELECT data,seq FROM chat_run_steps WHERE run_id=? ORDER BY seq`, r.ID)
		if e != nil {
			return out, e
		}
		for steps.Next() {
			var b string
			var seq int
			if e = steps.Scan(&b, &seq); e != nil {
				break
			}
			var s domain.ChatStep
			if e = json.Unmarshal([]byte(b), &s); e != nil {
				break
			}
			s.Seq = seq
			r.Steps = append(r.Steps, s)
		}
		if e == nil {
			e = steps.Err()
		}
		steps.Close()
		if e != nil {
			return out, e
		}
		out.Runs = append(out.Runs, r)
	}
	out.Requests, err = ListChatRequests(ctx, q, id)
	return out, err
}

// DeleteChatRun drops one chat turn: its steps, its messages (the user's and
// the replies), its approval requests and the run itself, for a turn the
// user edited away.
func DeleteChatRun(ctx context.Context, q Queryer, runID string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM chat_run_steps WHERE run_id = ?`, runID); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM chat_requests WHERE run_id = ?`, runID); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM chat_messages WHERE run_id = ?`, runID); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `DELETE FROM chat_runs WHERE id = ?`, runID)
	return err
}

// GetChatMessage reads one chat message.
func GetChatMessage(ctx context.Context, q Queryer, id string) (domain.ChatMessage, error) {
	return readChatJSON[domain.ChatMessage](q.QueryRowContext(ctx, `SELECT data FROM chat_messages WHERE id=?`, id))
}
