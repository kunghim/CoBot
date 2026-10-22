package collaboration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/wiretrace"
)

// BridgeEnv names the environment a spawned bridge reads. The core passes the
// endpoint and the bot token this way; the bridge never opens the database.
// The values live in auth so the application layer can build the thread
// config without importing this package.
const (
	BridgeEnvEndpoint = auth.BridgeEnvEndpoint
	BridgeEnvToken    = auth.BridgeEnvToken
)

// Bridge is the stdio MCP server the engine spawns per thread. It speaks MCP
// on stdin/stdout and translates every tool call into an HTTP request against
// the core's internal collaboration route, carrying the bot token that proves
// which bot is calling. It holds no delivery queue and no business state, so
// a bridge crash loses nothing: the core owns the facts.
type Bridge struct {
	client   *http.Client
	endpoint string
	logger   *slog.Logger
	trace    *wiretrace.Recorder
}

// SetTrace records the MCP stream and the core calls while a developer
// recording is on.
func (b *Bridge) SetTrace(rec *wiretrace.Recorder) {
	if rec == nil {
		return
	}
	b.trace = rec
	b.client.Transport = &wiretrace.Transport{Rec: rec, Channel: "mcp.http"}
}

// NewBridge builds a bridge bound to one core endpoint and one bot token.
func NewBridge(endpoint, token string, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{
		client:   &http.Client{Timeout: 60 * time.Second},
		endpoint: endpoint,
		logger:   logger,
	}
}

// Run serves the MCP stdio transport until the client disconnects.
func (b *Bridge) Run(ctx context.Context) error {
	if b.trace == nil {
		return b.Serve(ctx, &mcp.StdioTransport{})
	}
	in := wiretrace.NewLines(b.trace, "mcp.stdio", "codex→bridge", "")
	out := wiretrace.NewLines(b.trace, "mcp.stdio", "bridge→codex", "")
	return b.Serve(ctx, &mcp.IOTransport{
		Reader: io.NopCloser(wiretrace.TeeReader(os.Stdin, in)),
		Writer: stdoutTee{Writer: wiretrace.TeeWriter(os.Stdout, out), Closer: os.Stdout},
	})
}

// stdoutTee writes the MCP stream to stdout and to the recording.
type stdoutTee struct {
	io.Writer
	io.Closer
}

// Serve runs the bridge on an arbitrary transport. Run wires stdio, which is
// what the engine spawns; tests drive the same registration through an
// in-memory transport so the protocol framing is exercised for real.
func (b *Bridge) Serve(ctx context.Context, transport mcp.Transport) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "cobot-collaboration", Version: "0.1.0"}, &mcp.ServerOptions{
		// The tool descriptions are the model's only view of what it may do,
		// so they state the permission and idempotency rules explicitly.
		Instructions: "CoBot 协作工具。写操作的幂等键由宿主从调用标识生成，无需在参数中提供；" +
			"权限由宿主校验，调用被拒绝时如实返回 rejected，不会假装成功。",
	})
	b.register(server)
	return server.Run(ctx, callIDTransport{transport})
}

// register adds the tools of this bridge's role with their schemas.
func (b *Bridge) register(server *mcp.Server) {
	for _, name := range ToolsForRole(os.Getenv(BridgeEnvRole)) {
		mcp.AddTool(server, &mcp.Tool{
			Name:        name,
			Description: toolDescriptions[name],
			InputSchema: toolSchema(name),
		}, func(ctx context.Context, req *mcp.CallToolRequest, _ json.RawMessage) (*mcp.CallToolResult, any, error) {
			return b.callTool(ctx, req.Params.Name, req.Params.Arguments, req.Params.Meta)
		})
	}
}

