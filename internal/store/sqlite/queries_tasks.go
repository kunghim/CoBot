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

// ---- cards ----

const taskColumns = `id, team_id, owner_id, title, description, acceptance_criteria, status,
	review_type, requirement_id, stage_index, stage_kind, return_to, revision, created_by, created_at, updated_at`

func scanTask(row interface{ Scan(...any) error }) (domain.Task, error) {
	var t domain.Task
	var created, updated string
	err := row.Scan(&t.ID, &t.TeamID, &t.OwnerID, &t.Title, &t.Description, &t.AcceptanceCriteria, &t.Status,
		&t.ReviewType, &t.RequirementID, &t.StageIndex, &t.StageKind, &t.ReturnTo, &t.Revision, &t.CreatedBy, &created, &updated)
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

func scanTasks(rows *sql.Rows) ([]domain.Task, error) {
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTask reads one card.
func GetTask(ctx context.Context, q Queryer, id string) (domain.Task, error) {
	return scanTask(q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id))
}

// ListTasks returns a team's cards, optionally one owner's, newest first.
// Finished and cancelled cards are included so history stays visible.
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
	return scanTasks(rows)
}

// ListRequirementTasks returns a requirement's cards in the order they were
// handed out.
func ListRequirementTasks(ctx context.Context, q Queryer, requirementID string) ([]domain.Task, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE requirement_id = ? ORDER BY created_at, id`, requirementID)
	if err != nil {
		return nil, fmt.Errorf("list requirement tasks: %w", err)
	}
	return scanTasks(rows)
}

// InsertTask stores a new card inside the caller's transaction.
func InsertTask(ctx context.Context, tx *sql.Tx, task domain.Task) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO tasks (`+taskColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.TeamID, task.OwnerID, task.Title, task.Description, task.AcceptanceCriteria, task.Status,
		task.ReviewType, task.RequirementID, task.StageIndex, task.StageKind, task.ReturnTo, task.Revision,
		task.CreatedBy, now, now)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

