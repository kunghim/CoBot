package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// CreateTaskInput is the create request shared by the user API and the
// captain's create_task tool.
type CreateTaskInput struct {
	TeamID             string
	OwnerID            string
	Title              string
	Description        string
	AcceptanceCriteria string
	ParentID           string
	DependencyIDs      []string
	RequestID          string
}

// isLeaderOf reports whether the bot leads the team.
func (a *App) isLeaderOf(ctx context.Context, teamID, botID string) (bool, error) {
	team, _, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return false, err
	}
	return team.LeaderID == botID, nil
}

// assertSameTeamOwner verifies the owner is a live member of the team.
func (a *App) assertSameTeamOwner(ctx context.Context, q sqlite.Queryer, teamID, ownerID string) error {
	bot, err := sqlite.GetBot(ctx, q, ownerID)
	if err != nil {
		return sqlite.NormalizeError(err)
	}
	if bot.TeamID != teamID {
		return domain.InvalidRequest("error.task.owner_other_team", map[string]any{"ownerId": ownerID})
	}
	return nil
}

// validateDependencies proves every dependency is in the same team and that
// adding the edges keeps the graph acyclic.
func (a *App) validateDependencies(ctx context.Context, q sqlite.Queryer, taskID string, teamID string, deps []string) error {
	seen := map[string]bool{}
	for _, dep := range deps {
		if dep == "" || seen[dep] {
			continue
		}
		seen[dep] = true
		depTask, err := sqlite.GetTask(ctx, q, dep)
		if err != nil {
			return domain.InvalidRequest("error.task.dependency_unknown", map[string]any{"taskId": dep})
		}
		if depTask.TeamID != teamID {
			return domain.InvalidRequest("error.task.dependency_other_team", map[string]any{"taskId": dep})
		}
		// The cycle walk does not need the task row to exist, so it also
		// covers the create path: a task may not depend on a task that depends
		// on it, even while both rows are being written for the first time.
		cycle, err := sqlite.HasCycleFrom(ctx, q, taskID, dep)
		if err != nil {
			return err
		}
		if cycle {
			return domain.InvalidRequest("error.task.dependency_cycle", map[string]any{"taskId": taskID, "dependsOn": dep})
		}
	}
	return nil
}

// CreateTask creates a task with its dependencies and, when the owner is
// ready, one assignment delivery. captainOnly distinguishes the bot tool from
// the user path.
func (a *App) CreateTask(ctx context.Context, principal auth.Principal, captainOnly bool, in CreateTaskInput) (domain.Task, error) {
	team, _, err := sqlite.GetTeam(ctx, a.DB, in.TeamID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	if team.Status != domain.TeamStatusActive {
		return domain.Task{}, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}
	switch principal.Kind {
	case auth.KindUser:
		// the user may always create
	case auth.KindBot:
		isLeader, err := a.isLeaderOf(ctx, in.TeamID, principal.BotID)
		if err != nil {
			return domain.Task{}, sqlite.NormalizeError(err)
		}
		if captainOnly && !isLeader {
			return domain.Task{}, domain.Forbidden("error.task.captain_only", nil)
		}
	default:
		return domain.Task{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return domain.Task{}, domain.InvalidRequest("error.task.title_required", nil)
	}
	if err := a.assertSameTeamOwner(ctx, a.DB, in.TeamID, in.OwnerID); err != nil {
		return domain.Task{}, err
	}
	if in.ParentID != "" {
		parent, err := sqlite.GetTask(ctx, a.DB, in.ParentID)
		if err != nil {
			return domain.Task{}, domain.InvalidRequest("error.task.parent_unknown", map[string]any{"taskId": in.ParentID})
		}
		if parent.TeamID != in.TeamID {
			return domain.Task{}, domain.InvalidRequest("error.task.parent_other_team", map[string]any{"taskId": in.ParentID})
		}
	}

	createdBy := domain.TaskAuthorUser
	if principal.Kind == auth.KindBot {
		createdBy = principal.BotID
	}
	task := domain.Task{
		ID: domain.NewID(), TeamID: in.TeamID, OwnerID: in.OwnerID,
		ParentID: in.ParentID, Title: title, Description: in.Description,
		AcceptanceCriteria: in.AcceptanceCriteria, Status: domain.TaskTodo,
		Revision: 1, CreatedBy: createdBy,
	}

	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	if err := a.validateDependencies(ctx, tx, task.ID, in.TeamID, in.DependencyIDs); err != nil {
		return domain.Task{}, err
	}
	if err := sqlite.InsertTask(ctx, tx, task); err != nil {
		if isUniqueViolation(err) {
			return domain.Task{}, domain.IdempotencyConflict("error.task.request_reused", map[string]any{"requestId": in.RequestID})
		}
		return domain.Task{}, err
	}
	for _, dep := range in.DependencyIDs {
		if dep == "" {
			continue
		}
		if err := sqlite.AddDependency(ctx, tx, task.ID, dep); err != nil {
			return domain.Task{}, err
		}
	}
	ready, err := a.dependenciesSatisfied(ctx, tx, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := a.enqueueAssignmentLocked(ctx, tx, task, ready); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskCreated, TeamID: task.TeamID, ResourceID: task.ID, Revision: task.Revision})
	a.Hub.Publish()
	if ready {
		if a.Scheduler != nil {
			a.Scheduler.Wake(task.OwnerID)
		}
	}
	return sqlite.GetTask(ctx, a.DB, task.ID)
}

// dependenciesSatisfied reports whether every dependency of the task is done.
func (a *App) dependenciesSatisfied(ctx context.Context, q sqlite.Queryer, taskID string) (bool, error) {
	task, err := sqlite.GetTask(ctx, q, taskID)
	if err != nil {
		return false, err
	}
	for _, dep := range task.DependencyIDs {
		depTask, err := sqlite.GetTask(ctx, q, dep)
		if err != nil {
			return false, err
		}
		if depTask.Status != domain.TaskDone {
			return false, nil
		}
	}
	return true, nil
}

// enqueueAssignmentLocked creates the one task-assignment delivery for an
// owner inside the caller's transaction. The status reflects whether the
// dependencies allow the bot to start.
func (a *App) enqueueAssignmentLocked(ctx context.Context, tx *sql.Tx, task domain.Task, ready bool) error {
	status := domain.DeliveryQueued
	if !ready {
		status = domain.DeliveryWaitingDependency
	}
	text := fmt.Sprintf("你被指派了任务：%s\n完成标准：%s", task.Title, task.AcceptanceCriteria)
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return err
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: task.TeamID, BotID: task.OwnerID,
		Kind: domain.MessageKindSystem, Blocks: blocks, DeliveryStatus: status, RunID: task.ID,
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return err
	}
	return sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: domain.NewID(), MessageID: message.ID, TeamID: task.TeamID,
		TargetBotID: task.OwnerID, Status: status,
	})
}

