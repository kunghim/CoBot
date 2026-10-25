package domain

import "time"

// Team lifecycle states. Creation is one transaction, so a team is active
// from its first moment. Archived sets an idle team aside; directory_missing
// keeps the team read-only while its lifelong folder is gone and clears
// itself when the folder is back. Deleted is only ever a response value:
// deleting removes the team.
const (
	TeamStatusActive           = "active"
	TeamStatusArchived         = "archived"
	TeamStatusDeleted          = "deleted"
	TeamStatusDirectoryMissing = "directory_missing"
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
	// ArchivedAt is when the team was archived (RFC 3339), empty otherwise.
	ArchivedAt string `json:"archivedAt,omitempty"`
	// PermissionMode is how freely members may act (PermissionReadOnly,
	// PermissionDefault, PermissionFull); applied from the next turn.
	PermissionMode string `json:"permissionMode"`
	// ReasoningEffort is how hard members think each turn (an effort the
	// model supports, as Codex names it); empty follows the model's default.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// ProviderID is the model provider this team runs with. Empty means the
	// team follows the profile's default.
	//
	// It is named here rather than read from the team's threads: a thread
	// records the provider it had, not the one it should have.
	ProviderID string `json:"providerId,omitempty"`
}

// Team permission modes. CoBot only knows these three; each engine adapter
// maps them onto its own approval and sandbox settings.
const (
	PermissionReadOnly = "read_only"
	PermissionDefault  = "default"
	PermissionFull     = "full"
)

// ValidPermissionMode reports whether mode is one of the three modes.
func ValidPermissionMode(mode string) bool {
	return mode == PermissionReadOnly || mode == PermissionDefault || mode == PermissionFull
}

// Bot is one member of a team. The product status is derived from run
// facts; the UI never animates a status the backend does not know.
const (
	BotStatusIdle            = "idle"
	BotStatusQueued          = "queued"
	BotStatusWorking         = "working"
	BotStatusWaitingApproval = "waiting_approval"
	BotStatusWaitingInput    = "waiting_input"
	BotStatusUnknown         = "unknown"
)

// Bot is one agent instance in a team.
type Bot struct {
	ID           string `json:"id"`
	TeamID       string `json:"teamId"`
	Name         string `json:"name"`
	RoleID       string `json:"roleId"`
	RoleVersion  string `json:"roleVersion,omitempty"`
	IsLeader     bool   `json:"isLeader"`
	Status       string `json:"status"`
	CurrentRunID string `json:"currentRunId,omitempty"`
	// Avatar is the member's own avatar; nil follows its role's.
	Avatar *AvatarRef `json:"avatar,omitempty"`
	// ProviderID and Model are the model this member runs with, as the
	// member's own choice. Both empty means the member follows the default
	// model named in the settings.
	ProviderID string    `json:"providerId,omitempty"`
	Model      string    `json:"model,omitempty"`
	Revision   int64     `json:"revision"`
	CreatedAt  time.Time `json:"createdAt"`
	// LastMessage previews the latest message between the user and this
	// member; only team listings fill it.
	LastMessage *MessagePreview `json:"lastMessage,omitempty"`
}

// MessagePreview is the start of a conversation's latest message, for the
// team list.
type MessagePreview struct {
	Kind string `json:"kind"` // user|assistant|system|collaboration
	// SenderBotID names the member that sent it, for cards and notes from
	// other members; the member's exchange with the user has none.
	SenderBotID string    `json:"senderBotId,omitempty"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ProviderSession maps a bot onto its engine thread.
type ProviderSession struct {
	BotID            string `json:"botId"`
	ProviderThreadID string `json:"providerThreadId"`
	EngineEpoch      string `json:"engineEpoch"`
	Model            string `json:"model,omitempty"`
	ModelProvider    string `json:"modelProvider,omitempty"`
	// TranscriptPath is the file the engine keeps this thread's full
	// record in.
	TranscriptPath string `json:"transcriptPath,omitempty"`
	// Handover names the thread this one took over from, until the first
	// turn that tells the member about it is accepted.
	Handover  *ThreadHandover `json:"handover,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// ThreadHandover is the thread a member's new thread took over from: one
// started for another vendor's model, or after the old one could not be
// loaded again.
type ThreadHandover struct {
	ThreadID       string `json:"threadId"`
	TranscriptPath string `json:"transcriptPath,omitempty"`
	// ModelProvider and Model are what the old thread ran with.
	ModelProvider string `json:"modelProvider,omitempty"`
	Model         string `json:"model,omitempty"`
}

// Event types recorded by the M2 surface; append, never rename.
const (
	EventTeamCreated  = "team.created"
	EventTeamUpdated  = "team.updated"
	EventTeamArchived = "team.archived"
	EventTeamRestored = "team.restored"
	EventBotDeleted   = "bot.deleted"
	EventBotAdded     = "bot.added"
	// EventBotUpdated records a change to a member itself (its avatar).
	EventBotUpdated = "bot.updated"
	// EventRoleUpdated records a change to a role's avatar.
	EventRoleUpdated     = "role.updated"
	EventMessageCreated  = "message.created"
	EventMessageUpdated  = "message.updated"
	EventRunUpdated      = "run.updated"
	EventApprovalCreated = "approval.created"
	EventApprovalUpdated = "approval.updated"
)

// ValidReasoningEffort accepts empty (the model's default) or an effort name
// as Codex spells them (lowercase letters and underscores).
func ValidReasoningEffort(effort string) bool {
	if len(effort) > 24 {
		return false
	}
	for _, r := range effort {
		if (r < 'a' || r > 'z') && r != '_' {
			return false
		}
	}
	return true
}
