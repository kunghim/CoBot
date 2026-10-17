package codex

// HandledMessages are the app-server notifications and server requests CoBot
// acts on: the cases of Server.handleNotify and Server.handleRequest here and
// of Scheduler.onNotification in the app. Everything else Codex sends is
// forwarded but ignored, which is what a recording's report calls "not
// handled". TestHandledListsMatchTheCode keeps this list honest.
var HandledMessages = []string{
	"account/login/completed",
	"account/rateLimits/updated",
	"account/updated",
	"error",
	"hook/completed",
	"item/agentMessage/delta",
	"item/commandExecution/outputDelta",
	"item/commandExecution/requestApproval",
	"item/completed",
	"item/fileChange/requestApproval",
	"item/permissions/requestApproval",
	"item/reasoning/summaryTextDelta",
	"item/started",
	"mcpServer/elicitation/request",
	"thread/closed",
	"thread/started",
	"thread/status/changed",
	"thread/tokenUsage/updated",
	"turn/completed",
	"turn/started",
	"warning",
}

// HandledItemTypes are the thread item types normalizeStep turns into a
// visible step; other item types arrive but show nothing.
var HandledItemTypes = []string{
	"agentMessage",
	"collabAgentToolCall",
	"commandExecution",
	"dynamicToolCall",
	"fileChange",
	"mcpToolCall",
	"plan",
	"reasoning",
	"subAgentActivity",
	"webSearch",
}
