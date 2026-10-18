package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistentSessionsSurviveReloadAndRenew(t *testing.T) {
	file := filepath.Join(t.TempDir(), "credentials", "page-sessions.json")
	now := time.Now()
	store := NewPersistentSessionStore(10*time.Hour, file)
	store.now = func() time.Time { return now }
	sess, err := store.Create(LocalUserID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), sess.ID) {
		t.Fatal("saved file must not hold the raw session cookie")
	}

	reloaded := NewPersistentSessionStore(10*time.Hour, file)
	reloaded.now = func() time.Time { return now }
	got, ok := reloaded.Get(sess.ID)
	if !ok || got.CSRF != sess.CSRF {
		t.Fatal("session must survive a reload")
	}
	if _, renewed, _ := reloaded.Refresh(sess.ID); renewed {
		t.Fatal("a fresh session needs no renewal")
	}
	now = now.Add(6 * time.Hour)
	renewedSess, renewed, ok := reloaded.Refresh(sess.ID)
	if !ok || !renewed || !renewedSess.ExpiresAt.Equal(now.Add(10*time.Hour)) {
		t.Fatal("a session past half its lifetime must be extended")
	}

	reloaded.Delete(sess.ID)
	again := NewPersistentSessionStore(10*time.Hour, file)
	if _, ok := again.Get(sess.ID); ok {
		t.Fatal("a signed-out session must stay gone after reload")
	}
}

func TestBootstrapSingleUseAndExpiry(t *testing.T) {
	now := time.Now()
	issuer := NewBootstrapIssuer(time.Minute)
	issuer.now = func() time.Time { return now }

	secret, err := issuer.Issue()
	if err != nil {
		t.Fatal(err)
	}
	if err := issuer.Redeem(secret); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if err := issuer.Redeem(secret); err == nil {
		t.Fatal("reuse of a redeemed secret must fail")
	}

	future, _ := issuer.Issue()
	now = now.Add(2 * time.Minute)
	if err := issuer.Redeem(future); err == nil {
		t.Fatal("expired secret must fail")
	}

	// A wrong secret is rejected without consuming anything.
	other, _ := issuer.Issue()
	if err := issuer.Redeem("deadbeef"); err == nil {
		t.Fatal("forged secret must fail")
	}
	if err := issuer.Redeem(other); err != nil {
		t.Fatalf("valid secret was consumed by a forged attempt: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	now := time.Now()
	store := NewSessionStore(time.Minute)
	store.now = func() time.Time { return now }

	sess, err := store.Create(LocalUserID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.CSRF == "" || sess.ID == "" || sess.CSRF == sess.ID {
		t.Fatal("session and CSRF tokens must be distinct non-empty values")
	}
	if got, ok := store.Get(sess.ID); !ok || got.UserID != LocalUserID {
		t.Fatal("live session must resolve")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := store.Get(sess.ID); ok {
		t.Fatal("expired session must not resolve")
	}
}

func TestBearerRenewAndExpire(t *testing.T) {
	now := time.Now()
	store := NewBearerStore(time.Minute)
	store.now = func() time.Time { return now }

	token, err := store.Issue(LocalUserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(token.Token); !ok {
		t.Fatal("issued bearer must resolve")
	}
	now = now.Add(30 * time.Second)
	exp, ok := store.Renew(token.Token)
	if !ok || exp.Before(now.Add(time.Minute)) {
		t.Fatalf("renew must extend expiry, got %v ok=%v", exp, ok)
	}
	now = now.Add(2 * time.Minute)
	if _, ok := store.Get(token.Token); ok {
		t.Fatal("unrenewed expiry must drop the token")
	}
	if _, ok := store.Renew(token.Token); ok {
		t.Fatal("renew of expired token must fail")
	}
}
