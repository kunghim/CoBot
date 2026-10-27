package codex

import (
	"encoding/json"
	"strings"

	patches "github.com/kunghim/CoBot/internal/changes"
	"github.com/kunghim/CoBot/internal/engine"
)

// Size caps keep one step small enough to store and stream; the full output
// stays in the engine's own history.
const (
	maxStepOutput = 16 << 10
	// maxTurnDiff bounds the turn diff kept as a step.
	maxTurnDiff   = 256 << 10
	maxStepDiff   = 64 << 10
	maxStepResult = 4 << 10
)

// normalizeStep maps a Codex ThreadItem onto the engine-neutral Step the
// conversation shows. Replies, user input and bookkeeping items return nil.
func normalizeStep(raw json.RawMessage) *engine.Step {
	var head struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &head) != nil || head.ID == "" {
		return nil
	}
	step := &engine.Step{ID: head.ID, Status: stepStatus(head.Status)}
	var detail any
	switch head.Type {
	case "agentMessage":
		// What the model says is a step, in the place it was said, whether the
		// engine called it a working remark or gave it no phase at all (a
		// third-party model does not). Only the answer the engine marks as
		// final is left out: it is the turn's reply.
		var item struct {
			Text  string `json:"text"`
			Phase string `json:"phase"`
		}
		_ = json.Unmarshal(raw, &item)
		if item.Phase == "final_answer" {
			return nil
		}
		step.Kind = engine.StepMessage
		detail = map[string]any{"text": item.Text}
	case "collabAgentToolCall":
		var item struct {
			Tool              string   `json:"tool"`
			Prompt            *string  `json:"prompt"`
			Model             *string  `json:"model"`
			ReceiverThreadIDs []string `json:"receiverThreadIds"`
		}
		_ = json.Unmarshal(raw, &item)
		step.Kind = engine.StepAgent
		out := map[string]any{"tool": item.Tool, "agents": len(item.ReceiverThreadIDs)}
		if item.Prompt != nil {
			out["prompt"] = clipHead(*item.Prompt, maxStepOutput)
		}
		if item.Model != nil {
			out["model"] = *item.Model
		}
		detail = out
	case "subAgentActivity":
		var item struct {
			Kind      string `json:"kind"`
			AgentPath string `json:"agentPath"`
		}
		_ = json.Unmarshal(raw, &item)
		step.Kind = engine.StepAgent
		detail = map[string]any{"activity": item.Kind, "agent": item.AgentPath}
	case "reasoning":
		var item struct {
			Summary []string `json:"summary"`
		}
		_ = json.Unmarshal(raw, &item)
		step.Kind = engine.StepReasoning
		detail = map[string]any{"text": strings.Join(item.Summary, "\n\n")}
	case "commandExecution":
		var item struct {
			Command          string  `json:"command"`
			Cwd              string  `json:"cwd"`
			ExitCode         *int    `json:"exitCode"`
			DurationMs       *int64  `json:"durationMs"`
			AggregatedOutput *string `json:"aggregatedOutput"`
		}
		_ = json.Unmarshal(raw, &item)
		step.Kind = engine.StepCommand
		out := map[string]any{"command": item.Command, "cwd": item.Cwd}
		if item.ExitCode != nil {
			out["exitCode"] = *item.ExitCode
		}
		if item.DurationMs != nil {
			out["durationMs"] = *item.DurationMs
		}
		if item.AggregatedOutput != nil {
			out["output"] = clipTail(*item.AggregatedOutput, maxStepOutput)
		}
		detail = out
	case "fileChange":
		var item struct {
			Changes []struct {
				Path string `json:"path"`
				Diff string `json:"diff"`
				Kind struct {
					Type     string `json:"type"`
					MovePath string `json:"move_path"`
				} `json:"kind"`
			} `json:"changes"`
		}
		_ = json.Unmarshal(raw, &item)
		step.Kind = engine.StepFileChange
		changes := make([]map[string]any, 0, len(item.Changes))
		for _, change := range item.Changes {
			stats := patches.Count(change.Diff)
			entry := map[string]any{"path": change.Path, "kind": change.Kind.Type, "diff": clipHead(change.Diff, maxStepDiff),
				"additions": stats.Additions, "deletions": stats.Deletions, "statsComplete": stats.StatsComplete,
				"binary": stats.Binary, "diffTruncated": len(change.Diff) > maxStepDiff}
			if change.Kind.MovePath != "" {
				entry["movePath"] = change.Kind.MovePath
			}
			changes = append(changes, entry)
		}
		detail = map[string]any{"changes": changes}
	case "mcpToolCall":
		var item struct {
			Server     string          `json:"server"`
			Tool       string          `json:"tool"`
			Arguments  json.RawMessage `json:"arguments"`
			DurationMs *int64          `json:"durationMs"`
			Error      *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result *struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		_ = json.Unmarshal(raw, &item)
		step.Kind = engine.StepTool
		out := map[string]any{"server": item.Server, "tool": item.Tool, "arguments": rawOrNull(item.Arguments)}
		if item.DurationMs != nil {
			out["durationMs"] = *item.DurationMs
		}
		if item.Error != nil && item.Error.Message != "" {
			out["error"] = item.Error.Message
		}
		if item.Result != nil {
			var texts []string
			for _, content := range item.Result.Content {
				if content.Type == "text" && content.Text != "" {
					texts = append(texts, content.Text)
				}
			}
			if len(texts) > 0 {
				out["result"] = clipHead(strings.Join(texts, "\n"), maxStepResult)
			}
		}
		detail = out
	case "dynamicToolCall":
		var item struct {
			Namespace  string          `json:"namespace"`
			Tool       string          `json:"tool"`
			Arguments  json.RawMessage `json:"arguments"`
			DurationMs *int64          `json:"durationMs"`
			Success    *bool           `json:"success"`
		}
		_ = json.Unmarshal(raw, &item)
		step.Kind = engine.StepTool
		out := map[string]any{"server": item.Namespace, "tool": item.Tool, "arguments": rawOrNull(item.Arguments)}
		if item.DurationMs != nil {
			out["durationMs"] = *item.DurationMs
		}
		if item.Success != nil && !*item.Success && step.Status == engine.StepCompleted {
			step.Status = engine.StepFailed
		}
		detail = out
	case "webSearch":
		var item struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(raw, &item)
		step.Kind = engine.StepWebSearch
		detail = map[string]any{"query": item.Query}
	case "contextCompaction":
		step.Kind = engine.StepCompaction
		detail = map[string]any{}
	case "plan":
		var item struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &item)
		step.Kind = engine.StepPlan
		detail = map[string]any{"text": item.Text}
	default:
		return nil
	}
	data, err := json.Marshal(detail)
	if err != nil {
		return nil
	}
	step.Detail = data
	return step
}

