# Contributing to CoBot / 贡献指南

CoBot is a local multi-agent application with one Go core shared by Web and
desktop. This guide covers how to contribute. Engineering rules live in
[docs/](docs/README.md) (written in Chinese; start with the
[contributor handbook](docs/handbook.md)); coding agents start from
[AGENTS.md](AGENTS.md). Private notes are never a prerequisite.

CoBot 是 Web 与桌面共用一套 Go 核心的本地多智能体应用。本文说明如何参与贡献；
工程规范见 [docs/](docs/README.md)，先读给人看的 [共建者手册](docs/handbook.md)；编码智能体从 [AGENTS.md](AGENTS.md) 进入。参与开发不需要任何私人笔记。

## Where to start / 从哪里开始

Issues and pull requests in English or Chinese are welcome; do not open a
duplicate thread just to translate. Use [Issues](https://github.com/kunghim/CoBot/issues)
for bugs and feature requests, [Q&A](https://github.com/kunghim/CoBot/discussions/categories/q-a)
for setup and usage questions, and
[Show and tell](https://github.com/kunghim/CoBot/discussions/categories/show-and-tell)
for projects and workflows. Announcements appear in
[Announcements](https://github.com/kunghim/CoBot/discussions/categories/announcements).
Replies are best effort, with no fixed deadline.

For a first contribution, look for `good first issue` or `help wanted`.
Comment `/claim` alone on an open, unassigned issue to take it, and `/unclaim`
(after a short handoff note) to release it; `/help` shows the commands.
Claiming does not approve the proposal or schedule a release. Discuss
substantial features or architecture changes before implementing them.

欢迎中文或英文的 Issue 和 PR，无需为翻译另开重复帖子。Bug 和功能建议进 Issues，安装和使用问题进
Discussions 的 Q&A，成果分享进 Show and tell。维护者尽力回复，不承诺固定时限。
首次贡献可找 `good first issue` 或 `help wanted`。在开放且无人负责的 Issue 下单独评论 `/claim` 认领，
留言交代进度后评论 `/unclaim` 取消，`/help` 查看说明。认领不代表需求获批。较大的功能或架构改动先讨论。
命令细节与分诊标签见 [Issue 分诊](docs/reference/triage.md)。

## Reporting issues / 报告问题

Use one issue per problem or proposal. Include the CoBot version or commit,
OS/architecture, Web or desktop, installation method (MSI, ZIP, source) and
Codex version where relevant; unknown is acceptable. For bugs, give
reproduction steps or a commit with file/line references, and say whether the
evidence is a runtime reproduction, automated test, static analysis only, or
not yet verified. Feature requests need a use case. Select
**AI-discovered / AI 发现** when AI found the problem; it records provenance,
not verification.

每个 Issue 聚焦一个问题或建议。填写已知的版本或提交、系统与架构、Web/桌面、安装方式和 Codex 版本，
不清楚可写未知。Bug 提供复现步骤，或提交号加文件位置，并注明证据是运行复现、自动化测试、仅静态分析还是尚未验证。
功能建议说明使用场景即可。由 AI 发现的问题选择“AI 发现”，它只说明来源，不代表已验证。

Report security vulnerabilities through [SECURITY.md](SECURITY.md), not public issues.
安全漏洞通过 [SECURITY.md](SECURITY.md) 私下报告，不要公开提 Issue。

## Branches and pull requests / 分支与 PR

Work in your fork on a short-lived branch from upstream `dev`, and target
`dev` (dependency updates too). The flow is `topic -> dev -> main -> release`;
only this repository's `dev` promotes to `main`, and CI rejects other routes.
Keep one logical change per PR. Use Draft while unfinished. The PR template asks for:

| Section | What to provide |
| --- | --- |
| Problem and related issue | The problem, trigger, and `Refs #123` if an issue exists |
| Resulting behavior | What changes for users or developers, before and after |
| Validation | Commands actually run, results and limits; separate automated, real Codex and native-platform checks |
| Compatibility and data impact | API/CLI/configuration, database, upgrade and rollback effects; write None if unaffected |

Include sanitized screenshots for visible UI changes and both languages for UI
text. Explain changes to locks, generated contracts or migrations. Label
`breaking-change` only when users or callers must change how they use CoBot
after upgrading, and describe the migration. CI must pass and material review
feedback must be resolved; the maintainer decides whether and when to merge.
Merging into `dev` does not publish a release. Promoting `dev` to `main`
triggers the checked packaging and publication workflow. Commit messages are
in English.

在自己的 fork 中从上游 `dev` 建短期分支，PR 以 `dev` 为目标（依赖更新也是）。流程为
`主题分支 → dev → main → 发布`，只有本仓库的 `dev` 能晋级 `main`，CI 会拒绝其他路线。
每个 PR 聚焦一项改动，未完成用 Draft。按模板写清问题与关联、改动后的行为、实际验证、兼容与数据影响；
界面改动附脱敏截图并提供中英文文案；改锁文件、生成契约或迁移时说明原因。
只有升级后用户或调用方必须调整用法时才标 `breaking-change`，并写明迁移方法。
CI 通过、重要意见解决后由维护者决定是否合并；合入 `dev` 不发布，晋级 `main` 后触发检查、打包与自动发布。提交信息用英文。
维护者流程见[分支、审查与合并](docs/reference/branching.md)和[自动发布](docs/reference/release.md)。

## AI-assisted contributions / AI 辅助贡献

AI-assisted contributions are welcome. You remain responsible for
understanding the change, checking its sources and running the relevant
tests. Agents follow [AGENTS.md](AGENTS.md) and the [docs](docs/README.md).
Keep personal notes and drafts in the ignored `.local/` directory of your main
checkout. Agents create it as its own Git repository and push it to a `notes`
branch of your fork, which shares no history with the code and never enters a
pull request; you can switch it to a private repository at any time. Never
submit private prompts, account material or model transcripts as evidence.

欢迎 AI 辅助贡献，但贡献者仍要理解改动、核对来源并运行相关测试。智能体遵循 [AGENTS.md](AGENTS.md) 和 [docs/](docs/README.md)。
个人笔记和草稿放在主检出目录被忽略的 `.local/`：智能体会自动把它建成独立 Git 仓库，推到你 fork 的 `notes` 分支，永远不进 PR；也可以随时换成自己的私有仓库。规则见[文档流程](docs/workflows/docs.md)；
不要把私人提示词、账号资料或模型对话记录当作证据提交。

## Never commit / 不要提交

Credentials, bootstrap links, runtime databases, `cobot trace` recordings,
engine transcripts, raw logs and build output. Keep raw evidence under the
ignored `.tmp/`. Trace recordings are unredacted and may contain credentials,
prompts and file contents; share only a minimal, manually sanitized
reproduction. `node scripts/check-public-files.mjs` (also run in CI) rejects
private paths and unapproved AI instruction files.

凭据、启动授权链接、运行数据库、`cobot trace` 录制、引擎对话、原始日志和构建产物都不要提交，原始证据放在被忽略的 `.tmp/`。
录制未脱敏，可能包含凭据、提示词和文件内容，只分享最小且人工脱敏的复现。
`node scripts/check-public-files.mjs`（CI 也会运行）会拒绝私人路径和未经批准的 AI 指令文件。

## License / 许可

By intentionally submitting a contribution, you offer it under the project's
Apache-2.0 license. Preserve third-party attribution and only contribute
material you have the right to submit.

有意提交的贡献默认按 Apache-2.0 授权。保留第三方声明，只提交你有权提交的内容。
