package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLeaseLifecycle pins the four failure reasons the side-effect gate depends on: missing,
// corrupt, expired and foreign-instance all stay distinguishable, and a
// freshly written lease is valid for this instance.
func TestLeaseLifecycle(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	lease := Lease{BotID: "bot-1", TeamID: "team-1", InstanceID: "instance-a", Epoch: "epoch-1", UpdatedAt: now}

	if _, reason, _ := ReadLease(root, "bot-1", "instance-a", now); reason != LeaseReasonMissing {
		t.Fatalf("absent lease = %q, want missing", reason)
	}
	if err := WriteLease(root, lease); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, reason, err := ReadLease(root, "bot-1", "instance-a", now)
	if err != nil || reason != LeaseReasonOK {
		t.Fatalf("fresh lease = reason %q err %v", reason, err)
	}
	if got.BotID != "bot-1" || got.Epoch != "epoch-1" {
		t.Fatalf("lease round-trip lost fields: %+v", got)
	}

	// Another instance's lease is foreign, not merely expired.
	foreign := lease
	foreign.InstanceID = "instance-b"
	if err := WriteLease(root, foreign); err != nil {
		t.Fatalf("write foreign: %v", err)
	}
	if _, reason, _ := ReadLease(root, "bot-1", "instance-a", now); reason != LeaseReasonForeign {
		t.Fatalf("foreign lease = %q, want foreign_instance", reason)
	}
	// A bridge that cannot know the writer's instance trusts freshness only.
	if _, reason, _ := ReadLeaseDir(LeaseDir(root), "bot-1", "", now); reason != LeaseReasonOK {
		t.Fatalf("fresh lease without instance check = %q, want ok", reason)
	}

	// Past the TTL the same lease is expired.
	if err := WriteLease(root, lease); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, reason, _ := ReadLease(root, "bot-1", "instance-a", now.Add(LeaseTTL+time.Second)); reason != LeaseReasonExpired {
		t.Fatalf("stale lease = %q, want expired", reason)
	}

	// Unparseable bytes are corrupt.
	if err := os.WriteFile(filepath.Join(LeaseDir(root), "bot-1.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, reason, _ := ReadLease(root, "bot-1", "instance-a", now); reason != LeaseReasonCorrupt {
		t.Fatalf("corrupt lease = %q, want corrupt", reason)
	}

	// A lease naming a different bot is corrupt: it can never vouch for this
	// member.
	mismatch, _ := json.Marshal(Lease{BotID: "bot-2", TeamID: "team-1", InstanceID: "instance-a", UpdatedAt: now})
	if err := os.WriteFile(filepath.Join(LeaseDir(root), "bot-1.json"), mismatch, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, reason, _ := ReadLease(root, "bot-1", "instance-a", now); reason != LeaseReasonCorrupt {
		t.Fatalf("mismatched lease = %q, want corrupt", reason)
	}
}
