package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/failure"
	"github.com/kunghim/CoBot/internal/wiretrace"
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
	// Trace records the raw stdin, stdout and stderr while a developer
	// recording is on; nil records nothing.
	Trace *wiretrace.Recorder
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
	waitOnce  sync.Once
	waitDone  chan struct{}
	waitErr   error

	loaded map[string]bool   // threads this app-server has loaded
	phases map[string]string // agent message item -> phase, while it streams
	// parents maps a sub-agent thread to the thread that spawned it.
	parents map[string]string

	trace       *wiretrace.Recorder
	tracePeer   string
	traceStderr *wiretrace.Lines
}

type nativeItemKey struct{ ThreadID, TurnID, ItemID string }

type pendingApproval struct {
	threadID string
	turnID   string
}

// recordingLogFilter is the engine's log level (RUST_LOG) while a recording
// is on. Everything logs at info, so each turn's steps, JSON-RPC handling,
// tool and MCP calls, sandboxed commands and every warning or error stay.
// Network setup logs at debug, to see reconnects, retries and timeouts.
//
// Left out (kept at warn, so their warnings and errors still show) are the
// modules that repeat one line per small event and told nothing in past
// recordings; together they were about 80% of the engine's log. To look into
// one of them, change its entry to "=debug" or delete it.
var recordingLogFilter = []string{
	"info",
	// Network: new connections (through a proxy too), request status codes.
	"reqwest=debug",
	"hyper_util::client::legacy::connect=debug",
	"codex_http_client=debug",
	// Telemetry, one line per streamed piece of every model reply; the
	// protocol traffic already holds the same content.
	"codex_otel.log_only=warn",
	// One line per file read or metadata lookup.
	"codex_exec_server::local_file_system=warn",
	// HTTP connection pool reuse; new connections are logged above.
	"hyper_util::client::legacy::pool=warn",
	// Cookies set by every response.
	"cookie_store=warn",
	// "Using system root certificates", repeated on every connection.
	"codex_http_client::custom_ca=warn",
	// Skill selection experiments and skill state, repeated per turn.
	"codex_skills_extension=warn",
	// Model list and its cache, re-read again and again.
	"codex_models_manager=warn",
	// Plugin loading, repeated per thread.
	"codex_core_plugins=warn",
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
	cmd.WaitDelay = 2 * time.Second
	cmd.Dir = opts.Cwd
	hideChildWindow(cmd)
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	// While a recording is on, the engine also logs its own detail to stderr,
	// which the recording keeps; see recordingLogFilter for what is left out.
	tracing := opts.Trace.Active()
	if tracing {
		env := cmd.Env
		if env == nil {
			env = os.Environ()
		}
		cmd.Env = append(env, "RUST_LOG="+strings.Join(recordingLogFilter, ","))
	}
	stderrLines := wiretrace.NewLines(opts.Trace, "codex.stderr", "codex→cobot", "")
	cmd.Stderr = wiretrace.TeeWriter(&stderrLog{logger: opts.Logger}, stderrLines) // protocol is stdout-only
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		opts.Trace.Record("codex.process", "start-failed", "", []byte(err.Error()))
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}
	// Every line of the three streams is recorded under this process's pid,
	// along with how it was started and how it ended.
	peer := fmt.Sprintf("codex#%d", cmd.Process.Pid)
	if opts.Trace != nil {
		started, _ := json.Marshal(map[string]any{"pid": cmd.Process.Pid, "path": cmd.Path, "args": cmd.Args, "dir": cmd.Dir, "env": tracedEnv(cmd.Env)})
		opts.Trace.Record("codex.process", "start", peer, started)
	}
	stderrLines.SetPeer(peer)
	stdoutLines := wiretrace.NewLines(opts.Trace, "codex.stdio", "codex→cobot", peer)
	stdinLines := wiretrace.NewLines(opts.Trace, "codex.stdio", "cobot→codex", peer)
	stdout = io.NopCloser(wiretrace.TeeReader(stdout, stdoutLines))
	stdin = teeWriteCloser{Writer: wiretrace.TeeWriter(stdin, stdinLines), Closer: stdin}
	s := &Server{
		cmd:       cmd,
		epoch:     domain.NewEpochID(),
		logger:    opts.Logger,
		pending:   map[string]pendingApproval{},
		intents:   map[string]string{},
		cwd:       opts.Cwd,
		fileItems: map[nativeItemKey][]engine.FileChange{},
		callbacks: opts.Callbacks,
	}
	s.trace, s.tracePeer, s.traceStderr = opts.Trace, peer, stderrLines
	s.client = NewClient(stdin, stdout, ClientHooks{
		OnRequest: func(req InboundRequest) { s.handleRequest(req) },
		OnNotify:  func(method string, params json.RawMessage) { s.handleNotify(method, params) },
		OnExit:    s.transportExited,
		Logger:    opts.Logger,
	})

	handshakeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.client.Call(handshakeCtx, "initialize", InitializeParams{
		ClientInfo: opts.Client,
	}, nil); err != nil {
		s.kill()
		s.waitProcess()
		return nil, fmt.Errorf("codex initialize: %w", err)
	}
	if err := s.client.Notify("initialized", nil); err != nil {
		s.kill()
		s.waitProcess()
		return nil, fmt.Errorf("codex initialized notify: %w", err)
	}
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
		// permissions requests: the access asked for
		Permissions json.RawMessage `json:"permissions"`
		// command requests that need the network
		NetworkApprovalContext json.RawMessage `json:"networkApprovalContext"`
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
	if len(base.Permissions) > 0 && string(base.Permissions) != "null" {
		approval.Permissions = base.Permissions
	}
	approval.Network = len(base.NetworkApprovalContext) > 0 && string(base.NetworkApprovalContext) != "null"
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
	case "turn/diff/updated":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Diff     string `json:"diff"`
		}
		if json.Unmarshal(params, &p) == nil {
			n.ThreadID, n.TurnID = p.ThreadID, p.TurnID
			detail, _ := json.Marshal(map[string]any{"diff": clipHead(p.Diff, maxTurnDiff), "files": strings.Count(p.Diff, "diff --git ")})
			n.Step = &engine.Step{ID: "turn-diff", Kind: engine.StepDiff, Status: engine.StepCompleted, Detail: detail}
		}
	case "serverRequest/resolved":
		var p struct {
			ThreadID  string          `json:"threadId"`
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(params, &p) == nil {
			n.ThreadID, n.RequestID = p.ThreadID, p.RequestID
		}
	case "thread/started":
		// A sub-agent's thread names its parent; its activity belongs to
		// the member whose thread spawned it.
		var p struct {
			Thread struct {
				ID             string `json:"id"`
				ParentThreadID string `json:"parentThreadId"`
			} `json:"thread"`
		}
		if json.Unmarshal(params, &p) == nil && p.Thread.ID != "" {
			n.ThreadID = p.Thread.ID
			s.setParent(p.Thread.ID, p.Thread.ParentThreadID)
		}
	case "thread/closed", "thread/status/changed":
		// A thread Codex closed or unloaded needs a resume before its next turn.
		var p struct {
			ThreadID string `json:"threadId"`
			Status   struct {
				Type string `json:"type"`
			} `json:"status"`
		}
		if json.Unmarshal(params, &p) == nil && p.ThreadID != "" && (method == "thread/closed" || p.Status.Type == "notLoaded") {
			s.setLoaded(p.ThreadID, false)
		}
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
				n.Phase = item.Phase
				if item.Type == "agentMessage" {
					// Deltas name only the item, so its phase is kept until
					// the item completes.
					s.setItemPhase(item.ID, item.Phase, method == "item/completed")
				}
				s.learnSubAgents(p.ThreadID, p.Item)
				if step := normalizeStep(p.Item); step != nil {
					if step.Status == "" {
						step.Status = engine.StepInProgress
						if method == "item/completed" {
							step.Status = engine.StepCompleted
						}
					}
					n.Step = step
				}
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
			ItemID   string `json:"itemId"`
			Delta    string `json:"delta"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			n.ThreadID = p.ThreadID
			n.TurnID = p.TurnID
			n.ItemID = p.ItemID
			n.Text = p.Delta
			n.DeltaKind, n.Delta = "message", p.Delta
			if s.itemPhase(p.ItemID) == "commentary" {
				n.DeltaKind, n.Phase = "commentary", "commentary"
			}
		}
	case "item/reasoning/summaryTextDelta", "item/commandExecution/outputDelta":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			ItemID   string `json:"itemId"`
			Delta    string `json:"delta"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			n.ThreadID, n.TurnID, n.ItemID, n.Delta = p.ThreadID, p.TurnID, p.ItemID, p.Delta
			n.DeltaKind = "reasoning"
			if method == "item/commandExecution/outputDelta" {
				n.DeltaKind = "output"
			}
		}
	case "error":
		var p struct {
			ThreadID  string `json:"threadId"`
			TurnID    string `json:"turnId"`
			WillRetry bool   `json:"willRetry"`
			Error     struct {
				Message        string          `json:"message"`
				CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
			} `json:"error"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			n.ThreadID, n.TurnID = p.ThreadID, p.TurnID
			code := errorCode(p.Error.CodexErrorInfo)
			failureCode := ""
			if root, _, ok := failure.FromCodexErrorInfo(p.Error.CodexErrorInfo); ok {
				// The vendor's status is the root cause: an empty balance
				// and a rejected key both arrive as "the request failed",
				// and only the status tells them apart. The engine's own
				// message is kept as it is — the caller decides what is
				// safe to store, and the retry attempt count is read
				// from it.
				failureCode = root
			} else if status, ok := httpErrorStatus(p.Error.Message); ok {
				// Without structured detail the engine passes the
				// vendor's own error body on as its message; the status
				// and the body's wording are the same evidence.
				failureCode, _ = failure.ClassifyHTTP(status, p.Error.Message)
			} else if body, ok := vendorErrorBody(p.Error.Message); ok {
				// A body the vendor streamed back has no status in it,
				// only its own error code.
				failureCode, _ = failure.ClassifyBody(body)
			}
			n.Problem = &engine.Problem{Level: "error", Message: p.Error.Message, Code: code, FailureCode: failureCode, WillRetry: p.WillRetry}
		}
	case "warning":
		var p struct {
			ThreadID string `json:"threadId"`
			Message  string `json:"message"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			n.ThreadID = p.ThreadID
			n.Problem = &engine.Problem{Level: "warning", Message: p.Message}
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

// tracedEnv records the environment the engine was started with, with the
// values of credential-looking variables left out. A provider's API key
// rides in that environment, and a recording is a file the user can copy
// around; the name still says whether the variable was set.
func tracedEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			out = append(out, entry)
			continue
		}
		if secretEnvName(name) {
			out = append(out, name+"=<redacted>")
			continue
		}
		out = append(out, entry)
	}
	return out
}

