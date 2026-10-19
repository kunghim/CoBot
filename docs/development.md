# 开发指南

本文是人工与 AI 共建者共享的工程规范。
贡献与 PR 流程见[贡献指南](../CONTRIBUTING.md)，智能体工作流程见 [AI coding 规范](ai-coding.md)，
产品现状与平台支持范围见 [README](../README.md)。参与公共代码开发不需要私人笔记。
第三方来源与声明见[依赖说明](dependencies.md)，安装形态、迁移影响与回滚见[升级说明](upgrading.md)。

## 架构与职责边界

CoBot 本地运行，负责团队、角色、任务、消息、审批、用量和恢复。
当前通过 `codex app-server` 的 JSON-RPC 接入 Codex，由它执行模型与工具循环。
不要另建智能体循环、shell 执行器或 Git 管理器。
Web 与桌面共用一套 Go 核心和前端，不增加第二套后端或前端、云端业务服务，也不为尚未实现的功能摆放占位控件。

| 路径 | 职责 |
| --- | --- |
| `cmd/cobot` | 产品 CLI：`serve`、`mcp`、`hook`、`trace`、`version` |
| `cmd/cobot-desktop` | Wails 桌面壳，使用 `desktop` 构建标签 |
| `internal/app` | 业务行为、调度、任务流、上下文与恢复 |
| `internal/engine` | 引擎抽象与 Codex app-server 适配 |
| `internal/collaboration` | 协作工具定义、按角色提供与分发 |
| `internal/store/sqlite` | 持久化与内嵌 goose 迁移 |
| `internal/api/http` | HTTP 处理器与中间件 |
| `api/openapi.yaml` | 公共 HTTP 契约 |
| `internal/contract`、`frontend/src/api/generated` | 生成的契约类型 |
| `internal/hookrun` | 原生工具租约守卫 |
| `internal/wiretrace` | 开发录制与协议对照 |
| `internal/avatars`、`assets/avatars` | 头像库同步、资源源文件与生成产物 |
| `assets/branding` | 应用图标、界面标识与 favicon 的来源和导出 |
| `frontend` | 共享的 React/TypeScript 界面与翻译 |
| `tests/integration` | 真 SQLite 与假引擎的集成测试 |
| `tests/e2e` | 真实核心与假引擎的浏览器测试 |
| `scripts` | 开发、生成、验证与打包入口 |

业务行为放在 `internal/app`，HTTP 处理器、原生壳和 MCP 桥只做适配。
业务事实与对应的回放记录应原子落盘，不在 SQL 事务中执行模型操作。

## 环境与依赖

Go 最低版本以 `go.mod` 为准，当前为 1.25.12；另需 Node.js 24+ 和 npm 11+。
桌面打包还需要 Wails v2.15.0 及目标平台的原生依赖。最终用户运行独立服务程序不需要 Go 或 Node。

按锁文件安装项目依赖：

```sh
npm ci --prefix frontend
```

使用共建者已配置的工具链。本机解释器路径和环境处理方法写入 `.local/`，不写入公共规范。
缺少必需工具时应说明情况，不静默安装系统软件或修改全局配置。

## 开发命令

除非另有说明，以下命令均在仓库根目录运行。

| 用途 | PowerShell | POSIX shell |
| --- | --- | --- |
| 启动核心与 Vite | `./scripts/dev.ps1` | `./scripts/dev.sh` |
| 指定独立的开发数据根 | `./scripts/dev.ps1 -DataRoot .tmp/dev-data` | `./scripts/dev.sh --data-root .tmp/dev-data` |
| 生成 HTTP 类型 | `./scripts/generate.ps1` | `./scripts/generate.sh` |
| 完整检查共享核心 | `./scripts/check.ps1` | `./scripts/check.sh` |
| 构建独立服务程序 | `./scripts/build.ps1 -Target server` | `./scripts/build.sh --target server` |
| 构建桌面包 | `./scripts/build.ps1 -Target desktop` | `./scripts/build.sh --target desktop` |
| 生成当前组件画廊 | `npm run gallery --prefix frontend -- --out ../.tmp/gallery/index.html` | `npm run gallery --prefix frontend -- --out ../.tmp/gallery/index.html` |
| 校验头像资源生成结果 | `node assets/avatars/build.mjs --check` | `node assets/avatars/build.mjs --check` |
| 准备并校验品牌图标 | `node scripts/prepare-branding.mjs` | `node scripts/prepare-branding.mjs` |

