package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"strings"
	"time"
)

type ChatNavigationInput struct {
	RequestID        string `json:"requestId"`
	Action           string `json:"action"`
	ID               string `json:"id,omitempty"`
	Name             string `json:"name,omitempty"`
	BeforeID         string `json:"beforeId,omitempty"`
	Sort             string `json:"sort,omitempty"`
	Grouping         string `json:"grouping,omitempty"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func (a *App) ListOrganizedChats(ctx context.Context, p auth.Principal, cursor string, f domain.ChatListFilter) (domain.ChatList, error) {
	if p.Kind != auth.KindUser {
		return domain.ChatList{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	return sqlite.ListOrganizedChats(ctx, a.DB, cursor, f)
}
func (a *App) ChatNavigation(ctx context.Context, p auth.Principal) (domain.ChatNavigation, error) {
	if p.Kind != auth.KindUser {
		return domain.ChatNavigation{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	return sqlite.ChatNavigation(ctx, a.DB)
}
func (a *App) ChangeChatNavigation(ctx context.Context, p auth.Principal, in ChatNavigationInput) (domain.ChatNavigation, error) {
	var out domain.ChatNavigation
	if p.Kind != auth.KindUser {
		return out, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if a.Chats == nil {
		return out, domain.Conflict("error.chat.engine_unavailable", nil)
	}
	p, err := WithWriteRequest(p, "chat.navigation", in.RequestID, in)
	if err != nil {
		return out, err
	}
	if p.Write == nil {
		return out, domain.InvalidRequest("error.idempotency.required", nil)
	}
	cached, unlock, err := beginWrite[domain.ChatNavigation](ctx, a, p)
	defer unlock()
	if err != nil {
		return out, err
	}
	if cached != nil {
		return *cached, nil
	}
	release := a.Chats.lock("navigation")
	defer release()
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out, err = sqlite.ChatNavigation(ctx, tx)
	if err != nil {
		return out, err
	}
	if in.ExpectedRevision != out.Revision {
		return out, domain.Conflict("error.chat.revision_conflict", nil)
	}
	at := -1
	for i, s := range out.Sections {
		if s.ID == in.ID {
			at = i
		}
	}
	switch in.Action {
	case "preferences":
		if in.Sort != "recent" && in.Sort != "manual" {
			return out, domain.InvalidRequest("error.invalid_request", nil)
		}
		if in.Grouping != "sections" && in.Grouping != "merged" {
			return out, domain.InvalidRequest("error.invalid_request", nil)
		}
		out.Sort, out.Grouping = in.Sort, in.Grouping
	case "create", "rename":
		name := strings.TrimSpace(in.Name)
		if name == "" || len([]rune(name)) > 60 {
			return out, domain.InvalidRequest("error.chat.title_required", nil)
		}
		if in.Action == "create" {
			out.Sections = append(out.Sections, domain.ChatSection{ID: domain.NewID(), Name: name})
		} else {
			if at < 0 {
				return out, domain.Conflict("error.chat.section_missing", nil)
			}
			out.Sections[at].Name = name
		}
	case "delete":
		if at < 0 {
			return out, domain.Conflict("error.chat.section_missing", nil)
		}
		out.Sections = append(out.Sections[:at], out.Sections[at+1:]...)
		if _, err = tx.ExecContext(ctx, `UPDATE chats SET data=json_set(data,'$.sectionId','','$.revision',json_extract(data,'$.revision')+1) WHERE json_extract(data,'$.sectionId')=?`, in.ID); err != nil {
			return out, err
		}
	case "reorder":
		if at < 0 || in.ID == in.BeforeID {
			return out, domain.Conflict("error.chat.order_conflict", nil)
		}
		item := out.Sections[at]
		out.Sections = append(out.Sections[:at], out.Sections[at+1:]...)
		pos := len(out.Sections)
		if in.BeforeID != "" {
			pos = -1
			for i, s := range out.Sections {
				if s.ID == in.BeforeID {
					pos = i
				}
			}
			if pos < 0 {
				return out, domain.Conflict("error.chat.order_conflict", nil)
			}
		}
		out.Sections = append(out.Sections, domain.ChatSection{})
		copy(out.Sections[pos+1:], out.Sections[pos:])
		out.Sections[pos] = item
	default:
		return out, domain.InvalidRequest("error.invalid_request", nil)
	}
	for i := range out.Sections {
		out.Sections[i].Order = int64(i + 1)
	}
	out.Revision++
	if err = sqlite.SaveChatNavigation(ctx, tx, out); err != nil {
		return out, err
	}
	if err = a.saveWriteResult(ctx, tx, p, out); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	a.chatChanged(ctx, "", "chat.navigation.updated")
	return out, nil
}

type chatThreadArchiver interface {
	ArchiveThread(context.Context, string) error
	UnarchiveThread(context.Context, string) error
}
type chatArchiveInspector interface {
	ThreadArchiveState(context.Context, string) (bool, error)
}

// Persist intent before the RPC. An interrupted operation remains visible and blocks sending;
// an explicit retry inspects native state instead of blindly replaying an uncertain effect.
func (a *App) archiveChat(ctx context.Context, p auth.Principal, id, action string, in ChatActionInput) (domain.Chat, error) {
	var c domain.Chat
	if p.Kind != auth.KindUser {
		return c, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if a.Chats == nil {
		return c, domain.Conflict("error.chat.engine_unavailable", nil)
	}
	p, err := WithWriteRequest(p, "chat."+action, in.RequestID, struct {
		ID    string
		Input ChatActionInput
	}{id, in})
	if err != nil {
		return c, err
	}
	if p.Write == nil {
		return c, domain.InvalidRequest("error.idempotency.required", nil)
	}
	cached, unlock, err := beginWrite[domain.Chat](ctx, a, p)
	defer unlock()
	if err != nil {
		return c, err
	}
	if cached != nil {
		return sqlite.GetChat(ctx, a.DB, id)
	}
	operationRelease := a.Chats.lock("archive:" + id)
	defer operationRelease()
	navRelease := a.Chats.lock("navigation")
	releaseChat := a.Chats.lock(id)
	release := func() { releaseChat(); navRelease() }
	c, err = sqlite.GetChat(ctx, a.DB, id)
	if err != nil {
		release()
		return c, sqlite.NormalizeError(err)
	}
	if in.ExpectedRevision != 0 && in.ExpectedRevision != c.Revision {
		release()
		return c, domain.Conflict("error.chat.revision_conflict", nil)
	}
	if c.ArchiveOperation != "" && c.ArchiveOperation != action {
		release()
		return c, domain.Conflict("error.chat.archive_pending", nil)
	}
	runs, err := sqlite.ListChatRuns(ctx, a.DB, id)
	if err != nil {
		release()
		return c, err
	}
	for _, r := range runs {
		if domain.ChatRunActive(r.Status) {
			release()
			return c, domain.Conflict("error.chat.busy", nil)
		}
	}
	requests, err := sqlite.ListChatRequests(ctx, a.DB, id)
	if err != nil {
		release()
		return c, err
	}
	for _, req := range requests {
		if req.State == domain.ApprovalStatePending {
			release()
			return c, domain.Conflict("error.chat.busy", nil)
		}
	}
	recovering := c.ArchiveOperation != ""
	if !recovering && ((action == "archive") == (c.ArchivedAt != "")) {
		release()
		return c, domain.Conflict("error.chat.archive_state", nil)
	}
	c.ArchiveOperation = action
	c.ArchiveError = ""
	c.Revision++
	tx, err := a.DB.BeginTx(ctx, nil)
	if err == nil {
		err = sqlite.SaveChat(ctx, tx, c, false)
	}
	if err == nil {
		err = a.saveWriteResult(ctx, tx, p, c)
	}
	if err == nil {
		err = tx.Commit()
	}
	if tx != nil {
		_ = tx.Rollback()
	}
	release()
	if err != nil {
		return c, err
	}
	a.chatChanged(ctx, id, "chat.archive.updated")
	session, e := sqlite.GetChatSession(ctx, a.DB, id)
	if e != nil && !errors.Is(e, sqlite.ErrNotFound) {
		err = e
	}
	if e == nil {
		var rt *chatRuntime
		rt, err = a.Chats.ensure(c)
		if err == nil {
			srv, ok := rt.server.(chatThreadArchiver)
			if !ok {
				err = errors.New("thread archive unsupported")
			} else {
				callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				complete := false
				if recovering {
					if inspect, ok := rt.server.(chatArchiveInspector); ok {
						var archived bool
						archived, err = inspect.ThreadArchiveState(callCtx, session.ThreadID)
						complete = err == nil && archived == (action == "archive")
					} else {
						err = errors.New("thread archive state unavailable")
					}
				}
				if err == nil && !complete {
					if action == "archive" {
						err = srv.ArchiveThread(callCtx, session.ThreadID)
					} else {
						err = srv.UnarchiveThread(callCtx, session.ThreadID)
					}
				}
				cancel()
			}
		}
	}
	endCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	navRelease = a.Chats.lock("navigation")
	defer navRelease()
	release = a.Chats.lock(id)
	defer release()
	c, e = sqlite.GetChat(endCtx, a.DB, id)
	if e != nil {
		return c, e
	}
	if err != nil {
		c.ArchiveError = "error.chat.archive_unknown"
	} else {
		if action == "archive" {
			c.ArchivedAt = time.Now().UTC().Format(time.RFC3339Nano)
		} else {
			c.ArchivedAt = ""
		}
		c.ArchiveOperation = ""
		c.ArchiveError = ""
	}
	c.Revision++
	tx, e = a.DB.BeginTx(endCtx, nil)
	if e != nil {
		return c, e
	}
	defer tx.Rollback()
	if e = sqlite.SaveChat(endCtx, tx, c, false); e != nil {
		return c, e
	}
	raw, _ := json.Marshal(c)
	if _, e = tx.ExecContext(endCtx, `UPDATE idempotency SET response=? WHERE scope=? AND principal=? AND key=? AND request_hash=?`, string(raw), "resource."+p.Write.Scope, writePrincipal(p), p.Write.Key, p.Write.ArgsHash); e != nil {
		return c, e
	}
	if e = tx.Commit(); e != nil {
		return c, e
	}
	if err == nil {
		a.Chats.discard(id)
	}
	a.chatChanged(endCtx, id, "chat.archive.updated")
	return c, nil
}
