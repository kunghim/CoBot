package domain

import "time"

// Stage kinds of a requirement's route. Producing stages make something the
// following checks look at; checking stages (review, test, confirm) may send
// it back. The route always ends with the project manager's acceptance.
const (
	StageConfirm   = "confirm"    // the user confirms (requirement and route, or a design)
	StageDesign    = "design"     // product design and prototype
	StageReview    = "review"     // a reviewer applies the checklist of its review type
	StageTestCases = "test_cases" // test cases written before implementation
	StageImplement = "implement"  // the change itself
	StageTest      = "test"       // test execution
	StageAccept    = "accept"     // the project manager checks and reports to the user
)

// Card kinds that are not stages of the route: the project manager reworking
// the requirement document, and handling a member's escalation.
const (
	CardDraft      = "draft"
	CardEscalation = "escalation"
)

// Handoff outcomes and rework modes.
const (
	HandoffDone     = "done"
	HandoffBack     = "back"
	HandoffEscalate = "escalate"
	HandoffResume   = "resume"

	ReworkRerun      = "rerun"        // after the fix the route runs on from the fixed stage
	ReworkReturnToMe = "return_to_me" // after the fix the card comes straight back
)

// Requirement statuses.
const (
	RequirementActive    = "active"
	RequirementAccepting = "accepting" // waiting for the user's final acceptance
	RequirementDone      = "done"
	RequirementCancelled = "cancelled"
)

// Stage is one step of a requirement's route.
type Stage struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	OwnerID    string `json:"ownerId,omitempty"`
	ReviewType string `json:"reviewType,omitempty"`
	Criteria   string `json:"criteria,omitempty"`
}

// ProducesWork reports whether a stage makes the thing later checks inspect.
func (s Stage) ProducesWork() bool {
	switch s.Kind {
	case StageDesign, StageTestCases, StageImplement:
		return true
	}
	return false
}

// ChecksWork reports whether a stage may send work back.
func (s Stage) ChecksWork() bool {
	return s.Kind == StageReview || s.Kind == StageTest
}

// Requirement is one confirmed piece of work: its document and its route.
type Requirement struct {
	ID        string    `json:"id"`
	TeamID    string    `json:"teamId"`
	Title     string    `json:"title"`
	Document  string    `json:"document"`
	Stages    []Stage   `json:"stages"`
	Status    string    `json:"status"`
	CreatedBy string    `json:"createdBy"`
	ChainID   string    `json:"-"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Execution is what the engine reported a member actually ran and changed
// while it held a card.
type Execution struct {
	Commands []ExecutedCommand `json:"commands"`
	Files    []string          `json:"files"`
}

// ExecutedCommand is one command and how it ended.
type ExecutedCommand struct {
	Command  string `json:"command"`
	ExitCode *int   `json:"exitCode,omitempty"`
}

// Event types of the relay; never renamed.
const (
	EventRequirementUpdated = "requirement.updated"
	EventCardStarted        = "card.started"
)
