---
name: cobot-git-pr
description: CoBot 项目专用（kunghim/CoBot 仓库，其他项目不要用）。只在用户明确要求“提 PR / 开 PR / create PR”时使用：确认改动已被用户验收，按 CoBot 的 PR 模板向 dev 开 PR。不用于合并 PR、dev 晋级 main，也不在用户没说时主动使用。
---

# CoBot 开 PR

规则来源：仓库的 `docs/workflows/deliver.md` 和 `CONTRIBUTING.md`。先读它们。

## 前提（任一不满足就停下告诉用户，不开 PR）

- 用户这次明确说了开 PR，而且点名的就是这批改动。之前某次授权不算。
- 用户已经明确验收这批改动（例如说过“可以了”“验收通过”）。智能体自己跑通测试不算验收。
- 工作区没有未提交的、属于这批改动的文件。

## 步骤

1. 目标分支是 `dev`（只有本仓库 `dev` 能向 `main` 开晋级 PR，那是 `cobot-git-promote` 的事）。
2. 推送主题分支（属于本次授权），同时按 deliver.md 推送主目录 `.local/` 的笔记。
3. 标题用英文，写改动后的行为。正文按 `.github/pull_request_template.md` 四段填写：
   问题与关联（`Refs #编号`）、改动后的行为、实际验证（分开自动化 / 真实 Codex / 原生平台，写明未测范围）、兼容性与数据影响。
   界面改动附截图说明；不贴私人提示词、对话记录、本机路径或录制。
4. 加类型标签（`bug`、`enhancement`、`documentation`、`dependencies`），相关时加模块标签；
   只有升级后用户必须改用法时才加 `breaking-change` 并写迁移方法。未完成用 Draft。
5. 汇报 PR 链接和 CI 状态；不合并，不开启自动合并。