// callTool forwards one tool call to the core and renders the wire result.
// Tool failures are reported as error results, never as transport errors, so
// the model can read the reason and react.
func (b *Bridge) callTool(ctx context.Context, name string, args json.RawMessage, meta map[string]any) (*mcp.CallToolResult, any, error) {
	token := os.Getenv(BridgeEnvToken)
	if token == "" {
		return toolFailure(StatusUnavailable, "error.unauthorized.missing_token", nil), nil, nil
	}
	// This process may outlive the core that spawned it (an orphaned
	// engine keeps its bridge). Side effects then depend on a local lease
	// the core stops refreshing; reads still work so the model can inspect
	// state and report honestly.
	if IsSideEffectTool(name) {
		if dir := os.Getenv(BridgeEnvLeaseDir); dir != "" {
			if botID := os.Getenv(BridgeEnvBotID); botID != "" {
				if _, reason, _ := platform.ReadLeaseDir(dir, botID, "", time.Now()); reason != platform.LeaseReasonOK {
					return toolFailure(StatusRejected, "error.lease."+reason, nil), nil, nil
				}
			}
		}
	}
	threadID, turnID := nativeTurnIdentity(meta)
	callID, _ := meta["callId"].(string)
	body, err := json.Marshal(map[string]any{
		"providerThreadId": threadID, "providerTurnId": turnID,
		"callId":    callID,
		"tool":      name,
		"arguments": json.RawMessage(args),
		"requestId": newRequestID(),
	})
	if err != nil {
		return toolFailure(StatusUnavailable, "error.internal", nil), nil, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := newRequest(callCtx, b.endpoint, token, body)
	if err != nil {
		return toolFailure(StatusUnavailable, "error.engine.unavailable", nil), nil, nil
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.logger.Warn("collaboration call failed", "tool", name, "error", err)
		return toolFailure(StatusUnavailable, "error.engine.unavailable", nil), nil, nil
	}
	defer resp.Body.Close()
	var envelope struct {
		Data   ToolResult `json:"data"`
		Error  *ToolError `json:"error"`
		Status string     `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return toolFailure(StatusUnavailable, "error.internal", nil), nil, nil
	}
	if resp.StatusCode >= 400 || envelope.Error != nil {
		te := envelope.Error
		if te == nil {
			te = Unavailable("error.internal", "error.internal", nil)
		}
		return toolFailure(te.Status, te.MessageKey, te.Params), nil, nil
	}
	result := envelope.Data
	// A rejection is a failed call the model must be able to read, not a
	// success with a status field buried in the payload.
	if result.Status == StatusRejected || result.Status == StatusUnavailable {
		return toolFailure(result.Status, errorKeyOf(result), paramsOf(result)), nil, nil
	}
	text, err := json.Marshal(result)
	if err != nil {
		return toolFailure(StatusUnavailable, "error.internal", nil), nil, nil
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
		StructuredContent: result,
	}, nil, nil
}

// Identity comes from Codex's transport metadata, never tool arguments.
func nativeTurnIdentity(meta map[string]any) (string, string) {
	var native map[string]any
	switch value := meta["x-codex-turn-metadata"].(type) {
	case string:
		_ = json.Unmarshal([]byte(value), &native)
	case map[string]any:
		native = value
	}
	thread, _ := native["thread_id"].(string)
	if thread == "" {
		thread, _ = native["session_id"].(string)
	}
	turn, _ := native["turn_id"].(string)
	return thread, turn
}

// toolFailure reports a structured failure the model can read, marking it as
// an error result so the client does not treat it as success.
func toolFailure(status, messageKey string, params map[string]any) *mcp.CallToolResult {
	payload, _ := json.Marshal(ToolResult{
		Status: status,
		Error:  &ToolError{Status: status, Code: status, MessageKey: messageKey, Params: params},
	})
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		IsError: true,
	}
}

// PrincipalForToken is a convenience used by the core-side handler.
func PrincipalForToken(store *auth.TokenStore, token string) (auth.Principal, auth.BotBinding, error) {
	if store == nil {
		return auth.Principal{}, auth.BotBinding{}, errors.New("nil token store")
	}
	binding, err := store.Resolve(token)
	if err != nil {
		return auth.Principal{}, auth.BotBinding{}, err
	}
	principal := auth.NewPrincipalBot(binding.BotID, binding.TeamID)
	principal.RunID = binding.RunID
	return principal, binding, nil
}

// newRequestID is a cheap unique id for the HTTP envelope; the tool's own
// idempotency key stays in the arguments where the product validates it.
func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "req-0"
	}
	return hex.EncodeToString(buf)
}

// newRequest builds the internal collaboration call. The bot token is a
// header, never a query parameter, so it cannot end up in a log line.
func newRequest(ctx context.Context, endpoint, token string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/internal/v1/collaboration", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bot-Token", token)
	return req, nil
}

var _ = errors.Is

// errorKeyOf extracts the i18n key from a structured tool error, falling back
// to the status so the model never receives an empty reason.
func errorKeyOf(r ToolResult) string {
	if r.Error != nil && r.Error.MessageKey != "" {
		return r.Error.MessageKey
	}
	return "error.internal"
}

// paramsOf carries the structured error parameters into the rendered result.
func paramsOf(r ToolResult) map[string]any {
	if r.Error != nil {
		return r.Error.Params
	}
	return nil
}
