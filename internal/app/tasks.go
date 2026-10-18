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
	if principal.Write == nil && in.RequestID != "" {
		var err error
		principal, err = WithWriteRequest(principal, "task.create", in.RequestID, in)
		if err != nil {
			return domain.Task{}, err
		}
	}
	cached, unlock, writeErr := beginWrite[domain.Task](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Task{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
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
	// The wire contract promises arrays, never null: a Go nil slice would
	// serialize as null and break clients that read .length.
	deps := in.DependencyIDs
	if deps == nil {
		deps = []string{}
	}
	task := domain.Task{
		ID: domain.NewID(), TeamID: in.TeamID, OwnerID: in.OwnerID,
		ParentID: in.ParentID, Title: title, Description: in.Description,
		AcceptanceCriteria: in.AcceptanceCriteria, Status: domain.TaskTodo,
		DependencyIDs: deps, Revision: 1, CreatedBy: createdBy,
	}

	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	freshTeam, _, err := sqlite.GetTeam(ctx, tx, in.TeamID)
	if err != nil {
		return domain.Task{}, err
	}
	if freshTeam.Status != domain.TeamStatusActive {
		return domain.Task{}, domain.Conflict("error.team.not_active", nil)
	}
	if err := a.assertSameTeamOwner(ctx, tx, in.TeamID, in.OwnerID); err != nil {
		return domain.Task{}, err
	}
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
	chainID := ""
	if principal.Kind == auth.KindBot {
		chainID = chainOfRunLocked(ctx, tx, principal.BotID)
	}
	if err := a.enqueueAssignmentLocked(ctx, tx, task, ready, chainID, principal.BotID); err != nil {
		return domain.Task{}, err
	}
	task, err = sqlite.GetTask(ctx, tx, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, task); err != nil {
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

// dependenciesSatisfied reports whether no dependency of the task still
// blocks it: each is done, or cancelled and so never going to be.
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
		if blocksDependents(depTask) {
			return false, nil
		}
	}
	return true, nil
}

// blocksDependents reports whether a task still holds back the tasks that
// depend on it. A cancelled task will never be done, so waiting for it would
// park its dependents forever; the leader is told instead and decides.
func blocksDependents(task domain.Task) bool {
	return task.Status != domain.TaskDone && task.Status != domain.TaskCancelled
}

// enqueueAssignmentLocked creates the one task-assignment delivery for an
// owner inside the caller's transaction. The status reflects whether the
// dependencies allow the bot to start. chainID carries the causal chain this
// assignment belongs to: a hand-over continues the chain that produced the
// work, while a fresh user-directed assignment carries none.
// sourceBotID is the member who handed the task over, or "" when the user did;
// the assignment then reads as a message from that member in both
// conversations.
func (a *App) enqueueAssignmentLocked(ctx context.Context, tx *sql.Tx, task domain.Task, ready bool, chainID, sourceBotID string) error {
	status := domain.DeliveryQueued
	if !ready {
		status = domain.DeliveryWaitingDependency
	}
	text := fmt.Sprintf("你被指派了任务：%s\n任务 ID：%s\n详细要求：%s\n完成标准：%s", task.Title, task.ID, task.Description, task.AcceptanceCriteria)
	// The task block is for the conversation's task card; the engine reads
	// only the text block.
	card, err := json.Marshal(map[string]any{"taskId": task.ID, "title": task.Title, "description": task.Description, "acceptanceCriteria": task.AcceptanceCriteria, "ownerId": task.OwnerID})
	if err != nil {
		return err
	}
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}, {Type: domain.BlockTypeTask, Text: string(card)}})
	if err != nil {
		return err
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: task.TeamID, BotID: task.OwnerID,
		Kind: domain.MessageKindSystem, Blocks: blocks, DeliveryStatus: status, RunID: task.ID,
		SourceBotID: sourceBotID,
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return err
	}
	return sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: domain.NewID(), MessageID: message.ID, TeamID: task.TeamID,
		TargetBotID: task.OwnerID, Status: status, ChainID: chainID, TaskID: task.ID, Kind: "task_assignment",
	})
}

