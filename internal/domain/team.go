package domain

import "time"

// Team lifecycle states. Archived pauses dispatch; directory_missing pauses
// the team until the user rebinds; error keeps a retryable creation failure.
const (
	TeamStatusCreating         = "creating"
	TeamStatusActive           = "active"
	TeamStatusArchived         = "archived"
	TeamStatusDeleted          = "deleted"
	TeamStatusDirectoryMissing = "directory_missing"
	TeamStatusError            = "error"
)

// Team is one collaboration unit bound to exactly one working directory.
type Team struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	ProfileInstance string    `json:"profileInstanceId"`
	LeaderID        string    `json:"leaderId"`
	Directory       string    `json:"directory"`
	DirectoryKey    string    `json:"-"`
	Status          string    `json:"status"`
	StatusDetail    string    `json:"statusDetail,omitempty"`
	Revision        int64     `json:"revision"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Bot is one member of a team. The product status is derived from run
// facts; the UI never animates a status the backend does not know.
const (
	BotStatusIdle            = "idle"
	BotStatusQueued          = "queued"
	BotStatusWorking         = "working"
	BotStatusWaitingApproval = "waiting_approval"
	BotStatusWaitingInput    = "waiting_input"
	BotStatusInterrupted     = "interrupted"
	BotStatusUnknown         = "unknown"
)

// Bot is one agent instance in a team.
type Bot struct {
	ID           string    `json:"id"`
	TeamID       string    `json:"teamId"`
	Name         string    `json:"name"`
	RoleID       string    `json:"roleId"`
	RoleVersion  string    `json:"roleVersion,omitempty"`
	IsLeader     bool      `json:"isLeader"`
	Status       string    `json:"status"`
	CurrentRunID string    `json:"currentRunId,omitempty"`
	Revision     int64     `json:"revision"`
	CreatedAt    time.Time `json:"createdAt"`
}

// ProviderSession maps a bot onto its engine thread.
type ProviderSession struct {
	BotID            string    `json:"botId"`
	ProviderThreadID string    `json:"providerThreadId"`
	EngineEpoch      string    `json:"engineEpoch"`
	Model            string    `json:"model,omitempty"`
	ModelProvider    string    `json:"modelProvider,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Event types recorded by the M2 surface; append, never rename.
const (
	EventTeamCreated     = "team.created"
	EventTeamUpdated     = "team.updated"
	EventBotAdded        = "bot.added"
	EventMessageCreated  = "message.created"
	EventMessageUpdated  = "message.updated"
	EventRunUpdated      = "run.updated"
	EventApprovalCreated = "approval.created"
	EventApprovalUpdated = "approval.updated"
)
