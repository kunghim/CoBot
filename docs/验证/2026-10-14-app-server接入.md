# 2026-10-14 app-server 开发前接入验证

状态：四组接入实验已执行，部分通过；原生 shell 执行仍被 Windows 沙箱初始化错误阻塞，不能宣布编码闭环通过。

**后续更新：** 本文保留首轮失败证据。随后在同版 CLI 的测试进程优先 MXC 后，四组正常接入用例通过，详见 [Windows shell 复验](2026-10-14-Windows-shell复验.md)。旧 elevated 错误根因仍未查明，不再作为本机已验证候选配置的正常编码阻塞。

## 1. 范围与环境

- 用户同意先测基本接入、多 bot 独立运行、结构化协作工具、原生审批，不把整套崩溃恢复和实现验收作为开发前门槛。
- 平台：Windows；Node `24.21.0`；Codex CLI `0.161.0`。
- 本机生成稳定协议 schema，使用 stdio、`initialize` → `initialized`，未启用 experimentalApi。
- 有效模型 `gpt-6.1-sol`、modelProvider `custom`，沿用现有配置。`account/read` 返回账号类型 `chatgpt`；这不证明推理走官方 ChatGPT 通道，也不证明订阅计费方式已确定。
- 沙箱分别为 read-only/workspace-write，审批 reviewer 为 user。不修改全局模型、登录、配置、hook 信任、沙箱目录或 ACL。
- 模型推理是真实调用；MCP 的消息送达是明确标记的模拟回执，不冒充完整协作产品。
- 代码和复跑说明见 [experiments/app-server](../../experiments/app-server/README.md)。Node 是此次实验运行时，不据此冻结产品技术栈。

## 2. 结果与证据

| 验证内容 | 结果 | 可观察证据与边界 |
| --- | --- | --- |
| 握手、创建 thread、模型接入、流式消息 | 通过 | 两个 thread 收到 `item/agentMessage/delta`、终结事件与预期答复；不是只读取模型列表 |
| 同进程多 thread 并发 | 通过（对话与 MCP） | 两轮推理的起止时间有重叠；alpha/beta 分别返回自己的唯一标记，thread id 不同，cwd 响应正确 |
| 正常续聊 | 通过 | 关闭客户端/app-server，再启动新进程 `thread/resume`；保留 alpha 角色及上一条消息的 ALPHA_73，不带入 beta 的标记 |
| 原生读写与 shell | 未通过 | 两个 bot 的原生命令均在创建执行进程之前失败，未生成 result.txt；不是消息路由错误 |
| 实际 MCP 工具调用 | 通过（模拟业务） | 两个 thread 分别调用 send_message，各一条回执；alpha→beta / beta→alpha 和各自内容准确，无重复回执 |
| MCP caller 绑定 | 通过（受管连接路由） | 每 thread 的 stdio 启动参数绑定 caller；模型只传 target/text，后台日志 caller 与预期一致。未验证恶意进程伪造、跨团队攻击或真正投递 |
| 原生命令拒绝 | 通过 | 收到 commandExecution/requestApproval；回复 decline；item 为 declined，标记文件不存在 |
| 原生命令允许一次 | 仅审批往返通过，执行未通过 | 收到 request，等待时文件不存在，回复 accept；随后命令仍因沙箱初始化失败，未创建文件 |
| 原生文件允许一次 | 通过 | 收到 fileChange/requestApproval，等待时文件不存在；回复 accept 后 fileChange completed，隔离目标文件内容为 APPROVAL_ONCE |
| 原生文件拒绝 | 通过 | 同类型请求回复 decline，fileChange declined，目标文件不存在 |

所有审批保存 request id、thread id、turn id、item id 与决定。文件与命令拒绝的事件流均含 `serverRequest/resolved`；决定送回不等同于动作成功。审批前等待检查为半秒，未覆盖长等待、断线或双入口并发。

第一次“正常续聊”的种子值断言失败：上一轮没有成功读文件，不能要求续聊记住没读到的值。已修正验证器，将独立对话标记测试和原生文件产物测试分开。新实验明确验证正常 resume 通过，不掩盖原生命令失败。

## 3. 当前执行阻塞

普通 shell 与批准后 shell 均返回：

```text
sandbox provisioning failed: helper_sandbox_lock_failed:
lock sandbox bin dir ... .sandbox-bin failed: open directory ... .sandbox-bin
```

只读检查确认 `.sandbox-bin` 存在，有属主及访问规则；当前启动环境带有 `CODEX_WINDOWS_SANDBOX_PACKAGE_FAMILY` 标记。没有证据证明是目录缺失、CLI 缺件、ACL 错误或当前会话内嵌套运行造成；根因尚未确认。不能因此判断所有 Windows 部署都失败，也不能为通过实验关闭沙箱。

下一步先从普通 PowerShell（独立于本会话启动链）复跑原生工具及审批实验，对比同版 CLI 和有效配置。若仍失败，再单独排查沙箱初始化；安装、修复目录权限或改全局配置不包含在本次实验动作内，需要明确方案。

## 4. 本机证据索引

下列均为仓库根下忽略目录，不是必须随仓库分发的依赖：

| 实验 | 本机相对目录 | 证据 |
| --- | --- | --- |
| 最初原生工具/MCP | `.tmp/app-server-spike/run-1791946598691/` | report.json；inspect-<thread-id>.json 保存真实 commandExecution 错误与 MCP 调用 |
| 独立并发对话与 resume | `.tmp/app-server-spike/conversation-1791946875564/` | report.json，含 thread、模型/provider、回答与并发时间区间 |
| 四种原生审批 | `.tmp/app-server-spike/approval-1791946816798/` | report.json；inspect-<thread-id>.json 保存命令批准后的真实错误 |
| 协议依据 | `.tmp/app-server-spike/schema/` | 本机生成的 JSON Schema |

报告不含凭据值。本次没有保存全局配置原文，没有复制 auth.json，没有改动 multibot。测试 thread 正常由 Codex 留存；不清理已有会话。

## 5. 对首版的影响

- app-server 对话、流式事件、同进程并发和正常 resume 可以作为本机已验证能力；单进程多 thread 是可行候选，不代表所有配置、工具与故障隔离都已验证。
- stdio MCP 的 per-thread 配置与连接路由可继续用于接入设计；真正消息持久化、排队、唤醒和越权防护留到主体实现验收。
- 原生命令与文件审批都能送到客户端；文件批准/拒绝已有端到端证据。首版卡片可以围绕 request id 做准确映射，但常驻授权、网络批准、撤销、失效与多入口仍未验证。
- 当前本会话环境的 shell 执行未通过。在解决或排除该环境问题之前，不标记首版正常编码已经具备全部前提。
- S02～S06 的故障/降级、S08～S09 的恢复/事件落盘，以及消息队列、任务/worktree管理、记忆、预算、完整开发流程，不要求全部在开发前完成；随相应实现验证。

官方依据：[App Server](https://learn.chatgpt.com/docs/app-server)。当前版本实测结论以本记录为准，不将网页中的新能力自动算作本机通过。
