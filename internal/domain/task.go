package domain

import (
	"encoding/json"
	"time"
)

// Task lifecycle. done requires matching evidence confirmed by the captain or
// the user; a chat message saying "done" never changes status by itself.
const (
	TaskTodo      = "todo"
	TaskDoing     = "doing"
	TaskReview    = "review"
	TaskDone      = "done"
	TaskCancelled = "cancelled"
)

// TaskAuthorUser marks a task or evidence row created by the user rather than
// a bot.
const TaskAuthorUser = "user"

// Task is the unit of work with exactly one owner.
type Task struct {
	ID                 string    `json:"id"`
	TeamID             string    `json:"teamId"`
	OwnerID            string    `json:"ownerId"`
	ParentID           string    `json:"parentId,omitempty"`
	Title              string    `json:"title"`
	Description        string    `json:"description,omitempty"`
	AcceptanceCriteria string    `json:"acceptanceCriteria,omitempty"`
	Status             string    `json:"status"`
	BlockedReason      string    `json:"blockedReason,omitempty"`
	DependencyIDs      []string  `json:"dependencyIds"`
	Revision           int64     `json:"revision"`
	CreatedBy          string    `json:"createdBy"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

// IsTerminal reports whether a task still participates in dependency
// activation.
func (t Task) IsTerminal() bool {
	return t.Status == TaskDone || t.Status == TaskCancelled
}

// Evidence is a recorded proof reference; the product stores references and
// agent reports, never the artifacts themselves.
type Evidence struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"taskId"`
	AuthorID     string    `json:"authorId"`
	Summary      string    `json:"summary"`
	ArtifactRefs []string  `json:"artifactRefs"`
	CandidateRef string    `json:"candidateRef,omitempty"`
	TestResult   string    `json:"testResult,omitempty"`
	ReviewResult string    `json:"reviewResult,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Transfer states. The owner is unchanged until accepted.
const (
	TransferOffered   = "offered"
	TransferAccepted  = "accepted"
	TransferCancelled = "cancelled"
	TransferStale     = "stale"
)

// Transfer is one two-phase ownership hand-over proposal.
type Transfer struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"taskId"`
	FromBotID    string    `json:"fromBotId"`
	ToBotID      string    `json:"toBotId"`
	TaskRevision int64     `json:"taskRevision"`
	Note         string    `json:"note,omitempty"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Question states. A question is user input, never a security authorization.
const (
	QuestionOpen      = "open"
	QuestionAnswered  = "answered"
	QuestionCancelled = "cancelled"
)

// Question is an ask_user request awaiting the user.
type Question struct {
	ID              string    `json:"id"`
	TeamID          string    `json:"teamId"`
	RequesterBotID  string    `json:"requesterBotId"`
	TaskID          string    `json:"taskId,omitempty"`
	RunID           string    `json:"runId,omitempty"`
	Question        string    `json:"question"`
	Options         []string  `json:"options"`
	Status          string    `json:"status"`
	AnswerText      string    `json:"answerText,omitempty"`
	AnswerMessageID string    `json:"answerMessageId,omitempty"`
	Revision        int64     `json:"revision"`
	CreatedAt       time.Time `json:"createdAt"`
	AnsweredAt      time.Time `json:"answeredAt,omitempty"`
}

// Event types appended by the collaboration milestone; never renamed.
const (
	EventTaskCreated      = "task.created"
	EventTaskUpdated      = "task.updated"
	EventTransferOffered  = "transfer.offered"
	EventTransferAccepted = "transfer.accepted"
	EventQuestionCreated  = "question.created"
	EventQuestionAnswered = "question.answered"
)

// ParseStringList decodes a JSON string array column, tolerating legacy empty
// values.
func ParseStringList(raw json.RawMessage) []string {
	var out []string
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}
