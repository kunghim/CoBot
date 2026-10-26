---
name: cobot-issue
description: CoBot 项目专用（kunghim/CoBot 仓库，其他项目不要用）。用户在使用 CoBot 时报问题、提需求、丢截图，或说“提个 issue”时使用：拆分需求、在代码里定位、查重，然后提交给编码智能体开发的 Issue。不用于写代码修复、开 PR，也不用于关闭或清理 Issue。
---

# CoBot 需求分析与提 Issue

规则来源：仓库的 `docs/workflows/issue.md`（标签见 `docs/reference/triage.md`）。先读它们。

## 步骤

1. 把用户这次的消息拆成逐条需求；有需要用户拍板的产品决定，先问。
2. 每条在 `dev` 上定位到 `文件:行号`，查提交历史，查重（Issue、PR、分支、stash）。
3. 按详略写正文：小需求直接提交；大需求先给用户看草稿，确认后提交（可拆父子 Issue）。
4. 已有 Issue 能覆盖的，追加「补充」一节，不新开。
5. 汇报：每条的 Issue 链接、一句话原因、与哪些 Issue 改同一批文件或有依赖；没验证的说明没验证。

不写业务代码，不开分支，不提交仓库改动。
