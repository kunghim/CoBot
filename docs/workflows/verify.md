# 验证流程

适用：跑测试、起服务、真实 Codex 验证、汇报结果。测试和临时运行只用 `.tmp/test-runs/<名称>/` 这类显式数据根。数据根和环境的常见问题见 [踩坑记录](../pitfalls.md)。

## 选检查

| 改动 | 至少运行 |
| --- | --- |
| Go 代码 | `go test ./internal/... ./tests/...` |
| 前端代码 | `npm run typecheck --prefix frontend`、`npm test --prefix frontend` |
| 跨前后端的界面流程 | 浏览器测试：`cd tests/e2e`，`npm ci`，`npx playwright test` |
| `scripts/` 下的仓库策略、Issue 命令、发布脚本 | `node --test scripts/repository-policy.test.mjs scripts/issue-commands.test.mjs scripts/release.test.mjs scripts/check-docs.test.mjs scripts/live-test-plan.test.mjs` |
| 只改文档 | `node scripts/check-docs.mjs`（命名、索引、链接）和 `node scripts/check-public-files.mjs`；命令和说法自己核对 |

改代码后、交付前运行完整检查 `./scripts/check.ps1` 或 `./scripts/check.sh`（Windows 加 `-Desktop` 会额外编译桌面壳）。
通过后没有新原因就不重复跑无关检查。重要的状态变化补针对行为的回归测试，不写只复述实现的测试。

## 真实 Codex 验证

只在用户要求或改动确实依赖真实 Codex 行为时做，不放进默认检查：

- 握手冒烟：`COBOT_R_SMOKE=1 go test ./tests/integration/ -run TestRealCodexHandshake`，不跑模型轮次。
- 完整场景：从已登录的测试数据根复制一份到 `.tmp/test-runs/<名称>/` 再启动（做法见踩坑记录），会消耗账号额度，开始前告诉用户。
- 记录 Codex 版本、数据根、范围、观察结果、失败和未测部分。截图和日志留在测试数据根，记录里只写路径和结论。

## 开发中在真实应用里验证改动

改动影响界面或真实行为、自动化覆盖不到时：

1. `node scripts/live-test-plan.mjs touched` 列出这次改动（含未提交的）涉及的功能区和对应用例；默认对比 `origin/dev`，也可以 `--from <提交> --to <提交>`。
2. 读 [实测操作指南](../testing/guide.md)（怎么启动、数据根、界面里东西在哪），再读列出的 `cases/<功能区>.md`。列出的是整个功能区的用例，挑与改动相关的做，不必全跑。
3. 用隔离数据根验证，不碰正式数据。发布前的完整实测是另一回事，见 [发布前实测](../testing/README.md)，不用在开发中做。
4. 输出里有「未归类的文件」，说明你新增了不属于任何功能区的文件，先到 `docs/testing/areas.json` 里归类（CI 也会检查）。

## 汇报

三类验证分开写，不能互相替代：

- **自动化**：只证明所覆盖组件的行为；假引擎测试不证明真实 Codex 行为。
- **真实 Codex**：写明版本、账号类型和范围。
- **原生平台**：编译通过不等于安装或运行通过；桌面壳没有实际运行验证时不能说已验收。

没跑的检查写明原因；不能凭 mock 或模型自己的说法宣称成功。
