package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
)

// ServerOptions configure one team app-server process.
type ServerOptions struct {
	Binary    string   // resolved codex executable
	ExtraArgs []string // e.g. -c key=value overrides
	Cwd       string   // team working directory (startup cwd = writable root)
	Env       []string // full environment for the child; nil inherits ours
	Client    ClientInfo
	Callbacks engine.Callbacks
	Logger    *slog.Logger
}

// Server is the Codex implementation of engine.Server.
type Server struct {
	cmd       *exec.Cmd
	client    *Client
	epoch     string
	logger    *slog.Logger
	callbacks engine.Callbacks

	mu        sync.Mutex
	pending   map[string]pendingApproval // key: request-id JSON
	intents   map[string]string          // threadID -> turnID of active turn
	closed    bool
	cwd       string
	fileItems map[nativeItemKey][]engine.FileChange
}

type nativeItemKey struct{ ThreadID, TurnID, ItemID string }

type pendingApproval struct {
	threadID string
	turnID   string
}

// Start spawns the app-server, performs the handshake and returns a ready
// server. The process cwd is the team directory by design: workspace-write
// covers the startup cwd, so the boundary is set here, not per request.
func Start(ctx context.Context, opts ServerOptions) (*Server, error) {
	if opts.Binary == "" {
		return nil, errors.New("codex binary path is required")
	}
	if opts.Cwd == "" {
		return nil, errors.New("app-server cwd (team directory) is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	args := append([]string{"app-server", "--stdio"}, opts.ExtraArgs...)
	cmd := exec.Command(opts.Binary, args...)
	cmd.Dir = opts.Cwd
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	cmd.Stderr = &stderrLog{logger: opts.Logger} // protocol is stdout-only
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}
	s := &Server{
		cmd:       cmd,
		epoch:     domain.NewEpochID(),
		logger:    opts.Logger,
		pending:   map[string]pendingApproval{},
		intents:   map[string]string{},
		cwd:       opts.Cwd,
		fileItems: map[nativeItemKey][]engine.FileChange{},
	}
	s.client = NewClient(stdin, stdout, ClientHooks{
		OnRequest: func(req InboundRequest) { s.handleRequest(req) },
		OnNotify:  func(method string, params json.RawMessage) { s.handleNotify(method, params) },
		OnExit:    opts.Callbacks.OnExit,
		Logger:    opts.Logger,
	})

	handshakeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.client.Call(handshakeCtx, "initialize", InitializeParams{
		ClientInfo: opts.Client,
	}, nil); err != nil {
		s.kill()
		return nil, fmt.Errorf("codex initialize: %w", err)
	}
	if err := s.client.Notify("initialized", nil); err != nil {
		s.kill()
		return nil, fmt.Errorf("codex initialized notify: %w", err)
	}
	s.callbacks = opts.Callbacks
	s.logger.Info("codex app-server ready", "epoch", s.epoch, "cwd", opts.Cwd)
	return s, nil
}

func (s *Server) handleRequest(req InboundRequest) {
	switch req.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval":
		// Only verified native contracts become actionable product cards.
	case "mcpServer/elicitation/request":
		// MCP forms/URL authentication are not command approvals. This build
		// cannot render them, so answer with the verified cancellation shape.
		_ = s.client.Reply(req.ID, map[string]any{"action": "cancel"})
		return
	default:
		_ = s.client.ReplyError(req.ID, -32601, "CoBot does not support this server request")
		return
	}
	var base struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
		Command  string `json:"command"`
		Cwd      string `json:"cwd"`
		Reason   string `json:"reason"`
	}
	_ = json.Unmarshal(req.Params, &base)

	var paths []string
	var fileChange struct {
		Changes []engine.FileChange `json:"changes"`
	}
	if err := json.Unmarshal(req.Params, &fileChange); err == nil {
		for _, ch := range fileChange.Changes {
			if ch.Path != "" {
				paths = append(paths, ch.Path)
			}
		}
	}

	s.mu.Lock()
	if req.Method == "item/fileChange/requestApproval" {
		if cached := s.fileItems[nativeItemKey{base.ThreadID, base.TurnID, base.ItemID}]; len(cached) > 0 {
			fileChange.Changes = append([]engine.FileChange(nil), cached...)
			paths = nil
			for _, change := range cached {
				paths = append(paths, change.Path)
			}
		}
		if base.Cwd == "" {
			base.Cwd = s.cwd
		}
	}
	s.pending[string(req.ID)] = pendingApproval{threadID: base.ThreadID, turnID: base.TurnID}
	s.mu.Unlock()

	approval := engine.ApprovalRequest{
		RequestID: req.ID,
		Method:    req.Method,
		ThreadID:  base.ThreadID,
		TurnID:    base.TurnID,
		ItemID:    base.ItemID,
		Command:   base.Command,
		Cwd:       base.Cwd,
		Reason:    base.Reason,
		Paths:     paths,
		Changes:   fileChange.Changes,
		RawParams: req.Params,
	}
	if s.callbacks.OnApproval == nil {
		return
	}
	if req.Method == "item/fileChange/requestApproval" && len(approval.Changes) == 0 && s.client != nil {
		// Some versions request approval before publishing item/started. An RPC
		// must not run on Client's read loop, which also resolves that RPC.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if changes, err := s.inspectFileChanges(ctx, base.ThreadID, base.TurnID, base.ItemID); err == nil && len(changes) > 0 {
				approval.Changes = changes
				approval.Paths = nil
				for _, change := range changes {
					approval.Paths = append(approval.Paths, change.Path)
				}
			}
			s.callbacks.OnApproval(approval)
		}()
		return
	}
	s.callbacks.OnApproval(approval)
}

