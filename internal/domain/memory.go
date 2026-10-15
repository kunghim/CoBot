package domain

import (
	"strings"
	"time"
)

// Memory is one stored working method or preference, owned by exactly one
// member of exactly one team. It never holds task evidence, secrets or
// large source excerpts; the write path enforces size and refuses
// secret-shaped bodies (G05).
type Memory struct {
	ID           string    `json:"id"`
	TeamID       string    `json:"teamId"`
	BotID        string    `json:"botId"`
	Body         string    `json:"body"`
	SourceRunID  string    `json:"sourceRunId,omitempty"`
	SourceTaskID string    `json:"sourceTaskId,omitempty"`
	NormKey      string    `json:"-"`
	DeletedAt    string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

// IsLive reports whether the memory participates in retrieval. A tombstone
// stays in the table purely so the dedupe key blocks re-creation.
func (m Memory) IsLive() bool { return m.DeletedAt == "" }

// MemoryLimitMaxBody is the per-memory size cap. Larger bodies are refused
// rather than truncated silently: a half-remembered method is worse than
// none.
const MemoryLimitMaxBody = 2000

// MemorySearchMaxItems and MemorySearchMaxChars bound one retrieval so the
// caller's context stays bounded regardless of how much is stored (G05).
const (
	MemorySearchMaxItems = 10
	MemorySearchMaxChars = 6000
)

// NormalizeMemoryBody produces the dedupe key: case-folded, whitespace
// collapsed, trimmed. Two bodies that differ only in spacing or case are
// the same remembered fact.
func NormalizeMemoryBody(body string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(body)))
	return strings.Join(fields, " ")
}

// MemoryErrorKeys are the i18n keys the write path can return.
const (
	ErrorMemoryTooLong      = "error.memory.too_long"
	ErrorMemoryLooksSecret  = "error.memory.looks_secret"
	ErrorMemoryDuplicate    = "error.memory.duplicate"
	ErrorMemoryBodyRequired = "error.memory.body_required"
)

// EventMemoryChanged is published when a memory is stored or tombstoned.
const EventMemoryChanged = "memory.changed"
