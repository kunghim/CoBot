# 文档流程

适用：写方案、讨论、验收或审查记录，新建或整理公开文档，归档旧资料。

## 放哪里

| 内容 | 位置 | 提交 |
| --- | --- | --- |
| 所有人共同遵守的规则和流程 | `docs/`；入口 `AGENTS.md`，`CLAUDE.md` 只指向它 | 是 |
| 方案、讨论、验收与审查记录、个人笔记 | `.local/` | 否 |
| 构建产物、原始日志、截图、一次性脚本 | `.tmp/<任务名>/`，随时可删 | 否 |
| `cobot trace` 录制、运行数据库 | 对应数据根 | 否 |

公开文档在没有任何私人资料时也必须看得懂；不写私人提示词、本机路径、录制或个人偏好。

## 公开文档

- 中文书写；`README`、`CONTRIBUTING.md` 面向外部访客，保持中英双语。
- 文件名小写英文加连字符。
- 只写读代码得不到、或者容易做错的东西：边界、不变式、做过的决定、反复出现的坑、做完的标准。
  模块列表、文件清单、版本号这类能从代码查到的不写；需要时写“以哪个文件为准”。
- `rules/` 按改动路径触发，`workflows/` 按任务触发，`reference/` 很少需要。单篇尽量一屏读完。
- 同一条规则只写在一处，其他地方链接。新增、移动或删除文档时，同步 `docs/README.md` 和 AGENTS.md 的路由表。
- 智能体指令文件（`AGENTS.md`、`CLAUDE.md`、`GEMINI.md` 等）和助手配置目录默认不提交，目前只公开根目录的
  `AGENTS.md`、`CLAUDE.md`。要公开其他指令文件，先把路径加入 `scripts/public-files-policy.json` 的 `publicFiles`，
  再运行 `node scripts/check-public-files.mjs --write-ignore`。

## 私人资料（`.local/`）

### 只有一份，在主目录

`.local/` 只放在**主检出目录**（第一次 clone 出来的目录）里，所有 worktree 都直接读写这一份，不在 worktree 里另建、不复制、不事后合并。

在任何 worktree 里定位它：

```sh
git rev-parse --path-format=absolute --git-common-dir   # 输出 <主目录>/.git，它的同级 .local/ 就是
```

下文的 `<主目录>/.local` 都指这个路径。读写前先确认它存在；不存在说明这位共建者没有私人资料，直接继续，不要创建到 worktree 里。

worktree 在主目录之外运行时，Codex 沙箱和 Claude Code 权限默认不放行这个路径。各自在本机配置里把 `<主目录>/.local`
加入可写目录（Codex 的 `writable_roots` 或 `--add-dir`，Claude Code 的 `additionalDirectories`）。

### 它是一个独立的 Git 仓库

和 CoBot 代码没有共同历史；主仓库忽略 `.local/`，所以它的内容不会进入代码提交或 PR。

| 操作 | 命令 |
| --- | --- |
| 第一次建立 | `git init <主目录>/.local`，再 `git -C <主目录>/.local remote add origin <私有仓库或自己 fork 的地址>` |
| 换电脑 | `git clone <同一地址> <主目录>/.local` |
| 存到自己 fork 的单独分支（不另建仓库时） | `git -C <主目录>/.local push origin HEAD:notes` |

远端只能是自己的私有仓库或自己的 fork，**不能是上游 `kunghim/CoBot`**。维护者的上游就是自己的仓库，笔记要另建私有仓库存放。
什么时候提交、拉取、推送笔记，见 [交付流程](deliver.md)。

### 并行会话

- 一个功能或任务一份文件，各会话只改自己任务的文件；要改别的会话正在用的文件时先问用户。
- 提交笔记时只 `git add` 本任务写过的文件。

```
.local/
  README.md      索引：只列当前有效的文档
  plans/         进行中的功能方案，一个功能一份
  discussions/   未定的讨论和待复核问题
  validation/    验收记录
  reviews/       审查记录
  ops/           本机操作手册
  archive/       归档
```

- 文件名小写英文加连字符，正文标题可以用中文。
- 持续维护的文档不带日期，原地更新，不另存 `-v2`、“修改版”之类的副本。
- 一次性记录用日期开头：`2026-10-21-dev-full-test.md`。
- 每份文档开头写元信息，便于 `rg "status: active"` 定位：

  ```yaml
  ---
  status: active        # draft | active | done | superseded | archived
  updated: 2026-10-21
  code: eb35d18         # 核对时对应的提交
  superseded-by: plans/provider-switch.md   # 有替代关系时填写
  ---
  ```

- “已确认”不等于“已实现”，“已实现”不等于“已验证”。验收记录保留当时的版本和范围，不随后续实现倒写结论。

## 归档

- 何时：方案对应的功能已合入、被新文档替代，或一次性记录已经结束。
- 怎么做：移到 `archive/<年份>/<原分类>/`，文件名和正文不改，只把 `status` 改成 `archived`；
  在 `archive/README.md` 的表里登记一行：原路径、归档日期、原因、替代文档。
- 归档不等于删除：结论文档一律保留。原始日志、JSON 执行记录、截图、大于 200KB 的 HTML 不进归档，需要留存就放 `.tmp/`。
- 需求梳理时先看 `status: active` 的文档，再按需翻归档。
