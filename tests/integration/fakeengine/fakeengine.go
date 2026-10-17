// Package fakeengine provides a controllable engine.Server for integration
// tests. It never talks to a real model or account; production code cannot
// reach it (the fake lives under tests/).
package fakeengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
)

// Server is a scriptable fake app-server.
type Server struct {
	mu sync.Mutex

	epoch     string
	threadSeq int
	turnSeq   int
	closed    bool
	callbacks engine.Callbacks

	// Scripted behavior.
	BeforeStartTurn   func() error
	StartTurnErr      error
	ReplyErr          error
	BeforeReplyReturn func()
	ResumeErr         error
	TurnFacts         map[string]string // exact thread/turn -> scripted native fact
	threads           map[string]bool
	startedTurns      []StartedTurn
	interrupted       []string
	replied           []Reply
	ShutdownCalls     int
}

func (s *Server) SetTurnFact(threadID, turnID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.TurnFacts == nil {
		s.TurnFacts = map[string]string{}
	}
	s.TurnFacts[threadID+"/"+turnID] = status
}

func (s *Server) InspectTurn(ctx context.Context, threadID, turnID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.TurnFacts[threadID+"/"+turnID]
	if !ok {
		return "", fmt.Errorf("exact turn not found")
	}
	return status, nil
}

// StartedTurn records one accepted turn.
type StartedTurn struct {
	ThreadID string
	TurnID   string
	Text     string
	Model    string
}

// Reply records one approval answer.
type Reply struct {
	RequestID json.RawMessage
	Response  map[string]any
}

// New creates a fake server with the given callbacks.
func New(callbacks engine.Callbacks, epoch string) *Server {
	return &Server{epoch: epoch, callbacks: callbacks, threads: map[string]bool{}}
}

func (s *Server) Epoch() string { return s.epoch }

func (s *Server) StartThread(ctx context.Context, spec engine.ThreadSpec) (engine.ThreadInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threadSeq++
	id := fmt.Sprintf("thread-%d", s.threadSeq)
	s.threads[id] = true
	return engine.ThreadInfo{ThreadID: id, Cwd: spec.Cwd, Model: "fake-model", ModelProvider: "fake"}, nil
}

func (s *Server) ResumeThread(ctx context.Context, threadID string, spec engine.ThreadSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ResumeErr != nil {
		return s.ResumeErr
	}
	if !s.threads[threadID] {
		return fmt.Errorf("unknown thread %s", threadID)
	}
	return nil
}

func (s *Server) StartTurn(ctx context.Context, threadID, text, model string) (string, error) {
	if s.BeforeStartTurn != nil {
		if err := s.BeforeStartTurn(); err != nil {
			return "", err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.StartTurnErr != nil {
		return "", s.StartTurnErr
	}
	s.turnSeq++
	turnID := fmt.Sprintf("turn-%d", s.turnSeq)
	s.startedTurns = append(s.startedTurns, StartedTurn{ThreadID: threadID, TurnID: turnID, Text: text, Model: model})
	return turnID, nil
}

func (s *Server) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	if threadID == "" || turnID == "" {
		return fmt.Errorf("interrupt requires threadId and turnId")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interrupted = append(s.interrupted, threadID)
	return nil
}

// ReplyErr (set by tests) simulates a failed RPC delivery so the product's
// "decision kept, response unknown" path (F03) is exercised for real.
func (s *Server) ReplyApproval(ctx context.Context, requestID json.RawMessage, response map[string]any) error {
	s.mu.Lock()
	if s.ReplyErr != nil {
		s.mu.Unlock()
		return s.ReplyErr
	}
	s.replied = append(s.replied, Reply{RequestID: requestID, Response: response})
	hook := s.BeforeReplyReturn
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (s *Server) AccountRead(ctx context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"account":{"type":"chatgpt"}}`), nil
}

func (s *Server) AccountLoginStart(ctx context.Context) (engine.LoginInfo, error) {
	return engine.LoginInfo{Type: "chatgpt", LoginID: "fake-login", AuthURL: "https://example.invalid/login"}, nil
}
func (s *Server) AccountLoginCancel(ctx context.Context, loginID string) error {
	if loginID == "" {
		return fmt.Errorf("loginId required")
	}
	return nil
}

func (s *Server) ModelList(ctx context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"data":[{"id":"fake-model","model":"fake-model","displayName":"Fake Model"}],"nextCursor":null}`), nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ShutdownCalls++
	s.closed = true
	return nil
}

// ---- test controls ----

// Turns returns the accepted turns so far.
func (s *Server) Turns() []StartedTurn {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]StartedTurn, len(s.startedTurns))
	copy(out, s.startedTurns)
	return out
}

