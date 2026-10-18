package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The task flow a member's model sees has three steps: it is handed a task,
// it delivers, the delivery is accepted or sent back. CoBot moves the task
// through todo → doing → review → done behind these steps; the model never
// has to know the lifecycle, the revision or which transition comes next.
// A task marked for review is decided by its reviewer member; any other is
// accepted by the leader.

// DeliverTask records what the owner produced and puts the task up for
// review in one step. A task still in todo passes through doing; a task
// already in review takes the new delivery and goes back for review. When the
// leader owns the task itself, nobody else would review it, so the delivery
// completes it.
func (a *App) DeliverTask(ctx context.Context, principal auth.Principal, taskID, summary string, artifactRefs []string) (domain.Task, error) {
	cached, unlock, writeErr := beginWrite[domain.Task](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Task{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindBot {
		return domain.Task{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return domain.Task{}, domain.InvalidRequest("error.task.delivery_summary_required", nil)
	}
	if artifactRefs == nil {
		artifactRefs = []string{}
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
	if task.TeamID != principal.TeamID || task.OwnerID != principal.BotID {
		owner, _ := sqlite.GetBot(ctx, tx, task.OwnerID)
		return domain.Task{}, domain.Forbidden("error.task.deliver_owner_only", map[string]any{"title": task.Title, "owner": owner.Name})
	}
	if task.Status == domain.TaskDone || task.Status == domain.TaskCancelled {
		return domain.Task{}, domain.Conflict("error.task.terminal", map[string]any{"status": task.Status})
	}
	ready, err := a.dependenciesSatisfied(ctx, tx, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if !ready {
		return domain.Task{}, domain.Conflict("error.task.dependencies_pending", map[string]any{"title": task.Title})
	}
	if err := sqlite.InsertEvidence(ctx, tx, domain.Evidence{
		ID: domain.NewID(), TaskID: task.ID, AuthorID: principal.BotID, Summary: summary, ArtifactRefs: artifactRefs,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		return domain.Task{}, err
	}
	team, _, err := sqlite.GetTeam(ctx, tx, task.TeamID)
	if err != nil {
		return domain.Task{}, err
	}
	var updated domain.Task
	var activated []domain.Task
	var toWake []string
	if team.LeaderID == principal.BotID {
		if updated, err = sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, domain.TaskDone, "", 0); err != nil {
			return domain.Task{}, err
		}
		if activated, err = a.activateDependentDeliveriesLocked(ctx, tx, task.ID); err != nil {
			return domain.Task{}, err
		}
		leader, err := a.reportRequirementIfFinishedLocked(ctx, tx, updated)
		if err != nil {
			return domain.Task{}, err
		}
		toWake = append(toWake, leader)
	} else {
		if updated, err = sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, domain.TaskReview, "", 0); err != nil {
			return domain.Task{}, err
		}
		reviewer, err := a.liveReviewerLocked(ctx, tx, updated)
		if err != nil {
			return domain.Task{}, err
		}
		if reviewer != "" && !rejectsExhausted(ctx, tx, updated) {
			if err := a.enqueueReviewRequestLocked(ctx, tx, principal, updated, reviewer, summary, artifactRefs); err != nil {
				return domain.Task{}, err
			}
			toWake = append(toWake, reviewer)
		} else {
			leader, err := a.enqueueDeliveryNoticeLocked(ctx, tx, principal, updated, summary)
			if err != nil {
				return domain.Task{}, err
			}
			toWake = append(toWake, leader)
		}
	}
	if err := a.saveWriteResult(ctx, tx, principal, updated); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, BotID: principal.BotID, ResourceID: task.ID, Revision: updated.Revision,
		Payload: mustJSON(map[string]any{"change": "status", "status": updated.Status, "evidence": true})})
	a.Hub.Publish()
	a.wakeAll(toWake, activated)
	for _, dependent := range activated {
		a.recordTaskUpdated(ctx, dependent)
	}
	return updated, nil
}

// ReviewTask is the verdict on a delivery, given by the task's reviewer, the
// leader or the user. Accept completes the task and releases the tasks
// waiting on it; reject sends it back to the owner with the reason, as a
// message that wakes the owner. A reviewer's send-backs are counted: past the
// configured limit the task stays in review and the leader decides.
func (a *App) ReviewTask(ctx context.Context, principal auth.Principal, taskID string, accept bool, comment string) (domain.Task, error) {
	cached, unlock, writeErr := beginWrite[domain.Task](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Task{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	comment = strings.TrimSpace(comment)
	if !accept && comment == "" {
		return domain.Task{}, domain.InvalidRequest("error.task.reject_reason_required", nil)
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
	team, _, err := sqlite.GetTeam(ctx, tx, task.TeamID)
	if err != nil {
		return domain.Task{}, err
	}
	asReviewer := false
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		if principal.TeamID != task.TeamID {
			return domain.Task{}, domain.Forbidden("error.task.captain_only", nil)
		}
		switch {
		case principal.BotID == team.LeaderID:
		case task.ReviewerID != "" && principal.BotID == task.ReviewerID:
			asReviewer = true
		default:
			return domain.Task{}, domain.Forbidden("error.task.reviewer_or_captain", map[string]any{"title": task.Title})
		}
	default:
		return domain.Task{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if task.Status != domain.TaskReview {
		return domain.Task{}, domain.InvalidRequest("error.task.review_not_pending", map[string]any{"title": task.Title, "status": task.Status})
	}
	settings, err := sqlite.GetSettings(ctx, tx)
	if err != nil {
		return domain.Task{}, err
	}
	if asReviewer && task.RejectCount > settings.ReviewRejectLimit {
		// Past the limit the leader has the task; the reviewer is done with it.
		return domain.Task{}, domain.Conflict("error.task.review_escalated", map[string]any{"title": task.Title})
	}
	if asReviewer {
		result := "reject"
		if accept {
			result = "accept"
		}
		if err := sqlite.InsertEvidence(ctx, tx, domain.Evidence{
			ID: domain.NewID(), TaskID: task.ID, AuthorID: principal.BotID, Summary: reviewSummary(accept, comment),
			ArtifactRefs: []string{}, ReviewResult: result, CreatedAt: time.Now().UTC(),
		}); err != nil {
			return domain.Task{}, err
		}
	}
	var updated domain.Task
	var activated []domain.Task
	var toWake []string
	switch {
	case accept:
		if updated, err = sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, domain.TaskDone, "", 0); err != nil {
			return domain.Task{}, err
		}
		if activated, err = a.activateDependentDeliveriesLocked(ctx, tx, task.ID); err != nil {
			return domain.Task{}, err
		}
		if asReviewer {
			if err := a.noteReviewPassedLocked(ctx, tx, principal, team, updated, comment); err != nil {
				return domain.Task{}, err
			}
		}
		leader, err := a.reportRequirementIfFinishedLocked(ctx, tx, updated)
		if err != nil {
			return domain.Task{}, err
		}
		toWake = append(toWake, leader)
	case asReviewer && task.RejectCount+1 > settings.ReviewRejectLimit:
		// One send-back too many: the task stays in review and the leader is
		// woken with every reason so far.
		if err := sqlite.SetTaskRejectCount(ctx, tx, task.ID, task.RejectCount+1); err != nil {
			return domain.Task{}, err
		}
		if updated, err = sqlite.GetTask(ctx, tx, task.ID); err != nil {
			return domain.Task{}, err
		}
		leader, err := a.enqueueReviewEscalationLocked(ctx, tx, principal, team, updated, settings.ReviewRejectLimit)
		if err != nil {
			return domain.Task{}, err
		}
		toWake = append(toWake, leader)
	default:
		count := 0 // the leader's or the user's decision starts a fresh count
		if asReviewer {
			count = task.RejectCount + 1
		}
		if err := sqlite.SetTaskRejectCount(ctx, tx, task.ID, count); err != nil {
			return domain.Task{}, err
		}
		if updated, err = sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, domain.TaskDoing, "", 0); err != nil {
			return domain.Task{}, err
		}
		text := fmt.Sprintf("任务「%s」验收未通过（任务 ID：%s）：%s\n改好后再用 deliver_task 交付。", task.Title, task.ID, comment)
		if asReviewer {
			text = fmt.Sprintf("任务「%s」评审未通过（任务 ID：%s，第 %d 次打回）：%s\n改好后再用 deliver_task 交付，会再送评审。", task.Title, task.ID, count, comment)
		}
		owner, err := a.enqueueRejectionLocked(ctx, tx, principal, team, updated, text)
		if err != nil {
			return domain.Task{}, err
		}
		toWake = append(toWake, owner)
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
	}
	a.wakeAll(toWake, activated)
	return updated, nil
}

// wakeAll wakes the members a change reached and the owners of the tasks it
// released.
func (a *App) wakeAll(botIDs []string, released []domain.Task) {
	if a.Scheduler == nil {
		return
	}
	for _, id := range botIDs {
		if id != "" {
			a.Scheduler.Wake(id)
		}
	}
	for _, task := range released {
		a.Scheduler.Wake(task.OwnerID)
	}
}

// enqueueRejectionLocked tells the owner its delivery was sent back and why,
// as a message from the reviewer or the leader that wakes it. It reports the
// owner to wake.
func (a *App) enqueueRejectionLocked(ctx context.Context, tx *sql.Tx, principal auth.Principal, team domain.Team, task domain.Task, text string) (string, error) {
	if task.OwnerID == "" || task.OwnerID == team.LeaderID {
		return "", nil
	}
	source := ""
	chain := ""
	if principal.Kind == auth.KindBot {
		source = principal.BotID
		chain = chainOfRunLocked(ctx, tx, principal.BotID)
	}
	if err := enqueueSystemMessageLocked(ctx, tx, systemMessage{
		teamID: task.TeamID, target: task.OwnerID, source: source, text: text, about: task.ID,
		taskID: task.ID, chainID: chain, sourceRun: principal.RunID, kind: "review_result",
	}); err != nil {
		return "", err
	}
	return task.OwnerID, nil
}

// startAssignedTasks moves the tasks a member was just handed from todo to
// doing once its turn is accepted: picking the work up is what starting it
// means, and the model is never asked to say so.
func (a *App) startAssignedTasks(ctx context.Context, runID, botID string) {
	rows, err := a.DB.QueryContext(ctx, `SELECT DISTINCT d.task_id FROM deliveries d JOIN tasks t ON t.id = d.task_id
		WHERE d.run_id = ? AND d.task_id != '' AND t.owner_id = ? AND t.status = ?`, runID, botID, domain.TaskTodo)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		task, err := sqlite.GetTask(ctx, tx, id)
		if err != nil || task.Status != domain.TaskTodo {
			tx.Rollback()
			continue
		}
		updated, err := sqlite.UpdateTaskStatusTx(ctx, tx, id, domain.TaskDoing, "", 0)
		if err != nil || tx.Commit() != nil {
			tx.Rollback()
			continue
		}
		a.recordTaskUpdated(ctx, updated)
	}
}

// TaskLine is one task as a member sees it in a list: who has it, where it
// stands, and what it still waits for.
type TaskLine struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Owner     string   `json:"owner"`
	OwnerID   string   `json:"ownerId"`
	Status    string   `json:"status"`
	WaitingOn []string `json:"waitingOn,omitempty"`
}

// ViewTasks answers a member's look at the work: one task in full (with its
// deliveries) when taskID is given, otherwise every task of the team as one
// line each.
func (a *App) ViewTasks(ctx context.Context, principal auth.Principal, taskID string) (map[string]any, error) {
	if principal.Kind != auth.KindBot && principal.Kind != auth.KindUser {
		return nil, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if taskID != "" {
		return a.GetTaskDetail(ctx, principal, taskID)
	}
	lines, err := a.taskLines(ctx, principal.TeamID, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{"tasks": lines}, nil
}

// taskLines lists a team's tasks (or one owner's) with owner names and the
// unfinished dependencies each still waits for.
func (a *App) taskLines(ctx context.Context, teamID, ownerID string) ([]TaskLine, error) {
	tasks, err := sqlite.ListTasks(ctx, a.DB, teamID, ownerID, 100)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	if bots, err := sqlite.ListBots(ctx, a.DB, teamID); err == nil {
		for _, bot := range bots {
			names[bot.ID] = bot.Name
		}
	}
	byID := map[string]domain.Task{}
	for _, task := range tasks {
		byID[task.ID] = task
	}
	lines := make([]TaskLine, 0, len(tasks))
	for _, task := range tasks {
		line := TaskLine{ID: task.ID, Title: task.Title, Owner: names[task.OwnerID], OwnerID: task.OwnerID, Status: task.Status}
		for _, dep := range task.DependencyIDs {
			depTask, ok := byID[dep]
			if !ok {
				if depTask, err = sqlite.GetTask(ctx, a.DB, dep); err != nil {
					continue
				}
			}
			if blocksDependents(depTask) {
				line.WaitingOn = append(line.WaitingOn, depTask.Title)
			}
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// WaitingTasksOf lists the titles of a member's tasks that still wait for
// another task: a colleague messaging it should know its work has not
// reached it yet.
func (a *App) WaitingTasksOf(ctx context.Context, teamID, botID string) []string {
	lines, err := a.taskLines(ctx, teamID, botID)
	if err != nil {
		return nil
	}
	var titles []string
	for _, line := range lines {
		if len(line.WaitingOn) > 0 && line.Status == domain.TaskTodo {
			titles = append(titles, fmt.Sprintf("「%s」要等「%s」完成", line.Title, strings.Join(line.WaitingOn, "」「")))
		}
	}
	return titles
}
