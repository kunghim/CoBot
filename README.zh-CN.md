# CoBot

**AI 团队工作台，让需求逐步成为用得上的成果。**

[English](README.md) · 简体中文

CoBot 面向有想法、有实际需求，即使不会编程也希望动手实现的人。你可以先通过聊天梳理想法，也可以把目标交给 AI 团队，共同确认方案、推进实现、检查和修改，最后验收成果。网站和小型软件工具是当前团队场景，长期将逐步拓展更多工作。

[下载与开始](#快速开始) · [工作方式](#工作方式) · [参与共建](#参与开发)

> **项目处于早期阶段。** 当前公开版本是 [v0.0.1](https://github.com/kunghim/CoBot/releases/tag/v0.0.1)。本文介绍 `dev` 源码，具体安装包的条件与变化以对应发布说明为准。发布版本号不代表所有平台、模型和任务均已验证。

## 工作方式

**先从聊天开始。** 不需要先建团队或选择项目目录，就能提问、讨论想法。聊天有独立历史，可选择模型、思考方式和权限。

**需要做项目时，再交给团队。** 告诉项目经理你想解决什么问题、成果给谁使用。团队整理需求和工作步骤，交给你确认；成员接力实现、测试和评审，检查发现的问题可以退回修改。你处理必要的选择，并最终验收成果。

例如，你可以这样提出第一个需求：

> 我想做一个能录入和查询客户信息的小工具。先帮我问清需要哪些功能，再说明怎样检查是否完成。

这是需求示例，不是已经验证的客户案例。先约定小范围，用虚构数据试用；验收应包含实际操作和预期结果，不能只看一句“已经完成”。

## 当前可以做什么

- **聊天与整理历史。** 流式查看回复、停止当前轮次、重命名，并为后续消息调整模型、思考方式和权限。搜索聊天及团队成员会话的标题和消息内容；置顶聊天、归入分区，归档或恢复聊天与团队。
- **跟进团队工作。** 确认需求、查看交接记录、提出修改。提问、审批和验收卡显示在会话输入框上方。缺少角色时，团队可申请增加成员，由你确认。一条需求内的环节目前按顺序推进。
- **在会话旁查看成果。** 工作区提供浏览器、只读文件树、文件预览与变更记录，可查看常见文档、表格、演示文稿、PDF、图片和文本，具体呈现依格式而异。历史变更与当前磁盘文件分开显示；回复结束后也可从资源条进入文件和变更摘要。
- **试运行可启动的交付。** 验收汇报包含预览命令和访问地址时，验收卡会显示命令并提供“启动并打开”。你决定是否在团队目录运行，在内置浏览器查看，并可停止预览。这是本地试运行，不是自动托管或部署。
- **继续已有目录的工作。** “打开团队”可跳转到活跃团队、确认恢复已归档团队，或准备新建。目录留有其他环境的团队标记时，可显式确认接管；接管不会导入原对话。
- **选择模型服务。** 使用 ChatGPT 账号，或配置兼容 OpenAI Responses 的供应商。成员可各选模型，也可跟随默认设置；随程序附带、可更新的模型目录提供参数和思考方式，高级设置支持相应调整。连通检查成功不代表模型已通过完整团队工作流验证。
- **掌握决定与用量。** 查看命令、文件审批与模型用量，设置协作轮次限制和每日 token 预算。执行记录帮助核对实际过程，测试和 AI 评审仍不能保证结果正确。

当前只使用 **ChatGPT 桌面端内置的 Codex 引擎**，CoBot 有独立环境与登录，不使用单独安装的 Codex CLI。CoBot 组织会话与协作，模型和工具的执行由引擎负责。

## 快速开始

### 桌面版下载

[**v0.0.1 — 发布说明、下载与校验文件**](https://github.com/kunghim/CoBot/releases/tag/v0.0.1)

| 安装包 | 启动方式 |
| --- | --- |
| [Windows x64 MSI](https://github.com/kunghim/CoBot/releases/download/v0.0.1/CoBot-0.0.1-windows-x64.msi) | 为当前用户安装，从开始菜单打开 CoBot |
| [Windows x64 ZIP](https://github.com/kunghim/CoBot/releases/download/v0.0.1/CoBot-0.0.1-windows-x64.zip) | 完整解压后打开 `cobot-desktop.exe`，保留同目录的 `cobot-helper.exe` 与 `cobot.exe` |
| [macOS Apple Silicon DMG](https://github.com/kunghim/CoBot/releases/download/v0.0.1/CoBot-0.0.1-macos-arm64.dmg) | 将 `CoBot.app` 复制到「应用程序」，弹出映像后打开已安装的应用 |

**开始前：** 另行安装 [ChatGPT 桌面端](https://learn.chatgpt.com/docs/app)。CoBot 自动发现受支持的 macOS 应用和 Windows Store 包内的 Codex，引擎登录与配置独立。Windows 需 Windows 10/11 x64 与已有 WebView2 Runtime。使用这些下载包不需要 Go、Node.js 或单独安装 Codex CLI。

安装包没有发行签名或公证，macOS 使用临时签名；首次打开说明及相邻的 `.sha256` 校验文件见发布页。GitHub 的 **Source code** 压缩包用于构建，不能直接运行。

1. 打开 CoBot，在**设置 → 模型供应商**中为 CoBot 登录或配置兼容服务，选择默认模型。模型服务可能需要订阅或 API 费用。
2. 从**聊天**开始，或切换到**团队**，在一个目录中新建团队，向项目经理说明目标。
3. 团队任务中，确认提出的需求，检查结果并提出修改。出现预览入口时，先查看命令再决定启动。

界面初始为中文，可通过**设置 → 通用 → 语言**切换 English。找不到引擎时，检查桌面端安装和设置页状态，单独安装 CLI 不能替代。求助可到 [Q&A](https://github.com/kunghim/CoBot/discussions/categories/q-a)，提供 CoBot 版本、系统和脱敏错误信息。

### 数据与更新

CoBot 本地运行，只监听回环地址。发行包通常将设置、登录和工作记录保存在 `~/.cobot`，与程序文件分开。模型请求会将相关内容发送到所选服务，本地运行不等于离线 AI，也不等于数据全部留在电脑。

当前手动更新：关闭 CoBot 后安装新版 MSI，或完整替换 ZIP／应用包。保留数据目录，卸载或删除程序文件不会移除这些数据。切换版本前阅读[升级与回滚说明](docs/reference/upgrading.md)。删除聊天也会保留生成文件。

## 当前状态

| 平台 | 当前范围 |
| --- | --- |
| Windows x64 | 已发布 MSI 与 ZIP；需要 WebView2 及受支持的桌面引擎安装 |
| macOS Apple Silicon | 已发布 ARM64 DMG；未进行 Developer ID 签名和公证 |
| macOS Intel | 没有公开安装包；实验性源码构建不等于原生支持已验证 |
| Linux | 有共享核心 CI 覆盖；暂不支持桌面引擎自动发现，不能据 CI 判断终端用户可用 |

发布自动化检查构建与包的完整性，交互安装、真实模型行为和具体平台使用需要另行验证，见[发布说明](https://github.com/kunghim/CoBot/releases/tag/v0.0.1)。自定义角色和记忆写入尚未开放。详细行为与边界见[产品现状](docs/product/overview.md)和[决定索引](docs/product/decisions.md)。

## 参与开发

欢迎一起让不会编程的人也能获得 AI 完成工作的能力。真实需求示例、试用反馈、领域经验、设计、文档、翻译和工程修复都能帮助项目向前推进。先读 [CONTRIBUTING.md](CONTRIBUTING.md)，较大的功能先讨论再实现。欢迎中文或英文的 Issue 和 PR，核心工程文档目前以中文为主。

修改仓库时从 `dev` 建主题分支，每个 PR 聚焦一项改动，目标为 `dev`，说明实际检查与限制。参与不要求私人笔记或 AI 编码工具。

<details>
<summary>构建当前源码与运行检查</summary>

### 从源码构建

构建工具：Git、符合 [go.mod](go.mod) 的 Go（**1.26.0+**）、Node.js 24 与 npm 11+。执行智能体任务还需要上文所述的受支持 ChatGPT 桌面引擎与模型服务访问条件。

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

以上构建本机 Web 应用。使用时保持启动终端开启，在该终端按 **Ctrl+C** 停止；关闭浏览器不会停止服务。浏览器未打开时使用终端打印的启动链接，不要分享。再次进入本次试用时，沿用相同 `--data-root`。未显式指定时，开发环境使用 `~/.cobot-dev`，正式环境使用 `~/.cobot`。

桌面构建使用 [build.ps1](scripts/build.ps1) 或 [build.sh](scripts/build.sh) 的 `desktop` 目标，并准备相应 Wails 和原生构建工具。旧打包与原生验证细节保留在[固定版本参考](https://github.com/kunghim/CoBot/blob/727ea51/README.zh-CN.md#从源码构建)，其中历史条件不能替代当前脚本或发布说明。

只改文档时：

```bash
node scripts/check-docs.mjs
node scripts/check-public-files.mjs
```

代码改动运行 `./scripts/check.sh` 或 `./scripts/check.ps1`；按改动选择检查的规则见[验证流程](docs/workflows/verify.md)。自动化、真实模型和原生平台验证分别报告。

</details>

## 社区参与

- [报告问题或提出建议](https://github.com/kunghim/CoBot/issues/new/choose)。
- [安装与使用提问](https://github.com/kunghim/CoBot/discussions/categories/q-a)。
- [分享工作方式和成果](https://github.com/kunghim/CoBot/discussions/categories/show-and-tell)。
- [查看维护者公告](https://github.com/kunghim/CoBot/discussions/categories/announcements)。

安全漏洞按[安全政策](SECURITY.md)报告，参与时遵守[社区行为准则](CODE_OF_CONDUCT.md)。只分享最小、脱敏的证据，不公开凭据、启动链接和原始 trace。维护者尽力回复，不承诺固定时限。

## 发展方向

CoBot 从不会编程者的软件需求开始，未来逐步拓展其他场景与可复用工作方法，为每种工作分别设计工具、交付物和检查方式。这是发展方向，不是交付时间表。引擎接入保持可扩展，当前没有增加 Claude Code 的计划。

## 许可证

按 [Apache License 2.0](LICENSE) 许可。第三方依赖保留各自许可证，详见[依赖与声明](docs/reference/dependencies.md)。
