# 数据库迁移规则

触发：改 `internal/store/sqlite/**`。

- 只追加新的 goose 迁移（`internal/store/sqlite/migrations/`，编号递增）；已发布的迁移一个字都不改。
- 新增迁移时同步调高 `internal/store/sqlite/migrate.go` 的 `MaxSupportedVersion`：它决定本程序能打开的最高版本，
  比它新的数据库会被拒绝，防止旧程序静默降级新数据。
- SQL 事务里不调用引擎、不等待模型；先提交事务，再做外部操作。
- 迁移会删除或改写已有数据时，在 PR 的“兼容性与数据影响”写清楚，并核对 [升级与回滚](../reference/upgrading.md) 是否要补充。
- 迁移不改写成员的可见身份（`bots.role_id`、成员名字）；角色调整按 [接力与协作工具规则](relay.md) 新增并下架角色，已有成员原样保留。
- 测试迁移用临时数据根，不拿真实用户库试。需要旧数据时复制一份到 `.tmp/test-runs/`，源库只读。

做完：`go test ./internal/store/... ./tests/integration/...` 通过；用一份旧版本数据根打开一次，确认自动迁移成功。
