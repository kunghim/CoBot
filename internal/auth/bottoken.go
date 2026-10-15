// Bot bridge credentials live beside the other credential stores so that the
// application layer never imports the collaboration package: the dispatcher
// depends on the application, not the other way round.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrTokenUnknown reports a token that was never issued or belongs to a
// previous core lifetime.
// Environment names the bridge reads. The core passes these when it hands a
// thread to the engine; they live here so the application layer can build the
// thread config without importing the collaboration package.
// BridgeEnvBotID and BridgeEnvLeaseDir tell the spawned bridge which member
// it acts for and where the local leases live, so it can refuse side effects
// from a stale executor without depending on HTTP (H06).
const (
	BridgeEnvBotID    = "COBOT_BOT_ID"
	BridgeEnvLeaseDir = "COBOT_LEASE_DIR"
)

const (
	BridgeEnvEndpoint = "COBOT_ENDPOINT"
	BridgeEnvToken    = "COBOT_BOT_TOKEN"
)

var ErrTokenUnknown = errors.New("bot token unknown or expired")

// BotBinding is the identity a bridge token resolves to. It is minted by the
// core when a thread is created and is only valid for this core lifetime, so
// a restarted core never accepts a stale token.
type BotBinding struct {
	Token          string
	BotID          string
	TeamID         string
	EngineEpoch    string
	RunID          string
	NativeMetadata bool
	ExpiresAt      time.Time
}

// TokenStore keeps bot bridge tokens in memory only. Tokens never enter the
// business database, logs or metadata.
type TokenStore struct {
	mu      sync.Mutex
	tokens  map[string]BotBinding
	revoked map[string]bool // bot/epoch whose retained bridge was explicitly revoked
	ttl     time.Duration
	now     func() time.Time
}

// NewTokenStore creates a store with the given lifetime.
func NewTokenStore(ttl time.Duration) *TokenStore {
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	return &TokenStore{tokens: map[string]BotBinding{}, revoked: map[string]bool{}, ttl: ttl, now: time.Now}
}

// Issue mints a fresh token bound to one bot's current engine epoch.
func (s *TokenStore) Issue(botID, teamID, engineEpoch string) (string, error) {
	return s.IssueForRun(botID, teamID, engineEpoch, "")
}

// ActivateBridgeRun grants an execution lease to Codex's retained MCP
// process. Only the host can activate it; native per-call turn identity is
// required as well. Idle/expired grants reject every call.
func (s *TokenStore) ActivateBridgeRun(botID, teamID, epoch, runID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revoked[botID+"/"+epoch] {
		return "", errors.New("retained bridge was revoked; restart its engine before reauthorizing")
	}
	for key, binding := range s.tokens {
		if binding.BotID == botID && binding.NativeMetadata {
			if binding.EngineEpoch == epoch {
				binding.RunID, binding.ExpiresAt = runID, s.now().Add(s.ttl)
				s.tokens[key] = binding
				return key, nil
			}
			delete(s.tokens, key)
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := hex.EncodeToString(buf)
	s.tokens[key] = BotBinding{Token: key, BotID: botID, TeamID: teamID, EngineEpoch: epoch, RunID: runID, NativeMetadata: true, ExpiresAt: s.now().Add(s.ttl)}
	return key, nil
}

// IssueForRun rotates the member's credential when activating a thread.
func (s *TokenStore) IssueForRun(botID, teamID, engineEpoch, runID string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate bot token: %w", err)
	}
	token := hex.EncodeToString(buf)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if runID != "" {
		for key, binding := range s.tokens {
			if binding.BotID == botID && binding.RunID != "" {
				delete(s.tokens, key)
			}
		}
	}
	s.tokens[token] = BotBinding{
		Token: token, BotID: botID, TeamID: teamID,
		EngineEpoch: engineEpoch, RunID: runID, ExpiresAt: s.now().Add(s.ttl),
	}
	return token, nil
}

// RefreshRun renews only a still-live host execution. Expired/revoked tokens
// never resurrect; idle credentials are swept rather than kept alive.
func (s *TokenStore) RefreshRun(botID, epoch, runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	for key, binding := range s.tokens {
		if binding.BotID == botID && binding.EngineEpoch == epoch && binding.RunID == runID && runID != "" && binding.ExpiresAt.After(s.now()) {
			binding.ExpiresAt = s.now().Add(s.ttl)
			s.tokens[key] = binding
		}
	}
}

func (s *TokenStore) RevokeRun(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, binding := range s.tokens {
		if binding.RunID == runID {
			if binding.NativeMetadata {
				binding.RunID = ""
				binding.ExpiresAt = s.now().Add(-time.Nanosecond)
				s.tokens[key] = binding
			} else {
				delete(s.tokens, key)
			}
		}
	}
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
		if !binding.NativeMetadata {
			delete(s.tokens, token)
		}
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
			if binding.NativeMetadata {
				s.revoked[botID+"/"+binding.EngineEpoch] = true
			}
			delete(s.tokens, token)
		}
	}
}

func (s *TokenStore) sweepLocked() {
	now := s.now()
	for token, binding := range s.tokens {
		if now.After(binding.ExpiresAt) && !binding.NativeMetadata {
			delete(s.tokens, token)
		}
	}
}

// PublicBinding is the non-secret view of one live bridge credential, for
// the authorizations page. The token value itself is never exposed.
type PublicBinding struct {
	BotID       string    `json:"botId"`
	TeamID      string    `json:"teamId"`
	EngineEpoch string    `json:"engineEpoch"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// List reports every live credential; tokens are excluded by construction.
func (s *TokenStore) List() []PublicBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	out := make([]PublicBinding, 0, len(s.tokens))
	for _, binding := range s.tokens {
		if !binding.ExpiresAt.After(now) {
			continue
		}
		out = append(out, PublicBinding{
			BotID: binding.BotID, TeamID: binding.TeamID,
			EngineEpoch: binding.EngineEpoch, ExpiresAt: binding.ExpiresAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BotID < out[j].BotID })
	return out
}
