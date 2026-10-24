package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"reflect"
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

func TestTrustNativeHookWritesOnlyCoBotsEntry(t *testing.T) {
	const command = "cobot hook --native-codex --data-root /private"
	for _, tc := range []struct {
		name    string
		path    string
		managed bool
		want    string
		writes  bool
	}{
		{"own hook", "/private/hooks.json", false, "trusted", true},
		{"project hook", "/project/.codex/hooks.json", false, "unconfigured", false},
		{"managed hook", "/private/hooks.json", true, "unconfigured", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inR, inW := io.Pipe()
			outR, outW := io.Pipe()
			client := NewClient(nopCloser{inW}, outR, ClientHooks{})
			t.Cleanup(func() { inW.Close(); outW.Close(); client.Wait() })
			var written []any
			go func() {
				trusted := false
				scanner := bufio.NewScanner(inR)
				for scanner.Scan() {
					var request struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
						Params struct {
							Edits []map[string]any `json:"edits"`
						} `json:"params"`
					}
					if json.Unmarshal(scanner.Bytes(), &request) != nil {
						return
					}
					switch request.Method {
					case "hooks/list":
						trust := "untrusted"
						if trusted {
							trust = "trusted"
						}
						writeJSONLine(outW, map[string]any{"id": request.ID, "result": map[string]any{"data": []any{map[string]any{"errors": []any{}, "hooks": []any{map[string]any{
							"key": tc.path + ":pre_tool_use:0:0", "currentHash": "sha256:abc", "command": command, "sourcePath": tc.path,
							"eventName": "preToolUse", "enabled": true, "isManaged": tc.managed, "trustStatus": trust,
						}}}}}})
					case "config/batchWrite":
						for _, edit := range request.Params.Edits {
							written = append(written, edit)
						}
						trusted = true
						writeJSONLine(outW, map[string]any{"id": request.ID, "result": map[string]any{"status": "ok"}})
					default:
						return
					}
				}
			}()
			server := &Server{client: client}
			got, err := server.TrustNativeHook(context.Background(), "/e/Project/GitHub/CoBot", "/private/hooks.json", command)
			if err != nil || got != tc.want {
				t.Fatal(got, err)
			}
			if !tc.writes {
				if len(written) != 0 {
					t.Fatal("trusted a hook that is not CoBot's", written)
				}
				return
			}
			want := map[string]any{"keyPath": "hooks.state", "mergeStrategy": "upsert", "value": map[string]any{
				"/private/hooks.json:pre_tool_use:0:0": map[string]any{"trusted_hash": "sha256:abc"},
			}}
			if len(written) != 1 || !reflect.DeepEqual(written[0], want) {
				t.Fatalf("unexpected trust write: %#v", written)
			}
		})
	}
}
