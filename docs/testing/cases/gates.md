# 门禁：自动化基线

每次候选版本都跑，不看改了什么。门禁不过，先记录并告诉用户，不进入后面的用例。
全部在候选版本的 worktree 里执行（见 [操作指南](../guide.md#2-构建候选版本)）。

| ID | 层 | 步骤 | 期望 |
| --- | --- | --- | --- |
| A1 | 冒烟 | `pwsh -File ./scripts/check.ps1 -Desktop` | 全绿，桌面壳编译通过 |
| A2 | 冒烟 | `cd tests/e2e && npm ci && COBOT_E2E_CHANNEL=msedge npx playwright test` | 全部 spec 通过；结束后没有残留的 `msedge.exe`、`cobot*.exe`（`tasklist` 里只看本次启动的进程） |
| A3 | 冒烟 | `go test -race ./internal/browser/... ./internal/engine/... ./internal/app/...` | 无数据竞争 |
| A4 | 冒烟 | `cobot trace protocol`，对照仓库里的协议基线和本机 Codex | 0 差异，或差异逐项有说明 |
| A5 | 冒烟 | `node scripts/check-docs.mjs`、`node scripts/check-public-files.mjs`、`node assets/avatars/build.mjs --check` | 全绿 |
| A6 | 冒烟 | `gh run list -b dev -L 3`，并看 CodeQL 和 Dependabot 的未处理告警数 | `dev` 的 CI 成功；告警为 0 |

偶发失败：单独重跑 3 次，记录是否复现，不直接判通过。已知的偶发失败有 `TestNotesReachTheNextTurn`（`no such table`），重跑通过时在记录里注明。
