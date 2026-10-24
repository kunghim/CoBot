# 验证流程

适用：跑测试、起服务、真实 Codex 验证、汇报结果。测试和临时运行只用 `.tmp/test-runs/<名称>/` 这类显式数据根。数据根和环境的常见问题见 [踩坑记录](../pitfalls.md)。

## 选检查

| 改动 | 至少运行 |
| --- | --- |
| Go 代码 | `go test ./internal/... ./tests/...` |
| 前端代码 | `npm run typecheck --prefix frontend`、`npm test --prefix frontend` |
| 跨前后端的界面流程 | 浏览器测试：`cd tests/e2e`，`npm ci`，`npx playwright test` |
| `scripts/` 下的仓库策略、Issue 命令、发布脚本 | `node --test scripts/repository-policy.test.mjs scripts/issue-commands.test.mjs scripts/release.test.mjs scripts/check-docs.test.mjs` |
| 只改文档 | `node scripts/check-docs.mjs`（命名、索引、链接）和 `node scripts/check-public-files.mjs`；命令和说法自己核对 |

改代码后、交付前运行完整检查 `./scripts/check.ps1` 或 `./scripts/check.sh`（Windows 加 `-Desktop` 会额外编译桌面壳）。
通过后没有新原因就不重复跑无关检查。重要的状态变化补针对行为的回归测试，不写只复述实现的测试。

## 真实 Codex 验证

只在用户要求或改动确实依赖真实 Codex 行为时做，不放进默认检查：

- 握手冒烟：`COBOT_R_SMOKE=1 go test ./tests/integration/ -run TestRealCodexHandshake`，不跑模型轮次。
- 完整场景：从已登录的测试数据根复制一份到 `.tmp/test-runs/<名称>/` 再启动（做法见踩坑记录）。
  开发阶段不必顾虑账号额度，不用事先询问；测试需要多轮、长输出时直接做，但在记录里写下大概的轮数和用量，方便以后评估。
- 记录 Codex 版本、数据根、范围、观察结果、失败和未测部分。截图和日志留在测试数据根，记录里只写路径和结论。

## 汇报

三类验证分开写，不能互相替代：

- **自动化**：只证明所覆盖组件的行为；假引擎测试不证明真实 Codex 行为。
- **真实 Codex**：写明版本、账号类型和范围。
- **原生平台**：编译通过不等于安装或运行通过；桌面壳没有实际运行验证时不能说已验收。

没跑的检查写明原因；不能凭 mock 或模型自己的说法宣称成功。
