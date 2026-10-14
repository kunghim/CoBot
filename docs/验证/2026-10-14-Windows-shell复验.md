# 2026-10-14 Windows shell 复验

结论：当前会话可以执行 shell；测试 app-server 的旧 elevated 路径失败，但本机 `0.161.0` 在测试进程优先 MXC 时，原生读写、命令执行及审批闭环均通过。四组正常接入实验已有本机通过证据，不再把先前 shell 错误当成 Codex 整体不可执行。

## 1. 对比方法

用户提醒当前对话本身运行在 Codex，要求同时验证当前会话的执行能力。先用当前会话的 shell 执行 WriteAllText/ReadAllText，确认标记内容一致。

再使用 app-server 的 `command/exec` 直接执行相同类型的无害操作，不启动模型 turn，排除提示词、工具选择与提供方影响。两次均使用相同的 workspaceWrite 策略、相同 cwd/可写测试根、networkAccess=false，区别只在测试进程的 `features.prefer_mxc`。

| 路径 | 结果 |
| --- | --- |
| 当前 Codex 会话 shell | 写入、读取成功 |
| app-server，prefer_mxc=false，保留现有 windows.sandbox=elevated | 初始化失败，仍为 helper_sandbox_lock_failed / .sandbox-bin open directory |
| app-server，prefer_mxc=true，保留相同权限策略 | exitCode=0，创建标记并返回 SHELL_CHECK |
| 成功配置下尝试写入测试根外的隔离兄弟目录 | 返回 WRITE_BLOCKED，目标文件不存在 |

这不是关闭沙箱的对比；测试根外写入被拒绝。networkAccess=false 是实际提交的策略，本轮未做外部网络阻断矩阵，不能扩大为全部网络场景已验证。

平台为 Windows 11 专业版，版本 10.0.26200；CLI 0.161.0，Node 24.21.0。CLI feature list 将 prefer_mxc 标为 under development。官方说明桌面端与独立 CLI 的 MXC 默认选择可能不同；这里只证明本机临时配置的行为差异，不把新文档中的所有能力视为本版已支持。[官方 Windows 沙箱](https://learn.chatgpt.com/docs/windows/windows-sandbox)

## 2. 接入实验复跑

测试子进程临时传入 `-c features.prefer_mxc=true`，没有改全局配置。重新执行原接入与四种审批用例：

| 用例 | 结果及实物断言 |
| --- | --- |
| 两 bot 原生编码 | 各读取独立 seed.txt、写入 result.txt；分别为 ALPHA_73 / BETA_91；原生命令 item completed |
| 正常 resume | 关闭进程后恢复原 thread，保留 alpha 及已读取的 ALPHA_73 |
| MCP 实际调用与身份路由 | 两个受管 stdio 连接分别返回 alpha→beta、beta→alpha，内容准确，各一条；业务送达仍为模拟 |
| 命令允许一次 | 批准前/等待期间标记不存在；accept 后文件内容为 APPROVAL_ONCE |
| 命令拒绝 | decline 后文件不存在、item declined |
| 文件允许一次 | accept 后隔离目标文件内容正确、item completed |
| 文件拒绝 | decline 后目标文件不存在、item declined |

本轮测试使用此前相同模型 gpt-6.1-sol / custom 和现有登录。没有安装、更新 CLI、复制凭据、改 ACL、改 hook 信任或修改 multibot。

## 3. 可复跑证据

程序见 [实验说明](../../experiments/app-server/README.md)，补充 `shell-diagnostic.mjs` 直接命令对比；原有程序可通过 SPIKE_PREFER_MXC=1 选择仅对测试子进程生效的配置。

| 本机相对目录 | 内容 |
| --- | --- |
| `.tmp/app-server-spike/self-shell-comparison/` | 当前会话写入的标记 |
| `.tmp/app-server-spike/shell-diagnostic-1791948514429/` | configured 失败、prefer-mxc 成功，以及测试根外写入拒绝的 report.json |
| `.tmp/app-server-spike/run-1791948519863/` | 三个用例均通过的 report.json、原生工具结果、thread id 与生成文件 |
| `.tmp/app-server-spike/approval-1791948530281/` | 四种审批结果、原请求关联与工具状态、标记文件 |

本机报告不提交，结论和可复跑代码提交。旧失败证据仍保留在 [首轮记录](2026-10-14-app-server接入.md)。

## 4. 实施边界

- 本机正常接入的四组前提已通过，可以推进最小任务闭环；不再要求先修好旧 elevated 路径才能开发。
- 若采用当前已验证的 Windows 配置，应在程序启动时做能力检查，并记录准确 CLI 版本；prefer_mxc 是当前版本的实验特性，不作为所有 Windows 机器或未来版本的无条件保证。
- 本次仍在当前会话启动链内执行，没有从普通 PowerShell 独立启动进行对比。旧 elevated 失败是否由嵌套环境、锁定方式或权限引起，尚未查明；没有自动修复它。
- 没有冻结产品必须使用 MXC、单进程、stdio MCP 或 Node；它们是本机通过实验的候选组合。
- 未覆盖登录刷新、恶意越权、网络授权、常驻授权、断线、崩溃恢复、数据库队列、真实 bot 唤醒、worktree 生命周期或完整开发验收。