func (s *Server) inspectFileChanges(ctx context.Context, threadID, turnID, itemID string) ([]engine.FileChange, error) {
	var out struct {
		Thread struct {
			Turns []struct {
				ID    string `json:"id"`
				Items []struct {
					ID      string              `json:"id"`
					Type    string              `json:"type"`
					Changes []engine.FileChange `json:"changes"`
				} `json:"items"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if err := s.client.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true}, &out); err != nil {
		return nil, err
	}
	for _, turn := range out.Thread.Turns {
		if turn.ID != turnID {
			continue
		}
		for _, item := range turn.Items {
			if item.ID == itemID && item.Type == "fileChange" {
				return item.Changes, nil
			}
		}
	}
	return nil, errors.New("exact pending file item not found")
}

func (s *Server) handleNotify(method string, params json.RawMessage) {
	n := engine.Notification{Method: method, Raw: params}
	switch method {
	case "turn/started", "turn/completed":
		var p TurnCompletedParams
		if err := json.Unmarshal(params, &p); err == nil {
			n.ThreadID = p.ThreadID
			n.TurnID = p.Turn.ID
			n.TurnStatus = p.Turn.Status
			if method == "turn/completed" {
				s.mu.Lock()
				for key, pending := range s.pending {
					if pending.threadID == n.ThreadID && pending.turnID == n.TurnID {
						delete(s.pending, key)
					}
				}
				for key := range s.fileItems {
					if key.ThreadID == n.ThreadID && key.TurnID == n.TurnID {
						delete(s.fileItems, key)
					}
				}
				s.mu.Unlock()
			}
		}
	case "item/started", "item/completed":
		var p ItemCompletedParams
		if err := json.Unmarshal(params, &p); err == nil {
			n.ThreadID = p.ThreadID
			n.TurnID = p.TurnID
			var item ThreadItem
			if err := json.Unmarshal(p.Item, &item); err == nil {
				n.ItemID = item.ID
				n.ItemType = item.Type
				n.Text = item.Text
				if item.Type == "fileChange" {
					var file struct {
						Changes []engine.FileChange `json:"changes"`
					}
					if json.Unmarshal(p.Item, &file) == nil {
						s.mu.Lock()
						if s.fileItems == nil {
							s.fileItems = map[nativeItemKey][]engine.FileChange{}
						}
						s.fileItems[nativeItemKey{n.ThreadID, n.TurnID, n.ItemID}] = file.Changes
						s.mu.Unlock()
					}
				}
			}
		}
	case "item/agentMessage/delta":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Delta    string `json:"delta"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			n.ThreadID = p.ThreadID
			n.TurnID = p.TurnID
			n.Text = p.Delta
		}
	case "thread/tokenUsage/updated":
		var p TokenUsageParams
		if err := json.Unmarshal(params, &p); err == nil {
			n.ThreadID = p.ThreadID
			n.TurnID = p.TurnID
			n.TokenUsage = params
		}
	default:
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
		}
		_ = json.Unmarshal(params, &p)
		n.ThreadID = p.ThreadID
		n.TurnID = p.TurnID
	}
	if s.callbacks.OnNotification != nil {
		s.callbacks.OnNotification(n)
	}
}