// stepStatus maps a Codex item status; items without one (reasoning, plan,
// web search) are judged by the notification that carried them.
func stepStatus(status string) string {
	switch status {
	case "inProgress":
		return engine.StepInProgress
	case "failed":
		return engine.StepFailed
	case "declined":
		return engine.StepDeclined
	case "completed":
		return engine.StepCompleted
	default:
		return ""
	}
}

func rawOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

// clipTail keeps the end of long command output, where results and errors
// usually are.
func clipTail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := len(s) - limit
	for cut < len(s) && !utf8Start(s[cut]) {
		cut++
	}
	return "…\n" + s[cut:]
}

func clipHead(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// errorCode reduces Codex's error info (a bare string or a one-key object
// such as {"httpConnectionFailed": {...}}) to a stable name.
func errorCode(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return name
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		for key := range object {
			return key
		}
	}
	return ""
}

// Statistics and file positions are computed while the full turn patch is
// available; a clipped preview must never change the totals shown later.
func turnDiffDetail(diff string) map[string]any {
	files, filesComplete := patches.ParseTurn(diff)
	stored := clipHead(diff, maxTurnDiff)
	truncated := len(diff) > maxTurnDiff
	end := len(stored)
	if truncated {
		end -= len("\n…")
	}
	complete := filesComplete
	for index := range files {
		files[index].DiffTruncated = files[index].End > end
		complete = complete && files[index].StatsComplete
	}
	return map[string]any{"diff": stored, "files": len(files), "changes": files, "filesComplete": filesComplete, "statsComplete": complete, "diffTruncated": truncated}
}
