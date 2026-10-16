# Contributing to CoBot

CoBot is a local multi-agent application with one Go core shared by Web
and desktop. Keep changes small, observable, and suitable for future use
cases beyond coding. Please read `AGENTS.md` and the current development,
directory, and acceptance documents under `docs/` before implementation.

## Working locally

Use Go 1.25+, Node.js 24+, and npm 11+. Install frontend dependencies with
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
only the main SQLite file while WAL writers are active. See
[upgrade notes](docs/升级说明.md) for details.

The native lease hook must be reviewed through Codex's normal `/hooks`
flow in CoBot's private environment
(step-by-step commands: [开发指南](docs/开发指南.md#codex-原生租约-hook), in Chinese). Registration does not grant trust.
Settings reports whether this hook is trusted or unavailable. Coverage is
limited to native paths that actually invoke it; hooks do not revoke
commands already running or provide complete isolation.

## Validation

Run `scripts/check.ps1` or `scripts/check.sh`: contract drift, frontend
build/types/tests, Go vet/build, and unit/integration tests. On Windows,
`scripts/check.ps1 -Desktop` also compiles the native shell. These checks
do not prove macOS installation or native runtime behavior.

Browser regression tests use the real core with a fake engine:

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
the ignored `.tmp/` area and commit a sanitized summary in a tracked root
document. Subdirectories of `docs/` contain local material and are not
part of the required source distribution.

Report bugs with reproduction steps, platform/tool versions, expected and
actual behavior, and sanitized logs. Remove private paths/content when
unnecessary, and always remove tokens. Public releases, signing, and the
project license require a separate maintainer decision.
