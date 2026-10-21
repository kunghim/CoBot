package codex

import "testing"

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
	}
	for _, c := range cases {
		status, ok := httpErrorStatus(c.message)
		if status != c.status || ok != c.ok {
			t.Fatalf("%q: got %d/%v, want %d/%v", c.message, status, ok, c.status, c.ok)
		}
	}
}
