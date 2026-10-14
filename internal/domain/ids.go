package domain

import "github.com/google/uuid"

// NewID returns a fresh UUID string used for every public resource id.
func NewID() string {
	return uuid.NewString()
}

// NewEpochID returns the identifier of one core/app-server lifetime.
func NewEpochID() string {
	return uuid.NewString()
}
