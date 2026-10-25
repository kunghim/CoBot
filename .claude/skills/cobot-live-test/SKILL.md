---
name: cobot-live-test
description: CoBot 项目专用（kunghim/CoBot 仓库及其 worktree，其他项目不要用）。用户要求发布前实测、全面测试候选版本、安装最新版本逐项点击测试、写实测记录时使用：先用脚本算出这次要测的用例，再按操作指南和对应用例执行并记账。不用于单元测试、E2E、日常修 bug 或合并发布。
---

# CoBot 发布前实测

流程、用例和操作方法的唯一来源是仓库的 `docs/testing/`。先读 `docs/testing/README.md`，按它执行；本文件只列步骤，不重复规则。

## 步骤

1. 在候选版本所在的检出目录运行 `node scripts/live-test-plan.mjs`（候选默认是 `origin/dev` 最新提交；用户要求全量时加 `--full`）。
   退出码是 2、输出里有「需要人工归类的文件」时，先把这些文件归进 `docs/testing/areas.json` 再重跑，不要跳过。
2. 读一次 `docs/testing/guide.md`，再只读输出「要跑」里出现的 `docs/testing/cases/<功能区>.md`。不通读源码；对某个 PR 有疑问才读它的 diff。
3. 按指南构建候选版本、准备数据根。只用只读副本，不碰正式数据、不碰日常 Codex。
4. 先跑 `cases/gates.md` 的门禁，再跑「要跑」的用例，按功能区逐个做完，**要跑的全部跑完才算结束**，不要只挑冒烟。接口操作点源 `scripts/live-test-api.ps1`，不要重写辅助脚本。真实 Codex 会话是正常消耗，照常跑；需要用户配合的点（登录、网关 key）按指南第 9 节等用户。
5. **每做完一个功能区就立刻**写测试记录（主检出目录的 `.local/validation/`）并 `node scripts/live-test-plan.mjs mark …` 更新账本，只记真正跑过的用例；不要最后统一记。只测了一部分的用例记 `partial:没覆盖什么`，不要记 `pass`。沿用的用例在记录里写「沿用 `<提交>`」。用例文本和实际界面对不上时，在记录里写出来，并在测完后按实际修正对应的 `docs/testing/cases/*.md`。
6. 汇报：能否进入合并 `main` 的下一步；不能的话还差什么。问题先分「产品问题 / 待复现 / 环境问题」再定级，依赖旧数据根或本机特殊网络的，先在干净数据根复现，不直接当作阻止发布的问题。问题按指南第 8 节分级，不自行修代码。

合并 `main`、触发 `release draft`、公开发布都由用户决定，不替用户做。
