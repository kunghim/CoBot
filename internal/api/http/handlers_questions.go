package http

import (
	"net/http"
)

// handleListQuestions pages a team's questions. An open question is user input
// the user still owes; a decided one keeps its answer in the record.
func (s *Server) handleListQuestions(w http.ResponseWriter, r *http.Request) {
	s.handlePage(w, r, "questions")
}

type answerQuestionRequest struct {
	RequestID        string `json:"requestId"`
	AnswerText       string `json:"answerText"`
	ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
}

// handleAnswerQuestion delivers the user's answer. The answer is delivered as
// a user message to the asking bot; it is never a security authorization.
func (s *Server) handleAnswerQuestion(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req answerQuestionRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	var expected int64
	if req.ExpectedRevision != nil {
		expected = *req.ExpectedRevision
	}
	question, err := s.App.AnswerQuestion(r.Context(), principal, r.PathValue("id"), req.AnswerText, expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, question)
}

type cancelQuestionRequest struct {
	RequestID        string `json:"requestId"`
	ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
}

func (s *Server) handleCancelQuestion(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req cancelQuestionRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	var expected int64
	if req.ExpectedRevision != nil {
		expected = *req.ExpectedRevision
	}
	question, err := s.App.CancelQuestion(r.Context(), principal, r.PathValue("id"), expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, question)
}
