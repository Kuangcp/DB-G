# 设计：选中 SQL 执行 + MySQL 连接支持（含对象树）

日期：2026-10-19
状态：已批准（对话确认）
前置：M1（SQLite 执行）、M2（布局/连接管理/对象树）已完成于 main

## 目标

1. **执行选中的 SQL**：执行范围严格限定为编辑器选区，禁止执行整个控制台 buffer。
2. **MySQL 数据源**：可新建/连接/执行选中 SQL/查看对象树，体验与 SQLite 一致。

## 决策记录

| 问题 | 决策 |
|---|---|
| 无选区时执行 | 禁用执行并提示「请先选中要执行的 SQL」 |
| 密码存储 | 明文 JSON（`~/.config/db-g/connections.json`，0600） |
| 功能范围 | 连接 + 执行 + 对象树 |
| MySQL 驱动 | `github.com/go-sql-driver/mysql`（纯 Go，新依赖，经批准） |
| 数据库名 | 可选字段；填则 DSN 带库名且对象树只列该库；不填对象树显示「未选择数据库」 |

## 1. 选区执行

- 选区来源：mygo 既有 API，`Element.TextSelection() (start, end int)`（rune 偏移，`start==end` 为光标）+ `TextBuffer.Slice(start, end) string`。不迁移 `ui/editor` 包，不自建选区状态。
- `root` 增 `selStart, selEnd int`；每帧在编辑器元素构建完成处（`ui/root.go` 的 TextArea 之后）回写：`a.selStart, a.selEnd = in.TextSelection()`。
- 触发点（F5 / Ctrl+Enter 位于 `rootView` 帧首、「执行」按钮在顶栏）读取的是**上一帧缓存**的选区——一帧延迟，选完再按无可感知，属既定取舍（避免 Element 跨 pass panic）。
- `(*root).runSQL()`（`ui/run.go`）改为：
  - `selStart == selEnd` → 不调用 `con.Run`、不改任何 Console 状态（提示由结果面板按态渲染，见下）；
  - 否则 `a.con.Run(a.ed.Buf.Slice(a.selStart, a.selEnd))`。
- 「执行」按钮：`Disabled(Running || Session()==nil || selStart==selEnd)`。
- 结果面板默认提示改为按态优先级：未连接→「请先连接数据源」；未运行且无选区→「请先选中要执行的 SQL」；未运行有选区→「Ctrl+Enter 执行选中 SQL」。
- 选区文本**原样**投递 `RunStatement`（多语句由 driver 处理，应用层不切分；`splitStatementRanges` 属 M3）。
- `app.Console` 零改动。

## 2. 数据模型与校验

`app.ConnProfile` 增字段（均 `json` 标签；密码 `json:"password,omitempty"`）：

```go
Host     string `json:"host,omitempty"`
Port     int    `json:"port,omitempty"`
User     string `json:"user,omitempty"`
Password string `json:"password,omitempty"`
Database string `json:"database,omitempty"`
```

校验按 Kind 分支（`Add`/`Update` 共用私有 `validateProfile`，顺手消掉 M2 遗留的重复校验）：

- `sqlite`：现状（`os.Stat(Path)` 非目录且存在；顺手加 `IsDir` 拒绝——M2 终审 Minor）。
- `mysql`：Name 非空、Host 非空、Port ∈ [1,65535]；不要求 Path。
- 未知 Kind：拒绝（`engine.Register` 只有 sqlite/mysql 两个）。

旧数据兼容：存量 JSON 无新字段 → `Port==0`、其余空串，加载不受影响（字段全部 optional）。

`engine.Profile` 增对应字段 `Host/Password/User/Database string`、`Port int`（与 ConnProfile 同名同义）；`ui/openConn` 从 ConnProfile **逐字段搬运**，DSN 字符串只由 driver 拼装（ui 不拼 DSN）。`Kind/Name/DSN` 语义保留（sqlite 仍走 `DSN: p.Path`）。

## 3. driver/mysql

