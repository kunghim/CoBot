package domain

import (
	"strings"
	"time"
)

// Memory is one stored working method or preference, owned by exactly one
// member of exactly one team. Models have no tool to add one: the rows come
// from earlier builds.
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

// MemorySearchMaxItems and MemorySearchMaxChars bound one retrieval so the
// caller's context stays bounded regardless of how much is stored.
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

// EventMemoryChanged is published when a memory is tombstoned.
const EventMemoryChanged = "memory.changed"
