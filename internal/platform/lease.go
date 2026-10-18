package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Local leases (H06): the core refreshes one file per member every
// LeaseInterval while it runs; anything acting on that member's behalf from
// outside the core — the spawned MCP bridge, a hook process — checks the
// lease locally and refuses side effects when it is stale, missing, corrupt,
// or written by another instance. The check never depends on HTTP being
// reachable.
const (
	LeaseInterval = 2 * time.Second
	LeaseTTL      = 10 * time.Second
)

// Lease status reasons; they travel to the caller as i18n keys.
const (
	LeaseReasonOK      = ""
	LeaseReasonMissing = "missing"
	LeaseReasonCorrupt = "corrupt"
	LeaseReasonExpired = "expired"
	LeaseReasonForeign = "foreign_instance"
)

// Lease is the on-disk proof that this profile's core is alive for one
// member. The epoch records which engine connection the member was driven by
// at write time — diagnostics only; freshness plus instance identity decide.
type Lease struct {
	BotID      string    `json:"botId"`
	TeamID     string    `json:"teamId"`
	InstanceID string    `json:"instanceId"`
	Epoch      string    `json:"epoch,omitempty"`
	RunID      string    `json:"runId,omitempty"` // only a currently live host execution
	UpdatedAt  time.Time `json:"updatedAt"`
}

// LeaseDir returns the lease directory of a profile root.
func LeaseDir(root string) string { return filepath.Join(root, "leases") }

func leasePath(root, botID string) string {
	return filepath.Join(LeaseDir(root), botID+".json")
}

// WriteLease stores the lease atomically (temp file then rename), so a
// reader never observes a half-written JSON.
func WriteLease(root string, lease Lease) error {
	dir := LeaseDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("lease dir: %w", err)
	}
	data, err := json.Marshal(lease)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".lease-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, leasePath(root, lease.BotID)); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// ReadLease loads one lease from a profile root and reports why it cannot be
// trusted, if it cannot. The reason is never collapsed into a boolean:
// callers must be able to say which failure happened.
func ReadLease(root, botID string, instanceID string, now time.Time) (Lease, string, error) {
	return ReadLeaseDir(LeaseDir(root), botID, instanceID, now)
}

// ReadLeaseDir is ReadLease for callers that already hold the lease
// directory. An empty instanceID skips the foreign-instance check — the
// spawned bridge cannot know the writer's instance, so for it freshness
// decides; hook processes pass the expected instance explicitly.
func ReadLeaseDir(dir, botID string, instanceID string, now time.Time) (Lease, string, error) {
	data, err := os.ReadFile(filepath.Join(dir, botID+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Lease{}, LeaseReasonMissing, nil
		}
		return Lease{}, LeaseReasonCorrupt, err
	}
	var lease Lease
	if err := json.Unmarshal(data, &lease); err != nil {
		return Lease{}, LeaseReasonCorrupt, fmt.Errorf("parse lease: %w", err)
	}
	if lease.BotID != botID {
		return Lease{}, LeaseReasonCorrupt, errors.New("lease bot mismatch")
	}
	if lease.InstanceID != instanceID && instanceID != "" {
		return Lease{}, LeaseReasonForeign, nil
	}
	if now.Sub(lease.UpdatedAt) > LeaseTTL || lease.UpdatedAt.IsZero() {
		return Lease{}, LeaseReasonExpired, nil
	}
	return lease, LeaseReasonOK, nil
}
