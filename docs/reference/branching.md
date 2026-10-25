# 分支、审查与合并

适用场景：维护者审查和合并 PR、晋级 `dev` 到 `main`、清理分支。贡献者需要知道的部分见 [CONTRIBUTING.md](../../CONTRIBUTING.md)。

## 分支流

`主题分支 → dev → main → 发布`

- `main` 是默认的稳定分支；`dev` 是集成分支。两条长期分支都保留。
- 普通 PR（包括依赖更新）以 `dev` 为目标。`main` 只接受本仓库 `dev` 的晋级 PR 和 Dependabot 安全更新，
  由 `scripts/check-pr-route.mjs` 在 CI 中强制执行。
- 服务端分支保护取决于仓库可见性和 GitHub 套餐。

## 审查与合并

- 未完成的 PR 用 Draft；改动和证据就绪后再标记为 Ready for review。
- 合并前 CI 必须通过，重要审查意见已处理或由维护者明确了结。追加提交后重新核对差异和当前 CI。
- 合并由维护者决定；CI 通过不等于自动批准或合并。只有一位维护者时不设强制审批人数。
- 主题 PR 合入 `dev` 的方式由维护者选择；`dev` 晋级 `main` 使用 Merge commit，保留长期分支的共同历史。
- 晋级后把 `main` 同步回 `dev`，不改写历史，能快进就快进。下一项任务从新的主题分支开始。
- 确认工作已集成、且没有 worktree 或其他任务在用后，再删除已完成的主题分支。

## 关联与关闭 Issue

- 普通 PR 用 `Refs #123` 关联；合入 `dev` 不等于已发布的修复。
- `dev → main` 晋级 PR 中，对在默认分支上真正解决的问题使用 `Closes #123`。发布时可补充修复所在的版本。
- 普通 PR 合入 `dev` 不发布；晋级到 `main` 后触发检查、打包与自动发布，见[自动发布](release.md)。

## 标签

- PR 使用类型标签（`bug`、`enhancement`、`documentation`、`dependencies`）和相关模块标签
  （`area: installation`、`area: codex`、`area: collaboration`、`area: ui`），不是每个 PR 都需要模块标签。依赖机器人保留自己的生态标签。
- PR 进度看 GitHub 的 Draft、审查和合并/关闭状态，不用 Issue 的状态标签。
- `breaking-change`：只在升级后用户或调用方必须改变用法时添加，例如删除配置、不兼容的 API/CLI 变化、让现有使用或回滚失效的数据变化。
  PR 要写明用户需要采取的措施和迁移、回滚限制，供发布说明使用。新增可选功能、内部重构、普通数据库迁移不自动算破坏性变更；
  这个标签本身也不决定下一个版本号。
