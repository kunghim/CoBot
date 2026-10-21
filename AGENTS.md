# CoBot 编码智能体入口

CoBot 是本地运行的多智能体协作壳：管团队、需求接力、消息、审批、调度和用量；模型与工具循环由 Codex（`codex app-server`）执行。
不在 CoBot 里重做 Codex 已有的能力，也不加没有实现的界面入口。

## 始终适用

- 开始前看 `git status --short` 和当前分支，保留不属于你的改动。
- 不碰 `~/.cobot`、`~/.cobot-dev` 和日常 Codex 登录；不修改用户本机的 Codex 配置、沙箱、登录或防火墙。
- 缺少必需的工具或资源时说明情况并等待，不猜路径、不静默安装。
- 文档与代码不一致时以代码为准，并指出差异。
- 与用户沟通使用用户的语言；代码、标识符和提交信息用英文。

其余规则按下面两张表，到需要时再读。

## 改到哪里，读哪份规则

| 改动涉及 | 读 |
| --- | --- |
| `api/openapi.yaml`、`internal/api/**` | [rules/api-contract.md](docs/rules/api-contract.md) |
| `internal/store/sqlite/**` | [rules/migrations.md](docs/rules/migrations.md) |
| `internal/engine/**`、`internal/hookrun/**` | [rules/engine.md](docs/rules/engine.md) |
| 接力、卡片、协作工具、角色：`internal/app/relay*`、`chains.go`、`internal/collaboration/**`、`internal/roles/**` | [rules/relay.md](docs/rules/relay.md) |
| 重启、审批、租约：`recovery.go`、`approval*.go`、`lease.go` | [rules/recovery.md](docs/rules/recovery.md) |
| `frontend/**` | [rules/frontend.md](docs/rules/frontend.md) |
| `assets/**` | [rules/assets.md](docs/rules/assets.md) |

## 做到哪一步，读哪份流程

| 阶段 | 读 |
| --- | --- |
| 开始实现功能或修复 | [workflows/implement.md](docs/workflows/implement.md) |
| 跑测试、起服务、验证 | [workflows/verify.md](docs/workflows/verify.md)、[pitfalls.md](docs/pitfalls.md) |
| 做完汇报；提交、拉取、推送、开 PR | [workflows/deliver.md](docs/workflows/deliver.md) |
| 写笔记、方案、记录，整理或归档文档 | [workflows/docs.md](docs/workflows/docs.md) |
| 依赖、升级回滚、发布、Issue 分诊、hook 排障 | [docs/README.md](docs/README.md#很少需要) |

## 本机补充

私人资料只在主检出目录的 `.local/`（被 Git 忽略的独立仓库），worktree 里也读写那一份，定位方法见[文档流程](docs/workflows/docs.md#私人资料local)。
若它的 `README.md` 存在且非空，先读它再按需读引用的资料；不存在就直接继续，不索取。