生成工具版本已锁定：oapi-codegen 见 `scripts/generate.*`，openapi-typescript 见前端锁文件，不替换为浮动的 `latest`。
构建产物与校验和默认输出到 `.tmp/dist`。Windows 安装包另有 `scripts/` 下的打包脚本。

独立执行需要内嵌 `internal/webui/dist` 的 Go 构建与测试前，应先构建前端。新检出的工作目录先运行：

```sh
npm run build --prefix frontend
```

开发脚本只管理自己启动的资源。通过核心输出的授权链接打开界面，不公开该链接，也不将其写入示例。

### 界面与资源开发

修改界面前阅读 `frontend/src/gallery` 中的规则和真实组件示例，复用 `frontend/src/components/ui`。
可使用上表命令生成本机画廊；`--out` 相对于 npm 脚本的 `frontend` 工作目录，因此示例使用 `../.tmp/`。
画廊源码随代码维护，生成的 HTML、截图和旧静态稿不能代替当前源码。

头像系列在 `assets/avatars` 中维护源文件和生成结果，修改后运行 `node assets/avatars/build.mjs`，
再运行 `--check` 校验。系列文件包含版本、大小和校验值，不手工修改生成文件来绕过源文件更新。
素材来源与许可证和代码依赖分开维护，见依赖说明。

品牌图标遵循 [assets/branding/README.md](../assets/branding/README.md)，复用已提交的金属鸟标识，不自行换形或重着色。
准备脚本校验并将图标复制到 `.tmp/wails`，桌面构建入口会自动调用；日常构建不需要 Python 或图像导出工具。

## 运行与数据边界

服务只绑定本机回环地址。开发数据根默认为 `~/.cobot-dev`，生产数据根为 `~/.cobot`。
测试必须使用明确的临时数据根，例如 `.tmp/test-runs/<run>`，或测试框架提供的临时目录。
独立运行的 worktree 使用不同的数据根；worktree 本身不隔离运行数据。

每个团队选择一个成员共享的工作目录，其中的 `.cobot` 保存本地交互资源。
CoBot 不自动提交项目文件。它的私有 Codex 环境必须与共建者日常的登录和配置分开。
不能为通过测试而放宽沙箱、审批或 hook 信任要求。

启动时从 `PATH` 查找 Codex，按适配器的 `MinSupportedVersion` 检查版本，并在设置页展示结果。
设置 API 还接受 `enginePath`，用于假引擎测试等场景；界面不提供该字段。

Codex 最低版本按平台区分：macOS 为 0.146.0，Windows 和 Linux 为 0.161.0。
上游已记录 macOS ARM64 原生冒烟测试，覆盖登录、模型轮次、任务交付、文件工具、命令批准与拒绝及重启。
这些版本是兼容性下限，不代表所有平台都已完成完整验收。

升级与回滚遵循[升级说明](upgrading.md)。尤其是迁移 00023 会清理旧任务模型的数据，
不能把自动迁移理解为保留全部旧业务记录；具体版本的发布说明必须交代相关兼容性影响。

### 原生租约 hook

CoBot 只在自己的私有 Codex 环境中注册 `PreToolUse`，匹配器为 `Bash|apply_patch`。
覆盖范围取决于原生执行路径是否实际调用 hook；它不能撤销已运行的命令，也不提供完整隔离。

