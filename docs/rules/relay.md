# 接力与协作工具规则

触发：改 `internal/app/relay*.go`、`chains.go`、`collaboration.go`，`internal/collaboration/**`，`internal/roles/**`。

- 业务只靠卡片推进：成员用 `handoff` 交接，下一环由系统按用户确认过的链路决定。不给模型加“选下一个人”的能力。
- `send_message` 只留言，不唤醒成员、不推进业务。不要把它改成派活手段。
- 机器人互相唤醒消耗同一条因果链的预算；用户消息开启新链。模型不能延长预算，超限后等用户确认。
- 子智能体不能调用 `handoff`、`start_requirement`、`ask_user`（`internal/collaboration/tools.go` 的 `handoffOnly`）；
  它的步骤、审批和用量归到所属成员。
- 新增或修改协作工具时写清：对哪些身份开放、子智能体能否调用、参数里的身份字段一律不信任（身份来自宿主绑定）。
- 旧的任务模型和工具（`create_task`、`review_task`、`deliver_task` 等）已删除，见到旧资料提到它们不要恢复；以 `tools.go` 为准。
- `internal/roles/` 是产品内置的角色指令和检查清单，给 CoBot 里的成员看，不是开发文档；改它等于改产品行为。

做完：`go test ./internal/app/... ./internal/collaboration/... ./tests/integration/...` 通过；
改了模型可见的工具或指令时，说明是否需要真实 Codex 验证。
