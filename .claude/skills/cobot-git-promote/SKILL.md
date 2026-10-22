---
name: cobot-git-promote
description: CoBot 项目专用（kunghim/CoBot 仓库，维护者操作，其他项目不要用）。用户明确要求把 dev 晋级到 main、或晋级后把 main 同步回 dev 时使用。不用于普通主题 PR，也不发布版本。
---

# CoBot dev 晋级 main

规则来源：仓库的 `docs/reference/branching.md`。先读它。

## 步骤

1. `git fetch origin`。列出 `origin/main..origin/dev` 的提交和其中合入的 PR，给用户确认这次要晋级的范围。
2. 用户确认后，从本仓库 `dev` 向 `main` 开 PR（必须是本仓库的 `dev`，CI 会拒绝其他来源）：
   正文汇总合入的 PR；对在 `main` 上真正解决的 Issue 写 `Closes #编号`，只是有进展的写 `Refs #编号`；
   有 `breaking-change` 的 PR 单独列出用户需要做的事。
3. 等 CI 和用户决定。合并方式必须是 Merge commit（保留长期分支的共同历史），不 squash、不 rebase；用户明确说合并才合并。
4. 合并后把 `main` 同步回 `dev`：能快进就快进，不改写历史；推送 `dev` 属于本次授权内的步骤，完成后汇报。
5. 不触发发布草稿；发布是另一项需要用户单独决定的操作（见 `docs/reference/release.md`）。
