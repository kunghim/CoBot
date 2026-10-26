# Issue 分诊与认领命令

适用场景：维护者分诊 Issue，或需要了解 `/claim` 等命令的细节。贡献者的用法摘要见 [CONTRIBUTING.md](../../CONTRIBUTING.md)。

## 分诊标签

- 每个 Issue 一个类型标签：`bug`、`enhancement`、`documentation`、`dependencies`；加上相关模块标签
  （`area: installation`、`area: codex`、`area: collaboration`、`area: ui`）；只保留一个当前状态。
- 状态：`status: needs-triage` 新报告，`status: needs-info` 需要补充信息，`status: accepted` 已认可范围，
  `status: in-progress` 由认领命令自动设置。
- `priority: high` 只用于数据丢失、核心功能大面积受阻等严重影响；普通报告不加优先级。
- `good first issue` 要求任务有边界、有起点和验收标准；`help wanted` 表示可以接手。指引不足时不标新手友好。
- 关闭重复或超出范围的报告时说明原因，必要时链接原 Issue。
- 维护者尽量为影响其他贡献者的重要中文讨论补充英文决策摘要。

## `ai-discovered`

- 表示问题或需求由 AI 发现，与提交者是谁无关；只是 AI 翻译或润色不算。它说明来源，不代表已验证或已接受。
- 表单的“发现来源”选择 **AI-discovered / AI 发现** 时自动加标；修改或删除这项声明时自动去标。
  其他情况由维护者核实后添加，不根据文风猜测。
- 通过 CLI/API 创建 Issue 时，在正文加入下面这段即可触发自动标记：

  ```markdown
  ### Discovery source / 发现来源

  AI-discovered / AI 发现
  ```

## 认领命令

在 Issue 下单独发一条评论，命令不带参数：

| 命令 | 结果 |
| --- | --- |
| `/claim` | 认领开放且无人负责的 Issue，设为 `status: in-progress` |
| `/unclaim` | 只取消自己的认领，恢复认领前的状态 |
| `/help` | 显示命令说明 |

- 在 GitHub 上直接指派（包括指派自己）与 `/claim` 等效：自动设为 `status: in-progress` 并记录之前状态；移除最后一位负责人时自动恢复之前的认领状态。

- 认领无需维护者批准；已有负责人时不会被替换，维护者仍可在 GitHub 中调整。
- 负责人是承担交付责任的 GitHub 账号；`ai-discovered` 不代表认领，也不会启动 AI 编码服务。
- 认领不代表需求获批，也不会关闭 Issue、合并 PR 或安排发布。
- `/unclaim` 前先留言交代进度和分支/PR 链接。Issue 保持开放，其他负责人和维护者期间改过的状态都会保留。
  手工分配且没有历史状态的，取消后若仍是无人负责的 `in-progress`，回到 `status: needs-triage`。
- 提 PR 前先在 Issue 回复处理方案和完成状态；解决该问题的 PR 用 `Closes #123`，合入 `dev` 时自动关闭。
  只部分处理的用 `Refs #123` 关联并保持开放。流程见[分支、审查与合并](branching.md)。
- 命令按评论顺序处理，机器人回复结果。必须发新评论，编辑旧评论不会执行；PR 评论不处理。
- 不要删除机器人的回复：其中的元数据保存了先前状态和已处理命令的位置，用于安全重试。
- 工作流中途失败时，维护者可以重跑，或手动触发 **issue management** 并填入 Issue 编号。
- 实现见 `.github/workflows/issue-management.yml` 和 `scripts/issue-commands.mjs`。
