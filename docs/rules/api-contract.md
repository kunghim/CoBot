# HTTP 接口规则

触发：改 `api/openapi.yaml`、`internal/api/**`。

- 先改 `api/openapi.yaml`，再运行 `./scripts/generate.ps1` 或 `./scripts/generate.sh`；规范和生成的
  `internal/contract`、`frontend/src/api/generated` 一起提交。不手改生成文件，`check` 会拒绝漂移。
- 处理器只做参数转换和调用 `internal/app`，业务逻辑不写在 HTTP 层。
- 谁能看到什么、能做什么，一律在服务端判定；不依赖前端隐藏或过滤。
- 服务只监听回环地址，非回环地址启动即拒绝；不要为了调试放宽。
- 授权链接、会话密钥、机器人令牌不写进日志、示例或测试快照。
- 改了接口的请求或响应形状，检查 PR 是否需要写“兼容性与数据影响”；外部调用方必须改用法时才算破坏性变更。

做完：`./scripts/generate.*` 后无多余差异；相关 Go 测试和 `npm run typecheck --prefix frontend` 通过。
