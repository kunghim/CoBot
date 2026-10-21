---
name: cobot-git-cleanup
description: CoBot 项目专用（kunghim/CoBot 仓库及其 worktree，其他项目不要用）。用户要求清理 CoBot 的分支、worktree 或 .tmp 临时目录时使用：先列清单和依据，用户确认后再删。不用于删除 main/dev，也不用于 .local/ 笔记。
---

# CoBot 清理分支、worktree 与临时目录

规则来源：仓库的 `docs/reference/branching.md`（分支）、`docs/workflows/docs.md`（`.tmp/` 规则）。先读它们。

## 步骤

1. `git fetch --prune origin`，然后列出候选，每项写明依据：
   - 本地和远端主题分支：已合入 `origin/dev`（`git branch --merged origin/dev`，或对应 PR 已合并）；
   - worktree：`git worktree list`，标出分支已合入、目录无未提交改动、没有进程在用的；
   - `.tmp/` 下的子目录：大小、最后修改时间，标出不属于正在进行的任务的。
2. 以下永远不列为可删：`main`、`dev`、当前检出的分支、有未提交改动的 worktree、正被测试或服务使用的数据根、主目录 `.local/`。
3. 把清单给用户，等用户逐项或整体确认。没有确认不删任何东西。
4. 按确认执行：本地分支 `git branch -d`（不用 `-D`，删不掉就报告原因）；worktree `git worktree remove`（不加 `--force`）；
   远端分支删除属于推送类操作，只在用户明确点名时 `git push origin --delete <分支>`；`.tmp/` 子目录直接删除。
5. 汇报删了什么、跳过了什么及原因。
