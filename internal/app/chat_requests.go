package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

func (a *App) DecideChatRequest(ctx context.Context, p auth.Principal, id, requestID string, in ChatActionInput) (domain.ChatRequest, error) {
	if p.Kind != auth.KindUser {
		return domain.ChatRequest{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if a.Chats == nil {
		return domain.ChatRequest{}, domain.Conflict("error.chat.engine_unavailable", nil)
	}
	p, err := WithWriteRequest(p, "chat.request.reply", in.RequestID, struct {
		ChatID, ID string
		Input      ChatActionInput
	}{id, requestID, in})
	if err != nil {
		return domain.ChatRequest{}, err
	}
	cached, unlock, err := beginWrite[domain.ChatRequest](ctx, a, p)
	defer unlock()
	if err != nil {
		return domain.ChatRequest{}, err
	}
	if cached != nil {
		return sqlite.GetChatRequest(ctx, a.DB, cached.ID)
	}
	release := a.Chats.lock(id)
	req, err := sqlite.GetChatRequest(ctx, a.DB, requestID)
	if err != nil {
		release()
		return req, sqlite.NormalizeError(err)
	}
	if req.ChatID != id {
		release()
		return req, domain.Forbidden("error.forbidden.user_only", nil)
	}
	rt := a.Chats.runtime(id)
	if req.State != domain.ApprovalStatePending || rt == nil || rt.run.ID != req.RunID || req.Epoch != rt.server.Epoch() || in.ExpectedRevision != req.Revision {
		release()
		return req, domain.Conflict("error.chat.request_expired", nil)
	}
	var response map[string]any
	if req.Method == "item/tool/requestUserInput" {
		response, err = chatInputResponse(req.Params, in.Answers)
	} else {
		card := domain.Approval{Type: domain.ApprovalNativeTool, Method: req.Method, Summary: req.Summary, Options: req.Options}
		allowed := false
		for _, opt := range req.Options {
			if opt.ID == in.OptionID && !opt.Disabled {
				allowed = true
			}
		}
		if !allowed {
			release()
			return req, domain.Conflict("error.approval.option_unavailable", nil)
		}
		response, err = engineResponseFor(card, in.OptionID, in.Content)
	}
	if err != nil {
		release()
		return req, err
	}
	req.State = domain.ApprovalStateDecided
	req.Decision = in.OptionID
	if req.Method == "item/tool/requestUserInput" {
		req.Decision = "answered"
	}
	req.ResponseState = domain.ResponseQueued
	req.Revision++
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		release()
		return req, err
	}
	if err = sqlite.SaveChatRequest(ctx, tx, req); err == nil {
		err = a.saveWriteResult(ctx, tx, p, req)
	}
	if err == nil {
		err = tx.Commit()
	}
	_ = tx.Rollback()
	release()
	if err != nil {
		return req, err
	}
	replyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = rt.server.ReplyApproval(replyCtx, req.EngineID, response)
	cancel()
	release = a.Chats.lock(id)
	defer release()
	fresh, e := sqlite.GetChatRequest(context.Background(), a.DB, req.ID)
	if e != nil {
		return req, e
	}
	if fresh.State == domain.ApprovalStateDecided {
		fresh.ResponseState = domain.ResponseSent
		if err != nil {
			fresh.ResponseState = domain.ResponseUnknown
		}
		fresh.Revision++
		if e = sqlite.SaveChatRequest(context.Background(), a.DB, fresh); e != nil {
			return fresh, e
		}
	}
	if err == nil && a.Chats.runtime(id) == rt && (rt.run.Status == domain.RunWaitingApproval || rt.run.Status == domain.RunWaitingInput) {
		requests, e := sqlite.ListChatRequests(context.Background(), a.DB, id)
		if e != nil {
			return fresh, e
		}
		pending := false
		for _, v := range requests {
			if v.RunID == rt.run.ID && v.State == domain.ApprovalStatePending {
				pending = true
			}
		}
		if !pending {
			rt.run.Status = domain.RunRunning
			if e = sqlite.SaveChatRun(context.Background(), a.DB, rt.run); e != nil {
				return fresh, e
			}
		}
	}
	if err != nil && a.Chats.runtime(id) == rt && domain.ChatRunActive(rt.run.Status) {
		a.Chats.report(a.Chats.markUnknown(context.Background(), rt.run, "error.chat.unknown"))
		rt.run.Status = domain.RunUnknown
	}
	a.chatChanged(context.Background(), id, "chat.request.updated")
	return fresh, nil
}

func chatInputResponse(raw json.RawMessage, answers map[string][]string) (map[string]any, error) {
	var p struct {
		Questions []struct {
			ID string `json:"id"`
		} `json:"questions"`
	}
	if json.Unmarshal(raw, &p) != nil || len(p.Questions) == 0 || len(answers) != len(p.Questions) {
		return nil, domain.InvalidRequest("error.chat.answers_required", nil)
	}
	out := map[string]any{}
	for _, q := range p.Questions {
		values, ok := answers[q.ID]
		if !ok || len(values) == 0 || len(values) > 20 {
			return nil, domain.InvalidRequest("error.chat.answers_required", nil)
		}
		for _, v := range values {
			if strings.TrimSpace(v) == "" || len(v) > 16384 {
				return nil, domain.InvalidRequest("error.chat.answers_required", nil)
			}
		}
		out[q.ID] = map[string]any{"answers": values}
	}
	return map[string]any{"answers": out}, nil
}
