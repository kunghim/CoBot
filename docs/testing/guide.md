# 实测操作指南

适用：执行发布前实测的会话。读完这一篇就够，不需要再读源码来弄清 CoBot 怎么装、怎么启动、东西在界面哪里、怎么验证。
哪些用例要跑见 [README](README.md)；用例在 [cases/](cases/)。

## 1. 硬性规则

- **不碰正式数据**：不在 `~/.cobot`、`~/.cobot-dev` 和任何真实 profile 的原位置运行程序，只用只读复制出来的副本。复制前确认没有 CoBot 进程在用源目录（看 `runtime/instance.json` 里的 pid 是否还活着，活着就先问用户）。
- **不碰日常 Codex**：不碰 `~/.codex`，不改本机 Codex 配置、PATH、沙箱和防火墙。
- **不读凭据**：不读、不打印 `auth.json` 和 `runtime/credentials/` 里的内容。要比对密钥只用掩码（前 4 位 + `****` + 后 2 位）。停止核心需要实例密钥时，只用来调接口，不打印。
- **不提交、不推送、不合并**：记录和证据留在 `.local` 与 `.tmp`。触发 `release draft`、公开发布都由用户操作。
- **不自行修代码**：发现问题先记录，修复由用户决定。
- **测试设施的问题和产品问题分开记。**
- 真实 Codex 会话是正常消耗，照常跑；额度用完就暂停，记录时间，等恢复，不换用户的日常账号。

## 2. 构建候选版本

候选版本是 `origin/dev` 的最新提交。它测完，才合并 `main` 并打包发布。

```sh
git fetch origin
git worktree add --detach .claude/worktrees/rc-test origin/dev   # 测完用 git worktree remove 清掉，不建分支
cd .claude/worktrees/rc-test
npm ci --prefix frontend && npm run build --prefix frontend
go build -o .tmp/bin/cobot.exe ./cmd/cobot
./scripts/build.ps1 -Target desktop      # 桌面壳和同版本的 cobot.exe，输出在 .tmp/dist
```

记录：`git log -1 --format=%H`、`go version`、`node --version`、`codex --version`、Edge 版本。

打包（只有打包相关用例到期才做）：

```sh
./scripts/package-windows.ps1 -Version <候选产品版本> -MsiVersion <候选 MSI 版本> -PreviousMsi <上一个已发布的 MSI>
```

- 产品版本是计划发布的版本；MSI 版本必须大于上一个已发布的 MSI，`-PreviousMsi` 会替你校验。
- 上一个已发布版本：`gh release list`。beta 都是预发布，`gh release view` 不带 tag 会报找不到，要写明 tag。
- `gh release download <tag> -p release-manifest.json` 里有 `sourceCommit` 和 `tag`，可以确认某个已发布版本对应哪个提交。

## 3. 数据根

所有数据根放在**主检出目录**的 `.tmp/test-runs/<日期>-<名称>/` 下（下文简称 `$R`），不放在 worktree 里，worktree 删掉后数据还在。

| 名称 | 来源 | 用途 |
| --- | --- | --- |
| `$R/fresh-*` | 每次新建的空目录 | 新用户、头像、引擎检测 |
| `$R/upgrade` | 上一个已发布版本产生的真实数据的只读副本 | 升级 |
| `$R/real` | 已登录官方账号的测试数据根的只读副本 | 真实 Codex 主链路 |
| `$R/gw` | 配了网关供应商的测试数据根的副本 | 网关供应商 |

- 来源目录的实际路径记在各自的 `.local/` 里，不写进公开文档。
- 密钥用 DPAPI 绑定当前 Windows 用户，同一用户下复制目录就能用。
- 副本刷新令牌后，源目录里的登录可能失效，需要在正式版里重新登录一次，这是预期的。
- 需要官方账号和网关同时可用的用例，在 `$R/real` 里按 [模型](cases/models.md) D2-1 添加网关，key 请用户填。

## 4. 启动与停止核心

