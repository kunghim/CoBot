package codex

import "github.com/kunghim/CoBot/internal/engine"

// ThreadConfigOf is what a thread is started with, in Codex's own configuration
// names: the global overrides (for example the Windows MXC sandbox selection)
// and this thread's settings.
func ThreadConfigOf(spec engine.ThreadSpec) map[string]any {
	config := map[string]any{}
	for k, v := range WindowsSandboxConfig() {
		config[k] = v
	}
	if spec.ContextWindow > 0 {
		config["model_context_window"] = spec.ContextWindow
	}
	if spec.DisableWebSearch {
		config["web_search"] = "disabled"
	}
	if bridge := spec.ToolBridge; bridge != nil {
		prefix := "mcp_servers." + bridge.Name
		config[prefix+".enabled"] = true
		config[prefix+".command"] = bridge.Command
		config[prefix+".args"] = bridge.Args
		config[prefix+".env"] = bridge.Env
		config[prefix+".required"] = bridge.Required
		if bridge.AutoApprove {
			config[prefix+".default_tools_approval_mode"] = "approve"
		}
	}
	if len(config) == 0 {
		return nil
	}
	return config
}