// secretEnvName reports whether an environment variable holds a credential.
func secretEnvName(name string) bool {
	upper := strings.ToUpper(name)
	for _, marker := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

// Epoch identifies this app-server lifetime (leases/requests reference it).
func (s *Server) Epoch() string { return s.epoch }

// StartThread creates a new provider thread for one bot.
func (s *Server) StartThread(ctx context.Context, spec engine.ThreadSpec) (engine.ThreadInfo, error) {
	spec = applyPermission(spec)
	res := ThreadStartResult{}
	err := s.client.Call(ctx, "thread/start", ThreadStartParams{
		Cwd:                   spec.Cwd,
		Sandbox:               spec.Sandbox,
		ApprovalPolicy:        spec.ApprovalPolicy,
		ApprovalsReviewer:     spec.ApprovalsReviewer,
		Model:                 spec.Model,
		ModelProvider:         spec.ModelProvider,
		DeveloperInstructions: spec.DeveloperInstructions,
		Config:                spec.Config,
	}, &res)
	if err != nil {
		return engine.ThreadInfo{}, fmt.Errorf("thread/start: %w", err)
	}
	if res.Thread.ID == "" {
		return engine.ThreadInfo{}, errors.New("thread/start returned no thread id")
	}
	s.setLoaded(res.Thread.ID, true)
	return engine.ThreadInfo{ThreadID: res.Thread.ID, Cwd: res.Cwd, Model: res.Model, ModelProvider: res.ModelProvider}, nil
}

// ResumeThread reattaches a previously created thread.
//
// It reports the model and the provider the thread runs with, as the engine
// answered it: the engine ignores a provider for a thread that is already
// loaded here, and a record CoBot kept itself cannot say which of the two
// happened.
func (s *Server) ResumeThread(ctx context.Context, threadID string, spec engine.ThreadSpec) (engine.ThreadInfo, error) {
	spec = applyPermission(spec)
	res := ThreadResumeResult{}
	if err := s.client.Call(ctx, "thread/resume", ThreadResumeParams{
		ThreadID: threadID, Cwd: spec.Cwd, Sandbox: spec.Sandbox,
		ApprovalPolicy: spec.ApprovalPolicy, ApprovalsReviewer: spec.ApprovalsReviewer,
		Model: spec.Model, ModelProvider: spec.ModelProvider,
		DeveloperInstructions: spec.DeveloperInstructions, Config: spec.Config,
		ExcludeTurns: true,
	}, &res); err != nil {
		return engine.ThreadInfo{}, fmt.Errorf("thread/resume: %w", err)
	}
	s.setLoaded(threadID, true)
	return engine.ThreadInfo{ThreadID: threadID, Model: res.Model, ModelProvider: res.ModelProvider}, nil
}

// ArchiveThread sets a thread aside in the engine.
//
// This is how a thread lets go of the provider it was started with: the
// engine ignores a provider for a thread it already holds, so a thread that
// has to run with another one is archived and then loaded again.
func (s *Server) ArchiveThread(ctx context.Context, threadID string) error {
	if err := s.client.Call(ctx, "thread/archive", map[string]any{"threadId": threadID}, nil); err != nil {
		return fmt.Errorf("thread/archive: %w", err)
	}
	return nil
}

// UnarchiveThread takes a thread back out of the engine's archive, so it can
// be loaded again.
func (s *Server) UnarchiveThread(ctx context.Context, threadID string) error {
	if err := s.client.Call(ctx, "thread/unarchive", map[string]any{"threadId": threadID}, nil); err != nil {
		return fmt.Errorf("thread/unarchive: %w", err)
	}
	return nil
}

// ThreadLoaded reports whether this app-server has the thread loaded: it
// was started or resumed here and Codex has not closed or unloaded it since.
// A loaded thread takes a new turn directly, with no resume first.
func (s *Server) ThreadLoaded(threadID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loaded[threadID]
}

// learnSubAgents records the threads a member's sub-agent items name.
func (s *Server) learnSubAgents(threadID string, raw json.RawMessage) {
	var item struct {
		Type              string   `json:"type"`
		SenderThreadID    string   `json:"senderThreadId"`
		ReceiverThreadIDs []string `json:"receiverThreadIds"`
		AgentThreadID     string   `json:"agentThreadId"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	switch item.Type {
	case "collabAgentToolCall":
		sender := item.SenderThreadID
		if sender == "" {
			sender = threadID
		}
		for _, receiver := range item.ReceiverThreadIDs {
			s.setParent(receiver, sender)
		}
	case "subAgentActivity":
		s.setParent(item.AgentThreadID, threadID)
	}
}

func (s *Server) setParent(child, parent string) {
	if child == "" || parent == "" || child == parent {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parents == nil {
		s.parents = map[string]string{}
	}
	// A sub-agent's own items name the agent it talks to, its parent among
	// them; taking that as a new link would loop the tree.
	for up, i := parent, 0; i < 16; i++ {
		if up == child {
			return
		}
		next, ok := s.parents[up]
		if !ok {
			break
		}
		up = next
	}
	s.parents[child] = parent
}

// RootThread is the thread a (sub-agent) thread descends from: a member's own
// thread, or the thread itself when it has no known parent.
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

func (s *Server) setItemPhase(itemID, phase string, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if done || phase == "" {
		delete(s.phases, itemID)
		return
	}
	if s.phases == nil {
		s.phases = map[string]string{}
	}
	s.phases[itemID] = phase
}

func (s *Server) itemPhase(itemID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phases[itemID]
}

func (s *Server) setLoaded(threadID string, loaded bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded == nil {
		s.loaded = map[string]bool{}
	}
	if loaded {
		s.loaded[threadID] = true
	} else {
		delete(s.loaded, threadID)
	}
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

func (s *Server) StartTurn(ctx context.Context, threadID, text string, opts engine.TurnOptions) (string, error) {
	res := TurnStartResult{}
	params := TurnStartParams{
		ThreadID: threadID,
		Input:    []UserInput{{Type: "text", Text: text}},
		Model:    opts.Model,
		Effort:   opts.Effort,
	}
	if policy, ok := permissionPolicies[opts.Permission]; ok {
		params.ApprovalPolicy, params.SandboxPolicy = policy.approval, policy.sandboxPolicy()
	}
	err := s.client.Call(ctx, "turn/start", params, &res)
	if err != nil {
		var refused *RPCError
		if errors.As(err, &refused) {
			// Codex answered: the turn did not start. The thread may be one it
			// no longer holds, so the next turn resumes it first.
			s.setLoaded(threadID, false)
			return "", fmt.Errorf("turn/start: %w: %w", engine.ErrRejected, err)
		}
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

// SteerTurn adds the user's input to the running turn (turn/steer). The
// expected turn id makes the engine refuse when that turn already ended.
func (s *Server) SteerTurn(ctx context.Context, threadID, turnID, text string) error {
	if threadID == "" || turnID == "" {
		return errors.New("steer requires thread and turn identity")
	}
	if err := s.client.Call(ctx, "turn/steer", map[string]any{
		"threadId": threadID, "expectedTurnId": turnID,
		"input": []UserInput{{Type: "text", Text: text}},
	}, nil); err != nil {
		return fmt.Errorf("turn/steer: %w", err)
	}
	return nil
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

// DeleteThread removes a stored thread from this private environment.
func (s *Server) DeleteThread(ctx context.Context, threadID string) error {
	if threadID == "" {
		return errors.New("threadId is required")
	}
	if err := s.client.Call(ctx, "thread/delete", map[string]any{"threadId": threadID}, nil); err != nil {
		return fmt.Errorf("thread/delete: %w", err)
	}
	return nil
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
	done := s.waitProcess()
	select {
	case <-ctx.Done():
		s.kill()
		return fmt.Errorf("codex app-server did not exit in time; terminated: %w", ctx.Err())
	case <-done:
		select {
		case <-s.client.Closed():
		case <-ctx.Done():
			return ctx.Err()
		}
		err := s.waitErr
		if err != nil && !isExitError(err) {
			return fmt.Errorf("codex app-server exit: %w", err)
		}
		return nil
	}
}

// A failed transport is fatal even if the child ignores stdin EOF. Retain
// process ownership here until it is killed/reaped, before the host drops
// its runtime. This is not proof that any external work succeeded.
func (s *Server) transportExited(err error) {
	go func() {
		s.kill()
		<-s.waitProcess()
		if s.callbacks.OnExit != nil {
			s.callbacks.OnExit(err)
		}
	}()
}

func (s *Server) waitProcess() <-chan struct{} {
	s.waitOnce.Do(func() {
		s.waitDone = make(chan struct{})
		go func() {
			s.waitErr = s.cmd.Wait()
			s.traceExit()
			close(s.waitDone)
		}()
	})
	return s.waitDone
}

// traceExit records how the process ended, after the last stderr line.
func (s *Server) traceExit() {
	if s.trace == nil {
		return
	}
	s.traceStderr.Flush()
	state := map[string]any{"pid": s.cmd.Process.Pid, "error": fmt.Sprint(s.waitErr)}
	if s.cmd.ProcessState != nil {
		state["exitCode"] = s.cmd.ProcessState.ExitCode()
		state["state"] = s.cmd.ProcessState.String()
	}
	data, _ := json.Marshal(state)
	s.trace.Record("codex.process", "exit", s.tracePeer, data)
}

// teeWriteCloser records what is written to the engine's stdin and closes
// the real pipe.
type teeWriteCloser struct {
	io.Writer
	io.Closer
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
// stderrLevel reads the level Codex gave a log line ("<time>  INFO target: ..."),
// so its own errors and warnings stay visible while its info and debug
// output (a recording turns those on) stays out of the core's log; the
// recording keeps every line. A line without a level, such as a panic
// message, is kept as a warning.
func stderrLevel(line string) slog.Level {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return slog.LevelWarn
	}
	switch fields[1] {
	case "ERROR":
		return slog.LevelError
	case "WARN":
		return slog.LevelWarn
	case "INFO", "DEBUG", "TRACE":
		return slog.LevelDebug
	}
	return slog.LevelWarn
}

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
		w.logger.Log(context.Background(), stderrLevel(line), "codex stderr", "line", line)
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

// httpErrorStatus reads the HTTP status out of an error message that is a
// vendor's JSON error body, as the engine passes one on when it has nothing
// structured to say, for example
// {"type":"error","status":400,"error":{"message":"..."}}.
func httpErrorStatus(message string) (int, bool) {
	raw, ok := vendorErrorBody(message)
	if !ok {
		return 0, false
	}
	var body struct {
		Status int `json:"status"`
	}
	if json.Unmarshal([]byte(raw), &body) != nil || body.Status < 400 || body.Status > 599 {
		return 0, false
	}
	return body.Status, true
}

// vendorErrorBody reports whether an error message is a vendor's JSON error
// body, and returns it. A vendor that refuses a streamed request answers
// with one server-sent event, so the body may come as "data:{...}".
func vendorErrorBody(message string) (string, bool) {
	trimmed := strings.TrimSpace(message)
	if strings.HasPrefix(trimmed, "{") {
		return trimmed, json.Valid([]byte(trimmed))
	}
	// Server-sent events: the error is the first data line that holds a
	// JSON object; an event: line or a second event may come with it.
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if strings.HasPrefix(data, "{") && json.Valid([]byte(data)) {
			return data, true
		}
	}
	return "", false
}