// Epoch identifies this app-server lifetime (leases/requests reference it).
func (s *Server) Epoch() string { return s.epoch }

// StartThread creates a new provider thread for one bot.
func (s *Server) StartThread(ctx context.Context, spec engine.ThreadSpec) (engine.ThreadInfo, error) {
	res := ThreadStartResult{}
	err := s.client.Call(ctx, "thread/start", ThreadStartParams{
		Cwd:                   spec.Cwd,
		Sandbox:               spec.Sandbox,
		ApprovalPolicy:        spec.ApprovalPolicy,
		ApprovalsReviewer:     spec.ApprovalsReviewer,
		Model:                 spec.Model,
		DeveloperInstructions: spec.DeveloperInstructions,
		Config:                spec.Config,
	}, &res)
	if err != nil {
		return engine.ThreadInfo{}, fmt.Errorf("thread/start: %w", err)
	}
	if res.Thread.ID == "" {
		return engine.ThreadInfo{}, errors.New("thread/start returned no thread id")
	}
	return engine.ThreadInfo{ThreadID: res.Thread.ID, Cwd: res.Cwd, Model: res.Model, ModelProvider: res.ModelProvider}, nil
}

// ResumeThread reattaches a previously created thread.
func (s *Server) ResumeThread(ctx context.Context, threadID string, spec engine.ThreadSpec) error {
	if err := s.client.Call(ctx, "thread/resume", ThreadResumeParams{
		ThreadID: threadID, Cwd: spec.Cwd, Sandbox: spec.Sandbox,
		ApprovalPolicy: spec.ApprovalPolicy, ApprovalsReviewer: spec.ApprovalsReviewer,
		Model: spec.Model, DeveloperInstructions: spec.DeveloperInstructions, Config: spec.Config,
	}, nil); err != nil {
		return fmt.Errorf("thread/resume: %w", err)
	}
	return nil
}

// StartTurn dispatches one user turn; completion arrives via notifications.
func (s *Server) InspectTurn(ctx context.Context, threadID, turnID string) (string, error) {
	var out struct {
		Thread struct {
			Turns []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if err := s.client.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true}, &out); err != nil {
		return "", err
	}
	for _, turn := range out.Thread.Turns {
		if turn.ID == turnID {
			return turn.Status, nil
		}
	}
	return "", errors.New("exact provider turn not found")
}

func (s *Server) StartTurn(ctx context.Context, threadID, text, model string) (string, error) {
	res := TurnStartResult{}
	err := s.client.Call(ctx, "turn/start", TurnStartParams{
		ThreadID: threadID,
		Input:    []UserInput{{Type: "text", Text: text}},
		Model:    model,
	}, &res)
	if err != nil {
		return "", fmt.Errorf("turn/start: %w", err)
	}
	if res.Turn.ID == "" {
		return "", errors.New("turn/start returned no turn id")
	}
	s.mu.Lock()
	s.intents[threadID] = res.Turn.ID
	s.mu.Unlock()
	return res.Turn.ID, nil
}

// InterruptTurn asks the engine to stop the thread's current turn.
func (s *Server) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	if threadID == "" || turnID == "" {
		return errors.New("interrupt requires thread and turn identity")
	}
	if err := s.client.Call(ctx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID}, nil); err != nil {
		return fmt.Errorf("turn/interrupt: %w", err)
	}
	return nil
}

// ReplyApproval answers a pending native approval request.
func (s *Server) ReplyApproval(ctx context.Context, requestID json.RawMessage, response map[string]any) error {
	s.mu.Lock()
	delete(s.pending, string(requestID))
	s.mu.Unlock()
	if err := s.client.Reply(requestID, response); err != nil {
		return fmt.Errorf("approval reply: %w", err)
	}
	return nil
}

