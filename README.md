# CoBot

开源、本地运行的多 Agent 协作应用：CoBot 组织角色、任务、沟通与审批，业务操作交给外部 Agent。首版先做编程，目标是用 CoBot 开发 CoBot，后续面向办公、学习、创作等场景。

**当前阶段：M1～M5 已按 [首版开发方案](docs/首版开发方案.md) 全部交付并通过 Web 真实验收、跨模块自审与独立子 Agent 审查**（任务/审批/协作链/风险评估/记忆/用量预算/恢复/租约/事件落盘全部落地，工作面板五页接真实数据）。Windows 上的本机 Web 核心已验证；macOS 客户端与桌面壳原生运行/安装为**待测**，不冒充已通过。各阶段证据见 M1～M5 验收记录。

## 快速开始（开发者）

前置：Go 1.25+、Node.js 24+（npm 11+）。最终用户运行 `cobot` 单文件即可，不需要 Node/Go/数据库服务器。

```bash
# 首次：安装前端依赖并生成接口类型（OpenAPI → TS + Go DTO）
npm ci --prefix frontend
./scripts/generate.sh          # Windows: powershell -File scripts/generate.ps1

# 开发循环：启动 Go 核心(development profile) + Vite，退出时回收进程
./scripts/dev.sh               # Windows: powershell -File scripts/dev.ps1
# 打开终端输出的一次性链接（60 秒、单次有效）；丢失后再次运行 cobot serve 会打印新链接

# 全量检查：格式/静态分析、契约生成差异、前端类型与单测、Go 构建与测试
./scripts/check.sh             # Windows: powershell -File scripts/check.ps1

# 构建发行物（输出到 .tmp/dist，附 sha256）
./scripts/build.sh --target server    # 独立 cobot 可执行文件（内嵌前端/迁移/角色）
./scripts/build.sh --target desktop   # Wails 桌面壳 + 同版本辅助 cobot（需 wails v2.15.0 CLI）
```

独立运行核心（不依赖脚本）：

```bash
go build -o .tmp/bin/cobot ./cmd/cobot
.tmp/bin/cobot serve --profile development            # 默认开发 profile，数据在 ~/.cobot-dev
.tmp/bin/cobot serve --profile production             # 生产 profile，数据在 ~/.cobot
.tmp/bin/cobot serve --data-root <path>               # 显式数据根（测试用临时目录）
# 同 profile 重复启动不会开第二个调度器：报告已运行实例并打印新的授权链接
```

测试入口：`go test ./internal/... ./tests/...`（单元 + 真 SQLite 集成，全部使用 `.tmp`/临时数据根，不接触真实用户库）；`npm test --prefix frontend`（Vitest）；`cd tests/e2e && npx playwright test`（浏览器 E2E：真实核心 + 假引擎，5 个 spec 文件共 6 条）。真实 Codex 的 R 类验收单独显式执行，不在默认检查里消耗账号。

其他子命令：`cobot mcp`（引擎拉起的单成员 MCP stdio 桥，环境变量见 `internal/collaboration`）；`cobot hook`（stdin JSON 的 hook 适配器，只执行团队 `.cobot/hooks.json` 显式配置的命令，副作用先过本地租约）。

## 工程版本

Go 1.25、React 18 + TypeScript + Vite 6、TanStack Query、Tailwind CSS 4、i18next（zh-CN/en）、SQLite（modernc）+ goose、Wails v2.15.0、HTTP JSON + fetch SSE。生成工具锁定：oapi-codegen v2.4.1、openapi-typescript 7.13.0（`scripts/generate.*`，禁止浮动 latest）。

## 阅读入口