- 新包 `driver/mysql`，`init()` 里 `engine.Register("mysql", openSession)`；`main.go` blank-import。
- DSN：用 `mysql.NewConfig()` + `FormatDSN`（手拼易错）：
  - `User/Passwd/Net="tcp"/Addr="host:port"`；
  - `DBName = p.Database`（可空）；
  - `Params`：`multiStatements=true`、`parseTime=true`、`charset=utf8mb4`、`loc=Local`。
  - `multiStatements=true` 是选区多语句可直接执行的前提。
- `Open`：`sql.Open` + `PingContext`（5s 超时）；驱动名注册 `import _ "github.com/go-sql-driver/mysql"`。
- `RunStatement`：镜像 sqlite 模式——`isQueryLike`（首 token ∈ SELECT/WITH/EXPLAIN/SHOW/VALUES/DESCRIBE/DESC）走 Query，其余走 Exec；预算同值：`rowLimit=500`、`maxCellBytes=1MB`、`maxResultByte=16MB`；列类型/单元格格式化对齐 sqlite 现状（NULL/数字/文本/字节）。
- `Caps()` = `engine.CapObjects`；实现 `engine.ObjectLister`：
  - 表：`information_schema.TABLES` 中 `TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE())` 且 `TABLE_TYPE='BASE TABLE'`；
  - 视图：同上 `TABLE_TYPE='VIEW'`；
  - 索引：`information_schema.STATISTICS` 同 schema 限定，`GROUP BY INDEX_NAME` 去重；
  - 均按 `TABLE_NAME/INDEX_NAME` 排序；`ctx` 传递 + 5s 由调用方（objecttree 已有）约束。
- 未选择 Database 且对象树请求时：返回业务错误「未选择数据库」（红字路径已有）。
- `Close/Cancel` 语义同 sqlite。

## 4. 连接表单 UI

- `connModal` 加类型二选一（SQLite / MySQL），切换时互斥显示字段组；`saveConnForm` 按类型填 `Kind` 与对应字段。
- MySQL 字段：名称、数据库（可选）、Host、Port（placeholder 提示 3306，存用户输入，校验必填 1–65535）、用户、密码（先查 mygo 是否有掩码 TextInput API，有则用，无则明文，实现期定）。
- 「浏览…」按钮仅 sqlite 显示。
- 编辑回填全部新字段；`root` 表单状态字段扩展（`connFormKind/Host/Port/User/Pass/DB`）。
- 打开/删除/重命名/状态点/错误生命周期复用 M2 的 `activeConnID`/`failedConnID` 机制，零改动。

## 5. 对象树

零改动。`ObjectLister` + `CapObjects` 由 mysql driver 实现后，`objectGroups`/懒加载/缓存/失败红字/刷新/展开自动连接全部自动生效。

## 6. 测试

- `app`：mysql 校验分支（缺 Host/非法 Port/未知 Kind 拒绝）、含新字段 round-trip、sqlite `IsDir` 拒绝、旧 JSON 兼容加载。
- `driver/mysql`：
  - 纯单测：DSN 拼装（`FormatDSN` 输出含 multiStatements/库名）、`isQueryLike`、Objects SQL 文本与参数（不连库）；
  - 集成测试：环境变量 `DB_G_MYSQL_TEST`（或 DSN）存在才跑，否则 `t.Skip`——CI 无 MySQL 不红。
- `ui`：有选区→执行选区文本（真实 sqlite 种子 + 选中特定 SELECT）；无选区→按钮禁用 + 提示文案 + 快捷键不触发执行；选区执行结果正确回投。选区读取用 `SetTextSelection` 注入。
- 门禁回归：全套测试、`mygo vet`、spike/网格性能守门照跑。

## 7. 明确不做

连接搜索/右键菜单/密码加密或 keyring/Postgres/编辑器多标签/语句切分（M3+）/连接测试按钮/SSH 隧道。

## 8. 风险

- **多语句 + Query 路径**：`isQueryLike` 只看首 token；选区以 SELECT 开头但含后续语句时，mysql 驱动 Query 会执行全部并返回**最后一个**结果集（go-sql-driver 行为）。与「原样投递」决策一致，结果可能非首条——接受，M3 语句切分后自然消除。sqlite 现状同样存在（modernc 行为类似），不属本设计回归。
- **密码明文**：已知取舍（决策记录），0600 兜底。
- **新依赖供应链**：go-sql-driver/mysql 为事实标准，锁定版本进 go.sum。
