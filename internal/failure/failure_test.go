package failure

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"
)

// TestClassifyHTTPPrefersBodyOverStatus pins the order the classifier
// applies: a vendor error code in the body beats the HTTP status, because
// vendors reuse one status for several different failures.
func TestClassifyHTTPPrefersBodyOverStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"quota beats rate limit status", 429, `{"error":{"code":"insufficient_quota"}}`, CodeQuota},
		{"billing beats rate limit status", 429, `{"error":{"message":"Billing limit reached"}}`, CodeQuota},
		{"payment required", 400, `{"error":{"code":"payment_required"}}`, CodeQuota},
		{"rate limit", 429, `{"error":{"code":"rate_limit_exceeded"}}`, CodeRateLimit},
		{"too many requests", 429, `{"error":{"message":"Too Many Requests"}}`, CodeRateLimit},
		{"invalid key", 500, `{"error":{"code":"invalid_api_key"}}`, CodeAuthInvalid},
		{"unauthorized", 401, `{"error":{"message":"Unauthorized"}}`, CodeAuthInvalid},
		{"permission denied", 403, `{"error":{"code":"permission_denied"}}`, CodeAuthInvalid},
		{"authentication failed", 401, `{"error":{"message":"authentication failed"}}`, CodeAuthInvalid},
		{"model not found", 404, `{"error":{"code":"model_not_found"}}`, CodeModelUnknown},
		{"model does not exist", 400, `{"error":{"message":"The model gpt-9 does not exist"}}`, CodeModelUnknown},
		{"unknown model", 400, `{"error":{"message":"unknown model"}}`, CodeModelUnknown},
		{"context length", 400, `{"error":{"code":"context_length_exceeded"}}`, CodeContextExceeded},
		{"too many tokens", 400, `{"error":{"message":"too many tokens in request"}}`, CodeContextExceeded},
		{"maximum context", 400, `{"error":{"message":"maximum context length is 8192"}}`, CodeContextExceeded},
		{"not supported", 400, `{"error":{"message":"This endpoint is not supported"}}`, CodeProtocolUnsupported},
		{"unsupported", 501, `{"error":{"code":"unsupported_operation"}}`, CodeProtocolUnsupported},
		{"body wins over a 200", 200, `{"error":{"code":"insufficient_quota"}}`, CodeQuota},
		{"unrelated body falls back to status", 402, `{"error":{"message":"nope"}}`, CodeQuota},
	} {
		code, message := ClassifyHTTP(tc.status, tc.body)
		if code != tc.want {
			t.Errorf("%s: code=%q want %q", tc.name, code, tc.want)
		}
		if message == "" {
			t.Errorf("%s: empty message", tc.name)
		}
	}
}

// TestClassifyHTTPStatusBranch covers the fallback that decides a failure
// when the body carries no known error code.
func TestClassifyHTTPStatusBranch(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{400, CodeEngineInternal},
		{401, CodeAuthInvalid},
		{402, CodeQuota},
		{403, CodeAuthInvalid},
		{404, CodeModelUnknown},
		{405, CodeProtocolUnsupported},
		{408, CodeRateLimit},
		{429, CodeRateLimit},
		{500, CodeNetwork},
		{502, CodeNetwork},
		{503, CodeNetwork},
		{501, CodeProtocolUnsupported},
		{418, CodeEngineInternal},
		{0, CodeEngineInternal},
	} {
		code, message := ClassifyHTTP(tc.status, "")
		if code != tc.want {
			t.Errorf("status %d: code=%q want %q", tc.status, code, tc.want)
		}
		if message == "" {
			t.Errorf("status %d: empty message", tc.status)
		}
	}
}

// TestClassifyHTTPNotFoundNamesEndpointOrModel keeps the 404 wording honest:
// a 404 can name the model or the endpoint path, and the caller knows which
// one it asked for.
func TestClassifyHTTPNotFoundNamesEndpointOrModel(t *testing.T) {
	_, message := ClassifyHTTP(404, "")
	if !strings.Contains(message, "端点") || !strings.Contains(message, "模型") {
		t.Errorf("404 message=%q want it to name both the endpoint and the model", message)
	}
}