| 文档 | 用途 |
| --- | --- |
| [编码智能体入口](AGENTS.md) | 直接进入本仓库的阅读顺序、开发边界和提交规则，无需历史聊天 |
| [首版开发方案](docs/首版开发方案.md) | 编码智能体交接入口：明确功能、默认行为、API/状态、M1～M5步骤与完成条件 |
| [首版目录架构](docs/首版目录架构.md) | 确定两个入口、一套核心/前端、模块依赖、构建与运行数据位置 |
| [首版验收清单](docs/首版验收清单.md) | 94项按ID交付的场景、操作、预期结果、测试类型与证据格式 |
| [需求文档](docs/需求文档.md) | 当前产品共识、功能范围、使用流程、验收条件与未决问题 |
| [界面布局](docs/界面布局.md) | 首页导航、团队列表、会话、成员工作面板和审批交互 |
| [接入基线](docs/接入基线.md) | 已处理前提、版本证据摘要及私有状态/Windows沙箱边界，无需历史归档 |
| [M1验收记录](docs/M1验收记录.md) | M1 阶段实现范围、命令、验收子项状态与已知问题 |
| [M2验收记录](docs/M2验收记录.md) | M2 阶段团队/引擎执行/审批的实现范围、命令、验收子项状态与本阶段修复的缺陷 |
| [M3验收记录](docs/M3验收记录.md) | M3 阶段投递/任务/审批闭环与 MCP 桥的实现范围、验收子项与修复缺陷 |
| [M4验收记录](docs/M4验收记录.md) | M4 阶段协作链/风险评估/记忆/用量预算/恢复/租约/事件落盘的实现范围与验收证据 |
| [依赖清单](docs/依赖清单.md) | 编译进发行物的 Go 与前端依赖、版本与逐项核实的许可证声明（SBOM 摘要） |
| [升级说明](docs/升级说明.md) | 升级前备份、自动迁移、回滚边界与签名现状 |
| [接入实验程序](experiments/app-server/README.md) | 无第三方依赖的复跑程序，不是 CoBot 产品服务 |

## 文档约定

- **已确认**：用户明确同意的产品规则或设计方向。
- **建议**：根据讨论补充的候选做法，未确认前不当成硬要求。
- **待讨论**：需要继续决定的产品选择。
- **待技术验证**：设计有方向，目标环境的能力和可靠性尚未证明。

需求文档维护产品共识；编码实施从首版开发方案开始，界面细节见界面布局，旧待定细则由实施默认承接，最新用户明确决定始终优先。不能把旧验证清单重新当成前置阻塞，也不能把工程默认倒写成历史用户已确认。

`docs`根目录保存可交付的当前文档。其子目录统一英文且不提交Git：`archive/`为历史草案/决策/调研，`prototypes/`为本机原型，`validation/`为原始验收证据；`references/`仅作本机材料区。新clone没有这些目录也可以依据根文档开发。旧已提交资料可从Git历史检索；不以整理为由清理外部项目、原生会话或用户文件。

## 支持矩阵

| 平台/形态 | 状态 | 说明 |
| --- | --- | --- |
| Windows 10/11 x64 · 本机 Web 核心 | 已验证 | 开发与全部验收在此完成：`scripts/check.ps1`、Go 单元/集成、6 条浏览器 E2E |
| Windows · 独立 server 发行物 | 已构建校验 | `scripts/build.ps1 -Target server` 产出单文件（内嵌前端/迁移/角色），附 sha256 |
| Windows · 桌面壳（Wails/WebView2） | 待测 | 代码保留且随构建编译；原生运行/安装未验证 |
| macOS（Web 核心 / 桌面壳） | 待测 | 无法在本机验证；未做签名与公证 |
| 浏览器 | Chrome 系已验证（Playwright） | 纯 Web 无桌面依赖；其他 Chromium 内核预期一致 |

## 运行边界

仅绑定 loopback，不建设云端业务。生产根 `~/.cobot`、开发根 `~/.cobot-dev`，互不串库；团队仍必选工作目录，全员可见，目录内 `.cobot` 保存识别与交互资源，沿用 Git 本地排除、不自动提交。页面授权为一次性启动链接（60 秒、单次）兑换 HttpOnly 会话；桌面经原生桥取得一次性秘密并使用内存 Bearer。已处理的接入前提不再重复验证；证据记录对应机器、版本与范围，产品功能随实际开发验收。许可证公开发布前由用户确定。
