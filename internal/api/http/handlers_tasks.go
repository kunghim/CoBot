package http

import (
	"net/http"

	"github.com/kunghim/CoBot/internal/app"
)

// handleListTasks projects a team's tasks. A member's scope is narrowed
// server-side; the browser never decides who may see what.
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) { s.handlePage(w, r, "tasks") }

type createTaskRequest struct {
	RequestID          string   `json:"requestId"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptanceCriteria"`
	OwnerID            string   `json:"ownerId"`
	ParentID           string   `json:"parentId"`
	DependencyIDs      []string `json:"dependencyIds"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req createTaskRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	task, err := s.App.CreateTask(r.Context(), principal, false, app.CreateTaskInput{
		TeamID: r.PathValue("id"), OwnerID: req.OwnerID, Title: req.Title,
		Description: req.Description, AcceptanceCriteria: req.AcceptanceCriteria,
		ParentID: req.ParentID, DependencyIDs: req.DependencyIDs, RequestID: req.RequestID,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, task)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	detail, err := s.App.GetTaskDetail(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, detail)
}

type patchTaskRequest struct {
	RequestID        string `json:"requestId"`
	Status           string `json:"status"`
	BlockedReason    string `json:"blockedReason"`
	OwnerID          string `json:"ownerId"`
	ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
}

func (s *Server) handlePatchTask(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req patchTaskRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	var expected int64
	if req.ExpectedRevision != nil {
		expected = *req.ExpectedRevision
	}
	// Changing the owner goes through the same safety rules as a transfer; a
	// status change never silently moves ownership.
	if req.OwnerID != "" {
		task, err := s.App.AssignTask(r.Context(), principal, r.PathValue("id"), req.OwnerID, expected)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeData(w, r, http.StatusOK, task)
		return
	}
	task, err := s.App.UpdateTaskStatus(r.Context(), principal, r.PathValue("id"), req.Status, req.BlockedReason, expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, task)
}

type submitEvidenceRequest struct {
	RequestID    string   `json:"requestId"`
	Summary      string   `json:"summary"`
	ArtifactRefs []string `json:"artifactRefs"`
	CandidateRef string   `json:"candidateRef"`
	TestResult   string   `json:"testResult"`
	ReviewResult string   `json:"reviewResult"`
}

func (s *Server) handleSubmitEvidence(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req submitEvidenceRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	evidence, err := s.App.SubmitEvidence(r.Context(), principal, r.PathValue("id"), req.Summary, req.ArtifactRefs, req.CandidateRef, req.TestResult, req.ReviewResult)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, evidence)
}

type confirmTaskRequest struct {
	RequestID        string   `json:"requestId"`
	EvidenceIDs      []string `json:"evidenceIds"`
	ExpectedRevision *int64   `json:"expectedRevision,omitempty"`
}

func (s *Server) handleConfirmTask(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req confirmTaskRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	var expected int64
	if req.ExpectedRevision != nil {
		expected = *req.ExpectedRevision
	}
	task, err := s.App.ConfirmTask(r.Context(), principal, r.PathValue("id"), req.EvidenceIDs, expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, task)
}

type offerTransferRequest struct {
	RequestID string `json:"requestId"`
	ToBotID   string `json:"toBotId"`
	Note      string `json:"note"`
}

func (s *Server) handleOfferTransfer(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req offerTransferRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	transfer, err := s.App.OfferTransfer(r.Context(), principal, r.PathValue("id"), req.ToBotID, req.Note)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, transfer)
}

func (s *Server) handleAcceptTransfer(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID        string `json:"requestId"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	task, err := s.App.AcceptTransfer(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, task)
}

func (s *Server) handleCancelTransfer(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	transfer, err := s.App.CancelTransfer(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, transfer)
}
