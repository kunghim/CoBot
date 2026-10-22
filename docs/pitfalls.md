# 踩坑记录

只收每次做这类事都会碰到的问题；修一次就不再出现的问题由代码和测试负责，不记在这里。
只和某台机器有关的问题（解释器路径、PATH 等）写在各自的 `.local/`。

## 数据根与运行

- **用哪个数据根？** 一律显式传 `.tmp/test-runs/<名称>/`。`scripts/dev` 不带 `--data-root` 时默认用 `~/.cobot-dev`，
  测试时必须显式指定；`~/.cobot` 是正式数据，永远不碰。
- **真实 Codex 需要登录？** 准备一个专门用于测试、已登录的数据根（路径记在自己的 `.local/`），每次复制一份到
  `.tmp/test-runs/<名称>/` 再用，源目录只读，复制前确认没有 CoBot 在用它。不用日常的 Codex 登录。
  副本刷新令牌后，源目录的登录可能失效，需要重新登录一次。
- **启动后连到了已有实例？** 同一数据根上已有核心在跑时，`serve` 只会打开那个实例，不会新起。并行运行要用不同的数据根。
- **两个 worktree 同时 `dev` 失败？** Vite 固定使用 5173 端口（`strictPort`），同一时间只能有一个 `dev`。
- **打开界面要找一次性链接吗？** 不用，直接打开核心地址就能拿到会话。
- **录制放哪？** `cobot trace` 的录制在数据根的 `traces/`，未脱敏，不提交、不贴出去。

## 新环境

- **新 worktree 里 Go 测试或构建失败？** 先 `npm ci --prefix frontend`，再 `npm run build --prefix frontend`：Go 构建要内嵌前端产物。
- **新 worktree 里没有 `.local/`？** 正常，它只在主检出目录。用 `git rev-parse --git-common-dir` 找到主目录，读写那一份，不在 worktree 里新建或复制。
- **浏览器测试不想下载 Chromium？** 设 `COBOT_E2E_CHANNEL=chrome` 使用本机 Chrome，记录里写明浏览器渠道和版本。
- **Windows 上发布测试报 EPERM？** 缺符号链接权限时该用例按设计跳过，目录联接校验照常执行。不要为此改系统权限。

## 写代码时容易忘

- 写页面前没看组件画廊和 `components/ui` → 见 [前端规则](rules/frontend.md)。
- 只改了一个语言包。
- 改了 `api/openapi.yaml` 没重新生成；加了迁移没调 `MaxSupportedVersion`。
- 改了头像或品牌源文件没重新生成 → 见 [资源规则](rules/assets.md)。
