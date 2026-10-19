package collaboration

// hints say in plain words why a call was refused and what to do instead, so
// a model reading a rejection does not have to guess from an error key.
var hints = map[string]string{
	"error.card.owner_only":               "这张卡片不在你手上，只有持卡人能交接。有结果要告诉持卡人，用 send_message 留言。",
	"error.card.finished":                 "这张卡片已经交接过或已取消，不需要再操作。",
	"error.card.summary_required":         "交接时 summary 不能为空：写清做了什么、产出在哪里、实际验证和结果。",
	"error.card.outcome":                  "outcome 只能是 done、back、escalate 或 resume。",
	"error.card.back_not_allowed":         "只有评审和测试环节可以退回。发现前面的问题而你不负责检查时，用 outcome=\"escalate\" 上报项目经理。",
	"error.card.back_mode":                "退回时 mode 必须是 rerun（改完重新往下走）或 return_to_me（改完直接回到你）。",
	"error.card.leader_cannot_escalate":   "项目经理不能上报。需要用户决定的，用 ask_user 问用户。",
	"error.card.resume_not_escalation":    "只有上报卡可以用 resume 交回；其他卡片完成时用 outcome=\"done\"。",
	"error.card.resume_escalation":        "上报卡要用 outcome=\"resume\" 交回原环节，summary 写你的处理意见。",
	"error.card.document_required":        "这是需求文档的返工卡，交回时 document 必须是修改后的完整需求文档。",
	"error.requirement.leader_only":       "只有项目经理能提交需求和链路。",
	"error.requirement.document_required": "提交需求时 title 和 document 都不能为空。",
	"error.requirement.not_active":        "这个需求已经结束或被用户取消，卡片不再往下走，直接结束这一轮。",
	"error.requirement.route_invalid":     "链路不符合规则，按 reason 修改后重新提交。",
	"error.tool.subagent_forbidden":       "子智能体不能交接卡片、提交需求或向用户提问。把结果交给主智能体，由它来做。",
	"error.message.target_required":       "请在 targetBotIds 或 targetBotId 里写明发给谁。",
	"error.idempotency.required":          "这次调用缺少调用标识，请重试一次。",
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
