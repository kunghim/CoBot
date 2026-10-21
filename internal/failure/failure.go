// Package failure normalizes provider failures into stable machine codes.
//
// Two call sites need the same classification: the model probe that runs
// while a provider is configured, and the runtime path that turns an engine
// error into something the user can act on. They must agree, otherwise the
// probe says a model works and the first run fails, or the other way round.
//
// A code is an internal contract, not user-facing text. Callers branch on it
// (retry, disable the model, ask for a top-up) and use Text to render it.
// Messages are short Chinese phrases: they carry no secret, no vendor body
// text and no URL, because a vendor error body routinely echoes back the
// key that was rejected.
package failure

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Stable machine codes. These are internal contracts, not user-facing text.
const (
	// CodeOK marks a request that was answered.
	CodeOK                  = "ok"
	CodeQuota               = "quota"
	CodeRateLimit           = "rate_limit"
	CodeAuthInvalid         = "auth_invalid"
	CodeModelUnknown        = "model_unknown"
	CodeContextExceeded     = "context_exceeded"
	CodeProtocolUnsupported = "protocol_unsupported"
	CodeRequestRejected     = "request_rejected"
	CodeNetwork             = "network"
	CodeEngineInternal      = "engine_internal"
	CodeUnknown             = "unknown"
)

// bodyClue is a vendor error string that identifies a failure more precisely
// than the HTTP status does.
type bodyClue struct {
	code    string
	needles []string
}

// bodyClues are scanned in this order and the first match wins. The order
// matters: one body can mention several conditions, and only the vendor
// knows which one it meant. Quota is checked before rate limiting because a
// vendor that reports "insufficient_quota" under HTTP 429 wants the user to
// top up, not to slow down.
var bodyClues = []bodyClue{
	{CodeQuota, []string{"insufficient_quota", "billing", "payment_required"}},
	{CodeRateLimit, []string{"rate_limit", "too_many_requests"}},
	{CodeAuthInvalid, []string{"invalid_api_key", "unauthorized", "permission_denied", "authentication"}},
	{CodeModelUnknown, []string{"model_not_found", "does not exist", "unknown model", "model is not supported"}},
	{CodeContextExceeded, []string{"context_length", "too many tokens", "maximum context"}},
	{CodeProtocolUnsupported, []string{"not supported", "unsupported"}},
}

// ClassifyHTTP maps an HTTP status and response body to a machine code and a
// short human-readable message.
//
// The body is inspected first because vendors reuse one status for unrelated
// failures: 429 covers both rate limiting and an exhausted balance, 400
// covers a request the vendor rejects for a dozen reasons. Only the body
// carries the vendor's own error code, so it is the only place that tells
// those apart. The status is the fallback for a body that says nothing
// useful.
//
// The message never contains body text, so a rejected key, a full URL or
// any other detail the vendor echoed back cannot leak through it.
func ClassifyHTTP(status int, body string) (string, string) {
	if code, ok := classifyBody(body); ok {
		return code, Text(code)
	}
	return classifyStatus(status)
}

// classifyBody looks for a well-known vendor error code in the response body.
//
// The body is normalized first: vendors write the same reason in several
// ways ("rate limit exceeded", "rate-limit-exceeded", "rate_limit_exceeded")
// and the table lists one spelling of each.
func classifyBody(body string) (string, bool) {
	if body == "" {
		return "", false
	}
	lowered := normalizeBody(body)
	for _, clue := range bodyClues {
		for _, needle := range clue.needles {
			// The needle is normalized too: the table spells some reasons
			// with spaces ("does not exist") and others with the vendor's
			// own underscores ("model_not_found"), and the body may use
			// either form.
			if strings.Contains(lowered, normalizeBody(needle)) {
				return clue.code, true
			}
		}
	}
	return "", false
}

// normalizeBody folds a vendor's answer into a form the keyword search can
// use: lower case, with spaces and dashes turned into underscores.
func normalizeBody(body string) string {
	lowered := strings.ToLower(body)
	lowered = strings.ReplaceAll(lowered, " ", "_")
	lowered = strings.ReplaceAll(lowered, "-", "_")
	return lowered
}

