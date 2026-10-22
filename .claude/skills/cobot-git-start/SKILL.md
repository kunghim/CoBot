---
name: cobot-git-start
description: CoBot 项目专用（kunghim/CoBot 仓库及其 worktree，其他项目不要用）。用户要为 CoBot 开始新需求、新修复，或要求开分支、开 worktree 时使用：从最新 origin/dev 建主题分支或 worktree，并准备好前端构建、测试数据根和主目录 .local 笔记。不用于提交、推送或开 PR。
---

# CoBot 开始新任务

规则来源：仓库的 `docs/workflows/implement.md`、`docs/pitfalls.md`。先读它们；当前分支还没有这些文件时按 `AGENTS.md` 执行。

## 步骤

1. `git status --short` 有不属于本任务的改动时保留，不 stash、不丢弃，告诉用户。
2. `git fetch origin`，从 `origin/dev` 建主题分支：`feature/`、`fix/`、`docs/`、`refactor/` 加简短英文名。
   用户要求隔离或已有其他会话在主目录工作时，改用 worktree：`git worktree add <路径> -b <分支> origin/dev`。
3. 新 worktree 先准备：`npm ci --prefix frontend`，再 `npm run build --prefix frontend`（Go 构建要内嵌前端产物）。
4. 定位私人笔记：`git rev-parse --path-format=absolute --git-common-dir` 的同级 `.local/`；存在就读它的 `README.md`，
   不要在 worktree 里另建 `.local/`。
5. 需要运行或测试时，数据根用 `.tmp/test-runs/<任务名>/`，不用 `~/.cobot`、`~/.cobot-dev`；并行运行的 worktree 各用各的数据根，
   同一时间只能有一个 `dev`（Vite 5173 端口）。
6. 汇报：分支名、起点提交、工作目录、准备步骤结果。不主动问要不要提交或推送。
