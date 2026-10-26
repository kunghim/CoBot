package http

import (
	"net/http"
)

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	s.handlePage(w, r, "approvals")
}

type decideApprovalRequest struct {
	RequestID        string         `json:"requestId"`
	OptionID         string         `json:"optionId"`
	Comment          string         `json:"comment"`
	ExpectedRevision *int64         `json:"expectedRevision,omitempty"`
	Content          map[string]any `json:"content,omitempty"`
}

func (s *Server) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req decideApprovalRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	var expected int64
	if req.ExpectedRevision != nil {
		expected = *req.ExpectedRevision
	}
	approval, err := s.App.DecideApproval(r.Context(), principal, r.PathValue("id"), req.OptionID, req.Comment, expected, req.Content)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, approval)
}