```sh
.tmp/bin/cobot.exe serve --data-root <数据根> --listen 127.0.0.1:<端口> --no-open
```

- 端口：每个数据根一个固定端口，用 18861～18869。5173 留给 `dev`，18777 留给 E2E。
- 同一数据根上已有核心在跑时，`serve` 只会打开那个实例，不会新起；并行测试用不同的数据根。
- 操作界面用浏览器打开 `http://127.0.0.1:<端口>`，打开页面即获得会话。
- 正常停止：Ctrl+C 或 `POST /internal/v1/service/stop`（头 `X-Instance-Secret`），不直接杀进程。强杀只用于恢复类用例（[接力](cases/relay.md) D1-7）。
- 需要协议记录时用 `cobot trace on --data-root <数据根>`。录制未脱敏，只留在数据根里，不提交、不贴出。

## 5. 观察与操作手段

按便宜到贵选，能用前面的就不用后面的：

1. **接口回读（首选）**：确定、省 token。写操作之后一律回读确认落库，不以「页面看起来变了」为准。
   - 常用只读接口：`/api/v1/status`、`/settings`、`/engine`、`/providers`、`/models`、`/teams`、`/teams/{id}/requirements`、`/teams/{id}/tasks`、`/approvals`、`/questions`、`/teams/{id}/activity`、`/bots/{id}/stats`、`/avatars/catalog`。完整列表在 `api/openapi.yaml`，用 `grep` 查路径，不读处理器代码。
   - **直接用 `scripts/live-test-api.ps1`**，不要自己重写：`. scripts/live-test-api.ps1 -Port <端口>` 之后 `Api '/api/v1/engine'` 返回响应里的 `data`；写请求 `Api <路径> 'PATCH' @{…}` 自带 CSRF 和 Origin；`Stop-CobotCore -SecretFile <实例密钥文件>` 优雅停核心。每次 shell 调用都是新进程，每次都要重新点源。
   - 会话 cookie 只发给 `Accept` 含 `text/html` 的文档请求；普通接口请求拿不到会话会返回 401（辅助脚本已处理）。也可以用 `X-Instance-Secret` 走 `POST /internal/v1/session/link` 换一次性链接。
   - 卡片链、运行状态可以直接只读查询数据根里的 SQLite，不要写它。
2. **页面文本**：用 Playwright（本机 Edge，`channel=msedge`）或内置浏览器读页面文本和可访问性树。读文本比截图便宜得多。
3. **截图**：只用于视觉用例（主题、布局、空态），记录里写路径。
4. **电脑控制**：只用于桌面壳原生窗口和 MSI 对话框。原生窗口里的控件由 WebView2 渲染，写操作统一在核心的 HTTP 界面上做，两者是同一套前端。

- 模型和供应商以协议记录或数据库为准，不以模型自述为准。
- 内置浏览器曾在某次实测里查询 console 超时，同一地址用 Playwright 完全正常，判定为宿主问题；遇到类似情况换手段并记为测试设施问题。

## 6. 界面地图

| 要找的东西 | 在哪里 |
| --- | --- |
| 新建团队 | 侧栏的「新建团队」对话框：角色（项目经理必选）、工作目录、团队名称 |
| 待确认卡、验收卡、提问 | 直接出现在会话主区 |
| 原生审批卡 | 同在会话主区，显示命令、目录、理由和四个按钮 |
| 选模型 | 输入框里，按供应商分组列出所有已启用供应商的模型，选择写到成员 |
| 额度 | 输入框下方，显示最接近用完的窗口，80% 黄、95% 红 |
| 团队浏览器、标签、视口 | 工作区的浏览器标签 |
| 设置 | 左侧栏「设置」按钮；9 个分区：通用、外观、角色、Codex 引擎、模型供应商、默认模型、协作与预算、已归档、关于（无「安全」分区） |
| 头像库、检查更新 | 设置 → 外观 |
| 角色头像 | 设置 → 角色 |
| 记忆 | 成员的记忆页，可查看、删除 |

