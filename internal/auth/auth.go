// Package auth issues and verifies page sessions, desktop bearer tokens and
// one-time bootstrap secrets. All tokens live in memory only: a restarted
// core invalidates every previous authorization by design.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// DefaultBootstrapTTL matches the product rule: the one-time startup secret
// is valid for 60 seconds and can be redeemed exactly once.
const DefaultBootstrapTTL = 60 * time.Second

// DefaultSessionTTL is the page session lifetime; the browser re-authorizes
// through a fresh startup link after restart or expiry.
const DefaultSessionTTL = 12 * time.Hour

// DefaultBearerTTL is the desktop bearer lifetime (30 minutes, renewable).
const DefaultBearerTTL = 30 * time.Minute

func randomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---- bootstrap secrets ----

// BootstrapIssuer mints and redeems single-use secrets. Only hashes are kept.
type BootstrapIssuer struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]time.Time // sha256(secret) -> expiry
	now     func() time.Time
}

// NewBootstrapIssuer creates an issuer with the given TTL.
func NewBootstrapIssuer(ttl time.Duration) *BootstrapIssuer {
	if ttl <= 0 {
		ttl = DefaultBootstrapTTL
	}
	return &BootstrapIssuer{ttl: ttl, entries: map[string]time.Time{}, now: time.Now}
}

// Issue returns a fresh raw secret; the caller delivers it out of band
// (printed startup link, desktop bridge hand-off).
func (i *BootstrapIssuer) Issue() (string, error) {
	secret, err := randomToken(32)
	if err != nil {
		return "", err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.sweepLocked()
	i.entries[hashToken(secret)] = i.now().Add(i.ttl)
	return secret, nil
}

// Redeem validates and consumes a secret. Reuse returns an error because the
// entry is deleted on first success.
func (i *BootstrapIssuer) Redeem(secret string) error {
	if secret == "" {
		return fmt.Errorf("empty bootstrap secret")
	}
	key := hashToken(secret)
	i.mu.Lock()
	defer i.mu.Unlock()
	i.sweepLocked()
	exp, ok := i.entries[key]
	if !ok {
		return fmt.Errorf("bootstrap secret unknown or already used")
	}
	delete(i.entries, key)
	if i.now().After(exp) {
		return fmt.Errorf("bootstrap secret expired")
	}
	return nil
}

func (i *BootstrapIssuer) sweepLocked() {
	now := i.now()
	for k, exp := range i.entries {
		if now.After(exp) {
			delete(i.entries, k)
		}
	}
}

// ---- page sessions ----

// Session is one authorized browser page session.
type Session struct {
	ID        string
	CSRF      string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionStore keeps page sessions in memory.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
	ttl      time.Duration
	now      func() time.Time
}

// NewSessionStore creates an empty store.
func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &SessionStore{sessions: map[string]*Session{}, ttl: ttl, now: time.Now}
}

// Create registers a new session and returns its tokens.
func (s *SessionStore) Create(userID string) (*Session, error) {
	id, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	now := s.now()
	sess := &Session{ID: id, CSRF: csrf, UserID: userID, CreatedAt: now, ExpiresAt: now.Add(s.ttl)}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.sessions[id] = sess
	return sess, nil
}

// Get returns a live session.
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if s.now().After(sess.ExpiresAt) {
		delete(s.sessions, id)
		return nil, false
	}
	return sess, true
}

// Delete removes a session (logout).
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *SessionStore) sweepLocked() {
	now := s.now()
	for id, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

// ---- desktop bearer tokens ----

// BearerToken authorizes the desktop shell against loopback APIs.
type BearerToken struct {
	Token     string
	UserID    string
	ExpiresAt time.Time
}

// BearerStore keeps desktop tokens in memory.
type BearerStore struct {
	mu     sync.Mutex
	tokens map[string]*BearerToken
	ttl    time.Duration
	now    func() time.Time
}

// NewBearerStore creates an empty store.
func NewBearerStore(ttl time.Duration) *BearerStore {
	if ttl <= 0 {
		ttl = DefaultBearerTTL
	}
	return &BearerStore{tokens: map[string]*BearerToken{}, ttl: ttl, now: time.Now}
}

// Issue creates a new bearer token for the desktop host.
func (b *BearerStore) Issue(userID string) (*BearerToken, error) {
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	t := &BearerToken{Token: token, UserID: userID, ExpiresAt: b.now().Add(b.ttl)}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	b.tokens[token] = t
	return t, nil
}

// Get returns a live token.
func (b *BearerStore) Get(token string) (*BearerToken, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.tokens[token]
	if !ok {
		return nil, false
	}
	if b.now().After(t.ExpiresAt) {
		delete(b.tokens, token)
		return nil, false
	}
	return t, true
}

// Renew extends a live token, returning its new expiry.
func (b *BearerStore) Renew(token string) (time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.tokens[token]
	if !ok {
		return time.Time{}, false
	}
	if b.now().After(t.ExpiresAt) {
		delete(b.tokens, token)
		return time.Time{}, false
	}
	t.ExpiresAt = b.now().Add(b.ttl)
	return t.ExpiresAt, true
}

// Delete removes one token.
func (b *BearerStore) Delete(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.tokens, token)
}

func (b *BearerStore) sweepLocked() {
	now := b.now()
	for token, t := range b.tokens {
		if now.After(t.ExpiresAt) {
			delete(b.tokens, token)
		}
	}
}

// ---- principals ----

// Kind discriminates the two credential families. Bearer and cookie
// credentials are never interchangeable with bot credentials.
type Kind string

const (
	KindUser  Kind = "user"
	KindBot   Kind = "bot"
	KindLocal Kind = "local" // same-user runtime credential (internal routes)
)

// Principal is the authenticated identity of one request.
type Principal struct {
	Kind      Kind
	UserID    string
	BotID     string
	TeamID    string
	SessionID string
}

// The first version has a single local user; the id is stable per profile
// session but tokens never leave memory.
const LocalUserID = "user-local"

// NewPrincipalUser wraps the local user identity.
func NewPrincipalUser(sessionID string) Principal {
	return Principal{Kind: KindUser, UserID: LocalUserID, SessionID: sessionID}
}

// NewPrincipalBot wraps a bot identity bound to its team (MCP credentials).
func NewPrincipalBot(botID, teamID string) Principal {
	return Principal{Kind: KindBot, BotID: botID, TeamID: teamID}
}

// NewPrincipalLocal wraps the same-user runtime credential.
func NewPrincipalLocal() Principal {
	return Principal{Kind: KindLocal, UserID: LocalUserID}
}

// CompareSecret reports equality without leaking timing; used for the local
// runtime secret.
func CompareSecret(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
