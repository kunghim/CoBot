package http

import (
	"encoding/json"
	"net/http"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/collaboration"
	"github.com/kunghim/CoBot/internal/domain"
)

// collaborationRequest is the bridge's envelope. The tool's own idempotency
// key travels inside arguments, where the product validates it; the envelope
// requestId only identifies this HTTP call.
type collaborationRequest struct {
	RequestID        string          `json:"requestId"`
	Tool             string          `json:"tool"`
	Arguments        json.RawMessage `json:"arguments"`
	ProviderThreadID string          `json:"providerThreadId,omitempty"`
	ProviderTurnID   string          `json:"providerTurnId,omitempty"`
	CallID           string          `json:"callId,omitempty"`
}

// collaborationResponse matches what the bridge decodes. The status is the
// wire contract's tool status, which is separate from the HTTP status: a
// rejected tool call is still a successfully handled request.
type collaborationResponse struct {
	Status string         `json:"status"`
	Data   map[string]any `json:"data,omitempty"`
	Error  *toolErrorBody `json:"error,omitempty"`
}

type toolErrorBody struct {
	Status     string         `json:"status"`
	Code       string         `json:"code"`
	MessageKey string         `json:"messageKey"`
	Params     map[string]any `json:"params,omitempty"`
}

// handleCollaboration serves the MCP bridge. The bot token proves which bot
// is calling; the dispatcher resolves it to a principal and enforces
// permissions, so the bridge never needs to be trusted with identity.
func (s *Server) handleCollaboration(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-Bot-Token")
	if token == "" {
		writeCollabError(w, r, collaboration.Unavailable("error.unauthorized.missing_token", "error.unauthorized.missing_token", nil))
		return
	}
	binding, err := s.CollaborationTokens.Resolve(token)
	if err != nil {
		writeCollabError(w, r, collaboration.Reject("error.unauthorized.bad_token", "error.unauthorized.bad_token", nil))
		return
	}
	principal := auth.NewPrincipalBot(binding.BotID, binding.TeamID)
	principal.RunID = binding.RunID
	// The token is bound to one engine epoch: a bridge left behind by a
	// restarted connection can no longer act for the bot.
	if s.EngineEpochOfTeam != nil {
		if current := s.EngineEpochOfTeam(binding.TeamID); current == "" || binding.EngineEpoch == "" || current != binding.EngineEpoch {
			writeCollabError(w, r, collaboration.Reject("error.unauthorized.stale_token", "error.unauthorized.stale_token", nil))
			return
		}
	}

	var req collaborationRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeCollabError(w, r, collaboration.Reject("error.invalid_request", "error.invalid_json", nil))
		return
	}
	if req.Tool == "" {
		writeCollabError(w, r, collaboration.Reject("error.invalid_request", "error.tool.unknown", nil))
		return
	}

	if binding.NativeMetadata {
		if err := s.App.ValidateNativeToolSource(r.Context(), principal, req.ProviderThreadID, req.ProviderTurnID); err != nil {
			writeCollabError(w, r, collaboration.Reject("error.unauthorized.stale_execution", "error.tool.no_active_run", nil))
			return
		}
	}
	result := s.Dispatcher.CallTool(r.Context(), principal, collaboration.ToolCall{
		Name:      req.Tool,
		Arguments: req.Arguments,
		CallID:    req.CallID,
	})
	body := collaborationResponse{Status: result.Status, Data: result.Data}
	if result.Error != nil {
		body.Error = &toolErrorBody{
			Status: result.Error.Status, Code: result.Error.Code,
			MessageKey: result.Error.MessageKey, Params: result.Error.Params,
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// A tool rejection is an answered call, not a failed HTTP request; the
	// status carries the product outcome the model reads.
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(okEnvelope{Data: body, RequestID: requestIDOf(r)})
}

// writeCollabError reports a failure that happened before the tool ran, such
// as a bad credential or a core that cannot serve the call. These are
// transport-level, so they use the normal error envelope and a non-2xx status.
func writeCollabError(w http.ResponseWriter, r *http.Request, te *collaboration.ToolError) {
	status := http.StatusUnauthorized
	if te.Status == collaboration.StatusUnavailable {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{
		Error:     errorBody{Code: domain.Code(te.Code), MessageKey: te.MessageKey, Params: te.Params},
		RequestID: requestIDOf(r),
	})
}
