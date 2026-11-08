# 文档流程

适用：写方案、讨论、验收或审查记录，新建或整理公开文档，归档旧资料。

## 放哪里

| 内容 | 位置 | 提交 |
| --- | --- | --- |
| 所有人共同遵守的规则和流程 | `docs/`；入口 `AGENTS.md`，`CLAUDE.md` 只指向它 | 是 |
| 产品现状、业务规则、已做的决定 | `docs/product/` | 是 |
| 方案、讨论、验收与审查记录、个人笔记 | `.local/` | 否 |
| 构建产物、原始日志、截图、一次性脚本 | `.tmp/<任务名>/`，随时可删 | 否 |
| `cobot trace` 录制、运行数据库 | 对应数据根 | 否 |

公开文档在没有任何私人资料时也必须看得懂；不写私人提示词、本机路径、录制或个人偏好。

## 公开文档

- 中文书写；`README`、`CONTRIBUTING.md` 面向外部访客，保持中英双语。
- 文件名小写英文加连字符。
- 只写读代码得不到、或者容易做错的东西：边界、不变式、做过的决定、反复出现的坑、做完的标准。
  模块列表、文件清单、版本号这类能从代码查到的不写；需要时写“以哪个文件为准”。
- `rules/` 按改动路径触发，`workflows/` 按任务触发，`product/` 是产品现状和决定，`reference/` 很少需要。单篇尽量一屏读完。
- 新的产品决定定下来后，在 `product/decisions.md` 加一行；功能实现后同步 `product/overview.md`。
- 同一条规则只写在一处，其他地方链接。新增、移动或删除文档时，同步 `docs/README.md` 和 AGENTS.md 的路由表；`node scripts/check-docs.mjs` 会检查文件名、索引是否列全和链接是否有效。
- 智能体指令文件（`AGENTS.md`、`CLAUDE.md`、`GEMINI.md` 等）和助手配置目录默认不提交，目前只公开根目录的
  `AGENTS.md`、`CLAUDE.md`，以及 `.claude/skills/` 下名字以 `cobot-` 开头的项目级 skill（策略里的 `publicDirectories` 和 `publicSkillPrefix`）。
  你自己的个人 skill、`.claude/` 的其余内容照样不提交，名字不带 `cobot-` 的 skill 提交会被检查拒绝。
  skill 只写步骤并指向本目录的文档，规则不在 skill 里重复。要公开其他指令文件，先把路径加入 `scripts/public-files-policy.json` 的 `publicFiles`，
  再运行 `node scripts/check-public-files.mjs --write-ignore`。

## 私人资料（`.local/`）

### 只有一份，在主目录

`.local/` 只放在**主检出目录**（第一次 clone 出来的目录）里，所有 worktree 都直接读写这一份，不在 worktree 里另建、不复制、不事后合并。

在任何 worktree 里定位它：

```sh
git rev-parse --path-format=absolute --git-common-dir   # 输出 <主目录>/.git，它的同级 .local/ 就是
```

下文的 `<主目录>/.local` 都指这个路径。只需要读的时候它不存在就直接继续；需要写笔记而它还不存在时，按下一节自动建立，永远建在主目录，不建在 worktree 里。

在 worktree 里写这个路径时，你用来开发的编码工具（Codex、Claude Code 等）可能会请求批准，同意即可；
不要为了绕过批准去改沙箱。嫌每次批准麻烦，可以自行把 `<主目录>/.local` 加为工具的可写目录，这是可选的。

### 它是一个独立的 Git 仓库，只有一个分支

`.local/` 自己是一个 Git 仓库，和 CoBot 代码没有共同历史；主仓库忽略 `.local/`，所以笔记不会进入代码提交，也开不了 PR。
它只用来在多台设备之间同步笔记：**只有一个分支，不建其他分支，也不建 worktree**。CoBot 的所有分支和 worktree 都读写主目录这一份，
所以在任何分支上都能读到全部笔记。

这个分支叫什么取决于远端：

| 笔记远端 | 分支 |
| --- | --- |
| 单独的笔记仓库（可以是私有仓库；维护者用 `kunghim/CoBot-Notes`） | 该仓库的 `main` |
| 自己的 CoBot fork（共建者不另建仓库时的默认） | `notes`：fork 的 `main` 是代码，不能共用 |

**智能体第一次需要写笔记、而 `<主目录>/.local` 不存在时自动准备**：

1. 确定远端：主仓库 `origin` 指向自己的 fork 时（不是 `kunghim/CoBot`），用同一个地址，不用问用户；
   主仓库 `origin` 就是 `kunghim/CoBot`（维护者），或者用户想用单独的笔记仓库时，请用户给出地址，拿到之前不建立。
2. **远端上已经有这个分支就 clone，不要 init**（另一台设备建过笔记时就是这样）：
   笔记仓库用 `git clone <地址> <主目录>/.local`，fork 用 `git clone -b notes <地址> <主目录>/.local`。
   在已有笔记的远端旁边另 init 一份，会得到两份没有共同历史的笔记，只能事后手工合并。
3. 远端还没有这个分支（第一次用）时才 `git init <主目录>/.local`：写入下面目录结构里的 `README.md` 和 `archive/README.md`，
   提交一次，`git -C <主目录>/.local remote add origin <地址>`。远端是 fork 时**只在这一次**提醒用户：
   公开仓库的 fork 不能改成私有，`notes` 分支谁都能看到；想保密就另建一个私有仓库，给出地址后换成它。
4. 推送随代码一起（见 [交付与同步流程](deliver.md)）。第一次推送用 `git -C <主目录>/.local push -u origin HEAD:<分支>`，
   分支按上表取；之后的拉取、推送都对应这一个分支。

换电脑：按第 2 步 clone。

- 远端**永远不能是上游 `kunghim/CoBot`**；发现远端是它，停下告诉用户。
- 远端出现第二个分支，或本地分支和远端分支没有共同历史时，停下告诉用户，不另开分支，也不自动合并。
- 笔记里不写密钥和账号。

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