// AssignTask changes the owner through the same safety rules as a transfer:
// the caller must be the captain or the user, and a bot owner's active run on
// the task must have ended.
func (a *App) AssignTask(ctx context.Context, principal auth.Principal, taskID, newOwnerID string, expectedRevision int64) (domain.Task, error) {
	task, err := sqlite.GetTask(ctx, a.DB, taskID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		isLeader, err := a.isLeaderOf(ctx, task.TeamID, principal.BotID)
		if err != nil {
			return domain.Task{}, sqlite.NormalizeError(err)
		}
		if !isLeader {
			return domain.Task{}, domain.Forbidden("error.task.captain_only", nil)
		}
	default:
		return domain.Task{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if expectedRevision > 0 && task.Revision != expectedRevision {
		return domain.Task{}, domain.RevisionConflict("error.task.revision_conflict", map[string]any{"expected": expectedRevision, "actual": task.Revision})
	}
	if task.OwnerID == newOwnerID {
		return task, nil
	}
	if err := a.assertSameTeamOwner(ctx, a.DB, task.TeamID, newOwnerID); err != nil {
		return domain.Task{}, err
	}
	if err := a.assertOwnerRunEnded(ctx, task.OwnerID, task.ID); err != nil {
		return domain.Task{}, err
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	// Cancel the old owner's undelivered assignment, then give the new owner
	// exactly one.
	if err := a.cancelUndeliveriesForTaskLocked(ctx, tx, task.ID, task.OwnerID); err != nil {
		return domain.Task{}, err
	}
	if err := sqlite.ChangeTaskOwner(ctx, tx, task.ID, newOwnerID); err != nil {
		return domain.Task{}, err
	}
	task.OwnerID = newOwnerID
	task.Revision++
	ready, err := a.dependenciesSatisfied(ctx, tx, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := a.enqueueAssignmentLocked(ctx, tx, task, ready); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, ResourceID: task.ID, Revision: task.Revision})
	a.Hub.Publish()
	if ready && a.Scheduler != nil {
		a.Scheduler.Wake(newOwnerID)
	}
	return sqlite.GetTask(ctx, a.DB, task.ID)
}

// cancelUndeliveriesForTaskLocked cancels queued or waiting assignments of a
// task for one owner inside the caller's transaction.
func (a *App) cancelUndeliveriesForTaskLocked(ctx context.Context, tx *sql.Tx, taskID, botID string) error {
	deliveries, err := sqlite.ListDeliveriesByMessageOfTask(ctx, tx, taskID, botID)
	if err != nil {
		return err
	}
	for _, delivery := range deliveries {
		if delivery.Status == domain.DeliveryQueued || delivery.Status == domain.DeliveryWaitingDependency {
			if err := sqlite.SetDeliveryStatus(ctx, tx, delivery.ID, domain.DeliveryCancelled); err != nil {
				return err
			}
		}
	}
	return nil
}

// assertOwnerRunEnded refuses to move a task whose current owner may still
// be writing files for it.
func (a *App) assertOwnerRunEnded(ctx context.Context, ownerID, taskID string) error {
	run, err := sqlite.FindActiveRun(ctx, a.DB, ownerID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return nil
		}
		return err
	}
	if run.Status == domain.RunUnknown {
		return domain.Conflict("error.task.owner_run_unknown", map[string]any{"runId": run.ID})
	}
	// A run that is still starting, running or waiting may write files.
	return domain.Conflict("error.task.owner_run_active", map[string]any{"runId": run.ID, "status": run.Status})
}

// UpdateTaskStatus applies a lifecycle transition with the product rules:
// done requires evidence, review requires evidence, and only the owner moves
// doing/review; captain or user may cancel.
func (a *App) UpdateTaskStatus(ctx context.Context, principal auth.Principal, taskID, status, blockedReason string, expectedRevision int64) (domain.Task, error) {
	task, err := sqlite.GetTask(ctx, a.DB, taskID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	if expectedRevision > 0 && task.Revision != expectedRevision {
		return domain.Task{}, domain.RevisionConflict("error.task.revision_conflict", map[string]any{"expected": expectedRevision, "actual": task.Revision})
	}
	if err := a.authorizeTaskWrite(ctx, principal, task, status); err != nil {
		return domain.Task{}, err
	}
	switch status {
	case domain.TaskDoing, domain.TaskReview:
		has, err := sqlite.HasEvidence(ctx, a.DB, task.ID)
		if err != nil {
			return domain.Task{}, err
		}
		if status == domain.TaskReview && !has {
			return domain.Task{}, domain.InvalidRequest("error.task.review_needs_evidence", nil)
		}
	case domain.TaskDone:
		// done only through confirm, which checks evidence and the confirmer.
		return domain.Task{}, domain.InvalidRequest("error.task.done_needs_confirmation", nil)
	}
	updated, err := sqlite.UpdateTaskStatus(ctx, a.DB, task.ID, status, blockedReason, expectedRevision)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, ResourceID: task.ID, Revision: updated.Revision})
	a.Hub.Publish()
	return updated, nil
}

