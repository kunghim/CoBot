# 原生 hook 手动审查

适用：设置页显示 hook 自动授权被拒绝。规则见 [引擎适配规则](../rules/engine.md)。

CoBot 只在私有 Codex 环境中注册 `PreToolUse`（匹配 `Bash|apply_patch`）。覆盖范围取决于原生执行路径是否实际调用 hook；
它不能撤销已运行的命令，也不提供完整隔离。

启动时 CoBot 经 app-server 的 `hooks/list` 和 `config/batchWrite` 授权自己生成的 hook，写入与 `/hooks` 相同的 `trusted_hash`。
只有 CoBot 拥有、且内容逐字匹配的定义会被自动信任；项目 hook、受管 hook 和修改过的文件不会。设置页展示状态并提供重试。

## 手动审查

先结束运行中的工作，再在 CoBot 私有环境中启动 Codex，核对命令指向预期的辅助程序和数据根。
以下 PowerShell 示例结束后会恢复当前 shell 的环境变量，请替换两个路径：

```powershell
$cobotRoot = 'C:\your\cobot-data-root'
$teamDirectory = 'C:\your\team-directory'
$previousCodexHome = $env:CODEX_HOME
$previousCodexSQLiteHome = $env:CODEX_SQLITE_HOME
try {
    $env:CODEX_HOME = Join-Path $cobotRoot 'engines/codex/home'
    $env:CODEX_SQLITE_HOME = Join-Path $cobotRoot 'engines/codex/sqlite'
    $hooks = Get-Content (Join-Path $env:CODEX_HOME 'hooks.json') -Raw | ConvertFrom-Json
    $command = $hooks.hooks.PreToolUse[0].hooks[0].command
    if ($command -match '-EncodedCommand (\S+)$') {
        [Text.Encoding]::Unicode.GetString([Convert]::FromBase64String($Matches[1]))
    } else { $command }
    codex --no-daemon --no-alt-screen --cd $teamDirectory
    # In Codex, type /hooks, review the definition, then exit.
} finally {
    $env:CODEX_HOME = $previousCodexHome
    $env:CODEX_SQLITE_HOME = $previousCodexSQLiteHome
}
```

macOS 可在子 shell 中设置同样两个变量，再运行 `codex --no-daemon --cd "$teamDirectory"`。
`--no-daemon` 用于避开后台服务的 socket 路径长度限制，不绕过沙箱或信任。
这个手动会话只用于审查配置：没有 CoBot 宿主绑定的原生工具轮次会被守卫拒绝。不要使用绕过信任的参数。