// classifyStatus is the status-only fallback. It also fixes the wording of
// the codes whose meaning depends on what the caller asked for.
func classifyStatus(status int) (string, string) {
	switch status {
	case 401, 403:
		return CodeAuthInvalid, Text(CodeAuthInvalid)
	case 402:
		return CodeQuota, Text(CodeQuota)
	case 400:
		// The provider refused the request on its own terms and said
		// nothing about why. A wrong model name and a malformed request
		// both land here, and both are the user's to fix: sending the
		// same request again would fail the same way.
		return CodeRequestRejected, Text(CodeRequestRejected)
	case 404:
		// The request may have named a model that does not exist or an
		// endpoint path that does not exist; only the caller knows which,
		// so the message names both and the caller picks the wording.
		return CodeModelUnknown, "端点或模型不存在"
	case 405, 501:
		return CodeProtocolUnsupported, Text(CodeProtocolUnsupported)
	case 408, 429:
		return CodeRateLimit, Text(CodeRateLimit)
	}
	if status >= 500 && status <= 599 {
		return CodeNetwork, Text(CodeNetwork)
	}
	return CodeEngineInternal, Text(CodeEngineInternal)
}

// Text returns the user-facing Chinese text for a machine code. Unknown codes
// get a generic message.
func Text(code string) string {
	switch code {
	case CodeOK:
		return "连通正常"
	case CodeQuota:
		return "余额不足"
	case CodeRateLimit:
		return "触发限流"
	case CodeAuthInvalid:
		return "密钥无效"
	case CodeModelUnknown:
		return "模型不存在"
	case CodeContextExceeded:
		return "上下文超限"
	case CodeProtocolUnsupported:
		return "端点不支持"
	case CodeRequestRejected:
		return "请求被拒绝"
	case CodeNetwork:
		return "连不上"
	case CodeEngineInternal:
		return "引擎内部错误"
	case CodeUnknown:
		return "未知错误"
	}
	return "未知错误"
}

// FromCodexErrorInfo classifies Codex's codexErrorInfo payload, which nests
// the HTTP status one level down.
//
// Codex reports the transport symptom in the key and the vendor status in
// the value, so the code comes from the status while the message names the
// symptom. The split is deliberate: the code is what a caller branches on
// (a quota failure must not be retried), the message is what the user reads.
// Text(code) renders the root cause when the caller wants that instead.
//
// ok reports whether the payload carried a status to classify. It is false
// when Codex named a failure without saying why: the engine then knew what
// happened but not its cause, and the caller keeps Codex's own code and
// wording instead of pretending to know the reason.
func FromCodexErrorInfo(raw json.RawMessage) (string, string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return CodeEngineInternal, Text(CodeEngineInternal), false
	}
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		// A bare string names the failure but carries no status to classify.
		return CodeEngineInternal, Text(CodeEngineInternal), false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || len(object) == 0 {
		return CodeEngineInternal, Text(CodeEngineInternal), false
	}
	key, value := pickKey(object)
	status, ok := statusFrom(value)
	scenario := scenarioMessage(key)
	if !ok {
		// The payload named a failure but carried no status to classify.
		// That is the engine not understanding its own error, not a
		// provider refusing a request, and not a network problem either.
		if scenario != "" {
			return CodeEngineInternal, scenario, false
		}
		return CodeEngineInternal, Text(CodeEngineInternal), false
	}
	code, message := classifyStatus(status)
	if scenario != "" {
		return code, scenario, true
	}
	return code, message, true
}

// scenarioMessage names the Codex transport symptom. Codex keys are matched
// exactly; an unknown key keeps the status wording.
func scenarioMessage(key string) string {
	switch key {
	case "httpConnectionFailed":
		return "请求失败"
	case "responseStreamConnectionFailed":
		return "连接失败"
	case "responseStreamDisconnected":
		return "连接中断"
	case "responseTooManyFailedAttempts":
		return "多次尝试失败"
	}
	return ""
}

// pickKey picks the error key of a codexErrorInfo object. Codex sends one
// key, but when several arrive the result must not depend on Go's map
// iteration order, so the keys are sorted and the first one that carries a
// status wins.
func pickKey(object map[string]json.RawMessage) (string, json.RawMessage) {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := statusFrom(object[key]); ok {
			return key, object[key]
		}
	}
	return keys[0], object[keys[0]]
}

// statusFrom reads the HTTP status out of a codexErrorInfo value. Codex uses
// httpStatusCode, other producers use status or statusCode, and the number
// may arrive as a string.
func statusFrom(raw json.RawMessage) (int, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		for _, field := range []string{"httpStatusCode", "status", "statusCode"} {
			value, ok := object[field]
			if !ok {
				continue
			}
			if status, ok := numberFrom(value); ok {
				return status, true
			}
		}
		return 0, false
	}
	return numberFrom(raw)
}

// numberFrom reads an integer that may have been encoded as a JSON string.
// A JSON null is not a number: Codex declares the status as integer|null, so
// a null status means "the vendor did not say", not "status 0".
func numberFrom(raw json.RawMessage) (int, bool) {
	if strings.TrimSpace(string(raw)) == "null" {
		return 0, false
	}
	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if number, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
			return number, true
		}
	}
	return 0, false
}
