package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestNativeHookStatusUsesNativeTrustAndSource(t *testing.T) {
	for _, tc := range []struct {
		trust   string
		enabled bool
		path    string
		want    string
	}{
		{"untrusted", true, "/private/hooks.json", "review_required"},
		{"modified", true, "/private/hooks.json", "review_required"},
		{"trusted", true, "/private/hooks.json", "trusted"},
		{"trusted", false, "/private/hooks.json", "disabled"},
		{"trusted", true, "/project/hooks.json", "unconfigured"},
	} {
		t.Run(tc.want+tc.trust, func(t *testing.T) {
			inR, inW := io.Pipe()
			outR, outW := io.Pipe()
			client := NewClient(nopCloser{inW}, outR, ClientHooks{})
			t.Cleanup(func() { inW.Close(); outW.Close(); client.Wait() })
			go func() {
				scanner := bufio.NewScanner(inR)
				if !scanner.Scan() {
					return
				}
				var request struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if json.Unmarshal(scanner.Bytes(), &request) != nil || request.Method != "hooks/list" {
					return
				}
				writeJSONLine(outW, map[string]any{"id": request.ID, "result": map[string]any{"data": []any{map[string]any{"errors": []any{}, "hooks": []any{map[string]any{"command": "cobot hook --native-codex --data-root /private", "sourcePath": tc.path, "eventName": "preToolUse", "enabled": tc.enabled, "trustStatus": tc.trust}}}}}})
			}()
			server := &Server{client: client}
			if got := server.NativeHookStatus(context.Background(), "/workspace", "/private/hooks.json", "cobot hook --native-codex --data-root /private"); got != tc.want {
				t.Fatal(got, tc.want)
			}
		})
	}
}