启动时可经 app-server 的 `hooks/list` 和 `config/batchWrite` 授权生成的 hook，
写入与 `/hooks` 相同的 `trusted_hash` 记录。
这仅适用于 CoBot 拥有且内容逐字匹配的私有定义；项目 hook、受管 hook 和修改过的文件不会自动获得信任。
设置页展示观察到的状态，并提供重试入口。

自动授权被拒绝时，先结束运行中的工作，再在 CoBot 私有环境中启动 Codex，手动审查 hook。
核对命令指向预期的辅助程序和数据根。以下 PowerShell 示例结束后会恢复当前 shell 的环境变量，请替换两个路径。

```powershell
$cobotRoot = 'C:\your\cobot-data-root'
$teamDirectory = 'C:\your\team-directory'
$previousCodexHome = $env:CODEX_HOME
$previousCodexSQLiteHome = $env:CODEX_SQLITE_HOME
try {
    $env:CODEX_HOME = Join-Path $cobotRoot 'engines/codex/home'
    $env:CODEX_SQLITE_HOME = Join-Path $cobotRoot 'engines/codex/sqlite'
    $hooks = Get-Content (Join-Path $env:CODEX_HOME 'hooks.json') -Raw | ConvertFrom-Json
    $command = $hooks.hooks.PreToolUse[0].hooks[0].command
    if ($command -match '-EncodedCommand (\S+)$') {
        [Text.Encoding]::Unicode.GetString([Convert]::FromBase64String($Matches[1]))
    } else { $command }
    codex --no-daemon --no-alt-screen --cd $teamDirectory
    # In Codex, type /hooks, review the definition, then exit.
} finally {
    $env:CODEX_HOME = $previousCodexHome
    $env:CODEX_SQLITE_HOME = $previousCodexSQLiteHome
}
```

macOS 可在子 shell 中设置同样两个变量，再运行 `codex --no-daemon --cd "$teamDirectory"`。
`--no-daemon` 用于避开后台服务的 socket 路径长度限制，不绕过沙箱或信任。
此手动会话只用于配置审查：没有 CoBot 宿主绑定的原生工具轮次会被守卫拒绝。不能使用绕过信任的参数。

## 工程规则

- 修改 HTTP 契约时先改 `api/openapi.yaml`，重新生成，并将规范与生成的 Go/TypeScript 改动一起提交。
- 数据库只追加新的 goose 迁移，不修改已发布迁移；同步更新 `internal/store/sqlite/migrate.go` 中的 `MaxSupportedVersion`。
- 涉及引擎协议变化时，先运行 `cobot trace protocol`，将本机协议与 `internal/engine/codex/protocol_baseline.json` 对照，再修改适配器。
- `frontend/src/i18n/locales` 中的中英文键保持一致；英文允许额外的 `_one`/`_other` 复数形式。
  删除无用键，错误使用消息键，图标按钮提供可访问名称。
- 除非帮助用户作出判断，不在正常产品流程中暴露技术诊断细节。

## 验证要求

提交代码前运行 `scripts/check.ps1` 或 `scripts/check.sh`，覆盖公开文件策略、仓库工作流测试、头像目录生成校验、格式、生成契约漂移、
前端构建与类型检查及测试、Go vet、构建、单元与集成测试。
Windows 上的 `scripts/check.ps1 -Desktop` 会额外编译桌面壳。

实现过程中可先做相关检查：

```sh
go test ./internal/... ./tests/...
npm run typecheck --prefix frontend
npm test --prefix frontend
```

浏览器测试需要先安装前端依赖和 Playwright 浏览器：

```sh
cd tests/e2e
npm ci
npx playwright install chromium
npx playwright test
```

浏览器测试准备阶段会构建前端、核心和假引擎，再在 `.tmp/test-runs` 下启动隔离的核心。
只在适合的本机或 CI 环境中安装浏览器系统依赖。

已安装 Chrome 时，可将环境变量 `COBOT_E2E_CHANNEL` 设为 `chrome` 后运行 Playwright，使用已有浏览器而不另行下载。
验证记录应注明浏览器渠道和版本；CI 默认使用 Playwright 锁定的 Chromium。

