package http

import (
	"context"
	"net/http"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

// handleListQuestions pages a team's questions. An open question is user input
// the user still owes; a decided one keeps its answer in the record.
func (s *Server) handleListQuestions(w http.ResponseWriter, r *http.Request) {
	s.handlePage(w, r, "questions")
}

type answerQuestionRequest struct {
	RequestID  string `json:"requestId"`
	AnswerText string `json:"answerText"`
	// Decision answers a confirmation or acceptance: accept, or revise with the
	// changes in answerText.
	Decision string `json:"decision,omitempty"`
	// Mode is where a confirmation's changes go once made: rerun (through
	// the stages in between again, the default) or return_to_me.
	Mode             string `json:"mode,omitempty"`
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
	answer := s.App.AnswerQuestion
	if req.Decision != "" {
		answer = func(ctx context.Context, principal auth.Principal, id, text string, expected int64) (domain.Question, error) {
			return s.App.AnswerDecision(ctx, principal, id, req.Decision, req.Mode, text, expected)
		}
	}
	question, err := answer(r.Context(), principal, r.PathValue("id"), req.AnswerText, expected)
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
