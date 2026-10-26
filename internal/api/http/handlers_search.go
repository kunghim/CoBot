package http

import (
	"net/http"
)

func (s *Server) handleSearchConversations(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	v, e := s.App.SearchConversations(r.Context(), p, r.URL.Query().Get("q"))
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeData(w, r, http.StatusOK, v)
}
