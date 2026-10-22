# 文档索引

这些文档主要写给编码智能体，按需读取。入口是 [AGENTS.md](../AGENTS.md)：它按改动路径和任务类型指向下面的文件。
贡献流程见 [CONTRIBUTING.md](../CONTRIBUTING.md)；第一次参与、想了解整体怎么配合，先读给人看的 [共建者手册](handbook.md)。

## 按改动路径（`rules/`）

| 文档 | 触发 |
| --- | --- |
| [HTTP 接口](rules/api-contract.md) | `api/openapi.yaml`、`internal/api/**` |
| [数据库迁移](rules/migrations.md) | `internal/store/sqlite/**` |
| [引擎适配](rules/engine.md) | `internal/engine/**`、`internal/hookrun/**` |
| [接力与协作工具](rules/relay.md) | 接力、卡片、协作工具、内置角色 |
| [重启、审批与租约](rules/recovery.md) | `recovery.go`、`approval*.go`、`lease.go` |
| [前端](rules/frontend.md) | `frontend/**` |
| [资源](rules/assets.md) | `assets/**` |

## 按任务（`workflows/`）

| 文档 | 触发 |
| --- | --- |
| [实现流程](workflows/implement.md) | 实现功能、修复、重构 |
| [验证流程](workflows/verify.md) | 跑测试、起服务、真实 Codex 验证、汇报结果 |
| [交付与同步流程](workflows/deliver.md) | 做完汇报；提交、拉取、推送、开 PR（含私人笔记同步） |
| [文档流程](workflows/docs.md) | 写方案和记录、整理或归档文档 |
| [踩坑记录](pitfalls.md) | 每次都会碰到的环境与运行问题 |

## 产品（`product/`）

| 文档 | 触发 |
| --- | --- |
| [产品现状](product/overview.md) | 梳理需求、判断能力有没有、业务术语 |
| [接力规则](product/relay.md) | 需求、链路、卡片、交接 |
| [决定索引](product/decisions.md) | 某件事是否定过、做到哪一步 |

## 很少需要

| 文档 | 触发 |
| --- | --- |
| [依赖与第三方声明](reference/dependencies.md) | 增删或升级依赖、核对许可证 |
| [升级与回滚](reference/upgrading.md) | 迁移对用户数据的影响、发行形态替换与回滚 |
| [原生 hook 手动审查](reference/hook-review.md) | hook 自动授权被拒绝 |
| [分支、审查与合并](reference/branching.md) | 维护者合并 PR、晋级 `main`、清理分支 |
| [Issue 分诊与认领命令](reference/triage.md) | 分诊标签、`ai-discovered`、`/claim` |
| [发布草稿](reference/release.md) | 打包并发布版本 |
