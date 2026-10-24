package domain

import (
	"encoding/json"
	"time"
)

// Chat is a projectless Codex conversation, independent of teams and roles.
type Chat struct {
	Busy             bool      `json:"busy"`
	ArchivedAt       string    `json:"archivedAt,omitempty"`
	ArchiveOperation string    `json:"archiveOperation,omitempty"`
	ArchiveError     string    `json:"archiveError,omitempty"`
	SectionID        string    `json:"sectionId,omitempty"`
	Pinned           bool      `json:"pinned"`
	ManualOrder      int64     `json:"manualOrder"`
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	ProviderID       string    `json:"providerId"`
	Model            string    `json:"model"`
	PermissionMode   string    `json:"permissionMode"`
	ReasoningEffort  string    `json:"reasoningEffort"`
	Workspace        string    `json:"workspace"`
	Revision         int64     `json:"revision"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type ChatSection struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Order int64  `json:"order"`
}
type ChatNavigation struct {
	Revision int64         `json:"revision"`
	Sort     string        `json:"sort"`
	Grouping string        `json:"grouping"`
	Sections []ChatSection `json:"sections"`
}
type ChatListFilter struct {
	Pinned    string `json:"pinned"`
	Archived  bool   `json:"archived"`
	Sort      string `json:"sort"`
	Search    string `json:"search"`
	SectionID string `json:"sectionId"`
}

type ChatSession struct {
	ChatID   string `json:"chatId"`
	ThreadID string `json:"threadId"`
	Epoch    string `json:"epoch"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
}

type ChatMessage struct {
	ID        string    `json:"id"`
	ChatID    string    `json:"chatId"`
	RunID     string    `json:"runId"`
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

type ChatRun struct {
	StopRequested bool       `json:"stopRequested"`
	ID            string     `json:"id"`
	ChatID        string     `json:"chatId"`
	MessageID     string     `json:"messageId"`
	Status        string     `json:"status"`
	ThreadID      string     `json:"threadId"`
	TurnID        string     `json:"turnId"`
	Epoch         string     `json:"epoch"`
	ErrorKey      string     `json:"errorKey"`
	StartedAt     time.Time  `json:"startedAt"`
	EndedAt       time.Time  `json:"endedAt"`
	Steps         []ChatStep `json:"steps"`
}

type ChatStep struct {
	ID     string          `json:"id"`
	Seq    int             `json:"seq"`
	Kind   string          `json:"kind"`
	Status string          `json:"status"`
	Detail json.RawMessage `json:"detail"`
}

// ChatRequest preserves the native request separately from its reply delivery.
// Input requests use their native method rather than a new approval category.
type ChatRequest struct {
	Decision      string           `json:"decision"`
	ID            string           `json:"id"`
	ChatID        string           `json:"chatId"`
	RunID         string           `json:"runId"`
	Type          string           `json:"type"`
	Method        string           `json:"method"`
	Summary       string           `json:"summary"`
	Reason        string           `json:"reason"`
	Options       []ApprovalOption `json:"options"`
	State         string           `json:"state"`
	ResponseState string           `json:"responseState"`
	Revision      int64            `json:"revision"`
	Params        json.RawMessage  `json:"params,omitempty"`
	EngineID      json.RawMessage  `json:"-"`
	Epoch         string           `json:"-"`
}

func ChatRunActive(status string) bool {
	switch status {
	case "queued", RunStarting, RunRunning, RunWaitingApproval, RunWaitingInput, RunUnknown:
		return true
	}
	return false
}

type ChatPage struct {
	Chat       Chat          `json:"chat"`
	Messages   []ChatMessage `json:"messages"`
	Runs       []ChatRun     `json:"runs"`
	Requests   []ChatRequest `json:"requests"`
	NextCursor string        `json:"nextCursor"`
}

type ChatList struct {
	Items      []Chat `json:"items"`
	NextCursor string `json:"nextCursor"`
}