// UpdateTaskStatusTx moves a card to a status with optimistic revision.
func UpdateTaskStatusTx(ctx context.Context, tx *sql.Tx, taskID, status string, expectedRevision int64) (domain.Task, error) {
	task, err := GetTask(ctx, tx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	if expectedRevision > 0 && task.Revision != expectedRevision {
		return domain.Task{}, domain.RevisionConflict("error.task.revision_conflict", map[string]any{"expected": expectedRevision, "actual": task.Revision})
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, revision = revision + 1, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC().Format(time.RFC3339Nano), taskID); err != nil {
		return domain.Task{}, err
	}
	return GetTask(ctx, tx, taskID)
}

// ---- requirements ----

const requirementColumns = `id, team_id, title, document, stages, status, created_by, chain_id, revision, created_at, updated_at`

func scanRequirement(row interface{ Scan(...any) error }) (domain.Requirement, error) {
	var r domain.Requirement
	var stages, created, updated string
	err := row.Scan(&r.ID, &r.TeamID, &r.Title, &r.Document, &stages, &r.Status, &r.CreatedBy, &r.ChainID, &r.Revision, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Requirement{}, ErrNotFound
	}
	if err != nil {
		return domain.Requirement{}, fmt.Errorf("scan requirement: %w", err)
	}
	_ = json.Unmarshal([]byte(stages), &r.Stages)
	if r.Stages == nil {
		r.Stages = []domain.Stage{}
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, nil
}

// InsertRequirement stores a new requirement.
func InsertRequirement(ctx context.Context, tx *sql.Tx, r domain.Requirement) error {
	stages, err := json.Marshal(r.Stages)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO requirements (`+requirementColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TeamID, r.Title, r.Document, string(stages), r.Status, r.CreatedBy, r.ChainID, r.Revision, now, now)
	if err != nil {
		return fmt.Errorf("insert requirement: %w", err)
	}
	return nil
}

// GetRequirement reads one requirement.
func GetRequirement(ctx context.Context, q Queryer, id string) (domain.Requirement, error) {
	return scanRequirement(q.QueryRowContext(ctx, `SELECT `+requirementColumns+` FROM requirements WHERE id = ?`, id))
}

// ListRequirements returns a team's requirements, newest first; status ""
// lists all.
func ListRequirements(ctx context.Context, q Queryer, teamID, status string, limit int) ([]domain.Requirement, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + requirementColumns + ` FROM requirements WHERE team_id = ?`
	args := []any{teamID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list requirements: %w", err)
	}
	defer rows.Close()
	var out []domain.Requirement
	for rows.Next() {
		r, err := scanRequirement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateRequirement stores a requirement's document and status.
func UpdateRequirement(ctx context.Context, tx *sql.Tx, id, document, status string) (domain.Requirement, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE requirements SET document = ?, status = ?, revision = revision + 1, updated_at = ? WHERE id = ?`,
		document, status, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return domain.Requirement{}, fmt.Errorf("update requirement: %w", err)
	}
	return GetRequirement(ctx, tx, id)
}

// ---- handoff records ----

// InsertEvidence stores one handoff record.
func InsertEvidence(ctx context.Context, tx *sql.Tx, e domain.Evidence) error {
	refs, err := json.Marshal(e.ArtifactRefs)
	if err != nil {
		return err
	}
	execution := ""
	if e.Execution != nil {
		raw, err := json.Marshal(e.Execution)
		if err != nil {
			return err
		}
		execution = string(raw)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_evidence (id, task_id, author_id, summary, artifact_refs, outcome, mode, suggestions, execution, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TaskID, e.AuthorID, e.Summary, string(refs), e.Outcome, e.Mode, e.Suggestions, execution,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert evidence: %w", err)
	}
	return nil
}

// ListEvidence returns a card's handoff records, oldest first.
func ListEvidence(ctx context.Context, q Queryer, taskID string) ([]domain.Evidence, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, task_id, author_id, summary, artifact_refs, outcome, mode, suggestions, execution, created_at
		FROM task_evidence WHERE task_id = ? ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Evidence
	for rows.Next() {
		var e domain.Evidence
		var refs, execution, created string
		if err := rows.Scan(&e.ID, &e.TaskID, &e.AuthorID, &e.Summary, &refs, &e.Outcome, &e.Mode, &e.Suggestions, &execution, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(refs), &e.ArtifactRefs)
		if e.ArtifactRefs == nil {
			e.ArtifactRefs = []string{}
		}
		if execution != "" {
			var x domain.Execution
			if json.Unmarshal([]byte(execution), &x) == nil {
				e.Execution = &x
			}
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountSendBacks counts how often the cards of one stage of a requirement
// sent work back.
func CountSendBacks(ctx context.Context, q Queryer, requirementID string, stageIndex int) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM task_evidence e JOIN tasks t ON t.id = e.task_id
		WHERE t.requirement_id = ? AND t.stage_index = ? AND t.stage_kind != ? AND e.outcome = ?`,
		requirementID, stageIndex, domain.CardEscalation, domain.HandoffBack).Scan(&n)
	return n, err
}

// ---- questions ----

// InsertQuestion stores an open question.
func InsertQuestion(ctx context.Context, tx *sql.Tx, q domain.Question) error {
	options, err := json.Marshal(q.Options)
	if err != nil {
		return err
	}
	roleIDs, err := json.Marshal(q.RoleIDs)
	if err != nil {
		return err
	}
	if q.RoleIDs == nil {
		roleIDs = []byte("[]")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO questions (id, kind, requirement_id, stage_index, team_id, requester_bot_id, task_id, run_id, question, options, role_ids, status, revision, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.ID, q.Kind, q.RequirementID, q.StageIndex, q.TeamID, q.RequesterBotID, q.TaskID, q.RunID, q.Question, string(options), string(roleIDs), q.Status, q.Revision,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert question: %w", err)
	}
	return nil
}

// GetQuestion reads one question with its options.
func GetQuestion(ctx context.Context, q Queryer, id string) (domain.Question, error) {
	var out domain.Question
	var options, roleIDs, created, answered string
	err := q.QueryRowContext(ctx, `SELECT id, kind, requirement_id, stage_index, team_id, requester_bot_id, task_id, run_id, question, options, role_ids, status,
		answer_text, answer_message_id, revision, created_at, answered_at FROM questions WHERE id = ?`, id).
		Scan(&out.ID, &out.Kind, &out.RequirementID, &out.StageIndex, &out.TeamID, &out.RequesterBotID, &out.TaskID, &out.RunID, &out.Question, &options, &roleIDs,
			&out.Status, &out.AnswerText, &out.AnswerMessageID, &out.Revision, &created, &answered)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Question{}, ErrNotFound
	}
	if err != nil {
		return domain.Question{}, err
	}
	_ = json.Unmarshal([]byte(options), &out.Options)
	_ = json.Unmarshal([]byte(roleIDs), &out.RoleIDs)
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
