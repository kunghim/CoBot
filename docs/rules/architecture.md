# 模块边界规则

触发：改 `internal/app/**`、`frontend/src/**`，或增删这些目录下的文件。

## 后端

`internal/app` 的大部分还在一个 Go 包里，模块边界靠 `tests/architecture` 的检查守住，随 `go test ./tests/...` 运行。已搬出去的模块在它的子目录（`memories`、`preview`、`avatarservice`），只能建立在 `core` 上，彼此不引用；原方法留在 `internal/app` 做转发。事务和写锁的检查覆盖这些子目录。

- 新文件要在 `tests/architecture/modules.json` 里声明属于哪个模块，否则检查失败。
- 分层从低到高：`foundation`（底层）、`data`（只读写数据）、`business`（业务）、`orchestration`（编排）。低层不调用高层；同层之间目前不限制。
- 写消息和投递的函数只在 `internal/app/inbox.go` 调用，改成员状态的只在 `member_state.go`。收件箱只接受调用者的事务，不改成员状态，也不发事件：要不要改、要不要叫醒，由调用者决定。删除团队或成员（`Purge*`）是明确的例外，集中写在存储层。
- `internal/app` 里不手写改数据的 SQL，放进 `internal/store/sqlite`。
- 只有 `bootstrap`、`cli`、`engine/codex` 能引用 `engine/codex`；`internal/app` 不出现 Codex 的配置项名字。

### 事务

数据库只有一条连接（`store/sqlite/open.go`）。

- 事务进行中只用传进来的事务。再碰 `a.DB`，或调用任何会碰它的函数，都会自己等自己，一直卡到上下文结束，期间整个程序都拿不到数据库。
- 带 `beginWrite` 的入口不能再调用另一个带 `beginWrite` 的入口：防重复锁不可重入。确实要转交时，先清掉 `principal.Write`。
- 事件和叫醒放到提交之后。`recordEvent` 要写库。事务里攒进 `effects`，提交后调用 `a.publish`（`internal/app/effects.go`）。
- 保存失败时事务还开着，提交失败的分支后面还要碰数据库，就先 `tx.Rollback()`。`defer tx.Rollback()` 要到函数返回才执行，帮不上这个忙。
- 可能被放进别人事务里调用的函数接受 `*sql.Tx` 或 `sqlite.Queryer`。

这些检查看不到经由接口和函数值的调用，它们是护栏，不是证明。

## 前端

`frontend/dependency-cruiser.config.cjs` 定义规则，`npm run check:imports --prefix frontend` 检查：

- `components/` 不引用 `features/`。
- `components/content/` 放多个功能都要显示的内容（回复正文、链接、本地图片、命令文字）。它要打开文件或浏览器标签时，通过 `ContentActions` 问外面，由工作区回答；外面没有工作区时，文件链接提示打不开，网页链接开新标签。
- 功能目录有固定的上下顺序（见配置文件的 `featureOrder`），只能引用排在下面的，所以目录之间不会成环。新建功能目录要加进这个顺序。
- `features/` 引用 `app/` 只能引用保存共享状态的文件（配置文件的 `appState`）。

## 接口

`tests/contract` 在集成测试运行时，把经过接口层的真实请求和响应与 `api/openapi.yaml` 对照，比规范更严：对象不能带规范没写的字段。结果只在完整运行时比较，用 `-run` 缩小范围时跳过。

新测试尽量走 HTTP，这样也会被对照到。

## 现有违规的清单

三份清单只许变小，多了少了都会失败：

| 清单 | 内容 | 更新方式 |
| --- | --- | --- |
| `tests/architecture/baseline.json` | 后端现有违规；不是真问题的写 `reason` | `go test ./tests/architecture/ -args -update-baseline` |
| `frontend/import-violations.json` | 前端现有违规 | `node frontend/scripts/check-imports.mjs --update` |
| `tests/contract/known.json`、`covered.json` | 接口和规范的现有差异，以及测试已走到的接口 | `COBOT_CONTRACT_UPDATE=1 go test ./tests/integration/` |

修好一处违规就删掉对应的条目。不要为了让检查通过而往清单里加新违规。

## 不能悄悄变的东西

- `internal/collaboration/testdata/tools.golden.json` 固定了每种角色能用的工具和模型读到的说明。这是产品行为，改动要在 PR 里说明，并说明是否需要真实 Codex 验证。
- `tests/integration/behavior_*_test.go` 固定了一轮结束、删除、线程配置等的现有行为。重构时这些测试不改；要改说明行为真的变了。

做完：`go test ./tests/...`、`npm run check:imports --prefix frontend` 通过。
