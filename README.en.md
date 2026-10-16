# CoBot

[简体中文](README.md) · English

CoBot is a local application for teams of AI agents. It organizes roles,
tasks, conversations, approvals, and shared context while external agents
perform the work. Coding is the first use case; the product is intended to
support office work, learning, and creative work as it grows.

The first version is under repair and acceptance testing. Windows Web is
the current acceptance target. Windows desktop runtime/installation and
macOS runtime/packaging remain unverified. See the tracked acceptance
documents under `docs/` for evidence and limitations.

## Develop and check

Requirements: Go 1.25+, Node.js 24+, npm 11+, and an installed Codex CLI.
The currently verified Codex protocol baseline is 0.161.0. CoBot uses a
private Codex home and SQLite directory for each environment. Its official
account login is separate from your everyday Codex desktop login.

```sh
npm ci --prefix frontend
./scripts/generate.sh
./scripts/dev.sh
./scripts/check.sh
```

Windows PowerShell equivalents:

```powershell
npm ci --prefix frontend
./scripts/generate.ps1
./scripts/dev.ps1
./scripts/check.ps1 -Desktop
```

Open the one-time authorization link printed by the service. It expires
after 60 seconds and can be used once. Run `cobot serve` again with the same
profile/root to obtain a new link to the running service. Treat links and
runtime credentials as secrets; do not put them in issues or screenshots.

Build a server executable with embedded UI, roles, and migrations:

```sh
./scripts/build.sh --target server
```

On Windows, use `./scripts/build.ps1 -Target server`. Artifacts and SHA-256
checksums go to `.tmp/dist`. Users of a built artifact do not need Node.js,
Go, or a database server. Codex remains a separately installed dependency.
Desktop packaging additionally requires Wails v2.15.0 and platform tools;
compiling the shell alone does not validate an installed native app.
On macOS, `./scripts/build.sh --target desktop` embeds the matching helper
inside `CoBot.app/Contents/MacOS`, reapplies an ad-hoc trial signature, and
creates a ZIP/checksum. Developer ID signing, notarization, and native
runtime acceptance remain pending.

## Runtime and upgrades

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
flow in CoBot's private environment. Registration does not grant trust.
Settings reports whether this hook is trusted or unavailable. Coverage is
limited to native paths that actually invoke it; hooks do not revoke
commands already running or provide complete isolation.

## Architecture and contributions

One Go module and shared core serve both a React/TypeScript Web UI and a
thin Wails desktop shell. HTTP JSON and fetch-based SSE provide the shared
transport. SQLite uses modernc and embedded goose migrations. UI strings
are maintained in both `zh-CN` and `en`.

See [contribution guidance](CONTRIBUTING.md), [agent instructions](AGENTS.md),
and the current development, directory, and acceptance documents in `docs/`.
The tracked root documents are sufficient for a fresh checkout; local
archive/evidence directories are not prerequisites. A project license has
not yet been selected; this repository does not claim a license grant.
