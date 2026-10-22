---
name: cobot-git-sync
description: CoBot 项目专用（kunghim/CoBot 仓库及其 worktree，其他项目不要用）。用户在 CoBot 里要求提交、commit、拉取、pull、推送、push 时使用：按 CoBot 的交付与同步流程处理代码，并顺带同步主检出目录 .local/ 的私人笔记仓库。不用于开 PR、合并。
---

# CoBot 代码与笔记同步

规则的唯一来源是仓库里的 `docs/workflows/deliver.md`。先读它，按它执行；本文件只列步骤，不重复规则。

## 步骤

1. 读 `docs/workflows/deliver.md`。用户这次说的动作（提交 / 拉取 / 推送）才做，没说的不做，也不追问要不要做。
2. 定位笔记：`git rev-parse --path-format=absolute --git-common-dir` 的同级 `.local/`。
   不存在或不是 Git 仓库（没有 `.local/.git`）就只处理代码。
3. 按 deliver.md 的对照表同时处理代码和笔记：
   - 提交：代码只 `git add` 本任务的具体路径；笔记只 add 本任务写过的文件。
   - 拉取：笔记用 `pull --ff-only`，不能快进就停下告诉用户。
   - 推送：先看 `git -C <主目录>/.local remote -v`。远端是 `kunghim/CoBot` 就停下告诉用户；
     还没设远端时按 docs.md 的规则设置（主仓库 origin 是用户 fork 就用同一地址，否则请用户给地址），
     并只在这一次提醒：fork 是公开的，想保密可另建私有仓库换过去；已有远端就不提醒；
     第一次用 `push -u origin HEAD:notes`，之后直接 `push`。
4. 汇报：分开列出代码和笔记各自提交、拉取或推送了什么；跳过笔记时说明原因。
