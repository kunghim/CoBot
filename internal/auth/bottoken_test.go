package auth

import (
	"testing"
	"time"
)

func TestBotCredentialRenewRotationAndRevocation(t *testing.T) {
	now := time.Now()
	store := NewTokenStore(time.Minute)
	store.now = func() time.Time { return now }
	token, _ := store.IssueForRun("bot", "team", "epoch", "run-1")
	for range 5 {
		now = now.Add(40 * time.Second)
		store.RefreshRun("bot", "epoch", "run-1")
		if _, err := store.Resolve(token); err != nil {
			t.Fatal("live execution expired", err)
		}
	}
	store.RefreshRun("bot", "other-epoch", "run-1")
	now = now.Add(61 * time.Second)
	store.RefreshRun("bot", "epoch", "run-1")
	if _, err := store.Resolve(token); err == nil {
		t.Fatal("expired execution resurrected")
	}
	old, _ := store.IssueForRun("bot", "team", "epoch", "run-2")
	current, _ := store.IssueForRun("bot", "team", "epoch", "run-3")
	if _, err := store.Resolve(old); err == nil {
		t.Fatal("predecessor survived activation")
	}
	store.RevokeRun("run-3")
	store.RefreshRun("bot", "epoch", "run-3")
	if _, err := store.Resolve(current); err == nil {
		t.Fatal("revocation undone")
	}
}

func TestRetainedBridgeGrantExpiresAndOnlyHostReactivates(t *testing.T) {
	now := time.Now()
	store := NewTokenStore(time.Minute)
	store.now = func() time.Time { return now }
	key, _ := store.ActivateBridgeRun("bot", "team", "epoch", "run-1")
	store.RevokeRun("run-1")
	if _, err := store.Resolve(key); err == nil {
		t.Fatal("ended grant remained usable")
	}
	now = now.Add(24 * time.Hour)
	store.RefreshRun("bot", "epoch", "run-1")
	if _, err := store.Resolve(key); err == nil {
		t.Fatal("lease tick resurrected inactive grant")
	}
	next, err := store.ActivateBridgeRun("bot", "team", "epoch", "run-2")
	if err != nil || next != key {
		t.Fatal("retained process lost transport identity", err)
	}
	binding, _ := store.Resolve(key)
	if binding.RunID != "run-2" || !binding.NativeMetadata {
		t.Fatal("host run missing")
	}
	store.RevokeOfBot("bot")
	if _, err := store.Resolve(key); err == nil {
		t.Fatal("explicit revocation ignored")
	}
	if _, err := store.ActivateBridgeRun("bot", "team", "epoch", "run-3"); err == nil {
		t.Fatal("revoked retained process reauthorized")
	}
	fresh, err := store.ActivateBridgeRun("bot", "team", "new-epoch", "run-3")
	if err != nil || fresh == key {
		t.Fatal("new process requires new identity", err)
	}
}
