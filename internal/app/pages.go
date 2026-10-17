package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

type PageRequest struct {
	Kind, TeamID, BotID, ViewBotID, State, PeerBotID, Cursor string
	Limit                                                    int
}

// ListPage is the user projection, separate from MCP's bounded context reads.
// Scope is resolved against actual membership before applying SQL filters.
func (a *App) ListPage(ctx context.Context, p auth.Principal, in PageRequest) (map[string]any, error) {
	if p.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	// Loading the team list re-checks every team folder first (outside the
	// read transaction: the pool is one connection), so the list is honest
	// about moved or returned projects without a background timer.
	if in.Kind == "teams" && in.Cursor == "" {
		a.CheckTeamDirectories(ctx)
	}
	tx, err := a.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := sqlite.Queryer(tx)
	if in.BotID != "" {
		bot, err := sqlite.GetBot(ctx, q, in.BotID)
		if err != nil {
			return nil, sqlite.NormalizeError(err)
		}
		if in.TeamID != "" && in.TeamID != bot.TeamID {
			return nil, domain.InvalidRequest("error.scope.other_team", nil)
		}
		in.TeamID = bot.TeamID
	}
	if in.Kind != "teams" {
		if _, _, err := sqlite.GetTeam(ctx, q, in.TeamID); err != nil {
			return nil, sqlite.NormalizeError(err)
		}
	}
	member := ""
	if in.ViewBotID != "" {
		bot, err := sqlite.GetBot(ctx, q, in.ViewBotID)
		if err != nil {
			return nil, sqlite.NormalizeError(err)
		}
		if bot.TeamID != in.TeamID {
			return nil, domain.InvalidRequest("error.scope.other_team", nil)
		}
		if !bot.IsLeader {
			member = bot.ID
		}
	}
	scope := strings.Join([]string{in.Kind, in.TeamID, in.BotID, member, in.State, in.PeerBotID}, "/")
	from, idCol, sortCol := "", "id", "created_at"
	args := []any{}
	ascending := false
	switch in.Kind {
	case "teams":
		from = "FROM teams WHERE status != ?"
		args = []any{domain.TeamStatusDeleted}
	case "tasks":
		from = "FROM tasks WHERE team_id = ?"
		args = []any{in.TeamID}
		if member != "" {
			from += " AND owner_id = ?"
			args = append(args, member)
		}
	case "questions":
		from = "FROM questions WHERE team_id = ?"
		args = []any{in.TeamID}
		if in.State != "" {
			from += " AND status = ?"
			args = append(args, in.State)
		}
		if member != "" {
			from += " AND requester_bot_id = ?"
			args = append(args, member)
		}
	case "approvals":
		idCol, sortCol = "a.id", "a.created_at"
		if in.State == "history" {
			from = "FROM approvals a JOIN approval_decisions d ON d.approval_id=a.id WHERE a.team_id = ?"
			sortCol = "d.decided_at"
		} else if in.State == "pending" || in.State == "" {
			from = "FROM approvals a WHERE a.team_id = ? AND a.request_state = 'pending'"
			ascending = true
		} else {
			return nil, domain.InvalidRequest("error.invalid_request", nil)
		}
		args = []any{in.TeamID}
		if member != "" {
			from += " AND a.requester_bot_id = ?"
			args = append(args, member)
		}
	case "messages":
		from = "FROM messages WHERE bot_id = ? AND kind != ?"
		args = []any{in.BotID, domain.MessageKindCollaboration}
	case "collaboration":
		from = "FROM messages WHERE kind = ? AND (source_bot_id = ? OR bot_id = ?)"
		args = []any{domain.MessageKindCollaboration, in.BotID, in.BotID}
		if in.PeerBotID != "" {
			peer, err := sqlite.GetBot(ctx, q, in.PeerBotID)
			if err != nil || peer.TeamID != in.TeamID {
				return nil, domain.InvalidRequest("error.scope.other_team", nil)
			}
			from += " AND (source_bot_id = ? OR bot_id = ?)"
			args = append(args, in.PeerBotID, in.PeerBotID)
		}
	case "memories":
		from = "FROM memories WHERE bot_id = ? AND team_id = ? AND deleted_at = ''"
		args = []any{in.BotID, in.TeamID}
	case "activity":
		from = "FROM events e WHERE e.team_id = ?"
		idCol, sortCol = "e.id", "e.seq"
		args = []any{in.TeamID}
		if member != "" {
			from += " AND (e.bot_id = ? OR e.resource_id = ? OR e.resource_id IN (SELECT id FROM tasks WHERE team_id = ? AND owner_id = ?))"
			args = append(args, member, member, in.TeamID, member)
		}
	default:
		return nil, domain.InvalidRequest("error.invalid_request", nil)
	}
	ids, next, err := sqlite.PageIDs(ctx, q, from, idCol, sortCol, scope, in.Cursor, args, in.Limit, ascending)
	if err != nil {
		return nil, err
	}
	items := make([]any, 0, len(ids))
	for _, id := range ids {
		var value any
		switch in.Kind {
		case "teams":
			team, _, e := sqlite.GetTeam(ctx, q, id)
			err = e
			if err == nil {
				bots, e := sqlite.ListBots(ctx, q, id)
				err = e
				value = TeamWithBots{Team: team, Bots: bots}
			}
		case "tasks":
			value, err = sqlite.GetTask(ctx, q, id)
		case "questions":
			value, err = sqlite.GetQuestion(ctx, q, id)
		case "messages", "collaboration":
			value, err = sqlite.GetMessage(ctx, q, id)
		case "memories":
			value, err = sqlite.GetMemory(ctx, q, id)
		case "approvals":
			card, e := sqlite.GetApproval(ctx, q, id)
			err = e
			if err == nil && card.RequestState == domain.ApprovalStatePending {
				projectNativeFileOptions(&card)
				if card.Type == domain.ApprovalRecovery {
					a.projectRecoveryOptionsOn(ctx, q, &card)
				}
				if card.Type == domain.ApprovalMerge {
					if _, e := a.validateMergeDecision(ctx, q, card); e != nil {
						for i := range card.Options {
							if card.Options[i].ID == "approve" {
								card.Options[i].Disabled = true
								card.Options[i].DisabledReasonKey = domain.AsError(e).MessageKey
							}
						}
					}
				}
			}
			value = card
		case "activity":
			var event domain.Event
			var payload, created string
			err = q.QueryRowContext(ctx, `SELECT seq,id,instance_id,type,team_id,bot_id,resource_id,revision,payload,created_at FROM events WHERE id=?`, id).Scan(&event.Seq, &event.ID, &event.InstanceID, &event.Type, &event.TeamID, &event.BotID, &event.ResourceID, &event.Revision, &payload, &created)
			event.Payload = json.RawMessage(payload)
			event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			value = event
		}
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	result := map[string]any{"items": items, "nextCursor": next}
	if in.Kind == "messages" || in.Kind == "collaboration" {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
	if in.Kind == "approvals" && in.State != "history" {
		total, e := sqlite.CountPendingApprovals(ctx, q, in.TeamID, member)
		if e != nil {
			return nil, e
		}
		result["total"] = total
	}
	if in.Kind == "messages" {
		delete(result, "items")
		result["messages"] = items
		bot, e := sqlite.GetBot(ctx, q, in.BotID)
		if e != nil {
			return nil, e
		}
		result["bot"] = bot
		queued, e := sqlite.CountQueuedDeliveries(ctx, q, in.BotID)
		if e != nil {
			return nil, e
		}
		result["queuedCount"] = queued
		run, e := sqlite.FindActiveRun(ctx, q, in.BotID)
		if errors.Is(e, sqlite.ErrNotFound) {
			result["activeRun"] = nil
		} else if e != nil {
			return nil, e
		} else {
			result["activeRun"] = run
		}
		// The runs whose process belongs between this page's messages: from
		// its oldest message (open when it is the last page) up to the
		// anchor of the newer page (open on the newest page).
		from := ""
		if next != "" && len(items) > 0 {
			from = items[0].(domain.Message).CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		runs, e := sqlite.ListRunsWithSteps(ctx, q, in.BotID, from, sqlite.CursorValue(in.Cursor))
		if e != nil {
			return nil, e
		}
		if runs == nil {
			runs = []domain.RunWithSteps{}
		}
		result["runs"] = runs
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
