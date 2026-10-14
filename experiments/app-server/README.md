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
$env:SPIKE_PREFER_MXC = '1'
node experiments/app-server/run.mjs
node experiments/app-server/approvals.mjs
```

SPIKE_PREFER_MXC 只让实验客户端为子进程传入 features.prefer_mxc=true，不改全局配置。若 pwsh 不在 PATH，可用当前进程的 SPIKE_PWSH 指定已安装的真实可执行文件。prefer_mxc 在本版为 under development；偏好开关本身不能保证不会回退到旧沙箱。

**旧实验的执行结果仅对应当时版本，不应直接作为当前 Windows 环境的启动方式。** `shell-diagnostic.mjs` 的 false 分支会触发旧 elevated 初始化，可能重配共享 Windows 账号和防火墙；不调用模型、使用私有 CODEX_HOME 都不能保证没有系统副作用。该程序默认退出，只有明确授权诊断旧沙箱时才使用 `--allow-legacy-setup`。

## Windows 恢复后的共存复验

先完成日常桌面客户端重启验收，再运行本项。结果及范围见 [恢复与共存记录](../../docs/验证/2026-10-14-Windows恢复与共存验证.md)。

```powershell
# CODEX_BIN 必须显式指向已经验证的真实可执行文件，不能使用 mise shim。
$env:CODEX_BIN = '<当前已验证的 codex.exe 真实路径>'
node experiments/app-server/windows-coexistence.mjs
```

该程序在本机桌面自带的 0.162.0-alpha.17.2 上验证。它显式设置 `windows.sandbox="mxc"`，不回退旧沙箱；运行前需要确认指定版本支持该设置。两个私有目录实例与日常目录实例并存，直接使用 `command/exec` 验证文件读写、目录外写入拒绝、正常退出和进程重启。无模型 turn、无登录调用、不读取凭据值，不创建 thread。证据保存在忽略目录内。

每个私有实例的启动 cwd 固定为自身工作目录；workspaceWrite 的默认可写范围包含服务启动 cwd，不能把实例启动在更宽的公共目录后，只靠单次请求的 cwd 来缩小边界。私有状态目录也不等于操作系统资源隔离。

## 原有接入实验的执行范围

运行真实轮次会使用现有模型账号与额度，继承当前配置的模型和提供方。测试只生成 `.tmp/app-server-spike/` 中的工作目录、标记文件和报告；Codex 自己正常持久化这些新测试 thread。不访问已有工作会话，不安装组件，不切换登录，不修改 hook 信任或沙箱 ACL。

| 程序 | 范围 |
| --- | --- |
| `discover.mjs` | 握手、非秘密账号类型、模型列表、目标 thread 的有效模型/provider/沙箱 |
| `conversations.mjs` | 两个 thread 并行推理、角色/标记路由、cwd 配置、关闭进程后的正常 resume；不用工具 |
| `run.mjs` | 并行原生读写/命令、正常 resume、两个 thread 的独立 MCP 配置及模拟回执 |
| `approvals.mjs` | 原生命令/文件审批，各一次批准与拒绝；回复前延迟半秒并检查文件不存在 |
| `inspect.mjs` | 仅读取显式测试目录关联的 thread，帮助保存失败工具项的证据 |
| `shell-diagnostic.mjs` | 默认阻止运行；旧沙箱对比可能触发系统初始化，仅保留供明确授权的诊断 |
| `windows-coexistence.mjs` | 固定真实二进制与 MXC；私有实例及日常实例命令并存、写入边界、退出和重启 |

`run.mjs` 和 `approvals.mjs` 每次创建新目录。可以用 `SPIKE_OUTPUT` 指定一个尚未使用的本地输出目录。失败返回非零退出码；不要把没有产物的 `turn/completed` 当成测试成功。

## 会话隔离实验

使用独立的 `CODEX_HOME`、`CODEX_SQLITE_HOME` 和 `sqlite_home`，分别验证开发/生产的真实对话、跨进程恢复及原生会话列表隔离，结果见[会话隔离验证](../../docs/验证/2026-10-14-会话隔离验证.md)。实验客户端支持为子进程单独传入 `env`，不修改当前进程或用户的全局环境变量。

```powershell
node experiments/app-server/home-isolation.mjs --experimental-token-reuse
```

**显式选择的受控实验，不是首版登录实现。** 本机 0.161.0 将 `chatgptAuthTokens` 标为内部、不稳定接口；本程序通过实验能力开关验证内存复用，不以它作为产品依赖。需要已有文件式 ChatGPT 登录及有效 token，会运行四个短对话 turn、消耗现有额度。凭据只在内存和本机管道中使用，不复制/链接 `auth.json`，不落入参数、报告或日志；原生客户端数据目录只用于读取账号、非秘密配置及检查，没有创建新的 thread。

隔离目录在忽略的 `.tmp/app-server-spike/home-isolation-*/` 内，不创建正式的 `~/.cobot`、`~/.cobot-dev`。官方浏览器登录只验证生成与取消入口，没有打开登录页面或完成新授权。当前测试不涵盖 token 过期/刷新、系统钥匙串或所有平台；无该显式参数时程序不执行实验。

诊断某次实验，例如：

```powershell
node experiments/app-server/inspect.mjs .tmp/app-server-spike/<本次run目录>
node experiments/app-server/inspect.mjs .tmp/app-server-spike/<本次approval目录> command-accept
```

## 实验边界

- 原接入程序的稳定字段取自本机 `0.161.0` schema；共存复验核对了当前 `0.162.0-alpha.17.2` schema。会话隔离实验单独开启 experimentalApi，见上文边界。
- 全部实验使用 `read-only` 或 `workspace-write`，审批路由为 `user`；不使用全权限或绕过沙箱参数。
- 模型只操作生成的测试文件。审批程序按预期 request/thread/cwd/目标约束决定，其他请求拒绝；批准仅限创建固定无害标记文件，不保存常驻授权规则。
- 模拟 MCP 是独立 stdio 连接；caller 来自宿主配置的启动参数，工具输入没有 sender。它只记录并返回回执，不实现真正消息送达、唤醒、团队权限或数据库。
- 默认用户配置中的 `node_repl`、`rea` MCP 在测试 thread 局部禁用；其他环境可能有额外配置，需要在实际运行前检查。继承的 hook 不在本实验中修改。
- 对话并发/cwd 返回值正确，只证明线程路由与配置，不能代替文件边界或恶意越权测试。
- 正常 resume 不证明崩溃恢复；文件审批不代表网络授权或“总是允许这个 bot”已经验证。
- 测试报告与 schema 留在忽略的 `.tmp/`；正式结论保存在 `docs/验证/`，不提交凭据或本机路径快照。