// AssignTask changes the owner through the same safety rules as a transfer:
// the caller must be the captain or the user, and a bot owner's active run on
// the task must have ended.
func (a *App) AssignTask(ctx context.Context, principal auth.Principal, taskID, newOwnerID string, expectedRevision int64) (domain.Task, error) {
	cached, unlock, writeErr := beginWrite[domain.Task](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Task{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
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
	if task.Status == domain.TaskDone || task.Status == domain.TaskCancelled {
		return domain.Task{}, domain.Conflict("error.task.terminal", nil)
	}
	if err := a.assertSameTeamOwner(ctx, a.DB, task.TeamID, newOwnerID); err != nil {
		return domain.Task{}, err
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	// Re-read inside the transaction: two concurrent assignments must not
	// both succeed against the same revision (review B8).
	fresh, err := sqlite.GetTask(ctx, tx, taskID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	if expectedRevision > 0 && fresh.Revision != expectedRevision {
		return domain.Task{}, domain.RevisionConflict("error.task.revision_conflict", map[string]any{"expected": expectedRevision, "actual": fresh.Revision})
	}
	if fresh.OwnerID == newOwnerID {
		if err := a.saveWriteResult(ctx, tx, principal, fresh); err != nil {
			return domain.Task{}, err
		}
		if err := tx.Commit(); err != nil {
			return domain.Task{}, err
		}
		return fresh, nil
	}
	if fresh.Status == domain.TaskDone || fresh.Status == domain.TaskCancelled {
		return domain.Task{}, domain.Conflict("error.task.terminal", nil)
	}
	if err := a.assertSameTeamOwner(ctx, tx, fresh.TeamID, newOwnerID); err != nil {
		return domain.Task{}, err
	}
	if err := assertOwnerRunEndedOn(ctx, tx, fresh.OwnerID, fresh.ID); err != nil {
		return domain.Task{}, err
	}
	task = fresh
	// Cancel the old owner's undelivered assignment, then give the new owner
	// exactly one.
	if err := a.cancelUndeliveriesForTaskLocked(ctx, tx, task.ID, fresh.OwnerID); err != nil {
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
	if err := a.enqueueAssignmentLocked(ctx, tx, task, ready, "", principal.BotID); err != nil {
		return domain.Task{}, err
	}
	task, err = sqlite.GetTask(ctx, tx, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, task); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, BotID: principal.BotID, ResourceID: task.ID, Revision: task.Revision,
		Payload: mustJSON(map[string]any{"change": "assigned", "ownerId": newOwnerID})})
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
			if err := sqlite.SetMessageDeliveryStatus(ctx, tx, delivery.MessageID, domain.DeliveryCancelled); err != nil {
				return err
			}
		}
	}
	return nil
}

// assertOwnerRunEnded refuses to move a task whose current owner may still
// be writing files for it.
func (a *App) assertOwnerRunEnded(ctx context.Context, ownerID, taskID string) error {
	return assertOwnerRunEndedOn(ctx, a.DB, ownerID, taskID)
}

func assertOwnerRunEndedOn(ctx context.Context, q sqlite.Queryer, ownerID, taskID string) error {
	run, err := sqlite.FindActiveRun(ctx, q, ownerID)
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
	return a.updateTaskStatus(ctx, principal, taskID, status, blockedReason, expectedRevision, nil, false)
}

func (a *App) updateTaskStatus(ctx context.Context, principal auth.Principal, taskID, status, blockedReason string, expectedRevision int64, evidenceIDs []string, requireEvidenceIDs bool) (domain.Task, error) {
	cached, unlock, writeErr := beginWrite[domain.Task](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Task{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	task, err := sqlite.GetTask(ctx, tx, taskID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	if err := a.authorizeTaskWrite(ctx, tx, principal, task, status); err != nil {
		return domain.Task{}, err
	}
	if status == domain.TaskDone {
		return domain.Task{}, domain.InvalidRequest("error.task.done_needs_confirmation", nil)
	}
	if !validTaskTransition(task.Status, status) {
		return domain.Task{}, domain.InvalidRequest("error.task.invalid_transition", map[string]any{"from": task.Status, "to": status})
	}
	if status == domain.TaskReview {
		has, err := sqlite.HasEvidence(ctx, tx, task.ID)
		if err != nil {
			return domain.Task{}, err
		}
		if !has {
			return domain.Task{}, domain.InvalidRequest("error.task.review_needs_evidence", nil)
		}
		if requireEvidenceIDs {
			if len(evidenceIDs) == 0 {
				return domain.Task{}, domain.InvalidRequest("error.task.review_needs_evidence", nil)
			}
			evidence, err := sqlite.ListEvidence(ctx, tx, task.ID)
			if err != nil {
				return domain.Task{}, err
			}
			have := map[string]bool{}
			for _, item := range evidence {
				have[item.ID] = true
			}
			for _, id := range evidenceIDs {
				if !have[id] {
					return domain.Task{}, domain.InvalidRequest("error.task.evidence_unknown", map[string]any{"evidenceId": id})
				}
			}
		}
	}
	updated, err := sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, status, blockedReason, expectedRevision)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	var released []domain.Task
	leaderToWake := ""
	if status == domain.TaskCancelled {
		if err := a.cancelUndeliveriesForTaskLocked(ctx, tx, task.ID, ""); err != nil {
			return domain.Task{}, err
		}
		if released, err = a.activateDependentDeliveriesLocked(ctx, tx, task.ID); err != nil {
			return domain.Task{}, err
		}
		if leaderToWake, err = a.enqueueDependencyCancelledNoticeLocked(ctx, tx, principal, updated, released); err != nil {
			return domain.Task{}, err
		}
	}
	if status == domain.TaskReview && task.Status != domain.TaskReview {
		leaderToWake, err = a.enqueueReviewNoticeLocked(ctx, tx, principal, updated)
		if err != nil {
			return domain.Task{}, err
		}
	}
	if err := a.saveWriteResult(ctx, tx, principal, updated); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	if leaderToWake != "" && a.Scheduler != nil {
		a.Scheduler.Wake(leaderToWake)
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, BotID: principal.BotID, ResourceID: task.ID, Revision: updated.Revision,
		Payload: mustJSON(map[string]any{"change": "status", "status": updated.Status})})
	a.Hub.Publish()
	for _, dependent := range released {
		a.recordTaskUpdated(ctx, dependent)
	}
	if a.Scheduler != nil {
		a.Scheduler.Wake(task.OwnerID)
		for _, dependent := range released {
			a.Scheduler.Wake(dependent.OwnerID)
		}
	}
	return updated, nil
}

// enqueueDependencyCancelledNoticeLocked tells the leader that a cancelled
// task had tasks waiting on it, which of them now reached their owners and
// which still wait for something else, as a message that wakes it. It reports
// the leader to wake, or "" when nothing depended on the task or the leader
// cancelled it itself.
func (a *App) enqueueDependencyCancelledNoticeLocked(ctx context.Context, tx *sql.Tx, principal auth.Principal, cancelled domain.Task, released []domain.Task) (string, error) {
	team, _, err := sqlite.GetTeam(ctx, tx, cancelled.TeamID)
	if err != nil {
		return "", err
	}
	if team.LeaderID == "" || (principal.Kind == auth.KindBot && principal.BotID == team.LeaderID) {
		return "", nil
	}
	ids, err := sqlite.ListDependentTasks(ctx, tx, cancelled.ID)
	if err != nil {
		return "", err
	}
	releasedIDs := map[string]bool{}
	for _, task := range released {
		releasedIDs[task.ID] = true
	}
	var started, waiting []string
	for _, id := range ids {
		dependent, err := sqlite.GetTask(ctx, tx, id)
		if err != nil {
			return "", err
		}
		if !blocksDependents(dependent) {
			continue
		}
		owner, _ := sqlite.GetBot(ctx, tx, dependent.OwnerID)
		line := fmt.Sprintf("「%s」（任务 ID：%s，负责人：%s）", dependent.Title, dependent.ID, owner.Name)
		if releasedIDs[id] {
			started = append(started, line)
		} else {
			waiting = append(waiting, line)
		}
	}
	if len(started) == 0 && len(waiting) == 0 {
		return "", nil
	}
	text := fmt.Sprintf("任务「%s」（任务 ID：%s）已被取消，依赖它的任务不再等它。", cancelled.Title, cancelled.ID)
	if len(started) > 0 {
		text += "\n已送到负责人手上：" + strings.Join(started, "、")
	}
	if len(waiting) > 0 {
		text += "\n仍在等其他前置任务：" + strings.Join(waiting, "、")
	}
	text += "\n请判断这些任务没有被取消的前置任务是否还成立：需要调整就告诉负责人，超出你能决定的就问用户。"
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return "", err
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: cancelled.TeamID, BotID: team.LeaderID,
		Kind: domain.MessageKindSystem, Blocks: blocks, DeliveryStatus: domain.DeliveryQueued, RunID: cancelled.ID,
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return "", err
	}
	chain := ""
	if principal.Kind == auth.KindBot {
		chain = chainOfRunLocked(ctx, tx, principal.BotID)
	}
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: domain.NewID(), MessageID: message.ID, TeamID: cancelled.TeamID, TargetBotID: team.LeaderID,
		Status: domain.DeliveryQueued, ChainID: chain, SourceRunID: principal.RunID, Kind: "dependency_cancelled",
	}); err != nil {
		return "", err
	}
	return team.LeaderID, nil
}