// AccountRead returns the non-secret account projection.
func (s *Server) AccountRead(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := s.client.Call(ctx, "account/read", map[string]any{}, &out); err != nil {
		return nil, fmt.Errorf("account/read: %w", err)
	}
	return out, nil
}

// AccountLoginStart begins the official private-environment login flow.
func (s *Server) AccountLoginStart(ctx context.Context) (engine.LoginInfo, error) {
	var out engine.LoginInfo
	if err := s.client.Call(ctx, "account/login/start", map[string]any{"type": "chatgpt"}, &out); err != nil {
		return out, fmt.Errorf("account/login/start: %w", err)
	}
	if out.Type != "chatgpt" || out.LoginID == "" || out.AuthURL == "" {
		return engine.LoginInfo{}, errors.New("incomplete browser login response")
	}
	return out, nil
}

// AccountLoginCancel aborts a pending login.
func (s *Server) AccountLoginCancel(ctx context.Context, loginID string) error {
	if loginID == "" {
		return errors.New("loginId is required")
	}
	if err := s.client.Call(ctx, "account/login/cancel", map[string]any{"loginId": loginID}, nil); err != nil {
		return fmt.Errorf("account/login/cancel: %w", err)
	}
	return nil
}

// ModelList returns the engine's available models (non-secret).
func (s *Server) ModelList(ctx context.Context) (json.RawMessage, error) {
	all := []json.RawMessage{}
	cursor := ""
	seen := map[string]bool{}
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data       *[]json.RawMessage `json:"data"`
			NextCursor *string            `json:"nextCursor"`
		}
		if err := s.client.Call(ctx, "model/list", params, &page); err != nil {
			return nil, fmt.Errorf("model/list: %w", err)
		}
		if page.Data == nil {
			return nil, errors.New("model/list response missing data")
		}
		all = append(all, (*page.Data)...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = *page.NextCursor
		if seen[cursor] || len(seen) >= 100 {
			return nil, errors.New("model/list invalid pagination")
		}
		seen[cursor] = true
	}
	return json.Marshal(map[string]any{"data": all})
}

// Shutdown stops the process this server owns: stdin close, bounded wait,
// then terminate. It never touches processes it does not own.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	_ = s.client.Close()
	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()
	select {
	case <-ctx.Done():
		s.kill()
		s.client.Wait()
		return fmt.Errorf("codex app-server did not exit in time; terminated: %w", ctx.Err())
	case err := <-done:
		s.client.Wait()
		if err != nil && !isExitError(err) {
			return fmt.Errorf("codex app-server exit: %w", err)
		}
		return nil
	}
}

func (s *Server) kill() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

// stderrLog feeds engine stderr into the product log without touching the
// protocol stream. Lines are emitted at warn level: the engine writes its
// diagnostics to stderr, and hiding them below the default info threshold
// means a failing engine leaves no trace in the product log.
type stderrLog struct {
	logger *slog.Logger
	mu     sync.Mutex
	buf    []byte
}

func (w *stderrLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := -1
		for j, b := range w.buf {
			if b == '\n' {
				i = j
				break
			}
		}
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if len(w.buf) > 8*1024 {
			w.buf = w.buf[len(w.buf)-8*1024:]
		}
		w.logger.Warn("codex stderr", "line", line)
	}
	return len(p), nil
}

// WindowsSandboxConfig returns the thread config overrides that pin the
// Windows MXC sandbox on supported builds, without touching global config.
func WindowsSandboxConfig() map[string]any {
	if runtime.GOOS != "windows" {
		return nil
	}
	// Explicitly select the verified MXC path; never fall back to the legacy
	// elevated setup or full access from inside CoBot.
	return map[string]any{"windows.sandbox": "mxc"}
}

// Ping reports whether the app-server process is still connected.
func (s *Server) Ping(ctx context.Context) error {
	select {
	case <-s.client.Closed():
		return fmt.Errorf("app-server closed: %w", s.client.Err())
	default:
		return nil
	}
}

// Call exposes a generic RPC for protocol surfaces CoBot does not wrap yet
// (for example account/logout).
func (s *Server) Call(ctx context.Context, method string, params any, out any) error {
	return s.client.Call(ctx, method, params, out)
}
