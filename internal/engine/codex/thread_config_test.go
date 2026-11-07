package codex

import (
	"reflect"
	"testing"

	"github.com/kunghim/CoBot/internal/engine"
)

// TestThreadConfigInCodexsNames pins the names Codex is told a thread's settings
// by. They are Codex's own; a wrong one is silently ignored, not refused.
func TestThreadConfigInCodexsNames(t *testing.T) {
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range WindowsSandboxConfig() {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	env := map[string]string{"KEY": "value"}
	for name, tc := range map[string]struct {
		spec engine.ThreadSpec
		want map[string]any
	}{
		"nothing to say": {engine.ThreadSpec{Cwd: "here"}, with(nil)},
		"model": {
			engine.ThreadSpec{ContextWindow: 1000, DisableWebSearch: true},
			with(map[string]any{"model_context_window": 1000, "web_search": "disabled"}),
		},
		"bridge": {
			engine.ThreadSpec{ToolBridge: &engine.ToolBridge{Name: "cobot", Command: "cobot", Args: []string{"mcp"}, Env: env, Required: true, AutoApprove: true}},
			with(map[string]any{
				"mcp_servers.cobot.enabled":                     true,
				"mcp_servers.cobot.command":                     "cobot",
				"mcp_servers.cobot.args":                        []string{"mcp"},
				"mcp_servers.cobot.env":                         env,
				"mcp_servers.cobot.required":                    true,
				"mcp_servers.cobot.default_tools_approval_mode": "approve",
			}),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ThreadConfigOf(tc.spec); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("config = %#v, want %#v", got, tc.want)
			}
		})
	}
}