验证结果必须区分：

- 自动化测试只证明所覆盖组件的行为；假引擎测试不证明真实 Codex 行为。
- 真实 Codex 验收单独显式执行，使用隔离数据、官方登录和可用账号额度，记录版本、范围、观察结果、失败和未测部分。
- 原生编译不证明安装或原生运行通过；平台支持声明不能超出 README 中记录的证据范围。

纯文档改动检查路径、命令、链接和公开文件策略即可；未修改运行行为或可执行脚本时，无需运行应用全量测试。
相关检查未执行时应说明。

## 公共与私有文档

所有共建者共同遵守的项目要求必须在已提交的文档中可读。
文件名使用英文，公开文档原则上以英文为主，翻译版本应链接对应主版本。
当前本轮整理的规范及依赖、升级说明暂用中文讨论，稳定后由维护者安排改回英文。
同一条规则只在一个位置维护，其他地方引用；行为变化时同步更新对应文档。
区分已确认需求、已实现行为和已验证结果，设计决定本身不证明实现或验收通过。

私有文档统一写在当前工作目录根部的 `.local/`，包括本机环境、个人偏好、工作笔记、需求讨论、草稿和临时分析。
整个目录不提交 Git。共建者可自行维护 `.local/README.md` 作为私有索引。
它可以为空或不存在，新 clone 不会自动创建，正常开发不能依赖它。
智能体按 [AGENTS.md](../AGENTS.md) 中的可选入口读取。不提交用于保留目录的占位文件。
每个 worktree 拥有自己的本机文件，不会自动共享。

私有资料按用途分类：

| 目录 | 内容与维护方式 |
| --- | --- |
| `.local/knowledge/` | 当前有效的需求共识、业务说明与决策索引；变化时更新并保留依据 |
| `.local/discussions/` | 未实现方案、未决问题和待复核问题；不能自动当成现行实现 |
| `.local/operations/` | 本机维护、打包与排障手册；步骤与历史特例分开 |
| `.local/validation/` | 指定版本、环境、范围的验证记录；不随实现变化倒写通过结论 |
| `.local/archive/` | 被替代的方案、原型、历史报告与资料快照；退出默认阅读路径 |
| `.local/migrations/` | 私有资料迁移清单、原路径与新路径映射、文件校验值 |

文档注明用途、状态、适用版本或提交、核对日期和来源。状态与证据分开：
“已确认”不代表“已实现”，“已实现”不代表“已验证”。同一主题形成新决定时，标明替代关系。
需要多个 worktree 共用私人资料时，由所有者选择一个主目录，并在各自的私有 README 中指向它，避免多份各自更新。
迁移时先核对原文件与目标文件的校验值，再清理原路径；历史正文中的旧路径通过迁移映射查找，不批量改写历史结论。

这里约定的是文档存放位置。构建产物和原始测试证据仍放在被忽略的 `.tmp/`，运行录制仍在对应的数据根中。
已有本机私有文档可保留原位，待所有者明确迁移；不因无关任务顺带移动或重写它们。

`docs/` 下的文档和 AI 指令文件默认私有。
只公开经过审查、且具体路径已加入 `scripts/public-files-policy.json` 的 `publicFiles` 白名单的文件。
根目录隐藏项由 `rootDotEntries` 控制，不将 `.local` 加入其中。调整公开路径后运行：

```sh
node scripts/check-public-files.mjs --write-ignore
node scripts/check-public-files.mjs
```

检查器检查 Git 索引，包括被强行暂存的文件，CI 也会再次检查。
只暂存本次要公开的具体文件。忽略规则不会移除已跟踪内容或清除历史版本。

不公开凭据、启动授权链接、数据库、私人指令或原始引擎录制。
`cobot trace` 原样记录流量，可能包含提示词、文件内容和凭据。
普通问题反馈不应仅为报告问题而开启录制，只分享最小、人工脱敏的复现资料。
漏洞的私密反馈方式见[安全政策](../SECURITY.md)。
