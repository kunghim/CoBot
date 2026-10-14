# app-server 开发前接入验证

这是协议实验程序，不是 CoBot 产品、开发框架选型或完整多 bot 调度器。使用 Node 内置模块，无第三方包，无安装步骤。

目标是验证四件事：基本接入、多 bot 独立运行、MCP 结构化调用、原生审批。首轮结果见 [接入记录](../../docs/验证/2026-10-14-app-server接入.md)，当前通过范围见 [Windows shell 复验](../../docs/验证/2026-10-14-Windows-shell复验.md)。

## 执行

在仓库根使用本机已安装的 Node 与 Codex。`codex` 默认从 PATH 定位；需要指定真实可执行文件时设置当前进程的 `CODEX_BIN`。不要更改全局配置或复制凭据。

```powershell
codex --version
node --version
codex app-server generate-json-schema --out .tmp/app-server-spike/schema
node experiments/app-server/discover.mjs
node experiments/app-server/conversations.mjs
node experiments/app-server/run.mjs
node experiments/app-server/approvals.mjs
```

本机 0.161.0 的旧 elevated 路径在当前会话启动链内失败。使用已验证的临时 MXC 优先配置复跑：

```powershell
node experiments/app-server/shell-diagnostic.mjs
$env:SPIKE_PREFER_MXC = '1'
node experiments/app-server/run.mjs
node experiments/app-server/approvals.mjs
```

SPIKE_PREFER_MXC 只让实验客户端为子进程传入 features.prefer_mxc=true，不改全局配置。shell-diagnostic 明确覆盖该环境开关，分别测试 false/true；它不启动模型轮次，并检查测试根外写入被拒绝。若 pwsh 不在 PATH，可用当前进程的 SPIKE_PWSH 指定已安装的真实可执行文件。prefer_mxc 在本版为 under development；不能静默回退成全权限模式。

运行真实轮次会使用现有模型账号与额度，继承当前配置的模型和提供方。测试只生成 `.tmp/app-server-spike/` 中的工作目录、标记文件和报告；Codex 自己正常持久化这些新测试 thread。不访问已有工作会话，不安装组件，不切换登录，不修改 hook 信任或沙箱 ACL。

| 程序 | 范围 |
| --- | --- |
| `discover.mjs` | 握手、非秘密账号类型、模型列表、目标 thread 的有效模型/provider/沙箱 |
| `conversations.mjs` | 两个 thread 并行推理、角色/标记路由、cwd 配置、关闭进程后的正常 resume；不用工具 |
| `run.mjs` | 并行原生读写/命令、正常 resume、两个 thread 的独立 MCP 配置及模拟回执 |
| `approvals.mjs` | 原生命令/文件审批，各一次批准与拒绝；回复前延迟半秒并检查文件不存在 |
| `inspect.mjs` | 仅读取显式测试目录关联的 thread，帮助保存失败工具项的证据 |
| `shell-diagnostic.mjs` | 不调用模型，直接对比旧配置与临时 MXC 优先配置、检查目录外写入拒绝 |

`run.mjs` 和 `approvals.mjs` 每次创建新目录。可以用 `SPIKE_OUTPUT` 指定一个尚未使用的本地输出目录。失败返回非零退出码；不要把没有产物的 `turn/completed` 当成测试成功。

诊断某次实验，例如：

```powershell
node experiments/app-server/inspect.mjs .tmp/app-server-spike/<本次run目录>
node experiments/app-server/inspect.mjs .tmp/app-server-spike/<本次approval目录> command-accept
```

## 实验边界

- 稳定协议字段取自本机 `0.161.0` 生成的 JSON Schema；未开启 experimentalApi。
- 全部实验使用 `read-only` 或 `workspace-write`，审批路由为 `user`；不使用全权限或绕过沙箱参数。
- 模型只操作生成的测试文件。审批程序按预期 request/thread/cwd/目标约束决定，其他请求拒绝；批准仅限创建固定无害标记文件，不保存常驻授权规则。
- 模拟 MCP 是独立 stdio 连接；caller 来自宿主配置的启动参数，工具输入没有 sender。它只记录并返回回执，不实现真正消息送达、唤醒、团队权限或数据库。
- 默认用户配置中的 `node_repl`、`rea` MCP 在测试 thread 局部禁用；其他环境可能有额外配置，需要在实际运行前检查。继承的 hook 不在本实验中修改。
- 对话并发/cwd 返回值正确，只证明线程路由与配置，不能代替文件边界或恶意越权测试。
- 正常 resume 不证明崩溃恢复；文件审批不代表网络授权或“总是允许这个 bot”已经验证。
- 测试报告与 schema 留在忽略的 `.tmp/`；正式结论保存在 `docs/验证/`，不提交凭据或本机路径快照。
