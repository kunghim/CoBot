package collaboration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/platform"
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
	return b.Serve(ctx, &mcp.StdioTransport{})
}

// Serve runs the bridge on an arbitrary transport. Run wires stdio, which is
// what the engine spawns; tests drive the same registration through an
// in-memory transport so the protocol framing is exercised for real.
func (b *Bridge) Serve(ctx context.Context, transport mcp.Transport) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "cobot-collaboration", Version: "0.1.0"}, &mcp.ServerOptions{
		// The tool descriptions are the model's only view of what it may do,
		// so they state the permission and idempotency rules explicitly.
		Instructions: "CoBot 协作工具。所有写工具都必须携带 idempotencyKey；" +
			"权限由宿主校验，调用被拒绝时如实返回 rejected，不会假装成功。",
	})
	b.register(server)
	return server.Run(ctx, transport)
}

// register adds every collaboration tool with its schema.
func (b *Bridge) register(server *mcp.Server) {
	tools := []struct {
		name        string
		description string
	}{
		{"get_context", "读取当前 Bot 的团队、成员、任务与最近协作上下文"},
		{"list_tasks", "列出本团队任务（可按状态过滤）"},
		{"get_task", "读取一个任务及其证据与转交记录"},
		{"list_roles", "列出内置角色库（仅队长可见完整库）"},
		{"send_message", "给同团队成员发消息；同一内容发给多人用 targetBotIds 一次发完；只告知进展、不需要对方处理时设 notifyOnly=true，对方下一轮会看到但不会被唤醒（accepted 仅表示已持久化）"},
		{"create_task", "创建任务并指派唯一负责人（仅队长）"},
		{"propose_task", "以成员身份向队长提交任务提议（不直接建任务）"},
		{"assign_task", "更换任务负责人（仅队长，按安全换人规则）"},
		{"update_task", "更新任务状态（owner/队长，done 需 confirm_task）"},
		{"submit_evidence", "为任务提交证据引用（owner 或队长）"},
		{"report_completion", "报告完成，任务进入 review（不直接 done）"},
		{"confirm_task", "确认任务完成为 done（仅队长或用户，需证据）"},
		{"offer_transfer", "提议转交任务给同团队成员（owner）"},
		{"accept_transfer", "接受转交并正式更换负责人（目标成员）"},
		{"ask_user", "向用户提问；返回 waiting 项并请结束当前工作"},
		{"request_member", "请求用户批准引入指定角色成员（仅队长）"},
		{"request_merge", "请求用户批准合并指定候选版本（绑定 SHA 与证据）"},
		{"remember", "记录你的工作方法或偏好（只存方法：不存任务证据、秘密或大段源码；同来源同正文不会重建）"},
		{"search_memory", "检索你自己的记忆（按归属过滤，默认最多 10 条、共 6000 字符）"},
	}
	for _, tool := range tools {
		tool := tool
		mcp.AddTool(server, &mcp.Tool{
			Name:        tool.name,
			Description: tool.description,
			InputSchema: toolSchema(tool.name),
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
	// H06: this process may outlive the core that spawned it (an orphaned
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
	body, err := json.Marshal(map[string]any{
		"providerThreadId": threadID, "providerTurnId": turnID,
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