### 试跑中踩到的操作坑

- 接口响应都包在 `data` 里（`Api` 已经取出），列表的字段各接口不同，先看一次真实响应再取值。
- 自己写临时脚本时变量名别和调用方撞（`$S`/`$s`、`$args`）；`WebRequestSession` 不能用 `Export-Clixml` 保存。
- 内置浏览器：视口为 0 时先 `resize_window`；`get_page_text` 只返回 `<main>`；用 `find` + `form_input` 比坐标点击稳；测完 `resize_window` 还原 `desktop`。
- 停测试核心用 `POST /internal/v1/service/stop`（带 `X-Instance-Secret`），不要杀进程；只停自己启动的，别碰其他会话的进程。

## 7. 记录与账本

- **测试记录**：`.local/validation/<日期>-live-test-record.md`。开头写元信息（`status`、`updated`、`code` 填被测提交），然后写平台、各工具版本、数据根、操作方式。
  - 每条用例分开写三项判定：需求是否满足、有无问题、测试是否通过。
  - 三类验证分开写：自动化、真实 Codex、打包产物。假引擎通过不能证明真实 Codex 的行为。
  - 失败、跳过、未测的写明原因；沿用的用例不重测，把 `plan` 输出的「沿用」段写进去即可。
  - 证据（截图、日志、录制）留在 `$R` 下，记录里只写路径和结论。
  - 最后写结论：**能否进入合并 `main` 的下一步**；不能的话，还差什么。
- **边测边记**：每跑完一个功能区（或一组相关用例）就立刻写进记录并 `mark`，不要等全部测完。会话中途中断时，已记的结果才不会丢，下次的计划也只会重测没记的。
- **账本**：`.local/validation/live-test-ledger.json`，记录每条用例上次在哪个提交的结果。只记真正跑过的：

  ```sh
  node scripts/live-test-plan.mjs mark --commit <被测提交> --record <记录文件名> D1-1=pass D1-2=fail:一句话原因 AV-13=skip:代理环境不允许 D2-6=partial:余额不足路径未遇到
  ```

  - 结果五种：`pass` 全部满足；`fail` 未满足；`skip` 没法测；`partial` 只测了一部分，**必须写明没覆盖什么**；`waived` 用户已决定不改、不再测这一部分，**必须写明谁决定的和范围**（所在功能区的源码再变动时仍会重测）。部分覆盖不能记 `pass`，否则下次计划会把没测的部分当成已测；记 `partial` 的用例下次仍会列在「要跑」里并带着这句说明。
  - 写「能否合并」的结论前，先把每个问题标成「产品问题」「待复现」或「环境问题」。数据根是旧副本、带历史遗留配置，或只在本机特定网络下出现的，先标「待复现」，用干净数据根复现后再定级，不直接当作阻止发布。

## 8. 问题分级

| 级别 | 含义 | 对合并 |
| --- | --- | --- |
| P0 | 数据丢失、安全问题、主链路不通、产物无法启动 | 阻止 |
| P1 | 功能不符合需求，用户无法自行绕过 | 阻止 |
| P2 | 体验问题，或有明确绕过办法 | 用户决定：修复，或写进已知限制 |
| P3 | 文案、样式 | 记录 |

每个问题写明：复现步骤、期望、实际、证据路径、影响范围、建议处理。

## 9. 需要用户配合的点

| 时机 | 事项 |
| --- | --- |
| [启动](cases/start.md) C4 | 在浏览器里完成官方账号登录（智能体不输入密码） |
| [启动](cases/start.md) C5、[模型](cases/models.md) D2-4、D2-5 | 网关 key 需要填进新数据根时，由用户填 |
| [引擎](cases/engine.md) E3 | 是否下载最新正式版 Codex |
| 合并 `main` 与打包之后 | 触发 `release draft`、公开发布 |
| 额度用完 | 等待恢复，或用户决定先跳过 |
