package http

import (
	"net/http"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
)

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	v, e := s.App.ListOrganizedChats(r.Context(), p, q.Get("cursor"), domain.ChatListFilter{Pinned: q.Get("pinned"), Archived: q.Get("archived") == "true", Sort: q.Get("sort"), Search: q.Get("search"), SectionID: q.Get("section")})
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusOK, v)
}

func (s *Server) handleChatNavigation(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	v, e := s.App.ChatNavigation(r.Context(), p)
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusOK, v)
}
func (s *Server) handleChangeChatNavigation(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	var in app.ChatNavigationInput
	if e := decodeBody(w, r, &in); e != nil {
		writeError(w, r, e)
		return
	}
	v, e := s.App.ChangeChatNavigation(r.Context(), p, in)
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusOK, v)
}
func (s *Server) handleChatAction(w http.ResponseWriter, r *http.Request) {
	s.handleChangeChat(w, r, r.PathValue("action"))
}
func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	v, e := s.App.ChatPage(r.Context(), p, r.PathValue("chatId"), r.URL.Query().Get("cursor"))
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusOK, v)
}
func (s *Server) handleSendChat(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	var in app.ChatSendInput
	if e := decodeBody(w, r, &in); e != nil {
		writeError(w, r, e)
		return
	}
	v, e := s.App.SendChat(r.Context(), p, r.PathValue("chatId"), in)
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusAccepted, v)
}
func (s *Server) handleRenameChat(w http.ResponseWriter, r *http.Request) {
	s.handleChangeChat(w, r, "rename")
}
func (s *Server) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	s.handleChangeChat(w, r, "delete")
}
func (s *Server) handleChangeChat(w http.ResponseWriter, r *http.Request, action string) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	var in app.ChatActionInput
	if e := decodeBody(w, r, &in); e != nil {
		writeError(w, r, e)
		return
	}
	v, e := s.App.ChangeChat(r.Context(), p, r.PathValue("chatId"), action, in)
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusOK, v)
}
func (s *Server) handleChatRunAction(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	var in app.ChatActionInput
	if e := decodeBody(w, r, &in); e != nil {
		writeError(w, r, e)
		return
	}
	v, e := s.App.ChatRunAction(r.Context(), p, r.PathValue("chatId"), r.PathValue("runId"), r.PathValue("action"), in)
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusOK, v)
}
func (s *Server) handleChatReply(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	var in app.ChatActionInput
	if e := decodeBody(w, r, &in); e != nil {
		writeError(w, r, e)
		return
	}
	v, e := s.App.DecideChatRequest(r.Context(), p, r.PathValue("chatId"), r.PathValue("requestId"), in)
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusOK, v)
}
func (s *Server) handleOpenChatPath(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	var in struct {
		RequestID string `json:"requestId"`
		Path      string `json:"path"`
		Reveal    bool   `json:"reveal"`
	}
	if e := decodeBody(w, r, &in); e != nil {
		writeError(w, r, e)
		return
	}
	v, e := s.App.OpenChatPath(r.Context(), p, r.PathValue("chatId"), in.Path, in.Reveal)
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusOK, v)
}
