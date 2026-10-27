# 验证流程

适用：跑测试、起服务、真实 Codex 验证、汇报结果。测试和临时运行只用 `.tmp/test-runs/<名称>/` 这类显式数据根。数据根和环境的常见问题见 [踩坑记录](../pitfalls.md)。

## 选检查

| 改动 | 至少运行 |
| --- | --- |
| Go 代码 | `go test ./internal/... ./tests/...` |
| 前端代码 | `npm run typecheck --prefix frontend`、`npm test --prefix frontend` |
| 跨前后端的界面流程 | 浏览器测试：`cd tests/e2e`，`npm ci`，`npx playwright test` |
| `scripts/` 下的仓库策略、Issue 命令、发布脚本 | `node --test scripts/repository-policy.test.mjs scripts/issue-commands.test.mjs scripts/release.test.mjs scripts/frontend-bundle.test.mjs scripts/check-docs.test.mjs scripts/live-test-plan.test.mjs` |
| 只改文档 | `node scripts/check-docs.mjs`（命名、索引、链接）和 `node scripts/check-public-files.mjs`；命令和说法自己核对 |

改代码后、交付前运行完整检查 `./scripts/check.ps1` 或 `./scripts/check.sh`（Windows 加 `-Desktop` 会额外编译桌面壳）。
通过后没有新原因就不重复跑无关检查。重要的状态变化补针对行为的回归测试，不写只复述实现的测试。

## 浏览器失败证据

浏览器检查失败后，在该次 Actions 运行的 Artifacts 中下载 `browser-failure-...`，保留 7 天。
其中 HTML 报告可用 `npx playwright show-report <报告目录>` 打开；Trace Viewer 可查看操作、网络与失败截图。
上传前会移除临时 HTTP 凭据；脱敏失败时不上传。只上传合成测试报告，不上传 `.tmp/test-runs`、数据库或账号目录。
这些用例使用假引擎，不消耗模型额度；浏览器外观回归覆盖保存、刷新与系统明暗变化，不代表原生桌面已验收。
工作区预览截图单独上传为 `workspace-previews-...`，只收集合成 E2E 根目录下明确命名的 `filesystem-*.png`，用于 PR 界面审查；不上传整个测试数据根。

## 跨版本数据升级

前端构建完成后运行 `node scripts/check-upgrade.mjs <已发布版本标签>`；本地须已获取该标签，且它是当前候选提交的祖先。
脚本在 `.tmp/test-runs/upgrade-...` 中解出旧版源码，使用旧版存储实现生成全新合成数据，
再由候选版本启动核心、升级数据库，检查设置、团队、成员、消息及迁移备份，修改设置后重启再次核对。
独立的 **profile upgrade check** 工作流也可手动指定基线标签，上传的 `result.json` 仅记录版本与结果，不含数据根。
此检查覆盖数据升级与核心启动，不覆盖安装器、原生窗口或真实 Codex 登录。

## 真实 Codex 验证

只在用户要求或改动确实依赖真实 Codex 行为时做，不放进默认检查：

- 握手冒烟：`COBOT_R_SMOKE=1 go test ./tests/integration/ -run TestRealCodexHandshake`，不跑模型轮次。
- 完整场景：从已登录的测试数据根复制一份到 `.tmp/test-runs/<名称>/` 再启动（做法见踩坑记录）。
  开发阶段不必顾虑账号额度，不用事先询问；测试需要多轮、长输出时直接做，但在记录里写下大概的轮数和用量，方便以后评估。
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
