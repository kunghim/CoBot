package http

import (
	"net/http"
	"strconv"

	"github.com/kunghim/CoBot/internal/app"
)

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request, kind string) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	in := app.PageRequest{Kind: kind, TeamID: query.Get("teamId"), ViewBotID: query.Get("viewBotId"), State: query.Get("state"), PeerBotID: query.Get("peerBotId"), Cursor: query.Get("cursor"), Limit: limit}
	switch kind {
	case "tasks", "activity":
		in.TeamID = r.PathValue("id")
	case "messages", "collaboration", "memories":
		in.BotID = r.PathValue("id")
	case "questions":
		in.State = query.Get("status")
	}
	if in.ViewBotID == "" {
		in.ViewBotID = query.Get("requesterBotId")
	}
	if in.ViewBotID == "" && kind == "tasks" {
		in.ViewBotID = query.Get("ownerId")
	}
	if in.Cursor == "" && kind == "messages" {
		in.Cursor = query.Get("before")
	}
	data, err := s.App.ListPage(r.Context(), p, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, data)
}
