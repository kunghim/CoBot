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

Use [Issues](https://github.com/kunghim/CoBot/issues) for reproducible bugs
and feature requests, [Q&A](https://github.com/kunghim/CoBot/discussions/categories/q-a)
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
work. Maintainers use `status: needs-triage` for new reports,
`status: needs-info` when a report needs clarification, and `status: accepted`
for work agreed in scope. These states are updated manually; an accepted
request is not a release-date commitment. Dependency PRs follow their existing
bot labels. Do not label a task newcomer-friendly until it has enough guidance.

Contributors normally work in a fork on a short-lived topic branch based
on the upstream `dev` branch. Target `dev`, including dependency updates.
The flow is `topic branch -> dev -> main -> release`. Maintainers
integrate contributions into `dev`, promote tested changes to `main`, and
publish releases manually. A merged PR is not a promise of an immediate
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
`dev` 合并到 `main`，手动发布。较大的需求先讨论，小修复可直接提 PR。使用 AI 不
免除贡献者对代码、来源和测试的责任。贡献默认按 Apache-2.0 提交。

英文是公开文档、提交信息和公共技术摘要的主要语言，但中文反馈、讨论和 PR 同样欢迎，
无需另发英文副本。影响其他贡献者的重要中文讨论，由维护者尽量补充英文决策摘要。
Bug 与功能建议进入 Issues，安装和使用问题进入 Discussions 的 Q&A，成果进入
Show and tell；无需加入聊天群。维护者跨时区尽力回复，不承诺固定响应时限。
首次贡献可找 `good first issue` 或 `help wanted`，较大任务开始前先留言协调。
状态标签由维护者手动调整：待分诊、待补充信息、已接受；已接受不代表承诺发布日期。

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