// authorizeTaskWrite enforces the background permission rules; the UI never
// decides.
func (a *App) authorizeTaskWrite(ctx context.Context, principal auth.Principal, task domain.Task, newStatus string) error {
	switch principal.Kind {
	case auth.KindUser:
		return nil
	case auth.KindBot:
		isLeader, err := a.isLeaderOf(ctx, task.TeamID, principal.BotID)
		if err != nil {
			return err
		}
		if isLeader {
			return nil
		}
		if principal.BotID != task.OwnerID {
			return domain.Forbidden("error.task.owner_only", nil)
		}
		return nil
	default:
		return domain.Unauthorized("error.unauthorized.principal_required")
	}
}

// SubmitEvidence records a proof reference for a task; the owner or the
// captain may submit.
func (a *App) SubmitEvidence(ctx context.Context, principal auth.Principal, taskID, summary string, artifactRefs []string, candidateRef, testResult, reviewResult string) (domain.Evidence, error) {
	task, err := sqlite.GetTask(ctx, a.DB, taskID)
	if err != nil {
		return domain.Evidence{}, sqlite.NormalizeError(err)
	}
	author := domain.TaskAuthorUser
	if principal.Kind == auth.KindBot {
		isLeader, err := a.isLeaderOf(ctx, task.TeamID, principal.BotID)
		if err != nil {
			return domain.Evidence{}, sqlite.NormalizeError(err)
		}
		if !isLeader && principal.BotID != task.OwnerID {
			return domain.Evidence{}, domain.Forbidden("error.task.owner_or_captain", nil)
		}
		author = principal.BotID
	} else if principal.Kind != auth.KindUser {
		return domain.Evidence{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if strings.TrimSpace(summary) == "" {
		return domain.Evidence{}, domain.InvalidRequest("error.task.evidence_summary_required", nil)
	}
	evidence := domain.Evidence{
		ID: domain.NewID(), TaskID: task.ID, AuthorID: author, Summary: strings.TrimSpace(summary),
		ArtifactRefs: artifactRefs, CandidateRef: candidateRef, TestResult: testResult, ReviewResult: reviewResult,
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Evidence{}, err
	}
	defer tx.Rollback()
	if err := sqlite.InsertEvidence(ctx, tx, evidence); err != nil {
		return domain.Evidence{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Evidence{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, ResourceID: task.ID})
	a.Hub.Publish()
	return evidence, nil
}

// ConfirmTask moves a task to done; the captain or the user confirms against
// matching evidence. A chat message never does this by itself.
func (a *App) ConfirmTask(ctx context.Context, principal auth.Principal, taskID string, evidenceIDs []string, expectedRevision int64) (domain.Task, error) {
	task, err := sqlite.GetTask(ctx, a.DB, taskID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		isLeader, err := a.isLeaderOf(ctx, task.TeamID, principal.BotID)
		if err != nil {
			return domain.Task{}, sqlite.NormalizeError(err)
		}
		if !isLeader {
			return domain.Task{}, domain.Forbidden("error.task.captain_only", nil)
		}
	default:
		return domain.Task{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if task.Status != domain.TaskReview {
		return domain.Task{}, domain.InvalidRequest("error.task.confirm_needs_review", map[string]any{"status": task.Status})
	}
	evidence, err := sqlite.ListEvidence(ctx, a.DB, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if len(evidence) == 0 {
		return domain.Task{}, domain.InvalidRequest("error.task.confirm_needs_evidence", nil)
	}
	if len(evidenceIDs) > 0 {
		have := map[string]bool{}
		for _, e := range evidence {
			have[e.ID] = true
		}
		for _, id := range evidenceIDs {
			if !have[id] {
				return domain.Task{}, domain.InvalidRequest("error.task.evidence_unknown", map[string]any{"evidenceId": id})
			}
		}
	}
	updated, err := sqlite.UpdateTaskStatus(ctx, a.DB, task.ID, domain.TaskDone, "", expectedRevision)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, ResourceID: task.ID, Revision: updated.Revision})
	a.Hub.Publish()
	// A done task may release dependents that were waiting on it.
	if err := a.activateDependentDeliveries(ctx, task.ID); err != nil {
		a.Logger.Warn("activate dependents failed", "taskId", task.ID, "error", err)
	}
	return updated, nil
}

// activateDependentDeliveries promotes waiting_dependency deliveries whose
// dependencies are now all done.
func (a *App) activateDependentDeliveries(ctx context.Context, doneTaskID string) error {
	dependentIDs, err := sqlite.ListDependentTasks(ctx, a.DB, doneTaskID)
	if err != nil {
		return err
	}
	for _, taskID := range dependentIDs {
		ready, err := a.dependenciesSatisfied(ctx, a.DB, taskID)
		if err != nil {
			return err
		}
		if !ready {
			continue
		}
		task, err := sqlite.GetTask(ctx, a.DB, taskID)
		if err != nil {
			return err
		}
		if task.Status != domain.TaskTodo {
			continue
		}
		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := sqlite.ActivateWaitingDeliveries(ctx, tx, task.ID, task.OwnerID); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, ResourceID: task.ID, Revision: task.Revision})
		if a.Scheduler != nil {
			a.Scheduler.Wake(task.OwnerID)
		}
	}
	a.Hub.Publish()
	return nil
}

// ListTasks projects a team's tasks; the caller decides the owner scope.
func (a *App) ListTasks(ctx context.Context, principal auth.Principal, teamID, ownerID string, limit int) ([]domain.Task, error) {
	if principal.Kind != auth.KindUser && principal.Kind != auth.KindBot {
		return nil, domain.Unauthorized("error.unauthorized.principal_required")
	}
	return sqlite.ListTasks(ctx, a.DB, teamID, ownerID, limit)
}

// GetTaskDetail returns a task with its evidence and transfer history.
func (a *App) GetTaskDetail(ctx context.Context, principal auth.Principal, taskID string) (map[string]any, error) {
	if principal.Kind != auth.KindUser && principal.Kind != auth.KindBot {
		return nil, domain.Unauthorized("error.unauthorized.principal_required")
	}
	task, err := sqlite.GetTask(ctx, a.DB, taskID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	evidence, err := sqlite.ListEvidence(ctx, a.DB, taskID)
	if err != nil {
		return nil, err
	}
	transfers, err := sqlite.ListTransfersForTask(ctx, a.DB, taskID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"task": task, "evidence": evidence, "transfers": transfers}, nil
}

// recordTaskUpdated is a small helper for event publication.
func (a *App) recordTaskUpdated(ctx context.Context, task domain.Task) {
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, ResourceID: task.ID, Revision: task.Revision})
	a.Hub.Publish()
}

var _ = time.Now
