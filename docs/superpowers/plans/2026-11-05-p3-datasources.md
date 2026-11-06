# P3：方言层 + PostgreSQL + ClickHouse（实施记录）

> 本阶段按路线图 P3 实施。记录实际落地范围与延后项，供后续计划接手。

## 已完成

1. **方言占位符**：`app.Placeholder(kind, n)`（PostgreSQL `$n`，其余 `?`）；`BuildOps`/`pkWhere`/`Preview` 全部走它，写操作在 PG 上可直接用。
2. **PostgreSQL 驱动**（`driver/postgres`，`github.com/jackc/pgx/v5/stdlib` v5.12.0）
   - 连接串由 Host/Port/User/Password/Database 组装（`sslmode=disable`）
   - 执行/截断策略与现有驱动一致；`Objects`（当前 schema 的表/视图/索引）、`Columns`（含主键）、`Stream`、`Begin`
   - Caps：`CapObjects|CapColumns|CapFetchMore|CapEditableResult`（DDL 暂不声明）
3. **ClickHouse 驱动**（`driver/clickhouse`，`github.com/ClickHouse/clickhouse-go/v2` v2.50.0）
   - 只读优先：查询/对象树（`system.tables`）/列目录（`system.columns`）/流式导出；**不声明**事务与可编辑
4. **UI/校验**：连接表单支持 PostgreSQL / ClickHouse（与 MySQL 同一组字段），端口留空时按类型取默认（5432 / 9000）；`validateProfile` 支持两类。
5. 测试：占位符单测；两驱动 DSN/能力位单测 + `DB_G_PG_*` / `DB_G_CK_*` 环境变量门控的集成测试（无实例自动 skip）；连接校验测试。

## 延后（下一阶段候选）

- **多 schema 对象树**：`engine.SchemaLister` + `ObjectsIn(schema, kind)`，PG 多 schema 切换 UI；当前只取 `current_schema()` / `currentDatabase()`。
- **PG 的 DDL 查看**：需要 `pg_get_*`/`format('%I')` 拼装，暂未声明 `CapObjectDDL`。
- **ClickHouse 的 DDL/写操作**：只读。
- 补全的 `schema.table` 限定名支持（当前点号前只认表/别名）。

## 依赖（已获用户批准）

- `github.com/jackc/pgx/v5 v5.12.0`
- `github.com/ClickHouse/clickhouse-go/v2 v2.50.0`
- 备注：本机 sumdb（sum.golang.google.cn）不可达，拉依赖时用 `GOPRIVATE='*' GONOSUMDB='*'`，`go.sum` 仍记录代理返回的校验和。

## 验收

- 全量 `CGO_ENABLED=0 go test -p 1 ./...` 绿（PG/CK 集成无实例时 skip）。
- 有真实 PG/CK 时：设置 `DB_G_PG_HOST`（及 `_PORT/_USER/_PASSWORD/_DATABASE`）或 `DB_G_CK_*`，再跑
  `go test ./driver/postgres ./driver/clickhouse -v`；UI 用手动新建连接验证。

## 追加完成（task3）

- `engine.SchemaLister`（Schemas/ObjectsIn）+ `DBObject.Schema`
- PG/MySQL/CK 实现 schema 列表与按 schema 取对象；对象树先列 schema 再列分组（懒加载、错误行）
- PG 的 DDL：视图用 `pg_get_viewdef`；表用 information_schema 尽力拼 `CREATE TABLE`（列/类型/非空/默认/主键）
- CK 的 DDL：`SHOW CREATE TABLE`
- 预览/DDL 用 `"schema"."table"` 限定名；**跨 schema 查询结果只读**（写操作不越 schema）
- 测试：schema 树渲染（fake SchemaLister）、限定名校验、驱动 DSN/能力位；全量绿
