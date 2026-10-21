# 接力规则

适用：改需求、链路、卡片、交接相关的代码或角色指令，或梳理接力相关的需求。代码以 `internal/domain/requirement.go`、
`internal/app/relay.go`、`relay_context.go`、`internal/collaboration/` 为准。

## 需求与链路

- 项目经理用 `start_requirement` 提交需求文档和有序的环节（`stages`），用户确认后才开始流转。
- 链路是串行的，没有依赖图、并行或汇合点；确认后不能修改链路。
- 第一环必须是用户确认，前面最多一环需求评审；最后一环是项目经理验收汇报。
- 评审环节要给出合法的评审类型，负责人必须是团队现有成员；项目经理不承担普通工作环节。

| 环节 | 含义 |
| --- | --- |
| `confirm` | 用户确认（需求和链路，或某份设计） |
| `design` | 产品设计与原型 |
| `review` | 评审员按评审类型的检查清单审查 |
| `test_cases` | 实现前写测试用例 |
| `implement` | 实现 |
| `test` | 执行测试 |
| `accept` | 项目经理检查并向用户汇报 |

另有两种特殊卡片：修改需求的 `draft`，处理上报的 `escalation`。
卡片状态 `todo / doing / done / cancelled`；需求状态 `active / accepting / done / cancelled`。

## 工具

| 身份 | 工具 |
| --- | --- |
| 项目经理 | `start_requirement`、`handoff`、`view_tasks`、`send_message`、`ask_user` |
| 成员 | `handoff`、`view_tasks`、`send_message`、`ask_user`、`browser` |
| 子智能体 | 不能调用 `handoff`、`start_requirement`、`ask_user` |

## 交接

| 动作 | 结果 |
| --- | --- |
| `done` | 完成，系统按链路决定下一环；通过时可附不阻塞的 `suggestions` |
| `back` | 评审或测试把卡片退回产出环节：`rerun` 改完重走后续，`return_to_me` 改完直接回到检查者 |
| `escalate` | 上报项目经理 |
| `resume` | 项目经理处理后交回原环节 |

- 退回次数受团队设置 `reviewRejectLimit` 约束，超过后不再自动退回，交给用户决定。
- 确认卡可以要求修改并选择改完直接回到确认；最终验收由用户决定；用户可以取消需求。
- 下一环由系统决定，模型不能选择交给谁。留言不推进业务。

## 上下文与证据

- 卡片携带需求、链路、当前位置和相关交接；每轮只带变化的部分。
- 执行记录按接卡到交接的轮次收集，来源是命令和文件变化步骤。
  命令的退出码和模型的说明要分开核对；脚本写入的文件不一定出现在文件变化里。
