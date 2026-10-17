package collaboration

import "github.com/kunghim/CoBot/internal/auth"

// The tools a member's model is offered. Every member is its own Codex
// session that knows nothing of the others, so these tools are its only way
// to reach the team; they are kept to the few that move the work, and each
// role gets only what it may use. CoBot keeps the task lifecycle itself.
const (
	RoleLeader = "leader"
	RoleMember = "member"

	// BridgeEnvRole names the role a spawned bridge serves.
	BridgeEnvRole = auth.BridgeEnvRole
)

var (
	leaderTools = []string{"send_message", "ask_user", "create_task", "review_task", "view_tasks"}
	memberTools = []string{"send_message", "ask_user", "deliver_task", "view_tasks"}
)

// ToolsForRole lists the tools offered to a role. An unknown role (a bridge
// spawned without one) gets both sets; the core still checks every call.
func ToolsForRole(role string) []string {
	switch role {
	case RoleLeader:
		return leaderTools
	case RoleMember:
		return memberTools
	default:
		return []string{"send_message", "ask_user", "create_task", "review_task", "deliver_task", "view_tasks"}
	}
}

// toolDescriptions is what the model reads about each tool. The legacy tools
// stay callable for sessions that learned them, but are no longer offered.
var toolDescriptions = map[string]string{
	"send_message": "给同团队的其他成员发消息，对方会被唤醒处理。同一内容要告诉几个人时用 targetBotIds 一次发完。" +
		"只是顺带告知、对方不需要做任何事时设 notifyOnly=true：对方下一轮会看到，不会被唤醒。交付任务请用交付工具，不要用消息代替。",
	"ask_user": "向用户提问。调用后请结束这一轮，用户回答后你会被唤醒。",
	"create_task": "创建任务并指派给一名成员（用户确认方案后再用）。创建后系统会把任务送到负责人手上，不必另发消息。" +
		"有先后顺序时用 dependencyIds：后续任务会等前置任务完成后才送到负责人手上。",
	"review_task": "验收成员交付的任务。decision=accept：通过，任务完成，并放行依赖它的任务；" +
		"decision=reject：打回，必须在 comment 写明缺什么，任务会退回负责人。",
	"deliver_task": "交付你负责的任务：在 summary 里写清做了什么、怎么运行或查看成果、实际做了哪些验证以及结果。" +
		"交付后队长会收到通知并验收，你结束这一轮即可；被打回时你会收到原因。",
	"view_tasks": "查看任务。不传 taskId 时列出团队全部任务：负责人、状态、还在等哪些任务完成；传 taskId 时返回这个任务的详情和交付记录。",

	"get_context":       "读取当前 Bot 的团队、成员、任务与最近协作上下文",
	"list_tasks":        "列出本团队任务（可按状态过滤）",
	"get_task":          "读取一个任务及其证据与转交记录",
	"list_roles":        "列出内置角色库（仅队长可见完整库）",
	"propose_task":      "以成员身份向队长提交任务提议（不直接建任务）",
	"assign_task":       "更换任务负责人（仅队长，按安全换人规则）",
	"update_task":       "更新任务状态（owner/队长，done 需 confirm_task）",
	"submit_evidence":   "为任务提交证据引用（owner 或队长）",
	"report_completion": "报告完成，任务进入 review（不直接 done）",
	"confirm_task":      "确认任务完成为 done（仅队长或用户，需证据）",
	"offer_transfer":    "提议转交任务给同团队成员（owner）",
	"accept_transfer":   "接受转交并正式更换负责人（目标成员）",
	"request_member":    "请求用户批准引入指定角色成员（仅队长）",
	"request_merge":     "请求用户批准合并指定候选版本（绑定 SHA 与证据）",
	"remember":          "记录你的工作方法或偏好",
	"search_memory":     "检索你自己的记忆",
}

// offered reports whether any role is offered the tool.
func offered(name string) bool {
	for _, tool := range ToolsForRole("") {
		if tool == name {
			return true
		}
	}
	return false
}
