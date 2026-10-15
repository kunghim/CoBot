package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

const taskColumns = `id, team_id, owner_id, parent_id, title, description, acceptance_criteria,
	status, blocked_reason, revision, created_by, created_at, updated_at`

func scanTask(row interface{ Scan(...any) error }) (domain.Task, error) {
	var t domain.Task
	var created, updated string
	err := row.Scan(&t.ID, &t.TeamID, &t.OwnerID, &t.ParentID, &t.Title, &t.Description,
		&t.AcceptanceCriteria, &t.Status, &t.BlockedReason, &t.Revision, &t.CreatedBy, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, ErrNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("scan task: %w", err)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return t, nil
}

// GetTask reads one task and loads its dependency ids.
func GetTask(ctx context.Context, q Queryer, id string) (domain.Task, error) {
	task, err := scanTask(q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id))
	if err != nil {
		return domain.Task{}, err
	}
	deps, err := ListDependencies(ctx, q, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	task.DependencyIDs = deps
	return task, nil
}

// ListTasks returns a team's tasks, optionally filtered by owner, newest
// first. Cancelled tasks are included so history stays visible.
func ListTasks(ctx context.Context, q Queryer, teamID, ownerID string, limit int) ([]domain.Task, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE team_id = ?`
	args := []any{teamID}
	if ownerID != "" {
		query += ` AND owner_id = ?`
		args = append(args, ownerID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		deps, err := ListDependencies(ctx, q, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].DependencyIDs = deps
	}
	return out, nil
}

// InsertTask stores a new task inside the caller's transaction.
func InsertTask(ctx context.Context, tx *sql.Tx, task domain.Task) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO tasks (`+taskColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.TeamID, task.OwnerID, task.ParentID, task.Title, task.Description,
		task.AcceptanceCriteria, task.Status, task.BlockedReason, task.Revision,
		task.CreatedBy, now, now)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

// UpdateTaskStatus applies a status transition with optimistic revision.
func UpdateTaskStatus(ctx context.Context, db *sql.DB, taskID, status, blockedReason string, expectedRevision int64) (domain.Task, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	updated, err := UpdateTaskStatusTx(ctx, tx, taskID, status, blockedReason, expectedRevision)
	if err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	return updated, nil
}

func UpdateTaskStatusTx(ctx context.Context, tx *sql.Tx, taskID, status, blockedReason string, expectedRevision int64) (domain.Task, error) {
	task, err := GetTask(ctx, tx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	if expectedRevision > 0 && task.Revision != expectedRevision {
		return domain.Task{}, domain.RevisionConflict("error.task.revision_conflict", map[string]any{"expected": expectedRevision, "actual": task.Revision})
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, blocked_reason = ?, revision = revision + 1, updated_at = ? WHERE id = ?`,
		status, blockedReason, time.Now().UTC().Format(time.RFC3339Nano), taskID); err != nil {
		return domain.Task{}, err
	}
	return GetTask(ctx, tx, taskID)
}

// ChangeTaskOwner swaps the owner inside a transaction; callers must have
// verified the transfer is accepted and the old run ended.
func ChangeTaskOwner(ctx context.Context, tx *sql.Tx, taskID, ownerID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET owner_id = ?, revision = revision + 1, updated_at = ? WHERE id = ?`,
		ownerID, time.Now().UTC().Format(time.RFC3339Nano), taskID); err != nil {
		return fmt.Errorf("change task owner: %w", err)
	}
	return nil
}

// ---- dependencies ----

// ListDependencies returns the ids a task depends on. The result is never
// nil: the wire contract promises an array, not null (a nil slice would
// serialize as null and break array-typed consumers).
// (normalization happens at the end of the function)
func ListDependencies(ctx context.Context, q Queryer, taskID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT depends_on_task_id FROM task_dependencies WHERE task_id = ? ORDER BY depends_on_task_id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list dependencies: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// AddDependency records one edge; the caller has already proven the tasks
// share a team and that adding it keeps the graph acyclic.
func AddDependency(ctx context.Context, tx *sql.Tx, taskID, dependsOn string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO task_dependencies (task_id, depends_on_task_id) VALUES (?, ?)`, taskID, dependsOn)
	if err != nil {
		return fmt.Errorf("add dependency: %w", err)
	}
	return nil
}

// ListDependentTasks returns task ids that depend on the given task.
func ListDependentTasks(ctx context.Context, q Queryer, taskID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT task_id FROM task_dependencies WHERE depends_on_task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// HasCycleFrom reports whether adding edge (from -> to) would create a cycle,
// by walking the existing dependencies of `to` looking for `from`.
func HasCycleFrom(ctx context.Context, q Queryer, from, to string) (bool, error) {
	seen := map[string]bool{to: true}
	queue := []string{to}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		deps, err := ListDependencies(ctx, q, current)
		if err != nil {
			return false, err
		}
		for _, dep := range deps {
			if dep == from {
				return true, nil
			}
			if !seen[dep] {
				seen[dep] = true
				queue = append(queue, dep)
			}
		}
	}
	return false, nil
}

// ---- evidence ----

// InsertEvidence stores one proof reference.
func InsertEvidence(ctx context.Context, tx *sql.Tx, e domain.Evidence) error {
	refs, err := json.Marshal(e.ArtifactRefs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_evidence (id, task_id, author_id, summary, artifact_refs, candidate_ref, test_result, review_result, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TaskID, e.AuthorID, e.Summary, string(refs), e.CandidateRef, e.TestResult, e.ReviewResult,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert evidence: %w", err)
	}
	return nil
}

// ListEvidence returns a task's evidence, oldest first.
func ListEvidence(ctx context.Context, q Queryer, taskID string) ([]domain.Evidence, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, task_id, author_id, summary, artifact_refs, candidate_ref, test_result, review_result, created_at
		FROM task_evidence WHERE task_id = ? ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Evidence
	for rows.Next() {
		var e domain.Evidence
		var refs, created string
		if err := rows.Scan(&e.ID, &e.TaskID, &e.AuthorID, &e.Summary, &refs, &e.CandidateRef, &e.TestResult, &e.ReviewResult, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(refs), &e.ArtifactRefs)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// HasEvidence reports whether a task has at least one evidence row.
func HasEvidence(ctx context.Context, q Queryer, taskID string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_evidence WHERE task_id = ?`, taskID).Scan(&n)
	return n > 0, err
}

// ---- transfers ----

// InsertTransfer stores an offered transfer.
func InsertTransfer(ctx context.Context, tx *sql.Tx, t domain.Transfer) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO transfers (id, task_id, from_bot_id, to_bot_id, task_revision, note, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.TaskID, t.FromBotID, t.ToBotID, t.TaskRevision, t.Note, t.Status, now, now)
	if err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

// GetTransfer reads one transfer.
func GetTransfer(ctx context.Context, q Queryer, id string) (domain.Transfer, error) {
	var t domain.Transfer
	var created, updated string
	err := q.QueryRowContext(ctx, `SELECT id, task_id, from_bot_id, to_bot_id, task_revision, note, status, created_at, updated_at
		FROM transfers WHERE id = ?`, id).
		Scan(&t.ID, &t.TaskID, &t.FromBotID, &t.ToBotID, &t.TaskRevision, &t.Note, &t.Status, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, ErrNotFound
	}
	if err != nil {
		return domain.Transfer{}, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return t, nil
}

// SetTransferStatus transitions a transfer with optimistic revision on the
// owning task.
// SetTransferStatus moves a transfer only from the expected state: the guard
// lives in the WHERE clause, so two concurrent accept/cancel attempts cannot
// both succeed (one sees no row and gets a conflict).
func SetTransferStatus(ctx context.Context, tx *sql.Tx, id, fromStatus, toStatus string) error {
	res, err := tx.ExecContext(ctx, `UPDATE transfers SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		toStatus, time.Now().UTC().Format(time.RFC3339Nano), id, fromStatus)
	if err != nil {
		return fmt.Errorf("update transfer: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.Conflict("error.transfer.not_offered", map[string]any{"status": fromStatus})
	}
	return nil
}

// ListTransfersForTask returns transfers of one task, newest first.
func ListTransfersForTask(ctx context.Context, q Queryer, taskID string) ([]domain.Transfer, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM transfers WHERE task_id = ? ORDER BY created_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := make([]domain.Transfer, 0, len(ids))
	for _, id := range ids {
		t, err := GetTransfer(ctx, q, id)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// ---- questions ----

// InsertQuestion stores an open ask_user request.
func InsertQuestion(ctx context.Context, tx *sql.Tx, q domain.Question) error {
	options, err := json.Marshal(q.Options)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO questions (id, team_id, requester_bot_id, task_id, run_id, question, options, status, revision, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.ID, q.TeamID, q.RequesterBotID, q.TaskID, q.RunID, q.Question, string(options), q.Status, q.Revision,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert question: %w", err)
	}
	return nil
}

// GetQuestion reads one question with its options.
func GetQuestion(ctx context.Context, q Queryer, id string) (domain.Question, error) {
	var out domain.Question
	var options, created, answered string
	err := q.QueryRowContext(ctx, `SELECT id, team_id, requester_bot_id, task_id, run_id, question, options, status,
		answer_text, answer_message_id, revision, created_at, answered_at FROM questions WHERE id = ?`, id).
		Scan(&out.ID, &out.TeamID, &out.RequesterBotID, &out.TaskID, &out.RunID, &out.Question, &options,
			&out.Status, &out.AnswerText, &out.AnswerMessageID, &out.Revision, &created, &answered)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Question{}, ErrNotFound
	}
	if err != nil {
		return domain.Question{}, err
	}
	_ = json.Unmarshal([]byte(options), &out.Options)
	out.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	out.AnsweredAt, _ = time.Parse(time.RFC3339Nano, answered)
	return out, nil
}

// ListQuestions pages a team's questions by status, newest first.
func ListQuestions(ctx context.Context, q Queryer, teamID, status string, limit int) ([]domain.Question, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `SELECT id FROM questions WHERE team_id = ?`
	args := []any{teamID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := make([]domain.Question, 0, len(ids))
	for _, id := range ids {
		item, err := GetQuestion(ctx, q, id)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// AnswerQuestion records the user's answer inside a transaction; the caller
// links the answer to a user message in the same transaction.
func AnswerQuestion(ctx context.Context, tx *sql.Tx, id, answerText, messageID string, expectedRevision int64) (domain.Question, error) {
	question, err := GetQuestion(ctx, tx, id)
	if err != nil {
		return domain.Question{}, err
	}
	if question.Status != domain.QuestionOpen {
		return domain.Question{}, domain.Conflict("error.question.already_answered", map[string]any{"status": question.Status})
	}
	if expectedRevision > 0 && question.Revision != expectedRevision {
		return domain.Question{}, domain.RevisionConflict("error.question.revision_conflict", map[string]any{"expected": expectedRevision, "actual": question.Revision})
	}
	if _, err := tx.ExecContext(ctx, `UPDATE questions SET status = ?, answer_text = ?, answer_message_id = ?, revision = revision + 1, answered_at = ? WHERE id = ?`,
		domain.QuestionAnswered, answerText, messageID, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return domain.Question{}, err
	}
	return GetQuestion(ctx, tx, id)
}

// ListDeliveriesByMessageOfTask returns undelivered assignment rows for one
// task and owner. Assignment messages carry the task id in run_id, which is
// free for system messages.
func ListDeliveriesByMessageOfTask(ctx context.Context, q Queryer, taskID, botID string) ([]DeliveryRecord, error) {
	rows, err := q.QueryContext(ctx, `SELECT d.id, d.message_id, d.team_id, d.target_bot_id, d.status, d.dispatch_key, d.created_at
		FROM deliveries d JOIN messages m ON m.id = d.message_id
		WHERE d.task_id = ? AND (? = '' OR d.target_bot_id = ?)
		ORDER BY d.created_at ASC`, taskID, botID, botID)
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

// ActivateWaitingDeliveries promotes the waiting_dependency assignment of a
// task to queued, inside the caller's transaction.
func ActivateWaitingDeliveries(ctx context.Context, tx *sql.Tx, taskID, botID string) error {
	records, err := ListDeliveriesByMessageOfTask(ctx, tx, taskID, botID)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Status != domain.DeliveryWaitingDependency {
			continue
		}
		if err := SetDeliveryStatus(ctx, tx, record.ID, domain.DeliveryQueued); err != nil {
			return err
		}
		if err := SetMessageDeliveryStatus(ctx, tx, record.MessageID, domain.DeliveryQueued); err != nil {
			return err
		}
	}
	return nil
}

// CancelQuestionRow marks an open question as cancelled.
func CancelQuestionRow(ctx context.Context, tx *sql.Tx, id string, expectedRevision int64) (domain.Question, error) {
	question, err := GetQuestion(ctx, tx, id)
	if err != nil {
		return domain.Question{}, err
	}
	if question.Status != domain.QuestionOpen {
		return domain.Question{}, domain.Conflict("error.question.already_answered", map[string]any{"status": question.Status})
	}
	if expectedRevision > 0 && question.Revision != expectedRevision {
		return domain.Question{}, domain.RevisionConflict("error.question.revision_conflict", map[string]any{"expected": expectedRevision, "actual": question.Revision})
	}
	if _, err := tx.ExecContext(ctx, `UPDATE questions SET status = ?, revision = revision + 1 WHERE id = ?`,
		domain.QuestionCancelled, id); err != nil {
		return domain.Question{}, err
	}
	return GetQuestion(ctx, tx, id)
}
