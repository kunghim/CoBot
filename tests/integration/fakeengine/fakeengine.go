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
	SteerErr          error
	InterruptErr      error // set by tests: the engine refuses turn/interrupt
	ReplyErr          error
	BeforeReplyReturn func()
	ResumeErr         error
	TurnFacts         map[string]string // exact thread/turn -> scripted native fact
	threads           map[string]bool
	loaded            map[string]bool
	resumes           int
	instructions      []string
	parents           map[string]string
	startedTurns      []StartedTurn
	interrupted       []string
	steers            []Steer
	replied           []Reply
	ShutdownCalls     int
	// ThreadProviders records the provider each thread was started or
	// resumed with, so a test can see which one CoBot asked for.
	ThreadProviders []string
	// ThreadConfigs records the config each thread was started or resumed
	// with, in the same order.
	ThreadConfigs []map[string]any
	archived      []string
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
	ThreadID   string
	TurnID     string
	Text       string
	Model      string
	Permission string
	Effort     string
}

// Reply records one approval answer.
type Reply struct {
	RequestID json.RawMessage
	Response  map[string]any
}

// New creates a fake server with the given callbacks.
func New(callbacks engine.Callbacks, epoch string) *Server {
	return &Server{epoch: epoch, callbacks: callbacks, threads: map[string]bool{}, loaded: map[string]bool{}}
}

// ThreadLoaded mirrors the Codex adapter: a thread started or resumed here
// stays loaded until Unload.
func (s *Server) ThreadLoaded(threadID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loaded[threadID]
}

// Unload plays Codex closing an idle thread.
func (s *Server) Unload(threadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.loaded, threadID)
}

// SpawnSubAgent plays a member's thread spawning a Codex sub-agent.
func (s *Server) SpawnSubAgent(child, parent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parents == nil {
		s.parents = map[string]string{}
	}
	s.parents[child] = parent
}

// RootThread mirrors the Codex adapter: a sub-agent leads to its member.
func (s *Server) RootThread(threadID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < 16; i++ {
		parent, ok := s.parents[threadID]
		if !ok {
			return threadID
		}
		threadID = parent
	}
	return threadID
}

// Instructions lists the developer instructions of every started thread.
func (s *Server) Instructions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.instructions...)
}

// Resumes counts thread/resume calls.
func (s *Server) Resumes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumes
}

func (s *Server) Epoch() string { return s.epoch }

func (s *Server) StartThread(ctx context.Context, spec engine.ThreadSpec) (engine.ThreadInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threadSeq++
	id := fmt.Sprintf("thread-%d", s.threadSeq)
	s.threads[id] = true
	s.loaded[id] = true
	s.instructions = append(s.instructions, spec.DeveloperInstructions)
	s.ThreadProviders = append(s.ThreadProviders, spec.ModelProvider)
	s.ThreadConfigs = append(s.ThreadConfigs, spec.Config)
	return engine.ThreadInfo{ThreadID: id, Cwd: spec.Cwd, Model: answeredModel(spec.Model), ModelProvider: answeredProvider(spec.ModelProvider), Path: TranscriptPath(id)}, nil
}

func (s *Server) ResumeThread(ctx context.Context, threadID string, spec engine.ThreadSpec) (engine.ThreadInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resumes++
	if s.ResumeErr != nil {
		return engine.ThreadInfo{}, s.ResumeErr
	}
	if !s.threads[threadID] {
		return engine.ThreadInfo{}, fmt.Errorf("unknown thread %s", threadID)
	}
	s.loaded[threadID] = true
	s.ThreadProviders = append(s.ThreadProviders, spec.ModelProvider)
	s.ThreadConfigs = append(s.ThreadConfigs, spec.Config)
	return engine.ThreadInfo{ThreadID: threadID, Model: answeredModel(spec.Model), ModelProvider: answeredProvider(spec.ModelProvider), Path: TranscriptPath(threadID)}, nil
}

// answeredProvider is the provider the engine reports for a thread. Codex
// names its own account "openai" when a thread asks for none.
func answeredProvider(asked string) string {
	if asked == "" {
		return "openai"
	}
	return asked
}

// ArchiveThread plays the engine setting a thread aside, so it can be loaded
// again with another provider.
// TranscriptPath is where the fake engine says a thread's record is.
func TranscriptPath(threadID string) string {
	return "C:/fake-codex/sessions/rollout-" + threadID + ".jsonl"
}

// answeredModel is the model the engine reports for a thread: the one asked
// for, or its own default.
func answeredModel(model string) string {
	if model == "" {
		return "fake-model"
	}
	return model
}

// ThreadPath reads a thread's record path back, as thread/read does.
func (s *Server) ThreadPath(ctx context.Context, threadID string) (string, error) {
	return TranscriptPath(threadID), nil
}

func (s *Server) ArchiveThread(ctx context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.archived = append(s.archived, threadID)
	delete(s.loaded, threadID)
	return nil
}

// UnarchiveThread plays the engine taking a thread back out of its archive.
func (s *Server) UnarchiveThread(ctx context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nil
}

// Archived lists the threads the engine was asked to set aside.
func (s *Server) Archived() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.archived...)
}

func (s *Server) StartTurn(ctx context.Context, threadID, text string, opts engine.TurnOptions) (string, error) {
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
	s.startedTurns = append(s.startedTurns, StartedTurn{ThreadID: threadID, TurnID: turnID, Text: text, Model: opts.Model, Permission: opts.Permission, Effort: opts.Effort})
	return turnID, nil
}

// Steer is one input added to a running turn.
type Steer struct {
	ThreadID, TurnID, Text string
}

// SteerTurn records the input; SteerErr makes the engine refuse, as Codex
// does once the expected turn has ended.
func (s *Server) SteerTurn(ctx context.Context, threadID, turnID, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SteerErr != nil {
		return s.SteerErr
	}
	s.steers = append(s.steers, Steer{ThreadID: threadID, TurnID: turnID, Text: text})
	return nil
}

// Steers returns the inputs added to running turns so far.
func (s *Server) Steers() []Steer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Steer(nil), s.steers...)
}

func (s *Server) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	if threadID == "" || turnID == "" {
		return fmt.Errorf("interrupt requires threadId and turnId")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.InterruptErr != nil {
		return s.InterruptErr
	}
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