func validTaskTransition(from, to string) bool {
	switch from {
	case domain.TaskTodo:
		return to == domain.TaskTodo || to == domain.TaskDoing || to == domain.TaskCancelled
	case domain.TaskDoing:
		return to == domain.TaskDoing || to == domain.TaskReview || to == domain.TaskCancelled
	case domain.TaskReview:
		return to == domain.TaskReview || to == domain.TaskDoing || to == domain.TaskCancelled
	default:
		return false
	}
}

// authorizeTaskWrite enforces the background permission rules; the UI never
// decides.
func (a *App) authorizeTaskWrite(ctx context.Context, q sqlite.Queryer, principal auth.Principal, task domain.Task, newStatus string) error {
	switch principal.Kind {
	case auth.KindUser:
		return nil
	case auth.KindBot:
		team, _, err := sqlite.GetTeam(ctx, q, task.TeamID)
		isLeader := team.LeaderID == principal.BotID
		if err != nil {
			return err
		}
		if isLeader {
			return nil
		}
		if newStatus == domain.TaskCancelled {
			return domain.Forbidden("error.task.cancel_captain_only", nil)
		}
		if principal.TeamID != task.TeamID || principal.BotID != task.OwnerID {
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
	cached, unlock, writeErr := beginWrite[domain.Evidence](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Evidence{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
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
	if artifactRefs == nil {
		artifactRefs = []string{} // arrays on the wire, never null
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
	fresh, err := sqlite.GetTask(ctx, tx, taskID)
	if err != nil {
		return domain.Evidence{}, err
	}
	if principal.Kind == auth.KindBot {
		team, _, err := sqlite.GetTeam(ctx, tx, fresh.TeamID)
		if err != nil {
			return domain.Evidence{}, err
		}
		if fresh.TeamID != principal.TeamID || (fresh.OwnerID != principal.BotID && team.LeaderID != principal.BotID) {
			return domain.Evidence{}, domain.Forbidden("error.task.owner_or_captain", nil)
		}
	}
	if fresh.Status == domain.TaskCancelled || fresh.Status == domain.TaskDone {
		return domain.Evidence{}, domain.Conflict("error.task.terminal", nil)
	}
	evidence.CreatedAt = time.Now().UTC()
	if err := sqlite.InsertEvidence(ctx, tx, evidence); err != nil {
		return domain.Evidence{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, evidence); err != nil {
		return domain.Evidence{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Evidence{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, BotID: principal.BotID, ResourceID: task.ID,
		Payload: mustJSON(map[string]any{"change": "evidence"})})
	a.Hub.Publish()
	return evidence, nil
}

// ConfirmTask moves a task to done; the captain or the user confirms against
// matching evidence. A chat message never does this by itself.
func (a *App) ConfirmTask(ctx context.Context, principal auth.Principal, taskID string, evidenceIDs []string, expectedRevision int64) (domain.Task, error) {
	cached, unlock, writeErr := beginWrite[domain.Task](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Task{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	task, err := sqlite.GetTask(ctx, tx, taskID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		team, _, err := sqlite.GetTeam(ctx, tx, task.TeamID)
		isLeader := team.LeaderID == principal.BotID
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
	evidence, err := sqlite.ListEvidence(ctx, tx, task.ID)
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
	updated, err := sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, domain.TaskDone, "", expectedRevision)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	activated, err := a.activateDependentDeliveriesLocked(ctx, tx, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, updated); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	a.recordTaskUpdated(ctx, updated)
	for _, dependent := range activated {
		a.recordTaskUpdated(ctx, dependent)
		if a.Scheduler != nil {
			a.Scheduler.Wake(dependent.OwnerID)
		}
	}
	return updated, nil
}

// Completion and dependent activation commit together, so a restart cannot
// leave a done dependency with permanently parked successor deliveries.
func (a *App) activateDependentDeliveriesLocked(ctx context.Context, tx *sql.Tx, doneTaskID string) ([]domain.Task, error) {
	ids, err := sqlite.ListDependentTasks(ctx, tx, doneTaskID)
	if err != nil {
		return nil, err
	}
	var activated []domain.Task
	for _, id := range ids {
		task, err := sqlite.GetTask(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if task.Status != domain.TaskTodo {
			continue
		}
		ready, err := a.dependenciesSatisfied(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if !ready {
			continue
		}
		if err := sqlite.ActivateWaitingDeliveries(ctx, tx, id, task.OwnerID); err != nil {
			return nil, err
		}
		activated = append(activated, task)
	}
	return activated, nil
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
	// A bot may only read its own team's task: ownership rules already cover
	// writes, this closes the read path (review B6).
	if principal.Kind == auth.KindBot && task.TeamID != principal.TeamID {
		return nil, domain.Forbidden("error.message.other_team", map[string]any{"taskId": taskID})
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
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, ResourceID: task.ID, Revision: task.Revision,
		Payload: mustJSON(map[string]any{"change": "status", "status": task.Status})})
	a.Hub.Publish()
}

var _ = time.Now

// enqueueReviewNoticeLocked tells the leader that a member put a task up for
// review, as a message that wakes it: the leader is the one who confirms or
// sends it back, and a member's own report may well have been a note that
// wakes nobody. It reports the leader to wake, or "" when the leader itself
// moved the task.
func (a *App) enqueueReviewNoticeLocked(ctx context.Context, tx *sql.Tx, principal auth.Principal, task domain.Task) (string, error) {
	return a.enqueueDeliveryNoticeLocked(ctx, tx, principal, task, "")
}

// enqueueDeliveryNoticeLocked is the review notice carrying what the member
// said it delivered, so the leader can judge it without looking it up.
func (a *App) enqueueDeliveryNoticeLocked(ctx context.Context, tx *sql.Tx, principal auth.Principal, task domain.Task, summary string) (string, error) {
	if principal.Kind != auth.KindBot {
		return "", nil
	}
	team, _, err := sqlite.GetTeam(ctx, tx, task.TeamID)
	if err != nil {
		return "", err
	}
	if team.LeaderID == "" || principal.BotID == team.LeaderID {
		return "", nil
	}
	reporter, err := sqlite.GetBot(ctx, tx, principal.BotID)
	if err != nil {
		return "", err
	}
	text := fmt.Sprintf("%s 交付了任务「%s」（任务 ID：%s）。", reporter.Name, task.Title, task.ID)
	if strings.TrimSpace(summary) != "" {
		text += "\n交付说明：" + strings.TrimSpace(summary)
	}
	text += fmt.Sprintf("\n完成标准：%s\n请对照完成标准核对：通过就用 review_task 选 accept；不通过就选 reject 并写明缺什么，任务会退回给负责人。", task.AcceptanceCriteria)
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return "", err
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: task.TeamID, BotID: team.LeaderID,
		Kind: domain.MessageKindSystem, Blocks: blocks, DeliveryStatus: domain.DeliveryQueued, RunID: task.ID,
		SourceBotID: principal.BotID,
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return "", err
	}
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: domain.NewID(), MessageID: message.ID, TeamID: task.TeamID, TargetBotID: team.LeaderID,
		Status: domain.DeliveryQueued, ChainID: chainOfRunLocked(ctx, tx, principal.BotID), TaskID: task.ID,
		SourceRunID: principal.RunID, Kind: "review_notice",
	}); err != nil {
		return "", err
	}
	return team.LeaderID, nil
}
