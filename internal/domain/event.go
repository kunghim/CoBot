package domain

import (
	"encoding/json"
	"time"
)

// Event is a durable product fact recorded inside the same transaction as the
// change that caused it. Clients replay events after a snapshot cursor; chunk
// notifications are transient and never stored here.
type Event struct {
	Seq        int64           `json:"seq"`
	ID         string          `json:"id"`
	InstanceID string          `json:"instanceId"`
	Type       string          `json:"type"`
	TeamID     string          `json:"teamId"`
	BotID      string          `json:"botId"`
	ResourceID string          `json:"resourceId"`
	Revision   int64           `json:"revision"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// Event types recorded in the first version; new types are appended, never
// renamed, because clients match on the wire string.
const (
	EventSettingsUpdated = "settings.updated"
	// EventLogGap records that local event files ran past their budget with
	// un-imported evidence: the evidence stays on disk and the gap is
	// visible instead of silent (H08).
	EventLogGap = "log.gap"
	// EventLogQuarantined records that a corrupt ledger file was isolated
	// after its valid lines were imported (H08).
	EventLogQuarantined = "log.quarantined"
)

// Scope filters an event query by team and/or bot. Empty means "all visible
// to the caller"; the server decides the projection, not the client filter.
type Scope struct {
	TeamID string
	BotID  string
}
