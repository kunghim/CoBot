package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The task flow a member's model sees has three steps: it is handed a task,
// it delivers, the leader accepts or sends it back. CoBot moves the task
// through todo → doing → review → done behind these steps; the model never
// has to know the lifecycle, the revision or which transition comes next.

// DeliverTask records what the owner produced and puts the task up for the
// leader's review in one step. A task still in todo passes through doing;
// a task already in review takes the new delivery and goes back to the
// leader. When the leader owns the task itself, nobody else would review it,
// so the delivery completes it.
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
	leaderToWake := ""
	if team.LeaderID == principal.BotID {
		if updated, err = sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, domain.TaskDone, "", 0); err != nil {
			return domain.Task{}, err
		}
		if activated, err = a.activateDependentDeliveriesLocked(ctx, tx, task.ID); err != nil {
			return domain.Task{}, err
		}
	} else {
		if updated, err = sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, domain.TaskReview, "", 0); err != nil {
			return domain.Task{}, err
		}
		if leaderToWake, err = a.enqueueDeliveryNoticeLocked(ctx, tx, principal, updated, summary); err != nil {
			return domain.Task{}, err
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
	if a.Scheduler != nil {
		if leaderToWake != "" {
			a.Scheduler.Wake(leaderToWake)
		}
		for _, dependent := range activated {
			a.Scheduler.Wake(dependent.OwnerID)
		}
	}
	for _, dependent := range activated {
		a.recordTaskUpdated(ctx, dependent)
	}
	return updated, nil
}

// ReviewTask is the leader's answer to a delivery: accept completes the task
// and releases the tasks waiting on it; reject sends it back to the owner
// with the reason, as a message that wakes the owner.
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
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		if principal.BotID != team.LeaderID || principal.TeamID != task.TeamID {
			return domain.Task{}, domain.Forbidden("error.task.captain_only", nil)
		}
	default:
		return domain.Task{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if task.Status != domain.TaskReview {
		return domain.Task{}, domain.InvalidRequest("error.task.review_not_pending", map[string]any{"title": task.Title, "status": task.Status})
	}
	var updated domain.Task
	var activated []domain.Task
	ownerToWake := ""
	if accept {
		if updated, err = sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, domain.TaskDone, "", 0); err != nil {
			return domain.Task{}, err
		}
		if activated, err = a.activateDependentDeliveriesLocked(ctx, tx, task.ID); err != nil {
			return domain.Task{}, err
		}
	} else {
		if updated, err = sqlite.UpdateTaskStatusTx(ctx, tx, task.ID, domain.TaskDoing, "", 0); err != nil {
			return domain.Task{}, err
		}
		if ownerToWake, err = a.enqueueRejectionLocked(ctx, tx, principal, team, updated, comment); err != nil {
			return domain.Task{}, err
		}
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
	if a.Scheduler != nil {
		if ownerToWake != "" {
			a.Scheduler.Wake(ownerToWake)
		}
		for _, dependent := range activated {
			a.Scheduler.Wake(dependent.OwnerID)
		}
	}
	return updated, nil
}

// enqueueRejectionLocked tells the owner its delivery was sent back and why,
// as a message from the leader that wakes it. It reports the owner to wake.
func (a *App) enqueueRejectionLocked(ctx context.Context, tx *sql.Tx, principal auth.Principal, team domain.Team, task domain.Task, comment string) (string, error) {
	if task.OwnerID == "" || task.OwnerID == team.LeaderID {
		return "", nil
	}
	text := fmt.Sprintf("任务「%s」验收未通过（任务 ID：%s）：%s\n改好后再用 deliver_task 交付。", task.Title, task.ID, comment)
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return "", err
	}
	source := ""
	if principal.Kind == auth.KindBot {
		source = principal.BotID
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: task.TeamID, BotID: task.OwnerID,
		Kind: domain.MessageKindSystem, Blocks: blocks, DeliveryStatus: domain.DeliveryQueued, RunID: task.ID,
		SourceBotID: source,
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return "", err
	}
	chain := ""
	if principal.Kind == auth.KindBot {
		chain = chainOfRunLocked(ctx, tx, principal.BotID)
	}
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: domain.NewID(), MessageID: message.ID, TeamID: task.TeamID, TargetBotID: task.OwnerID,
		Status: domain.DeliveryQueued, ChainID: chain, TaskID: task.ID,
		SourceRunID: principal.RunID, Kind: "review_result",
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
			if depTask.Status != domain.TaskDone {
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
