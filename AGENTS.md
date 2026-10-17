# CoBot 编码智能体约定

## 项目是什么

CoBot 是本地运行的多智能体协作壳：用户建一个团队（队长 + 若干角色成员），每个成员是一个独立的 Codex 会话。核心能力在 Codex（通过 `codex app-server` 的 JSON-RPC 接入）；CoBot 只负责团队与成员、消息投递和调度、任务流转、审批卡、用量与界面。不要在 CoBot 里重做 Codex 已有的能力。

直接在本仓库开发，不另建子仓库，不切换到历史实验项目。

## 仓库结构

| 路径 | 内容 |
| --- | --- |
| `cmd/cobot` | 唯一的产品命令：`serve`、`mcp`（成员的 MCP 桥）、`hook`（原生工具守卫）、`trace`（开发录制）、`version` |
| `cmd/cobot-desktop` | Wails 桌面壳（`-tags desktop`），内嵌同一套 Web 界面 |
| `internal/app` | 业务核心：调度（`scheduler.go`）、引擎事件（`engine_events.go`）、线程指令与每轮上下文（`context.go`）、任务流（`task_flow.go`）、审批、子智能体、用量与统计 |
| `internal/engine` | 引擎抽象；`codex/` 是 app-server 适配，`protocol_baseline.json` + `handled.go` 用来对照 Codex 协议变化 |
| `internal/collaboration` | 给模型的协作工具：定义、参数、按角色提供、分发 |
| `internal/store/sqlite` | SQLite 存储与 goose 迁移（`migrations/`，`migrate.go` 的 `MaxSupportedVersion`） |
| `internal/api/http` | HTTP API 与中间件；契约在 `api/openapi.yaml`，生成到 `internal/contract` 与 `frontend/src/api/generated` |
| `internal/hookrun` | Codex 原生 PreToolUse 守卫，只匹配 `Bash\|apply_patch` |
| `internal/wiretrace` | `cobot trace` 的录制、索引与查看页 |
| `internal/roles/defaults` | 内置角色（队长、产品、前端、后端、测试、审查）的指令 |
| `frontend/` | React + Vite + Tailwind 界面，i18n 在 `src/i18n/locales/{zh-CN,en}.json` |
| `tests/integration` | 真 SQLite + 假引擎（`fakeengine`）的集成测试 |
| `tests/e2e` | Playwright 浏览器测试：真实核心 + 假引擎 |
| `experiments/app-server` | 早期协议实验脚本，不是产品 |
| `scripts/` | `dev`、`check`、`generate`、`build`（各有 `.sh` 与 `.ps1`） |

## 当前行为要点

- **协作工具按角色提供**：队长 `send_message`、`ask_user`、`create_task`、`review_task`、`view_tasks`；成员 `send_message`、`ask_user`、`deliver_task`、`view_tasks`。任务状态由 CoBot 维护：派给成员即开始，`deliver_task` 交付进入验收，队长 `review_task` 通过或打回。已删除的旧工具见 `docs/archive/已删除的协作工具.md`。
- **上下文**：线程启动时一次性给出角色指令、团队名单和记忆；之后每轮只带变化的部分。只有 Codex 不再持有线程时才 `thread/resume`，恢复失败就新开线程。
- **审批**：只有三类卡片：原生工具（Codex 的命令和文件审批）、协作链超限的继续确认、中断恢复。Codex 不再等待的请求（`serverRequest/resolved`）会自动关闭对应卡片。
- **子智能体**：成员在 Codex 里派生的子智能体算在该成员的运行里，步骤、审批、守卫和用量都归到成员名下。
- **用量与统计**：每轮用量合计全部模型调用（含缓存写入和思考 token）；输入框下方有会话统计。团队可以设思考强度，默认跟随模型。
- **记忆**：已存记忆可在记忆页查看和删除，并随线程指令带给成员。模型目前没有写入工具，记忆功能后续会重新开发。
- **登录会话**：保留 30 天，服务重启后仍然有效。

## 命令

| 用途 | 命令 |
| --- | --- |
| 本地开发（默认数据根 `~/.cobot-dev`） | `scripts/dev.sh` / `scripts/dev.ps1`，可加 `--data-root <路径>` |
| 完整检查（提交前） | `scripts/check.sh` / `scripts/check.ps1`：gofmt、契约生成无漂移、前端构建、vet、Go 测试、前端类型检查与测试 |
| 改了 `api/openapi.yaml` 之后 | `scripts/generate.sh`，生成结果和规范一起提交 |
| Go 测试 | `go test ./internal/... ./tests/...` |
| 前端测试 | `npm test --prefix frontend`、`npm run typecheck --prefix frontend` |
| 浏览器测试 | `cd tests/e2e && npx playwright test` |
| 开发录制 | `cobot trace on\|off\|status\|view\|protocol --data-root <路径>` |

版本：Go 见 `go.mod`（1.25），Node 24。正式数据根是 `~/.cobot`（production），开发用 `~/.cobot-dev`。测试必须用显式的临时数据根（`.tmp/test-runs` 等），不能碰真实用户库。

## 开发规则

- 真实消息、审批、任务和恢复不能用 mock 冒充成功。需要真实 Codex 的验证单独显式执行，不放进默认检查。
- 桌面壳只做了编译和静态审查，没有在原生平台实际运行验证，不要声称原生已验收。
- 数据库改动只加新的 goose 迁移，并同步调高 `MaxSupportedVersion`；不改已发布的迁移。
- 改 HTTP 接口时同步 `api/openapi.yaml` 并重新生成；`check` 会拒绝生成文件漂移。
- 两个语言包的键必须一致（en 可以多 `_one`/`_other` 复数形式）；不留没人用的键。
- 引擎协议有变化时，先用 `cobot trace protocol` 对照 `protocol_baseline.json`，再改适配代码。
- 不擅改用户本机的 Codex 配置、沙箱、登录或防火墙；CoBot 只改自己私有的 Codex home。
- 缺必需的工具或资源时说明情况并等待处理，不猜路径、不静默安装；项目依赖按锁文件获取。
- 不写云服务、Git 管理器、第二套后端或前端，也不加没有实现的功能按钮。

## 文档

`docs/` 只在维护者本机，整体不进 Git（`.gitignore` 为 `/docs/**`）。新 clone 没有这些文件时向用户索取，不要凭记忆补写。常用入口：

1. `docs/开发指南.md`：命令、测试和文档索引。
2. `docs/首版开发方案.md`、`docs/需求文档.md`：产品范围和已定决策（后来的改动在原处注明了日期）。
3. `docs/界面布局.md`：现行界面规则。
4. `docs/trace分析记录.md`：最近一轮问题分析、修复状态和待办。

`docs/archive/` 是归档，不是必读。最新的用户决定优先于文档；发现实质矛盾再询问，不重复问已经定了的事。

## Git

- 开始前先 `git status --short`；不属于你的改动保持原样。
- 只 `git add <具体路径>`，不用 `add -A`、`add .`、`commit -a`。不擅自 reset、stash、rebase 或强推；push 和发布要用户明确同意。
- 提交信息用英文，标题写清改动后的行为（参考 `git log`）。
- 不提交 token、用户数据、日志、`.tmp/`、`docs/`，也不提交 `cobot trace` 录制（`<数据根>/traces/`）：录制是未脱敏的原始数据，可能含提示词、代码和凭据。

## 沟通

始终用中文回复用户。代码、标识符、提交信息和英文文档用英文。
