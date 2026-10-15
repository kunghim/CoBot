// Bot bridge credentials live beside the other credential stores so that the
// application layer never imports the collaboration package: the dispatcher
// depends on the application, not the other way round.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrTokenUnknown reports a token that was never issued or belongs to a
// previous core lifetime.
// Environment names the bridge reads. The core passes these when it hands a
// thread to the engine; they live here so the application layer can build the
// thread config without importing the collaboration package.
const (
	BridgeEnvEndpoint = "COBOT_ENDPOINT"
	BridgeEnvToken    = "COBOT_BOT_TOKEN"
)

var ErrTokenUnknown = errors.New("bot token unknown or expired")

// BotBinding is the identity a bridge token resolves to. It is minted by the
// core when a thread is created and is only valid for this core lifetime, so
// a restarted core never accepts a stale token.
type BotBinding struct {
	Token       string
	BotID       string
	TeamID      string
	EngineEpoch string
	ExpiresAt   time.Time
}

// TokenStore keeps bot bridge tokens in memory only. Tokens never enter the
// business database, logs or metadata.
type TokenStore struct {
	mu     sync.Mutex
	tokens map[string]BotBinding
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenStore creates a store with the given lifetime.
func NewTokenStore(ttl time.Duration) *TokenStore {
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	return &TokenStore{tokens: map[string]BotBinding{}, ttl: ttl, now: time.Now}
}

// Issue mints a fresh token bound to one bot's current engine epoch.
func (s *TokenStore) Issue(botID, teamID, engineEpoch string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate bot token: %w", err)
	}
	token := hex.EncodeToString(buf)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.tokens[token] = BotBinding{
		Token: token, BotID: botID, TeamID: teamID,
		EngineEpoch: engineEpoch, ExpiresAt: s.now().Add(s.ttl),
	}
	return token, nil
}

// Resolve validates a token and returns its binding.
func (s *TokenStore) Resolve(token string) (BotBinding, error) {
	if token == "" {
		return BotBinding{}, ErrTokenUnknown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.tokens[token]
	if !ok {
		return BotBinding{}, ErrTokenUnknown
	}
	if s.now().After(binding.ExpiresAt) {
		delete(s.tokens, token)
		return BotBinding{}, ErrTokenUnknown
	}
	return binding, nil
}

// RevokeOfBot drops every token issued for one bot; called when the bot's
// engine connection changes so a stale bridge cannot keep writing.
func (s *TokenStore) RevokeOfBot(botID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, binding := range s.tokens {
		if binding.BotID == botID {
			delete(s.tokens, token)
		}
	}
}

func (s *TokenStore) sweepLocked() {
	now := s.now()
	for token, binding := range s.tokens {
		if now.After(binding.ExpiresAt) {
			delete(s.tokens, token)
		}
	}
}
