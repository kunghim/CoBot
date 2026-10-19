# Contributing to CoBot

CoBot is a local multi-agent application with one Go core shared by Web
and desktop. Keep changes small, observable, and suitable for future use
cases beyond coding. This guide is the public development entry point;
maintainers' local design notes and AI instructions are not prerequisites.

## Issues, pull requests and releases

Issues and pull requests in English or Chinese are welcome. Search existing
issues first. Discuss substantial features or architecture changes before
implementing them; small bug fixes can go straight to a pull request.

English is the primary language for public documentation, commit messages
and shared technical summaries. You may report issues, discuss ideas and
submit pull requests in English or Chinese; do not open duplicate threads
just to provide a translation. Maintainers aim to add an English summary
when a Chinese discussion leads to a decision affecting other contributors.
There is no requirement to join a chat group or use a separate community
account to participate.

Use [Issues](https://github.com/kunghim/CoBot/issues) for bugs with reproduction
or code evidence and feature requests, [Q&A](https://github.com/kunghim/CoBot/discussions/categories/q-a)
for setup and usage questions, and
[Show and tell](https://github.com/kunghim/CoBot/discussions/categories/show-and-tell)
to share projects and workflows. Maintainer announcements appear in
[Announcements](https://github.com/kunghim/CoBot/discussions/categories/announcements).
If you are unsure where a report belongs, use the closest entry; maintainers
can help route it. Replies are best effort across time zones, with no fixed
response deadline.

For a first contribution, look for `good first issue` (a bounded task with
starting points and acceptance criteria) or `help wanted` (work ready for
contributions). Comment before starting a substantial task to avoid duplicate
work using the comment commands below. Maintainers use `status: needs-triage`
for new reports, `status: needs-info` when clarification is needed, and
`status: accepted` for work agreed in scope. A claim sets `status: in-progress`
automatically; acceptance and release dates remain maintainer decisions.
Dependency PRs follow their existing
bot labels. Do not label a task newcomer-friendly until it has enough guidance.

Contributors normally work in a fork on a short-lived topic branch based
on the upstream `dev` branch. Target `dev`, including dependency updates.
The flow is `topic branch -> dev -> main -> release`. Maintainers
integrate contributions into `dev`, promote tested changes to `main`, and
dispatch release drafts from GitHub Actions, then publish them after acceptance.
A merged PR is not a promise of an immediate
release. Repository write access is not required to propose a change.

Keep `main` as the default stable branch. Only this repository's `dev`
branch may open a promotion PR into `main`. Squash topic PRs into `dev`;
use a merge commit for `dev -> main` so repeated promotions retain shared
ancestry. Retain both long-lived branches and delete only finished topic
branches. After promotion, bring `main` back into `dev` before the next
promotion. Do not require a second approving account while there is only
one maintainer; maintainer review and passing CI remain the working policy.
Server-enforced protections depend on repository visibility and GitHub plan.

Keep each PR focused. Explain the problem, resulting behavior, validation
and limitations. Include screenshots for visible UI changes, sanitized logs
only when relevant, and the issue number if one exists. CI and maintainer
review are required before accepting a change. Do not change dependency
locks, generated contracts or migrations without explaining why.

AI-assisted contributions are welcome. You are responsible for understanding
the change, checking its sources and running the relevant tests. Summarize
AI assistance when it helps reviewers assess the work; never upload private
prompts, account material or model transcripts as review evidence.

By intentionally submitting a contribution for inclusion in CoBot, you offer
it under the project's Apache-2.0 license. Preserve third-party attribution
and only contribute material you have the right to submit.

中文说明：欢迎中文和英文 Issue/PR。一般先 fork，从上游 `dev` 建立短期功能分支，
向 `dev` 提交 PR；依赖更新也进入 `dev`。维护者负责审查、集成，再将验证后的
`dev` 合并到 `main`，从 GitHub Actions 生成发布草稿，验收后再公开发布。较大的需求先讨论，小修复可直接提 PR。使用 AI 不
免除贡献者对代码、来源和测试的责任。贡献默认按 Apache-2.0 提交。

英文是公开文档、提交信息和公共技术摘要的主要语言，但中文反馈、讨论和 PR 同样欢迎，
无需另发英文副本。影响其他贡献者的重要中文讨论，由维护者尽量补充英文决策摘要。
Bug 与功能建议进入 Issues，安装和使用问题进入 Discussions 的 Q&A，成果进入
Show and tell；无需加入聊天群。维护者跨时区尽力回复，不承诺固定响应时限。
首次贡献可找 `good first issue` 或 `help wanted`，使用下方评论命令认领，较大改动先讨论。
待分诊、待补充信息、已接受由维护者判断；认领自动标记正在处理，不代表需求获批或承诺发布日期。

## Reporting and claiming issues / 报告与认领 Issue

Use one issue per problem or proposal. Include the CoBot version/commit,
OS/architecture, Web or desktop, installation method (MSI, ZIP, source or
unknown), and Codex version where relevant. Unknown information is acceptable.
For bugs, provide reproduction steps or a concrete commit and file/line
references with the code path explaining the concern. Identify whether your
evidence is a runtime reproduction, automated test, static analysis only, or
not yet verified. Do not describe a suspected failure as observed behavior.
Feature requests need a use case, not a mandatory solution or reproduction.

`ai-discovered` means AI found the problem or need, regardless of who submits
it. AI translation or rewriting alone does not qualify. It describes provenance,
not verification or acceptance. The optional discovery-source field adds this
label automatically when **AI-discovered / AI 发现** is selected. If that
declaration is removed or corrected, the workflow removes its label. Other
cases can be labeled by maintainers; do not guess provenance from writing style.
Do not upload private prompts, credentials or model transcripts.

CLI/API submissions can bypass the form. Include the same evidence and declare
the source explicitly; to use automatic source labeling, add this exact section
to the issue body:

```markdown
### Discovery source / 发现来源

AI-discovered / AI 发现
```

Comment one command by itself on an issue; commands have no arguments:

| Command | Result |
| --- | --- |
| `/claim` | Assign yourself on an open, unassigned issue and set `status: in-progress` |
| `/unclaim` | Release only your own assignment and restore the status before claiming |
| `/help` | Show the command instructions |

No maintainer approval is required to claim an unassigned issue. An existing
assignee is never replaced; maintainers can still adjust assignments in GitHub.
An AI-assisted contributor uses the GitHub account responsible for delivering
the work. `ai-discovered` does not claim an issue or start an AI coding service.
Claiming does not approve the proposal, close the issue, merge a PR or schedule
a release. Describe substantial changes before implementing them.

Before `/unclaim`, leave a short handoff with progress and any branch/PR link.
The issue stays open. Other assignees and maintainer status changes are preserved.
For a manual assignment with no recorded previous status, cancellation returns
an otherwise unowned in-progress issue to `status: needs-triage`. If maintainers
changed the status meanwhile, that decision is retained.

Commands are processed in comment order and the bot replies with the result.
Write a new comment to issue a command; editing an old comment does not execute
it. PR comments are excluded. If a workflow fails after a partial update, a
maintainer can rerun it or dispatch **issue management** with the issue number.
Do not delete the bot's command replies: their metadata stores the previous
status and processed-command cursor for safe retries.

For triage, use one type label (`bug`, `enhancement`, `documentation`, or
`dependencies`), the relevant area labels (`area: installation`, `area: codex`,
`area: collaboration`, `area: ui`), and a single current status. Reserve
`priority: high` for serious impact such as data loss or broadly blocked core
functionality; ordinary reports do not need a priority label. `good first issue`
requires a bounded task with guidance and acceptance criteria. Explain why
duplicates or out-of-scope reports are closed and link the original where relevant.

Topic PRs targeting `dev` should reference the issue (`Refs #123`). A merge into
`dev` is not a released fix; the `dev -> main` promotion can use `Closes #123`
for issues actually resolved on the default branch. On release, add the released
version when useful. Merging an ordinary PR does not automatically publish it.

中文说明：每个 Issue 聚焦一个问题或建议。填写已知的版本、系统与架构、Web/桌面、
MSI/ZIP/源码安装方式及相关 Codex 版本；不清楚可标未知。Bug 可提供运行复现，
也可提供提交、文件位置和代码路径，但要区分运行复现、自动化测试、仅静态分析、尚未验证。
功能建议只需说明使用场景，不强制解决方案或复现。

`ai-discovered` 表示由 AI 发现问题或需求，不取决于提交者是谁；仅翻译、润色不算。
它不代表经过验证或维护者已接受。表单选“AI 发现”会自动加标，修改或移除这项声明
会自动去标；其他情况由维护者核实，不根据文风猜测。CLI/API 报告也应遵守上述结构，
可使用上面的同名来源段落触发标记；不要提交私人提示词、凭据或完整模型对话。

在开放且无人负责的 Issue 单独评论 `/claim` 即可自动认领并标记正在处理，无需等待批准；
已有负责人时不会抢占。取消前留言交代进度和分支/PR，再单独评论 `/unclaim`，只取消
自己的认领并恢复原状态，Issue 保持开放。手工分配而没有历史状态的，取消后若仍是
无人负责的正在处理状态，则回到待分诊；其他负责人和维护者期间修改的状态会保留。
`/help` 查看说明。命令不带参数，需发新评论，编辑旧评论不会执行；PR 评论不执行。
机器人回复保留重跑所需的状态，请勿删除；中途失败由维护者重跑或手动触发同名工作流。

负责人是承担交付责任的 GitHub 账号，来源标签不代表认领，也不会启动 AI 编码服务。
认领不代表需求获批，PR、关闭 Issue 和发版仍由维护者决定。正常问题无需优先级标签，
严重影响才标 `priority: high`。PR 合入 `dev` 先说明进展，晋级 `main` 后再关闭对应问题，
发布时补充修复版本。

## Maintainer release drafts

After promoting tested changes to `main`, open **Actions → release draft →
Run workflow** and select `main`. Supply a product version without the leading
v (for example, 0.1.0-beta.2) and a separate numeric Windows Installer version
(for example, 0.1.2). The MSI version must exceed that of the most recently
published Windows MSI; the workflow downloads that package and reads its
ProductVersion without installing it.

The workflow runs CI again for the selected immutable commit, then builds
Windows x64 on Windows and macOS ARM64 on Apple Silicon. Go 1.25.12, Node
24.18.0, Wails 2.15.0 and WiX/UI extension 6.0.2 are pinned for packaging.
It inspects MSI contents, compares the Windows ZIP with the MSI payload,
mounts and copies the DMG, checks both macOS executables and the copied
ad-hoc signature, and verifies all upload checksums.

The resulting unpublished draft contains exactly three application downloads:
Windows x64 MSI and ZIP, and an Apple Silicon DMG, each with a SHA-256 file.
An additional release-manifest.json records the source commit, build run,
product/MSI versions, file sizes and hashes. Intel, server binaries, raw build
logs and runtime profiles are not release assets. The local macOS build may
also produce a ZIP; the release workflow uploads only the DMG.

Only the final draft job has repository contents write permission. Branch
pushes, tag pushes and PR merges do not publish a release. Existing release
versions and tags are rejected rather than replaced. If an upload fails after
creating a partial draft, inspect it before retrying; the workflow will not
overwrite it. Do not publish a draft with incomplete assets.

Download the exact draft assets and record native installation, first launch
and the real Codex checks appropriate to the change before publishing.
Windows packages are unsigned; macOS has only an ad-hoc bundle signature,
without Developer ID signing or notarization. Automated packaging does not
establish real-account or interactive installation acceptance. Preserve those
limits in the release notes and add actual acceptance evidence before the
maintainer publishes the draft. No signing credentials or Codex login belong
in this workflow.

中文说明：维护者将已测试代码晋级到 main 后，在 Actions 的 release draft 中选择
main，输入产品版本（不带 v）和递增的三段 MSI 版本。流水线完成 CI、Windows x64
MSI/ZIP 与 Apple Silicon DMG 构建和结构校验，生成含校验文件、源码与构建记录的草稿；
暂不上传 Intel 包。工作流不会自动公开发布，也不覆盖已有版本。首次使用仍需实际跑通
GitHub 构建；随后下载草稿中的同一批产物，补充安装、首次打开和必要的真实 Codex
验收记录，再由维护者发布。账号登录和签名凭据不进入流水线。

## Working locally

Use Go 1.25.12+ (the `go.mod` minimum), Node.js 24+, and npm 11+. Install frontend dependencies with
`npm ci --prefix frontend`. Run `scripts/dev.ps1` on Windows or
`scripts/dev.sh` on macOS/Linux. Use the development profile or an explicit
temporary data root; never test with a production database or everyday
Codex credentials. Do not disable native sandboxing, approvals, or hook
trust to make a test pass.

API contract changes originate in `api/openapi.yaml`. Regenerate with
`scripts/generate.ps1` or `scripts/generate.sh`, and include the generated
Go/TypeScript changes in the same commit. Keep application behavior in
`internal/app`; HTTP handlers, native shells, and MCP bridges should remain
adapters. Persist business facts and their replay records atomically. Keep
model operations outside SQL transactions.

Provide both Chinese and English UI text in `frontend/src/i18n/locales`.
Use message keys for errors and accessible names for icon buttons. Do not
embed technical diagnostics in normal product flows unless users need
them to make a decision.

## Runtime, upgrades and the native hook

The service binds to loopback. Development defaults to `~/.cobot-dev` and
production to `~/.cobot`. Tests must use an explicit temporary `--data-root`.
Each team selects a working directory, visible to every member; its `.cobot`
directory holds local interaction resources. CoBot does not automatically
commit project files or implement a Git execution engine.

Before upgrading, stop the service normally and preserve its entire data
root. Startup runs embedded forward SQL migrations and backs up an existing
database before migrating. Restore a stopped, matching database/profile
backup to roll back; do not run an old binary on a newer schema. Do not copy
only the main SQLite file while WAL writers are active.

At startup CoBot locates Codex on `PATH`, requires at least the verified
version (`MinSupportedVersion`), and reports the result in Settings, which
only displays it. The settings API still accepts `enginePath`; tests use it
to point at a fake engine, and the UI does not expose it.

The minimum Codex version is platform-specific: macOS uses 0.146.0 after an
ARM64 native smoke test covering sign-in, model turns, task delivery, file tools,
command approval/rejection and restart; Windows and Linux retain the 0.161.0
baseline. This is a compatibility floor, not full acceptance of every platform.

Codex skips hooks that are untrusted or have changed. CoBot registers its
native lease hook only in its private Codex environment and then records
trust itself through the app-server (`hooks/list`, then `config/batchWrite`
of `hooks.state.<key>.trusted_hash`), the same record `/hooks` writes. It
does so only when the private `hooks.json` is byte-for-byte the guard CoBot
generated and matches its ownership record, and only for that one entry:
project hooks in team directories, managed hooks and edited files are never
trusted automatically. Upgrades re-trust the new definition the same way.
Settings shows whether the hook is authorized and offers a retry. The hook
matches only shell commands and file patches (`Bash|apply_patch`), and
coverage is limited to native paths that actually invoke it; hooks do not revoke
commands already running or provide complete isolation.

If automatic trust is refused (for example because the private hooks file
was edited), you can still review it manually: finish any running work,
start Codex against CoBot's private environment and type `/hooks`. Check
that the command points to your CoBot helper and current data root before
trusting it. Never use flags that bypass trust. The example sets the
variables for this shell only and restores them afterwards; replace both
paths with your own absolute paths.

```powershell
$cobotRoot = 'C:\your\cobot-data-root'
$teamDirectory = 'C:\your\team-directory'
$previousCodexHome = $env:CODEX_HOME
$previousCodexSQLiteHome = $env:CODEX_SQLITE_HOME
try {
    $env:CODEX_HOME = Join-Path $cobotRoot 'engines/codex/home'
    $env:CODEX_SQLITE_HOME = Join-Path $cobotRoot 'engines/codex/sqlite'
    # On Windows the hook command calls cobot directly when the paths need no quoting,
    # otherwise it is a base64 -EncodedCommand; decode it to inspect, do not run it.
    $hooks = Get-Content (Join-Path $env:CODEX_HOME 'hooks.json') -Raw | ConvertFrom-Json
    $command = $hooks.hooks.PreToolUse[0].hooks[0].command
    if ($command -match '-EncodedCommand (\S+)$') {
        [Text.Encoding]::Unicode.GetString([Convert]::FromBase64String($Matches[1]))
    } else { $command }
    codex --no-daemon --no-alt-screen --cd $teamDirectory
    # Inside Codex, type /hooks, review, then exit.
} finally {
    $env:CODEX_HOME = $previousCodexHome
    $env:CODEX_SQLITE_HOME = $previousCodexSQLiteHome
}
```

On macOS, set the same two variables in a subshell and run
`codex --no-daemon --cd "$teamDirectory"`. `--no-daemon` avoids a socket path
length limit with long private paths; it does not change sandboxing or hook
trust. This CLI session is only for reviewing configuration: tool turns not
dispatched by CoBot have no host binding and are refused by the guard.
Re-review after an upgrade changes the helper path.

## Validation

Run `scripts/check.ps1` or `scripts/check.sh`: gofmt, contract drift,
frontend build/types/tests, Go vet/build, and unit/integration tests. On Windows,
`scripts/check.ps1 -Desktop` also compiles the native shell. These checks
do not prove macOS installation or native runtime behavior.

Browser regression tests (10 Playwright spec files under `tests/e2e/specs`)
use the real core with a fake engine. Global setup builds the frontend,
`cobot` and the fake Codex host into `.tmp/bin` and starts a core on a
throwaway data root under `.tmp/test-runs`, so frontend dependencies must
already be installed:

```sh
cd tests/e2e
npm ci
npx playwright install chromium
npx playwright test
```

If Chrome is already installed, `COBOT_E2E_CHANNEL=chrome npx playwright test`
uses that browser without downloading another one. Record the channel and version
with results; CI defaults to Playwright's pinned Chromium.

Install browser system dependencies only where appropriate for your
machine or CI environment. Real Codex acceptance is an explicit separate
test: it requires an isolated profile, official login, and an available
account quota. Record versions, exact scope, observed facts, failures, and
untested portions. Do not describe a mock, standalone adapter invocation,
or model's unverified claim as real engine acceptance.

## Changes and reports

Use staged commits with descriptive messages. Stage only named paths
belonging to the change, and preserve unrelated local work. Pull requests
should explain the concrete behavior before and after, validation, and
remaining limitations. Significant state changes need focused behavioral
regressions; avoid tests that merely restate implementation details.

Never commit credentials, bootstrap links, runtime databases, engine
transcripts, raw acceptance logs, or build output. Keep raw evidence under
the ignored `.tmp/` area. Documents and AI instructions are private by default.
Selected public docs and official `AGENTS.md` / `CLAUDE.md` can be published
after adding their exact paths to `publicFiles` in
`scripts/public-files-policy.json`. Root hidden entries are governed by
`rootDotEntries` in that same policy. Run
`node scripts/check-public-files.mjs --write-ignore` to update the final
managed `.gitignore` block, then explicitly stage the intended public files.
Personal instructions and assistant settings stay local. Keep personal
additions separate from any official instruction file at the same path.
Ignore rules do not remove already tracked files or historical versions;
maintainers handle the existing historical material separately.

中文说明：`docs/` 与 AI 指令默认仅本地保存。整理好的公开文档和官方指令，先将具体
文件路径加入 `scripts/public-files-policy.json` 的 `publicFiles`，再运行上述命令同步
忽略例外。名单只放行指定文件，不放行相邻私人资料。提交前运行
`node scripts/check-public-files.mjs`；CI 会再次检查。检查不会清除旧历史，也不能撤回
已经推送到公开 PR 的内容。

Raw `cobot trace` recordings are deliberately unredacted developer data and
can include HTTP credentials, prompts, file contents and tool results. Do
not attach them to issues or pull requests. Share only a minimal, manually
sanitized reproduction. Do not enable recording just to report a normal bug.

Report bugs with reproduction steps, platform/tool versions, expected and
actual behavior, and sanitized logs. Remove private paths/content when
unnecessary, and always remove tokens. Public releases, signing, and the
release timing require a separate maintainer decision. Report security
vulnerabilities through [SECURITY.md](SECURITY.md), rather than public issues.

## Pull request review / PR 审查规范

Work on a short-lived branch based on `dev`, normally in your own fork, and
target `dev`. Only this repository's `dev` branch promotes changes to `main`.
Keep one logical change per PR without an arbitrary line-count limit. Discuss
substantial features or architecture changes first; a small fix may go straight
to a PR without creating an issue. Titles should describe the resulting change;
English is recommended, and descriptions may be English or Chinese. Maintainers
aim to add an English summary of substantial Chinese discussions.

The PR template asks for four things:

| Section | What to provide |
| --- | --- |
| Problem and related issue | The problem, trigger, and `Refs #123` if an issue exists |
| Resulting behavior | What changes for users or developers, before and after |
| Validation | Commands actually run, results, and limitations; distinguish automated, real Codex and native-platform checks |
| Compatibility and data impact | API/CLI/configuration, database, upgrade and rollback effects; write None if unaffected |

Include sanitized screenshots for visible UI changes. Small documentation fixes
can use concise answers. Explain changes to locks, generated contracts or
migrations when relevant; preserve third-party attribution and provide both UI
languages for UI text changes. AI assistance does not change the contributor's
responsibility for the code, sources and validation. Mention it when useful for
review; do not submit private prompts or transcripts as evidence.

Use a type label (`bug`, `enhancement`, `documentation`, `dependencies`) and
relevant area labels (`area: installation`, `area: codex`, `area: collaboration`,
`area: ui`); not every PR needs an area. Dependency bots retain their existing
ecosystem labels. Track PR progress with GitHub's Draft, review and merged/closed
states rather than issue-status labels. `ai-discovered` describes the linked
issue's discovery source, not PR quality or whether AI wrote the code.

Add `breaking-change`, or ask a maintainer to add it, when existing users or
callers must change how they use CoBot after upgrading. Examples include removed
configuration, incompatible API/CLI changes, or data changes that invalidate
existing use or rollback. Explain required user actions and migration/rollback
limits in the PR so they can be included in release notes. An additive optional
feature, internal refactor or ordinary database migration is not automatically
a breaking change. This label does not itself choose the next version number.

Use Draft for unfinished work; mark Ready for review when the change and its
evidence are ready. Before merging, CI must pass and material review feedback
must be addressed or explicitly resolved by a maintainer. New commits require
checking the resulting diff and current CI again. The maintainer makes the
final merge decision; passing CI does not automatically approve or merge a PR.
There is no additional mandatory approval-count gate while one maintainer runs
the project. Issue claim commands do not operate on PRs.

| Merge route | Method |
| --- | --- |
| Topic branch into `dev` | Squash and merge: one PR becomes one commit |
| `dev` into `main` | Merge commit: retain shared history between long-lived branches |

Squashing a topic PR combines its individual development commits; they do not
all appear separately on the mainline. This applies to new PRs and does not
rewrite existing published history. Start the next task on a new topic branch.
After promotion, synchronize `main` back into `dev` without rewriting history,
using a fast-forward when possible. Keep both long-lived branches; only remove
a finished topic branch after confirming its work is integrated and it is no
longer used by a worktree or another task.

Ordinary PRs use `Refs #123`; a merge into `dev` is not a released fix. The
`dev -> main` promotion may use `Closes #123` for issues actually resolved on
the default branch. Release timing and assets require a separate maintainer
decision; merging a PR does not publish a release. See GitHub's documentation
on [issue closing keywords](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue)
and [merge methods](https://docs.github.com/en/pull-requests/reference/pull-request-merges).

中文说明：从 `dev` 建短期功能分支，普通 PR 向 `dev` 提交；本仓库 `dev` 才能晋级
`main`。每个 PR 聚焦一项改动，不设机械行数上限；大改动先讨论，小修复无需先建
Issue。标题建议英文，描述中英文均可。模板说明问题与关联、改动后的行为、实际验证、
兼容与数据影响；界面变化附脱敏截图。AI 辅助贡献接受同样的代码、来源与验证审查。

PR 复用类型和模块标签，进度用 Draft、审查、合并或关闭状态；`ai-discovered` 保留为
Issue 来源标识。只有升级后用户或调用方需要调整用法的实际不兼容变更才标
`breaking-change`，并提供迁移措施和回滚限制。普通迁移、内部重构、增加可选能力
不自动算破坏性变更，标签本身也不决定下次版本号。

未完成用 Draft，完成后请求审查。CI 通过、重要审查意见解决后，由维护者决定合并；
追加提交后重新核对差异与当前检查。暂不增加强制审批人数，也不自动批准或合并。
普通 PR 默认 Squash 成一条提交，开发中的各条 commit 不逐条进入主线；`dev` 晋级
`main` 使用 Merge commit。这不改写既有历史。晋级后将 `main` 同步回 `dev`，有条件
时快进；确认无 worktree 或其他任务继续使用后再清理已完成的功能分支。

普通 PR 用 `Refs #编号` 关联，合入 `dev` 先说明进展，真正解决的问题在晋级 `main`
时再关闭。合并不等于发布，新版本和发布时间仍另行决定。
