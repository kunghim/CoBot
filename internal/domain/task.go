package domain

import (
	"encoding/json"
	"time"
)

// Card lifecycle. A card reaches its owner as todo, turns doing when the
// owner's turn takes it up, and ends done when the owner hands it off; a
// cancelled requirement cancels its open cards.
const (
	TaskTodo      = "todo"
	TaskDoing     = "doing"
	TaskDone      = "done"
	TaskCancelled = "cancelled"
)

// TaskAuthorUser marks a card or handoff record created by the user rather
// than a bot.
const TaskAuthorUser = "user"

// Task is a card: one stage of a requirement's route, held by one member.
type Task struct {
	ID                 string `json:"id"`
	TeamID             string `json:"teamId"`
	OwnerID            string `json:"ownerId"`
	Title              string `json:"title"`
	Description        string `json:"description,omitempty"`
	AcceptanceCriteria string `json:"acceptanceCriteria,omitempty"`
	Status             string `json:"status"`
	// ReviewType names the checklist of a review stage's card.
	ReviewType string `json:"reviewType,omitempty"`
	// RequirementID, StageIndex and StageKind place a card on its
	// requirement's route; a draft card (the requirement document) has stage
	// index -1, an escalation card the index of the stage it came from.
	RequirementID string `json:"requirementId,omitempty"`
	StageIndex    int    `json:"stageIndex"`
	StageKind     string `json:"stageKind,omitempty"`
	// ReturnTo is the stage a rework card goes back to when done (a
	// return-to-me send-back); -1 runs the route on from its own stage.
	ReturnTo  int       `json:"returnTo"`
	Revision  int64     `json:"revision"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsTerminal reports whether a card is finished with.
func (t Task) IsTerminal() bool {
	return t.Status == TaskDone || t.Status == TaskCancelled
}

// Evidence is a handoff record: what a member said it did when it handed a
// card off, and what the engine reported it actually ran and changed.
type Evidence struct {
	ID           string   `json:"id"`
	TaskID       string   `json:"taskId"`
	AuthorID     string   `json:"authorId"`
	Summary      string   `json:"summary"`
	ArtifactRefs []string `json:"artifactRefs"`
	// Outcome is how the card was handed off (done, back, escalate,
	// resume); Mode the rework mode of a send-back.
	Outcome     string `json:"outcome,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Suggestions string `json:"suggestions,omitempty"`
	// Execution is what the engine reported was run and changed, never the
	// member's own account of it.
	Execution *Execution `json:"execution,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// Question states. A question is user input, never a security authorization.
const (
	QuestionOpen      = "open"
	QuestionAnswered  = "answered"
	QuestionCancelled = "cancelled"
)

// Question kinds. A plain question is ask_user; a confirm question is a
// confirm stage of a route; an acceptance question hands the finished
// requirement to the user.
const (
	QuestionKindAsk        = ""
	QuestionKindConfirm    = "confirm"
	QuestionKindAcceptance = "acceptance"
)

// Decisions on a confirm or acceptance question.
const (
	AcceptanceAccept = "accept"
	AcceptanceRevise = "revise"
)

// Question is an ask_user request, a confirmation or a requirement's
// acceptance, awaiting the user.
type Question struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	RequirementID   string    `json:"requirementId,omitempty"`
	StageIndex      int       `json:"stageIndex"`
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
	// CanSkipChecks is a confirm stage's projection: whether a revision can
	// come straight back to it, which only offers a choice when the stages
	// a straight return skips include one that checks the work. Not stored;
	// filled where the question is served.
	CanSkipChecks bool `json:"canSkipChecks"`
}

// Event types appended by the collaboration milestone; never renamed.
const (
	EventTaskCreated      = "task.created"
	EventTaskUpdated      = "task.updated"
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