// Replies returns the approval replies so far.
func (s *Server) Replies() []Reply {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Reply, len(s.replied))
	copy(out, s.replied)
	return out
}

// Interrupts returns the interrupted thread ids.
func (s *Server) Interrupts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.interrupted))
	copy(out, s.interrupted)
	return out
}

// EmitTurnCompleted simulates the engine finishing a turn.
func (s *Server) EmitTurnCompleted(threadID, turnID, status, assistantText string) {
	if assistantText != "" {
		item, _ := json.Marshal(map[string]any{"type": "agentMessage", "id": "item-1", "text": assistantText})
		params, _ := json.Marshal(map[string]any{"completedAtMs": 1, "item": item, "threadId": threadID, "turnId": turnID})
		s.callbacks.OnNotification(engine.Notification{
			Method: "item/completed", ThreadID: threadID, TurnID: turnID,
			ItemID: "item-1", ItemType: "agentMessage", Text: assistantText, Raw: params,
		})
	}
	params, _ := json.Marshal(map[string]any{
		"threadId": threadID,
		"turn":     map[string]any{"id": turnID, "status": status, "items": []any{}},
	})
	s.callbacks.OnNotification(engine.Notification{
		Method: "turn/completed", ThreadID: threadID, TurnID: turnID, TurnStatus: status, Raw: params,
	})
}

// EmitTokenUsage simulates the engine reporting this turn's newest token
// snapshot. The payload matches thread/tokenUsage/updated.
func (s *Server) EmitTokenUsage(threadID, turnID string, input, output, cached int64) {
	params, _ := json.Marshal(map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"tokenUsage": map[string]any{
			"last": map[string]any{
				"inputTokens":       input,
				"outputTokens":      output,
				"cachedInputTokens": cached,
			},
			"total": map[string]any{},
		},
	})
	s.callbacks.OnNotification(engine.Notification{
		Method: "thread/tokenUsage/updated", ThreadID: threadID, TurnID: turnID, TokenUsage: params,
	})
}

// EmitApproval simulates a native tool approval request.
func (s *Server) EmitApproval(requestID string, threadID, turnID, command string) {
	params, _ := json.Marshal(map[string]any{
		"threadId": threadID, "turnId": turnID, "itemId": "item-2",
		"command": command, "cwd": "/tmp", "reason": "test",
	})
	s.callbacks.OnApproval(engine.ApprovalRequest{
		RequestID: json.RawMessage(`"` + requestID + `"`),
		Method:    "item/commandExecution/requestApproval",
		ThreadID:  threadID, TurnID: turnID, ItemID: "item-2",
		Command: command, Cwd: "/tmp", Reason: "test", RawParams: params,
	})
}

// EmitExit simulates a crash of the app-server process.
func (s *Server) EmitExit(err error) {
	s.callbacks.OnExit(err)
}

var _ engine.Server = (*Server)(nil)
var _ = domain.BotStatusIdle

// Emit delivers an arbitrary normalized notification, for progress events
// (steps, deltas, problems) a scripted turn reports before it completes.
func (s *Server) Emit(n engine.Notification) {
	s.callbacks.OnNotification(n)
}
