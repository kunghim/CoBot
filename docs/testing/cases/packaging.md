# 打包产物与桌面壳

候选版本在合并 `main`、触发 `release draft` 之前要先在本机打包并验收。打包命令和版本号约定见 [操作指南](../guide.md#2-构建候选版本)。
最终发布的文件由 `release draft` 工作流生成，是否触发、是否公开发布由用户决定。

| ID | 层 | 步骤 | 期望 |
| --- | --- | --- | --- |
| F1 | 回归 | `./scripts/package-windows.ps1 -Version <候选版本> -MsiVersion <候选 MSI 版本>`，产物在 `.tmp/release-assets` | 脚本自带的检查全部通过；MSI 和 ZIP 载荷一致；ZIP 里有 `cobot-desktop.exe`、`cobot-helper.exe`、`cobot.exe`、`LICENSE`、`THIRD-PARTY-NOTICES.txt` |
| F2 | 回归 | ZIP 解压到新目录，用里面的 `cobot.exe serve` 在新数据根上启动；再用 `$R/real` 的一份副本启动并跑通一个三环小需求 | 内嵌前端可用；能找到 Codex；头像库下载成功（AV-12） |
| F4 | 回归 | `scripts/test-msi.ps1 -Path <msi> -Version <v> -MsiVersion <m>`（只读检查，不安装） | 全部检查通过；MSI 载荷与 ZIP 里的三个可执行文件逐字节相同 |
| F5 | 回归 | **桌面壳原生窗口**：用 ZIP 或安装出来的 `cobot-desktop.exe`，先用空数据根，再用真实已登录 profile 的副本 | 窗口标题 `CoBot`、界面完整渲染（自绘标题栏、团队栏、引导页）；核心版本和 profile 正确；已有团队正确列出；关窗退出码 0，无残留进程。原生窗口里的控件由 WebView2 渲染，写操作统一在核心的 HTTP 界面上做 |
| F6 | 回归 | **MSI 安装**：静默安装 `msiexec /i <msi> /qn /l*v <日志>`；再装一次；卸载 `msiexec /x /qn` | 无需提权，装到 `%LOCALAPPDATA%\Programs\CoBot`，恰好 5 个文件，开始菜单有快捷方式；同版本再装走维护模式；卸载后安装目录、快捷方式、注册项和进程全部清掉；全程不碰 `~/.cobot` |
| F7 | 回归 | **装上真用**：安装候选 MSI 后从安装目录启动，在真实 profile 上走一遍设置页（[设置](settings.md) SE-1），测完还原 | 零控制台错误；改过的设置全部还原 |
| F8 | 回归 | macOS DMG | 本机是 Windows 时无法测，在记录里写「未测」并沿用维护者此前的测试 |
| F9 | 回归 | **桌面壳可写**：在桌面壳窗口里（页面在另一个来源）改一个设置、给角色换头像、切换一个供应商的开关、给成员换模型 | 全部写成功，没有 CSRF 拒绝；回读接口确认落库。写操作在核心的 HTTP 界面上做，原生窗口只看结果 |
| F10 | 回归 | **单实例**：桌面壳已打开时再启动一次 | 第二次启动交给已打开的窗口（窗口被激活），不会起第二个核心；退出码和日志没有错误 |
| F11 | 回归 | 运行 `node --test scripts/frontend-bundle.test.mjs`，再按目标平台打包并核对随包声明 | 主页面、Worker、独立渲染器、CSS 和复制资源的实际依赖有声明；未进入产物的安装依赖不误阻塞；构建记录缺失、过期、产物变化、已用包缺声明或二进制不匹配时失败 |
| G1 | 冒烟 | README 中英文：状态表、安装步骤、最低 Codex 版本（Windows 与 macOS）与本次实测一致 | 一致；不一致的记下来，由用户决定是否改 |
| G2 | 冒烟 | 起草发布说明（写进测试记录，不直接改发布草稿） | 已知限制至少核对：Windows 未签名、macOS 只有临时签名；需要另装 Codex CLI；API 供应商没有联网搜索；从旧版升级的数据影响；头像库需要访问 GitHub；没有自动更新 |

已知观察（P3，未处理）：按用户安装的 MSI 把「已安装的应用」条目注册到了 HKLM（包里没有 `ALLUSERS` 属性）；`scripts/test-msi.ps1` 的「按用户安装」断言只看属性，看不出条目实际落在哪个 hive。再次测到时只记录是否仍然如此。
