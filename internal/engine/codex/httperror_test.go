package codex

import (
	"testing"

	"github.com/kunghim/CoBot/internal/failure"
)

// Without structured detail the engine passes the vendor's error body on as
// its message; its status is read from there.
func TestHTTPErrorStatusReadsAVendorErrorBody(t *testing.T) {
	cases := []struct {
		message string
		status  int
		ok      bool
	}{
		{`{"type":"error","status":400,"error":{"message":"bad model"}}`, 400, true},
		{` {"status":429} `, 429, true},
		{`{"status":200}`, 0, false},
		{`stream disconnected before completion`, 0, false},
		{`{not json`, 0, false},
		{"data:{\"status\":502}\n\n", 502, true},
	}
	for _, c := range cases {
		status, ok := httpErrorStatus(c.message)
		if status != c.status || ok != c.ok {
			t.Fatalf("%q: got %d/%v, want %d/%v", c.message, status, ok, c.status, c.ok)
		}
	}
}

// A vendor that refuses a streamed request answers with one server-sent
// event and no status; its own error code still names the failure.
func TestAStreamedVendorErrorIsClassified(t *testing.T) {
	message := "data:{\"error\":{\"code\":\"responses_feature_not_supported\",\"message\":\"tool type 'web_search' is not supported by this gateway phase.\",\"type\":\"unsupported_feature\"}}\n\n"
	if _, ok := httpErrorStatus(message); ok {
		t.Fatal("the body carries no status")
	}
	body, ok := vendorErrorBody(message)
	if !ok {
		t.Fatal("the streamed body is not recognized")
	}
	if code, ok := failure.ClassifyBody(body); !ok || code != failure.CodeProtocolUnsupported {
		t.Fatalf("classified as %q/%v", code, ok)
	}
	if _, ok := vendorErrorBody("stream disconnected before completion"); ok {
		t.Fatal("plain text is not a vendor body")
	}
	// An event line, or a second event, may come with the error.
	for _, raw := range []string{
		"event: error\ndata: {\"error\":{\"code\":\"rate_limit_exceeded\"}}\n\n",
		"data:{\"error\":{\"code\":\"rate_limit_exceeded\"}}\n\ndata:[DONE]\n\n",
	} {
		body, ok := vendorErrorBody(raw)
		if !ok {
			t.Fatalf("%q is not recognized", raw)
		}
		if code, _ := failure.ClassifyBody(body); code != failure.CodeRateLimit {
			t.Fatalf("%q classified as %q", raw, code)
		}
	}
}
