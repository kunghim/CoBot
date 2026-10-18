package collaboration

// hints say in plain words why a call was refused and what to do instead, so
// a model reading a rejection does not have to guess from an error key.
var hints = map[string]string{
	"error.task.deliver_owner_only":        "这个任务不归你负责，只有负责人能交付。如果你有结果要告诉负责人，用 send_message 发给他。",
	"error.task.dependencies_pending":      "这个任务的前置任务还没完成，现在还不能交付。前置任务完成后系统会通知你。",
	"error.task.delivery_summary_required": "交付时 summary 不能为空：写清做了什么、怎么运行或查看、实际验证和结果。",
	"error.task.terminal":                  "这个任务已经结束（完成或取消），不需要再操作。",
	"error.task.review_not_pending":        "这个任务现在不是待验收状态，还不能验收；等负责人交付后系统会通知你。",
	"error.task.reject_reason_required":    "打回时必须在 comment 里写明缺什么。",
	"error.task.review_decision":           "decision 只能是 accept（通过）或 reject（打回）。",
	"error.task.captain_only":              "只有队长能做这个操作。",
	"error.message.target_required":        "请在 targetBotIds 或 targetBotId 里写明发给谁。",
	"error.idempotency.required":           "这次调用缺少调用标识，请重试一次。",
}

// withHint adds the plain-words hint of a refusal, when there is one.
func withHint(te *ToolError) *ToolError {
	if te == nil {
		return nil
	}
	hint, ok := hints[te.MessageKey]
	if !ok {
		return te
	}
	params := map[string]any{}
	for k, v := range te.Params {
		params[k] = v
	}
	params["hint"] = hint
	te.Params = params
	return te
}
