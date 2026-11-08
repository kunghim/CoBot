# CoBot

**An AI team workspace for turning your goals into practical results.**

English · [简体中文](README.zh-CN.md)

CoBot is designed for people who have something they want to make, even if they do not write code. Start a conversation to explore an idea, or give an AI team a goal and work through a plan, implementation, checks and final acceptance. Websites and small software tools are the current team scenario; the longer-term direction includes other kinds of work.

[Download](#quick-start) · [How it works](#how-it-works) · [Contribute](#development)

> **Early-stage software.** The current published release is [v0.0.1](https://github.com/kunghim/CoBot/releases/tag/v0.0.1). This README describes the `dev` source; use the release notes for the exact package requirements and changes. A published version number does not establish that every platform, model or task has been tested.

## How it works

**Start with a chat.** Ask a question or work through an idea without creating a team or selecting a project folder. Conversations have their own history, model, reasoning and permission controls.

**Use a team for a project.** Tell the project manager what you want to achieve and who will use it. The team proposes a requirement and a sequence of work for you to confirm. Members carry out implementation, testing and review; checks can send work back for revision. You handle necessary choices and accept the result.

For example:

> I want a small tool for entering and finding customer information. First help me clarify what it should do and how I can check the result.

This is a starting request, not a verified customer case. Agree on a small scope and try it with fictional data. Acceptance should include actual operations and expected results, not just a statement that the work is finished.

## What you can do

- **Chat and return to your work.** Stream replies, stop a turn, rename conversations and adjust model, reasoning and permissions for subsequent messages. Search titles and message content across chats and team-member conversations. Pin chats, organize them into sections, and archive or restore chats and teams.
- **Follow a team's progress.** Confirm requirements, follow recorded handoffs and ask for changes. Questions, approvals and acceptance cards appear above the conversation input. A team can request an additional role member, subject to your confirmation. Stages within a requirement currently run in sequence.
- **Inspect the results beside the conversation.** The workspace includes a browser, a read-only file tree, file previews and recorded changes. View common documents, spreadsheets, presentations, PDFs, images and text; rendering depends on the format. Historical changes are separate from the current file on disk. Files and change summaries can also appear below a completed reply.
- **Try a runnable delivery.** When the team's acceptance report includes a preview command and address, the acceptance card shows the command and offers **Start and open**. You choose whether to run it in the team folder, open its page in the built-in browser, and stop the preview. This is a local preview, not automatic hosting or deployment.
- **Resume an existing folder.** **Open team** finds its active team, offers to restore an archived one, or prepares a new team. A folder marked by another environment can be adopted after explicit confirmation; adoption does not import its old conversations.
- **Choose model services.** Use a ChatGPT account or configure OpenAI Responses-compatible providers. Members can use different models or follow the default. The bundled, updatable model catalog supplies model limits and reasoning controls; advanced settings allow supported adjustments. A connectivity check alone does not establish that a model can complete a team workflow.
- **Keep decisions and usage visible.** Review command/file approvals, see model usage, and set collaboration limits and daily token budgets. Execution records help inspect what happened; testing and AI review do not guarantee correctness.

CoBot currently uses the **Codex engine bundled with the ChatGPT desktop app**, in a separate environment with its own sign-in. Standalone Codex CLI installations are not used. CoBot manages conversations and collaboration; the engine performs model and tool execution.

## Quick start

### Desktop downloads

[**v0.0.1 — release notes, downloads and checksums**](https://github.com/kunghim/CoBot/releases/tag/v0.0.1)

| Package | Start |
| --- | --- |
| [Windows x64 MSI](https://github.com/kunghim/CoBot/releases/download/v0.0.1/CoBot-0.0.1-windows-x64.msi) | Install for the current user, then open CoBot from the Start menu |
| [Windows x64 ZIP](https://github.com/kunghim/CoBot/releases/download/v0.0.1/CoBot-0.0.1-windows-x64.zip) | Extract all files and open `cobot-desktop.exe`; keep `cobot-helper.exe` and `cobot.exe` beside it |
| [macOS Apple Silicon DMG](https://github.com/kunghim/CoBot/releases/download/v0.0.1/CoBot-0.0.1-macos-arm64.dmg) | Copy `CoBot.app` to Applications, eject the image, then open the installed app |

**Before starting:** install the [ChatGPT desktop app](https://learn.chatgpt.com/docs/app) separately. CoBot discovers its bundled Codex engine on supported macOS app and Windows Store layouts. It keeps its own login and configuration. Windows requires Windows 10/11 x64 and an existing WebView2 Runtime. Go, Node.js and a standalone Codex CLI are not needed to use these downloads.

The packages have no release signing or notarization; macOS uses an ad-hoc signature. See the release notes for first-launch instructions and the adjacent `.sha256` files. GitHub's **Source code** archives are for building, not ready-to-run applications.

1. Open CoBot. In **Settings → Model providers**, sign in for CoBot or configure a compatible service, then choose a default model. The model service may require a subscription or API payment.
2. Start with **Chat**, or switch to **Team**, create a team in a folder and describe your goal to the project manager.
3. For a team task, confirm the proposed requirement, inspect the result and request changes as needed. If a preview is offered, review its command before starting it.

The interface initially opens in Chinese. For English, select **设置 → 通用 → 语言 → English**. If the engine is not found, check the desktop app installation and the status shown in Settings; installing a standalone CLI is not a substitute. Ask in [Q&A](https://github.com/kunghim/CoBot/discussions/categories/q-a) with the CoBot version, OS and a sanitized error message.

### Data and updates

CoBot runs locally and listens only on loopback. Release packages normally store settings, sign-in and work history under `~/.cobot`, separately from application files. Model requests send relevant content to the selected service; local operation does not mean offline AI or that all data stays on the computer.

Updates are manual: close CoBot before installing a new MSI or replacing the complete ZIP/app bundle. Preserve the data directory; uninstalling or removing application files retains it. Read [upgrade and rollback notes](docs/reference/upgrading.md) before changing versions. Deleting a chat keeps its generated files.

## Status

| Platform | Current scope |
| --- | --- |
| Windows x64 | MSI and ZIP published; requires WebView2 and supported desktop-engine installation |
| macOS Apple Silicon | ARM64 DMG published; not Developer ID signed or notarized |
| macOS Intel | No published package; experimental source builds do not establish native support |
| Linux | Shared-core CI coverage; desktop-engine discovery is not supported, so CI is not evidence of end-user readiness |

Release automation checks builds and package integrity. Interactive installation, real model behavior and platform-specific use require separate validation; see the [release notes](https://github.com/kunghim/CoBot/releases/tag/v0.0.1). Custom roles and writable memory are not yet available. Detailed behavior and current boundaries are in the [product overview](docs/product/overview.md) and [decisions](docs/product/decisions.md), currently in Chinese.

## Development

Help make AI work accessible to people who do not program. Real task examples, usability feedback, domain knowledge, design, documentation, translations and engineering fixes all contribute. Start with [CONTRIBUTING.md](CONTRIBUTING.md); substantial features should be discussed before implementation. English and Chinese issues and PRs are welcome. Core engineering documents are currently in Chinese.

For repository changes, branch from `dev`, keep the change focused and target `dev` in your PR. Include checks actually run and their limits. Private notes and AI coding tools are not prerequisites.

<details>
<summary>Build current source and run checks</summary>

### Build from source

Build tools: Git, Go compatible with [go.mod](go.mod) (**1.26.0+**), Node.js 24 and npm 11+. Agent execution also requires the supported ChatGPT desktop engine and model-service access described above.

**Windows — PowerShell**

```powershell
git clone --branch dev https://github.com/kunghim/CoBot.git
cd CoBot
npm ci --prefix frontend
powershell -NoProfile -File scripts/build.ps1 -Target server
$trialRoot = Join-Path (Get-Location).Path (".tmp\test-runs\readme-" + [guid]::NewGuid().ToString("N"))
.\.tmp\dist\cobot.exe serve --data-root $trialRoot
```

**macOS — Bash**

```bash
git clone --branch dev https://github.com/kunghim/CoBot.git
cd CoBot
npm ci --prefix frontend
./scripts/build.sh --target server
mkdir -p .tmp/test-runs
trial_root="$(mktemp -d "$(pwd)/.tmp/test-runs/readme.XXXXXX")"
./.tmp/dist/cobot serve --data-root "$trial_root"
```

These commands build the local Web app. Keep its terminal open and press **Ctrl+C** there to stop it; closing the browser does not stop the service. If the browser does not open, use the startup link printed in the terminal and keep it private. Reuse the same `--data-root` to return to this trial. Without an explicit root, development uses `~/.cobot-dev` and production uses `~/.cobot`.

For desktop builds, use the `desktop` target in [build.ps1](scripts/build.ps1) or [build.sh](scripts/build.sh), with the platform's Wails/native build tools. Older packaging and native-test details remain available as a [versioned reference](https://github.com/kunghim/CoBot/blob/727ea51/README.md#build-from-source); its historical requirements do not override the current build scripts or release notes.

For documentation changes:

```bash
node scripts/check-docs.mjs
node scripts/check-public-files.mjs
```

For code changes, run `./scripts/check.sh` or `./scripts/check.ps1`; see the [verification workflow](docs/workflows/verify.md) for checks appropriate to the change. Automated tests, real-model checks and native-platform validation should be reported separately.

</details>

## Community

- [Report a bug or propose a change](https://github.com/kunghim/CoBot/issues/new/choose).
- [Ask about setup or usage](https://github.com/kunghim/CoBot/discussions/categories/q-a).
- [Share a workflow or result](https://github.com/kunghim/CoBot/discussions/categories/show-and-tell).
- [Read announcements](https://github.com/kunghim/CoBot/discussions/categories/announcements).

For vulnerabilities, follow [SECURITY.md](SECURITY.md). Follow the [Code of conduct](CODE_OF_CONDUCT.md). Share only minimal, sanitized evidence; do not publish credentials, startup links or raw traces. Maintainer replies are best effort, with no fixed deadline.

## Direction

CoBot starts with software needs for people who do not code. Future work can extend to other scenarios and reusable working methods, with tools, deliverables and checks designed for each. These are development directions, not a delivery schedule. The engine integration remains extensible; there is currently no plan to add Claude Code.

## License

Licensed under the [Apache License 2.0](LICENSE). Third-party dependencies retain their licenses; see [dependencies and notices](docs/reference/dependencies.md).
