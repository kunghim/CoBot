package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

type ChatSendInput struct {
	RequestID       string `json:"requestId"`
	Text            string `json:"text"`
	ProviderID      string `json:"providerId,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}
type ChatActionInput struct {
	SectionID        string              `json:"sectionId,omitempty"`
	BeforeID         string              `json:"beforeId,omitempty"`
	RequestID        string              `json:"requestId"`
	Title            string              `json:"title,omitempty"`
	OptionID         string              `json:"optionId,omitempty"`
	ExpectedRevision int64               `json:"expectedRevision,omitempty"`
	Answers          map[string][]string `json:"answers,omitempty"`
}

func (a *App) ListChats(ctx context.Context, p auth.Principal, cursor string) (domain.ChatList, error) {
	if p.Kind != auth.KindUser {
		return domain.ChatList{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	return sqlite.ListOrganizedChats(ctx, a.DB, cursor, domain.ChatListFilter{})
}
func (a *App) ChatPage(ctx context.Context, p auth.Principal, id, cursor string) (domain.ChatPage, error) {
	if p.Kind != auth.KindUser {
		return domain.ChatPage{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	v, e := sqlite.GetChatPage(ctx, a.DB, id, cursor)
	return v, sqlite.NormalizeError(e)
}

func (a *App) chatChoice(ctx context.Context, providerID, model string) (ModelChoice, error) {
	if providerID == "" {
		choice, err := a.MemberModel(ctx, "", "")
		if err != nil {
			return choice, err
		}
		if model != "" {
			choice.Model = model
		}
		providerID, model = choice.ProviderID, choice.Model
	}
	providers, err := sqlite.ListProviders(ctx, a.DB)
	if err != nil {
		return ModelChoice{}, err
	}
	for _, p := range providers {
		if p.ID == providerID || (providerID == "" && p.AuthMode == domain.ProviderAuthChatGPT) {
			if !p.Enabled {
				return ModelChoice{}, domain.InvalidRequest("error.provider.disabled", nil)
			}
			choice := choiceFor(p, model, "")
			if model != "" && choice.Model != model {
				return ModelChoice{}, domain.InvalidRequest("error.provider.model_not_kept", nil)
			}
			if choice.Model == "" && p.AuthMode != domain.ProviderAuthChatGPT {
				return ModelChoice{}, domain.InvalidRequest("error.provider.model_required", nil)
			}
			return choice, nil
		}
	}
	return ModelChoice{}, domain.InvalidRequest("error.provider.not_found", nil)
}

func (a *App) SendChat(ctx context.Context, p auth.Principal, id string, in ChatSendInput) (domain.ChatPage, error) {
	if p.Kind != auth.KindUser {
		return domain.ChatPage{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if a.Chats == nil {
		return domain.ChatPage{}, domain.Conflict("error.chat.engine_unavailable", nil)
	}
	if strings.TrimSpace(in.Text) == "" || len(in.Text) > 512*1024 || !domain.ValidReasoningEffort(in.ReasoningEffort) {
		return domain.ChatPage{}, domain.InvalidRequest("error.invalid_request", nil)
	}
	var err error
	p, err = WithWriteRequest(p, "chat.send", in.RequestID, struct {
		ID    string
		Input ChatSendInput
	}{id, in})
	if err != nil {
		return domain.ChatPage{}, err
	}
	if p.Write == nil {
		return domain.ChatPage{}, domain.InvalidRequest("error.idempotency.required", nil)
	}
	cached, unlock, err := beginWrite[domain.ChatPage](ctx, a, p)
	defer unlock()
	if err != nil {
		return domain.ChatPage{}, err
	}
	if cached != nil {
		return *cached, nil
	}
	creating := id == ""
	if creating {
		id = domain.NewID()
	}
	navRelease := a.Chats.lock("navigation")
	defer navRelease()
	release := a.Chats.lock(id)
	defer release()
	now := time.Now().UTC()
	var chat domain.Chat
	if creating {
		choice, e := a.chatChoice(ctx, in.ProviderID, in.Model)
		if e != nil {
			return domain.ChatPage{}, e
		}
		title := []rune(strings.Join(strings.Fields(in.Text), " "))
		if len(title) > 24 {
			title = append(title[:24], '…')
		}
		chat = domain.Chat{ManualOrder: now.UnixNano(), ID: id, Title: string(title), ProviderID: choice.ProviderID, Model: choice.Model, PermissionMode: domain.PermissionDefault, ReasoningEffort: in.ReasoningEffort, Workspace: filepath.Join(a.Paths.Root, "chat-workspaces", id), Revision: 1, CreatedAt: now, UpdatedAt: now}
	} else {
		chat, err = sqlite.GetChat(ctx, a.DB, id)
		if err != nil {
			return domain.ChatPage{}, sqlite.NormalizeError(err)
		}
		if in.Model != "" || in.ProviderID != "" || in.ReasoningEffort != "" {
			return domain.ChatPage{}, domain.InvalidRequest("error.chat.model_fixed", nil)
		}
		if chat.ArchivedAt != "" || chat.ArchiveOperation != "" {
			return domain.ChatPage{}, domain.Conflict("error.chat.archived", nil)
		}
		runs, e := sqlite.ListChatRuns(ctx, a.DB, id)
		if e != nil {
			return domain.ChatPage{}, e
		}
		for _, r := range runs {
			if domain.ChatRunActive(r.Status) {
				return domain.ChatPage{}, domain.Conflict("error.chat.busy", nil)
			}
		}
		if _, e = a.chatChoice(ctx, chat.ProviderID, chat.Model); e != nil {
			return domain.ChatPage{}, e
		}
		chat.Revision++
		chat.UpdatedAt = now
	}
	run := domain.ChatRun{ID: domain.NewID(), ChatID: id, Status: "queued", StartedAt: now, Steps: []domain.ChatStep{}}
	msg := domain.ChatMessage{ID: domain.NewID(), ChatID: id, RunID: run.ID, Kind: domain.MessageKindUser, Text: in.Text, CreatedAt: now}
	run.MessageID = msg.ID
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.ChatPage{}, err
	}
	defer tx.Rollback()
	if err = sqlite.SaveChat(ctx, tx, chat, true); err != nil {
		return domain.ChatPage{}, err
	}
	if err = sqlite.SaveChatMessage(ctx, tx, msg); err != nil {
		return domain.ChatPage{}, err
	}
	if err = sqlite.SaveChatRun(ctx, tx, run); err != nil {
		return domain.ChatPage{}, err
	}
	page, err := sqlite.GetChatPage(ctx, tx, id, "")
	if err != nil {
		return page, err
	}
	if err = a.saveWriteResult(ctx, tx, p, page); err != nil {
		return page, err
	}
	if err = tx.Commit(); err != nil {
		return page, err
	}
	a.chatChanged(ctx, id, "chat.message.updated")
	a.Chats.wake(id, run.ID, in.Text)
	return page, nil
}

func (a *App) ChangeChat(ctx context.Context, p auth.Principal, id, action string, in ChatActionInput) (domain.Chat, error) {
	if action == "archive" || action == "restore" {
		return a.archiveChat(ctx, p, id, action, in)
	}
	if p.Kind != auth.KindUser {
		return domain.Chat{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	var err error
	p, err = WithWriteRequest(p, "chat."+action, in.RequestID, struct {
		ID    string
		Input ChatActionInput
	}{id, in})
	if err != nil {
		return domain.Chat{}, err
	}
	cached, unlock, err := beginWrite[domain.Chat](ctx, a, p)
	defer unlock()
	if err != nil {
		return domain.Chat{}, err
	}
	if cached != nil {
		return *cached, nil
	}
	if a.Chats == nil {
		return domain.Chat{}, domain.Conflict("error.chat.engine_unavailable", nil)
	}
	navRelease := a.Chats.lock("navigation")
	defer navRelease()
	release := a.Chats.lock(id)
	defer release()
	c, err := sqlite.GetChat(ctx, a.DB, id)
	if err != nil {
		return c, sqlite.NormalizeError(err)
	}
	if in.ExpectedRevision != 0 && in.ExpectedRevision != c.Revision {
		return c, domain.Conflict("error.chat.revision_conflict", nil)
	}
	if c.ArchiveOperation != "" {
		return c, domain.Conflict("error.chat.archive_pending", nil)
	}
	if action == "delete" {
		runs, e := sqlite.ListChatRuns(ctx, a.DB, id)
		if e != nil {
			return c, e
		}
		for _, r := range runs {
			if domain.ChatRunActive(r.Status) {
				return c, domain.Conflict("error.chat.busy", nil)
			}
		}
	} else if action == "rename" {
		title := strings.TrimSpace(in.Title)
		if title == "" || len([]rune(title)) > 120 {
			return c, domain.InvalidRequest("error.chat.title_required", nil)
		}
		c.Title = title
		c.Revision++
		c.UpdatedAt = time.Now().UTC()
	} else if action == "pin" || action == "unpin" || action == "move" || action == "reorder" {
		if c.ArchivedAt != "" {
			return c, domain.Conflict("error.chat.archived", nil)
		}
		if action == "move" {
			n, e := sqlite.ChatNavigation(ctx, a.DB)
			if e != nil {
				return c, e
			}
			found := in.SectionID == ""
			for _, s := range n.Sections {
				if s.ID == in.SectionID {
					found = true
				}
			}
			if !found {
				return c, domain.Conflict("error.chat.section_missing", nil)
			}
			c.SectionID = in.SectionID
		} else if action == "pin" || action == "unpin" {
			c.Pinned = action == "pin"
		}
		c.Revision++
	} else {
		return c, domain.InvalidRequest("error.invalid_request", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	if action == "delete" {
		_, err = tx.ExecContext(ctx, `DELETE FROM chats WHERE id=?`, id)
	} else {
		err = sqlite.SaveChat(ctx, tx, c, false)
	}
	if err != nil {
		return c, err
	}
	if action == "reorder" || action == "move" {
		if err = sqlite.ReorderChat(ctx, tx, c, in.BeforeID); err != nil {
			return c, err
		}
		c, err = sqlite.GetChat(ctx, tx, id)
		if err != nil {
			return c, err
		}
	}
	if err = a.saveWriteResult(ctx, tx, p, c); err != nil {
		return c, err
	}
	if err = tx.Commit(); err != nil {
		return c, err
	}
	if action == "delete" {
		a.Chats.discard(id)
	}
	a.chatChanged(ctx, id, "chat."+action)
	return c, nil
}

func (a *App) chatChanged(ctx context.Context, id, kind string) {
	_ = a.recordEvent(ctx, domain.Event{Type: kind, ResourceID: id, Payload: json.RawMessage(`{"chatId":` + strconvQuote(id) + `}`)})
	if a.Hub != nil {
		a.Hub.Publish()
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

func (a *App) ChatRunAction(ctx context.Context, p auth.Principal, chatID, runID, action string, in ChatActionInput) (domain.ChatRun, error) {
	if p.Kind != auth.KindUser {
		return domain.ChatRun{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if a.Chats == nil {
		return domain.ChatRun{}, domain.Conflict("error.chat.engine_unavailable", nil)
	}
	return a.Chats.runAction(ctx, p, chatID, runID, action, in)
}
