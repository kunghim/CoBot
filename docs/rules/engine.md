# 引擎适配规则

触发：改 `internal/engine/**`、`internal/hookrun/**`，或 Codex 协议升级。

- CoBot 只管理自己的私有 Codex 环境（数据根下的 `CODEX_HOME`）。启动引擎时不继承用户已有的 `CODEX_HOME` 或供应商密钥，
  不读写用户日常的 Codex 状态。
- 只运行 ChatGPT 桌面端内置的 Codex，不从 PATH、standalone、npm、Cargo 或 Homebrew 回退。macOS 按应用包身份发现，Windows 按已安装 Store 包元数据发现；其他平台暂不自动发现。手动路径也必须属于桌面端应用包，符号链接解析后仍须在包内。找不到时提示安装或更新桌面端，不自动安装、不改用户的 shell 或 PATH。
- Windows 桌面端仍使用 `OpenAI.Codex` 包身份，见[官方部署文档](https://learn.chatgpt.com/docs/enterprise/windows-deployment)；应用显示名称与安装包身份不能混用。
- 协议有变化时先运行 `cobot trace protocol`，对照 `internal/engine/codex/protocol_baseline.json`（已处理的方法见 `handled.go`），
  确认差异后再改适配代码；更新基线用 `cobot trace protocol --write-baseline <文件>`。
- 需要看真实流量时用 `cobot trace on|view|off --data-root <路径>`。录制未脱敏，留在数据根，不提交、不贴进 Issue/PR。
- Codex 客户端的读循环同时负责接收回应，不在读循环里同步处理需要回应的请求。
- 原生 hook 只注册在私有环境的 `PreToolUse`，匹配 `Bash|apply_patch`；只自动信任 CoBot 自己生成且内容逐字匹配的定义，
  不为通过测试放宽沙箱、审批或 hook 信任。授权被拒绝时的手动审查见 [hook 排障](../reference/hook-review.md)。
- 不限制 Codex 版本：只要 `codex --version` 能读出且 app-server 能握手就放行，版本不兼容时按实际报错处理。

做完：`go test ./internal/engine/...` 通过。涉及真实 Codex 行为时按 [验证流程](../workflows/verify.md) 单独做真实验证。