// TestTextCoversEveryCode checks that every machine code renders to a short
// Chinese phrase and that an unknown code still gets a usable fallback.
func TestTextCoversEveryCode(t *testing.T) {
	codes := []string{
		CodeQuota,
		CodeRateLimit,
		CodeAuthInvalid,
		CodeModelUnknown,
		CodeContextExceeded,
		CodeProtocolUnsupported,
		CodeNetwork,
		CodeEngineInternal,
		CodeUnknown,
	}
	for _, code := range codes {
		text := Text(code)
		if text == "" {
			t.Errorf("code %q: empty text", code)
			continue
		}
		if !containsHan(text) {
			t.Errorf("code %q: text %q is not Chinese", code, text)
		}
		if n := len([]rune(text)); n > 20 {
			t.Errorf("code %q: text %q is %d runes, want at most 20", code, text, n)
		}
	}
	if fallback := Text("no_such_code"); fallback == "" || !containsHan(fallback) {
		t.Errorf("unknown code: text %q is not a usable fallback", fallback)
	}
}

// TestFromCodexErrorInfo covers the shapes Codex sends: a one-key object with
// the status one level down, a bare string, and malformed input.
func TestFromCodexErrorInfo(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"http connection failed with 402", `{"httpConnectionFailed": {"httpStatusCode": 402}}`, CodeQuota},
		{"stream disconnected with 401", `{"responseStreamDisconnected": {"httpStatusCode": 401}}`, CodeAuthInvalid},
		{"too many attempts with 429", `{"responseTooManyFailedAttempts": {"httpStatusCode": 429}}`, CodeRateLimit},
		{"status field name", `{"httpConnectionFailed": {"status": 404}}`, CodeModelUnknown},
		{"statusCode field name", `{"httpConnectionFailed": {"statusCode": 405}}`, CodeProtocolUnsupported},
		{"status as string", `{"httpConnectionFailed": {"httpStatusCode": "402"}}`, CodeQuota},
		{"no status", `{"responseStreamDisconnected": {}}`, CodeEngineInternal},
		{"null status", `{"responseStreamDisconnected": {"httpStatusCode": null}}`, CodeEngineInternal},
		{"stream connection failed with 500", `{"responseStreamConnectionFailed": {"httpStatusCode": 500}}`, CodeNetwork},
		{"bare string", `"other"`, CodeEngineInternal},
		{"null", `null`, CodeEngineInternal},
		{"empty", ``, CodeEngineInternal},
		{"invalid json", `{not json`, CodeEngineInternal},
		{"array", `["other"]`, CodeEngineInternal},
	} {
		code, message := FromCodexErrorInfo(json.RawMessage(tc.raw))
		if code != tc.want {
			t.Errorf("%s: code=%q want %q", tc.name, code, tc.want)
		}
		if message == "" {
			t.Errorf("%s: empty message", tc.name)
		}
		if n := len([]rune(message)); n > 20 {
			t.Errorf("%s: message %q is %d runes, want at most 20", tc.name, message, n)
		}
	}
}

// TestFromCodexErrorInfoNamesTheScenario checks that the message names the
// Codex symptom while the code keeps the root cause.
func TestFromCodexErrorInfoNamesTheScenario(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{`{"httpConnectionFailed": {"httpStatusCode": 402}}`, "请求失败"},
		{`{"responseStreamConnectionFailed": {"httpStatusCode": 500}}`, "连接失败"},
		{`{"responseStreamDisconnected": {"httpStatusCode": 500}}`, "连接中断"},
		{`{"responseTooManyFailedAttempts": {"httpStatusCode": 429}}`, "多次尝试失败"},
	} {
		_, message := FromCodexErrorInfo(json.RawMessage(tc.raw))
		if message != tc.want {
			t.Errorf("%s: message=%q want %q", tc.raw, message, tc.want)
		}
	}
}

// TestMessagesCarryNoSecret checks that no vendor body text reaches the
// message: a rejected key or a full URL must not be shown to the user.
func TestMessagesCarryNoSecret(t *testing.T) {
	secret := "sk-live-SECRET-1234"
	body := `{"error":{"message":"Incorrect API key provided: ` + secret + `","url":"https://api.example.invalid/v1/chat?key=` + secret + `"}}`
	for _, status := range []int{400, 401, 402, 403, 404, 429, 500} {
		_, message := ClassifyHTTP(status, body)
		if strings.Contains(message, secret) {
			t.Errorf("status %d: message %q leaks the secret", status, message)
		}
		if strings.Contains(message, "api.example.invalid") {
			t.Errorf("status %d: message %q leaks the url", status, message)
		}
	}
	_, message := FromCodexErrorInfo(json.RawMessage(`{"httpConnectionFailed": {"httpStatusCode": 401, "apiKey": "` + secret + `"}}`))
	if strings.Contains(message, secret) {
		t.Errorf("codex error info: message %q leaks the secret", message)
	}
}

// containsHan reports whether the text holds at least one Chinese character.
func containsHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
