package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

func jsonMarshal(v any) ([]byte, error)      { return json.Marshal(v) }
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// ---- messages ----

// messageColumns is the shared projection of the messages table.
const messageColumns = `id, team_id, bot_id, kind, blocks, request_id, delivery_status, run_id, source_bot_id, created_at`

func scanMessage(row interface{ Scan(...any) error }) (domain.Message, error) {
	var m domain.Message
	var blocks, created string
	err := row.Scan(&m.ID, &m.TeamID, &m.BotID, &m.Kind, &blocks, &m.RequestID, &m.DeliveryStatus, &m.RunID, &m.SourceBotID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Message{}, ErrNotFound
	}
	if err != nil {
		return domain.Message{}, fmt.Errorf("scan message: %w", err)
	}
	m.Blocks = []byte(blocks)
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return m, nil
}

// InsertMessage writes a message; when requestID is non-empty the unique
// index enforces one message per (team, requestId). Collaboration messages
// carry source_bot_id so both ends of the exchange are recorded.
func InsertMessage(ctx context.Context, tx *sql.Tx, msg domain.Message) error {
	created := msg.CreatedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO messages (id, team_id, bot_id, kind, blocks, request_id, delivery_status, run_id, source_bot_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.TeamID, msg.BotID, msg.Kind, string(msg.Blocks), msg.RequestID, msg.DeliveryStatus, msg.RunID, msg.SourceBotID, created.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// GetMessage reads one message.
func GetMessage(ctx context.Context, q Queryer, id string) (domain.Message, error) {
	return scanMessage(q.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE id = ?`, id))
}

// FindMessageByRequestID returns the message stored for an idempotent retry.
func FindMessageByRequestID(ctx context.Context, q Queryer, teamID, requestID string) (domain.Message, error) {
	if requestID == "" {
		return domain.Message{}, ErrNotFound
	}
	return scanMessage(q.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE team_id = ? AND request_id = ?`, teamID, requestID))
}

// ListMessages pages one bot's user conversation, newest last. Collaboration
// messages are excluded: they belong to the collaboration record, never to
// the user session.
func ListMessages(ctx context.Context, q Queryer, botID string, before string, limit int) ([]domain.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `SELECT ` + messageColumns + ` FROM messages WHERE bot_id = ? AND kind != ?`
	args := []any{botID, domain.MessageKindCollaboration}
	if before != "" {
		query += ` AND created_at < ?`
		args = append(args, before)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	// Reverse to chronological order for the client.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// previewRunes bounds a message preview; the list shows one truncated line.
const previewRunes = 120

// LatestMessagePreviews returns, per member of a team, the start of the
// latest user or assistant message that has text. Members without one are
// absent.
func LatestMessagePreviews(ctx context.Context, q Queryer, teamID string) (map[string]domain.MessagePreview, error) {
	rows, err := q.QueryContext(ctx, `SELECT bot_id, kind, blocks, created_at FROM (
		SELECT bot_id, kind, blocks, created_at,
			ROW_NUMBER() OVER (PARTITION BY bot_id ORDER BY created_at DESC, id DESC) AS rn
		FROM messages
		WHERE team_id = ? AND kind IN (?, ?) AND blocks LIKE '%"text"%'
	) WHERE rn = 1`, teamID, domain.MessageKindUser, domain.MessageKindAssistant)
	if err != nil {
		return nil, fmt.Errorf("latest message previews: %w", err)
	}
	defer rows.Close()
	out := map[string]domain.MessagePreview{}
	for rows.Next() {
		var botID, kind, blocks, created string
		if err := rows.Scan(&botID, &kind, &blocks, &created); err != nil {
			return nil, fmt.Errorf("scan message preview: %w", err)
		}
		text := previewText(blocks)
		if text == "" {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, created)
		out[botID] = domain.MessagePreview{Kind: kind, Text: text, CreatedAt: at}
	}
	return out, rows.Err()
}

// blockText joins a message's text blocks into one normalized line.
func blockText(blocks string) string {
	var parsed []domain.TextBlock
	if err := json.Unmarshal([]byte(blocks), &parsed); err != nil {
		return ""
	}
	parts := make([]string, 0, len(parsed))
	for _, block := range parsed {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

// previewText cuts the joined text blocks to previewRunes.
func previewText(blocks string) string {
	text := blockText(blocks)
	if runes := []rune(text); len(runes) > previewRunes {
		text = string(runes[:previewRunes]) + "…"
	}
	return text
}

// ListCollaborationMessages returns exchanges between the given bot and one
// peer, oldest first. Both directions are included so either collaboration
// page can show the same read-only segment. An empty peerBotID means the
// bot's whole record: every message it sent or received, which is what the
// user reads on a member's collaboration page.
func ListCollaborationMessages(ctx context.Context, q Queryer, botID, peerBotID string, limit int) ([]domain.Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `SELECT ` + messageColumns + ` FROM messages WHERE kind = ?`
	args := []any{domain.MessageKindCollaboration}
	if peerBotID == "" {
		query += ` AND (source_bot_id = ? OR bot_id = ?)`
		args = append(args, botID, botID)
	} else {
		query += ` AND ((source_bot_id = ? AND bot_id = ?) OR (source_bot_id = ? AND bot_id = ?))`
		args = append(args, botID, peerBotID, peerBotID, botID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list collaboration messages: %w", err)
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// SetMessageDeliveryStatus updates the delivery projection of a user message.
func SetMessageDeliveryStatus(ctx context.Context, tx *sql.Tx, messageID, status string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET delivery_status = ? WHERE id = ?`, status, messageID); err != nil {
		return fmt.Errorf("update message delivery: %w", err)
	}
	return nil
}

// ---- deliveries ----

// DeliveryRecord is the dispatch row.
type DeliveryRecord struct {
	ID          string
	MessageID   string
	TeamID      string
	TargetBotID string
	Status      string
	DispatchKey string
	ChainID     string
	RunID       string
	TaskID      string
	SourceRunID string
	Kind        string
	CreatedAt   time.Time
}

// InsertDeliveryRow writes a delivery row.
func InsertDeliveryRow(ctx context.Context, tx *sql.Tx, record DeliveryRecord) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if record.Kind == "" {
		record.Kind = "message"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO deliveries (id, message_id, team_id, target_bot_id, status, dispatch_key, chain_id, task_id, source_run_id, kind, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.MessageID, record.TeamID, record.TargetBotID, record.Status, record.DispatchKey, record.ChainID, record.TaskID, record.SourceRunID, record.Kind, now, now)
	if err != nil {
		return fmt.Errorf("insert delivery: %w", err)
	}
	return nil
}

// deliveryColumns is the shared projection of the deliveries table.
const deliveryColumns = `id, message_id, team_id, target_bot_id, status, dispatch_key, chain_id, run_id, task_id, source_run_id, kind, created_at`

func scanDelivery(row interface{ Scan(...any) error }) (DeliveryRecord, error) {
	var r DeliveryRecord
	var created string
	err := row.Scan(&r.ID, &r.MessageID, &r.TeamID, &r.TargetBotID, &r.Status, &r.DispatchKey, &r.ChainID, &r.RunID, &r.TaskID, &r.SourceRunID, &r.Kind, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryRecord{}, ErrNotFound
	}
	if err != nil {
		return DeliveryRecord{}, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return r, nil
}

// ListQueuedDeliveries returns the FIFO queue of one bot.
func ListQueuedDeliveries(ctx context.Context, q Queryer, botID string, limit int) ([]DeliveryRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := q.QueryContext(ctx, `SELECT `+deliveryColumns+`
		FROM deliveries WHERE target_bot_id = ? AND status = ? ORDER BY created_at ASC, id ASC LIMIT ?`,
		botID, domain.DeliveryQueued, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeliveryRecord
	for rows.Next() {
		r, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetDeliveryStatus transitions one delivery with optimistic state check.
// ListDispatchableDeliveries preserves FIFO among eligible input. A waiting
// question parks only related automatic work, so an unrelated user message
// behind it is still allowed without implicitly answering that question. An
// acceptance question parks nothing.
func ListDispatchableDeliveries(ctx context.Context, q Queryer, botID string, limit int) ([]DeliveryRecord, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries
		WHERE target_bot_id = ? AND status = ?
		AND NOT EXISTS (SELECT 1 FROM chains c WHERE c.id = deliveries.chain_id AND c.status IN ('awaiting_approval','evaluating')) AND (
		EXISTS (SELECT 1 FROM messages m WHERE m.id = deliveries.message_id AND m.kind = ?) OR
		NOT EXISTS (SELECT 1 FROM questions q WHERE q.status = 'open' AND q.kind = '' AND (
		(q.task_id = '' AND q.requester_bot_id = deliveries.target_bot_id) OR
		(deliveries.task_id != '' AND q.task_id = deliveries.task_id))))
		ORDER BY created_at ASC,id ASC LIMIT ?`, botID, domain.DeliveryQueued, domain.MessageKindUser, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeliveryRecord
	for rows.Next() {
		r, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func HasBlockingQuestion(ctx context.Context, q Queryer, botID, taskID string) (bool, error) {
	var n int
	// An acceptance question waits on the user's verdict on finished work; it
	// never holds the member back.
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM questions WHERE status = 'open' AND kind = '' AND
		((task_id = '' AND requester_bot_id = ?) OR (? != '' AND task_id = ?))`, botID, taskID, taskID).Scan(&n)
	return n > 0, err
}

func SetDeliveryStatus(ctx context.Context, tx *sql.Tx, id, status string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return fmt.Errorf("update delivery: %w", err)
	}
	response := ""
	switch status {
	case domain.DeliveryQueued:
		response = domain.ResponseQueued
	case domain.DeliveryAccepted:
		response = domain.ResponseSent
	case domain.DeliveryUnknown:
		response = domain.ResponseUnknown
	case domain.DeliveryCancelled:
		response = domain.ResponseExpired
	}
	if response != "" {
		_, err := tx.ExecContext(ctx, `UPDATE approval_responses SET state = ?, updated_at = ? WHERE delivery_id = ? AND NOT (state = ? AND ? = ?)`, response, time.Now().UTC().Format(time.RFC3339Nano), id, domain.ResponseSent, response, domain.ResponseUnknown)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetDelivery reads one delivery.
func GetDelivery(ctx context.Context, q Queryer, id string) (DeliveryRecord, error) {
	return scanDelivery(q.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE id = ?`, id))
}

// ClaimDeliveryRun links a claimed delivery to the run that will execute it
// and moves it to dispatching. Chain attribution and later reconciliation both
// read this link.
func ClaimDeliveryRun(ctx context.Context, tx *sql.Tx, deliveryID, runID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET status = ?, run_id = ?, updated_at = ? WHERE id = ?`,
		domain.DeliveryDispatching, runID, time.Now().UTC().Format(time.RFC3339Nano), deliveryID); err != nil {
		return fmt.Errorf("claim delivery: %w", err)
	}
	return nil
}

// CountQueuedDeliveries counts a bot's queued items (busy indicator).
func CountQueuedDeliveries(ctx context.Context, q Queryer, botID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM deliveries WHERE target_bot_id = ? AND status = ?`,
		botID, domain.DeliveryQueued).Scan(&n)
	return n, err
}

// ---- runs ----

// InsertRun creates a run row.
func InsertRun(ctx context.Context, tx *sql.Tx, run domain.Run) error {
	started := run.StartedAt.UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO runs (id, team_id, bot_id, provider_session_id, provider_turn_id, engine_epoch, status, error_key, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.TeamID, run.BotID, run.ProviderSessionID, run.ProviderTurnID, run.EngineEpoch, run.Status, run.ErrorKey, started)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

// UpdateRunStatus persists a run lifecycle transition.
// UpdateRunStatus transitions one run. providerTurnID and engineEpoch are
// always written, never coalesced: a turn id is only meaningful inside the
// engine connection that minted it, so the row must record which connection
// it belongs to. Passing empty values clears the stale pair rather than
// keeping a dead connection's ids alongside a live run's status.
func UpdateRunStatus(ctx context.Context, tx *sql.Tx, runID, status, providerTurnID, engineEpoch, errorKey string, ended bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if ended {
		_, err := tx.ExecContext(ctx, `UPDATE runs SET status = ?, provider_turn_id = ?, engine_epoch = ?, error_key = ?, ended_at = ? WHERE id = ?`,
			status, providerTurnID, engineEpoch, errorKey, now, runID)
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE runs SET status = ?, provider_turn_id = ?, engine_epoch = ?, error_key = ? WHERE id = ?`,
		status, providerTurnID, engineEpoch, errorKey, runID)
	return err
}

// GetRun reads one run.
// AcknowledgeRunStart records provider identity without undoing facts that
// arrived before the RPC response (approval/completion/connection loss).
func AcknowledgeRunStart(ctx context.Context, tx *sql.Tx, runID, turnID, epoch string) error {
	_, err := tx.ExecContext(ctx, `UPDATE runs SET
		status = CASE WHEN status = ? THEN ? ELSE status END,
		provider_turn_id = CASE WHEN provider_turn_id = '' THEN ? ELSE provider_turn_id END,
		engine_epoch = CASE WHEN engine_epoch = '' THEN ? ELSE engine_epoch END
		WHERE id = ?`, domain.RunStarting, domain.RunRunning, turnID, epoch, runID)
	return err
}

func GetRun(ctx context.Context, q Queryer, id string) (domain.Run, error) {
	var r domain.Run
	var started, ended string
	err := q.QueryRowContext(ctx, `SELECT id, team_id, bot_id, provider_session_id, provider_turn_id, engine_epoch, status, error_key, started_at, ended_at
		FROM runs WHERE id = ?`, id).
		Scan(&r.ID, &r.TeamID, &r.BotID, &r.ProviderSessionID, &r.ProviderTurnID, &r.EngineEpoch, &r.Status, &r.ErrorKey, &started, &ended)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Run{}, ErrNotFound
	}
	if err != nil {
		return domain.Run{}, err
	}
	r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	r.EndedAt, _ = time.Parse(time.RFC3339Nano, ended)
	return r, nil
}

// FindActiveRun returns the bot's non-terminal run, if any.
func FindActiveRun(ctx context.Context, q Queryer, botID string) (domain.Run, error) {
	var r domain.Run
	var started, ended string
	err := q.QueryRowContext(ctx, `SELECT id, team_id, bot_id, provider_session_id, provider_turn_id, engine_epoch, status, error_key, started_at, ended_at
		FROM runs WHERE bot_id = ? AND status IN (?, ?, ?, ?, ?) ORDER BY started_at DESC LIMIT 1`,
		botID, domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval, domain.RunWaitingInput, domain.RunUnknown).
		Scan(&r.ID, &r.TeamID, &r.BotID, &r.ProviderSessionID, &r.ProviderTurnID, &r.EngineEpoch, &r.Status, &r.ErrorKey, &started, &ended)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Run{}, ErrNotFound
	}
	if err != nil {
		return domain.Run{}, err
	}
	r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	r.EndedAt, _ = time.Parse(time.RFC3339Nano, ended)
	return r, nil
}

// ---- approvals ----

// InsertApproval stores an approval request. The state defaults to pending;
// callers that seed an already-decided record (crash-window tests) may pass
// it explicitly instead.
func InsertApproval(ctx context.Context, tx *sql.Tx, a domain.Approval) error {
	options, err := jsonMarshal(a.Options)
	if err != nil {
		return err
	}
	state := a.RequestState
	if state == "" {
		state = domain.ApprovalStatePending
	}
	created := a.CreatedAt.UTC().Format(time.RFC3339Nano)
	if a.CreatedAt.IsZero() {
		created = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if a.Revision < 1 {
		a.Revision = 1
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO approvals (id, team_id, requester_bot_id, run_id, engine_request_id, engine_epoch, type, method, summary, reason, options, request_state, created_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.TeamID, a.RequesterBotID, a.RunID, string(a.EngineRequestID), a.EngineEpoch, a.Type, a.Method, a.Summary, a.Reason, string(options), state, created, a.Revision)
	if err != nil {
		return fmt.Errorf("insert approval: %w", err)
	}
	return nil
}

// GetApproval reads one approval with its decision and response state.
func GetApproval(ctx context.Context, q Queryer, id string) (domain.Approval, error) {
	var a domain.Approval
	var options, engineReqID, created string
	err := q.QueryRowContext(ctx, `SELECT id, team_id, requester_bot_id, run_id, engine_request_id, engine_epoch, type, method, summary, reason, options, request_state, created_at, revision
		FROM approvals WHERE id = ?`, id).
		Scan(&a.ID, &a.TeamID, &a.RequesterBotID, &a.RunID, &engineReqID, &a.EngineEpoch, &a.Type, &a.Method, &a.Summary, &a.Reason, &options, &a.RequestState, &created, &a.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Approval{}, ErrNotFound
	}
	if err != nil {
		return domain.Approval{}, err
	}
	a.EngineRequestID = []byte(engineReqID)
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if err := jsonUnmarshal([]byte(options), &a.Options); err != nil {
		return domain.Approval{}, err
	}
	var decision domain.ApprovalDecision
	var decidedAt string
	err = q.QueryRowContext(ctx, `SELECT option_id, comment, actor, decided_at FROM approval_decisions WHERE approval_id = ?`, id).
		Scan(&decision.OptionID, &decision.Comment, &decision.Actor, &decidedAt)
	if err == nil {
		decision.DecidedAt, _ = time.Parse(time.RFC3339Nano, decidedAt)
		a.Decision = &decision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.Approval{}, err
	}
	var responseState string
	err = q.QueryRowContext(ctx, `SELECT state FROM approval_responses WHERE approval_id = ?`, id).Scan(&responseState)
	if err == nil {
		a.ResponseState = responseState
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.Approval{}, err
	}
	return a, nil
}

// ListApprovals pages approvals of a team filtered by state, newest first.
func ListApprovals(ctx context.Context, q Queryer, teamID, requestState string, limit int) ([]domain.Approval, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `SELECT id FROM approvals WHERE team_id = ?`
	args := []any{teamID}
	if requestState != "" {
		query += ` AND request_state = ?`
		args = append(args, requestState)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]domain.Approval, 0, len(ids))
	for _, id := range ids {
		a, err := GetApproval(ctx, q, id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// CountPendingApprovals counts pending cards of a scope.
func CountPendingApprovals(ctx context.Context, q Queryer, teamID, botID string) (int, error) {
	query := `SELECT COUNT(*) FROM approvals WHERE team_id = ? AND request_state = ?`
	args := []any{teamID, domain.ApprovalStatePending}
	if botID != "" {
		query += ` AND requester_bot_id = ?`
		args = append(args, botID)
	}
	var n int
	err := q.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

// DecideApproval atomically records the decision, marks the request decided
// and queues the engine response. expectedRevision drives CAS.
func DecideApproval(ctx context.Context, db *sql.DB, approvalID, optionID, comment, actor string, expectedRevision int64) (domain.Approval, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Approval{}, err
	}
	defer tx.Rollback()
	approval, err := DecideApprovalTx(ctx, tx, approvalID, optionID, comment, actor, expectedRevision)
	if err != nil {
		return domain.Approval{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Approval{}, err
	}
	return approval, nil
}

func DecideApprovalTx(ctx context.Context, tx *sql.Tx, approvalID, optionID, comment, actor string, expectedRevision int64) (domain.Approval, error) {
	approval, err := GetApproval(ctx, tx, approvalID)
	if err != nil {
		return domain.Approval{}, err
	}
	if approval.RequestState != domain.ApprovalStatePending {
		return domain.Approval{}, domain.Conflict("error.approval.already_decided", map[string]any{"state": approval.RequestState})
	}
	if expectedRevision > 0 && approval.Revision != expectedRevision {
		return domain.Approval{}, domain.RevisionConflict("error.approval.revision_conflict", map[string]any{"expected": expectedRevision, "actual": approval.Revision})
	}
	valid := false
	for _, opt := range approval.Options {
		if opt.ID == optionID {
			if opt.Disabled {
				return domain.Approval{}, domain.Conflict("error.approval.option_unavailable", nil)
			}
			valid = true
			break
		}
	}
	if !valid {
		return domain.Approval{}, domain.InvalidRequest("error.approval.unknown_option", map[string]any{"optionId": optionID})
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO approval_decisions (approval_id, option_id, comment, actor, decided_at) VALUES (?, ?, ?, ?, ?)`,
		approvalID, optionID, comment, actor, now); err != nil {
		return domain.Approval{}, err
	}
	// The decision and the engine response are separate facts: the request
	// becomes decided here; response delivery is tracked in approval_responses.
	if _, err := tx.ExecContext(ctx, `UPDATE approvals SET request_state = ?, revision = revision + 1 WHERE id = ?`,
		domain.ApprovalStateDecided, approvalID); err != nil {
		return domain.Approval{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO approval_responses (approval_id, state, detail, updated_at) VALUES (?, ?, '', ?)`,
		approvalID, domain.ResponseQueued, now); err != nil {
		return domain.Approval{}, err
	}
	return GetApproval(ctx, tx, approvalID)
}

// SetApprovalResponseState updates the response delivery projection.
func SetApprovalResponseState(ctx context.Context, db Queryer, approvalID, state, detail string) error {
	_, err := db.ExecContext(ctx, `UPDATE approval_responses SET state = ?, detail = ?, updated_at = ? WHERE approval_id = ?`,
		state, detail, time.Now().UTC().Format(time.RFC3339Nano), approvalID)
	return err
}

// EnsureApprovalResponseState records a response outcome even when no
// response row was ever created (an approval invalidated at startup never
// had a decision to seed one).
func EnsureApprovalResponseState(ctx context.Context, db Queryer, approvalID, state, detail string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO approval_responses (approval_id, state, detail, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(approval_id) DO UPDATE SET state = excluded.state, detail = excluded.detail, updated_at = excluded.updated_at`,
		approvalID, state, detail, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// InvalidateApprovalsOfEpoch marks undecided requests of a dead connection as
// invalidated; decided history is preserved untouched.
func InvalidateApprovalsOfEpoch(ctx context.Context, db *sql.DB, epoch string) (int64, error) {
	res, err := db.ExecContext(ctx, `UPDATE approvals SET request_state = ?, revision = revision + 1
		WHERE engine_epoch = ? AND request_state = ?`,
		domain.ApprovalStateInvalidated, epoch, domain.ApprovalStatePending)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---- idempotency ----

// LookupIdempotency returns the stored response for a key, if any.
func LookupIdempotency(ctx context.Context, q Queryer, scope, principal, key, requestHash string) (string, bool, error) {
	var storedHash, response string
	err := q.QueryRowContext(ctx, `SELECT request_hash, response FROM idempotency WHERE scope = ? AND principal = ? AND key = ?`,
		scope, principal, key).Scan(&storedHash, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if storedHash != requestHash {
		return "", false, domain.IdempotencyConflict("error.idempotency.reused_key", map[string]any{"key": key})
	}
	return response, true, nil
}

// SaveIdempotency records a completed write for replay.
func SaveIdempotency(ctx context.Context, tx *sql.Tx, scope, principal, key, requestHash, response string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO idempotency (scope, principal, key, request_hash, response, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		scope, principal, key, requestHash, response, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save idempotency: %w", err)
	}
	return nil
}

// FindBotByThread resolves the bot owning a provider thread. Thread ids are
// only unique inside one engine: the engine epoch is part of the key, so two
// app-servers that hand out the same thread id cannot collide.
func FindBotByThread(ctx context.Context, q Queryer, epoch, threadID string) (string, error) {
	var botID string
	// The join keeps a tombstoned member's old thread from resolving: after
	// a delete, engine events for that thread are reported as unknown rather
	// than routed back to a bot that no longer exists.
	err := q.QueryRowContext(ctx, `SELECT ps.bot_id FROM provider_sessions ps
		JOIN bots b ON b.id = ps.bot_id AND b.deleted_at = ''
		WHERE ps.provider_thread_id = ? AND ps.engine_epoch = ?`, threadID, epoch).Scan(&botID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return botID, err
}

// ListActiveRunsByTeam returns every non-terminal run of a team.
func ListActiveRunsByTeam(ctx context.Context, q Queryer, teamID string) ([]domain.Run, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, team_id, bot_id, provider_session_id, provider_turn_id, engine_epoch, status, error_key, started_at, ended_at
		FROM runs WHERE team_id = ? AND status IN (?, ?, ?, ?, ?)`,
		teamID, domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval, domain.RunWaitingInput, domain.RunUnknown)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Run
	for rows.Next() {
		var r domain.Run
		var started, ended string
		if err := rows.Scan(&r.ID, &r.TeamID, &r.BotID, &r.ProviderSessionID, &r.ProviderTurnID, &r.EngineEpoch, &r.Status, &r.ErrorKey, &started, &ended); err != nil {
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		r.EndedAt, _ = time.Parse(time.RFC3339Nano, ended)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListDeliveriesByMessage returns every delivery row of one message.
func ListDeliveriesByMessage(ctx context.Context, q Queryer, messageID string) ([]DeliveryRecord, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, message_id, team_id, target_bot_id, status, dispatch_key, created_at
		FROM deliveries WHERE message_id = ? ORDER BY created_at ASC`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeliveryRecord
	for rows.Next() {
		var r DeliveryRecord
		var created string
		if err := rows.Scan(&r.ID, &r.MessageID, &r.TeamID, &r.TargetBotID, &r.Status, &r.DispatchKey, &created); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListUnresolvedRuns returns every run in a non-terminal state across all
// teams; after a restart none of them may be taken over.
func ListUnresolvedRuns(ctx context.Context, q Queryer) ([]domain.Run, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, team_id, bot_id, provider_session_id, provider_turn_id, engine_epoch, status, error_key, started_at, ended_at
		FROM runs WHERE status IN (?, ?, ?, ?, ?) ORDER BY started_at ASC`,
		domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval, domain.RunWaitingInput, domain.RunUnknown)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Run
	for rows.Next() {
		var r domain.Run
		var started, ended string
		if err := rows.Scan(&r.ID, &r.TeamID, &r.BotID, &r.ProviderSessionID, &r.ProviderTurnID, &r.EngineEpoch, &r.Status, &r.ErrorKey, &started, &ended); err != nil {
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		r.EndedAt, _ = time.Parse(time.RFC3339Nano, ended)
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkDeliveryUnknownByRun flips a run's claimed deliveries to unknown (its
// turn is gone with the previous process) and mirrors the messages; the head
// delivery's id (the first a merged turn claimed) is returned so a recovery
// card can bind it.
func MarkDeliveryUnknownByRun(ctx context.Context, tx *sql.Tx, runID string) (string, error) {
	if runID == "" {
		return "", nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET delivery_status = ? WHERE id IN (SELECT message_id FROM deliveries WHERE run_id = ? AND status = ?)
		AND id NOT IN (SELECT message_id FROM deliveries WHERE run_id = ? ORDER BY created_at ASC, id ASC LIMIT 1)`,
		domain.DeliveryUnknown, runID, domain.DeliveryDispatching, runID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET status = ?, updated_at = ? WHERE run_id = ? AND status = ?
		AND id NOT IN (SELECT id FROM deliveries WHERE run_id = ? ORDER BY created_at ASC, id ASC LIMIT 1)`,
		domain.DeliveryUnknown, time.Now().UTC().Format(time.RFC3339Nano), runID, domain.DeliveryDispatching, runID); err != nil {
		return "", err
	}
	var deliveryID, messageID, status string
	err := tx.QueryRowContext(ctx, `SELECT id, message_id, status FROM deliveries WHERE run_id = ? ORDER BY created_at ASC, id ASC LIMIT 1`, runID).
		Scan(&deliveryID, &messageID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if status != domain.DeliveryDispatching && status != domain.DeliveryAccepted {
		return deliveryID, nil // already terminal or queued; leave the fact alone
	}
	if status == domain.DeliveryDispatching {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET status = ?, updated_at = ? WHERE id = ?`,
			domain.DeliveryUnknown, now, deliveryID); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET delivery_status = ? WHERE id = ?`,
			domain.DeliveryUnknown, messageID); err != nil {
			return "", err
		}
	}
	return deliveryID, nil
}

// ListPendingApprovalsOfType returns undecided requests of one type across
// teams; startup reconciliation uses it for connection-bound approvals.
func ListPendingApprovalsOfType(ctx context.Context, q Queryer, approvalType string) ([]domain.Approval, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM approvals WHERE request_state = ? AND type = ? ORDER BY created_at ASC`,
		domain.ApprovalStatePending, approvalType)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []domain.Approval
	for _, id := range ids {
		approval, err := GetApproval(ctx, q, id)
		if err != nil {
			return nil, err
		}
		out = append(out, approval)
	}
	return out, nil
}

// SetApprovalRequestState moves one request's state with a revision bump
// (used by startup invalidation; decisions keep their own CAS path).
func SetApprovalRequestState(ctx context.Context, tx *sql.Tx, id, state string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE approvals SET request_state = ?, revision = revision + 1 WHERE id = ? AND request_state = ?`,
		state, id, domain.ApprovalStatePending); err != nil {
		return fmt.Errorf("set approval state: %w", err)
	}
	return nil
}

// HasPendingRecoveryForRun keeps reconciliation idempotent: one open recovery
// card per unresolved run, however many times the process starts.
func HasPendingRecoveryForRun(ctx context.Context, q Queryer, runID string) (bool, error) {
	var n int
	pattern := `%"runId":"` + runID + `"%`
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM approvals WHERE type = ? AND request_state = ? AND engine_request_id LIKE ?`,
		domain.ApprovalRecovery, domain.ApprovalStatePending, pattern).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetRunDeliveriesStatus moves every delivery a run claimed (a merged turn
// claims several) from one of the given states to status, mirroring each
// message. It reports the message ids it touched.
func SetRunDeliveriesStatus(ctx context.Context, tx *sql.Tx, runID, status string, from ...string) ([]string, error) {
	if runID == "" || len(from) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")
	args := []any{runID}
	for _, f := range from {
		args = append(args, f)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, message_id FROM deliveries WHERE run_id = ? AND status IN (`+marks+`) ORDER BY created_at ASC, id ASC`, args...)
	if err != nil {
		return nil, err
	}
	var ids, messages []string
	for rows.Next() {
		var id, message string
		if err := rows.Scan(&id, &message); err != nil {
			rows.Close()
			return nil, err
		}
		ids, messages = append(ids, id), append(messages, message)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range ids {
		// SetDeliveryStatus also moves an approval result's response state.
		if err := SetDeliveryStatus(ctx, tx, ids[i], status); err != nil {
			return nil, err
		}
		if err := SetMessageDeliveryStatus(ctx, tx, messages[i], status); err != nil {
			return nil, err
		}
	}
	return messages, nil
}
