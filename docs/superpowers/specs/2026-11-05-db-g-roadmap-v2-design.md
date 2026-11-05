# db-g 路线图 v2（对标 db-k，按使用频次排序）设计

日期：2026-11-05
状态：待批准（阶段顺序与范围已由用户拍板；本文档过审后按阶段出 writing-plans 计划执行）

## 1. 背景与定位

db-g 是 Go + mygo 的数据库客户端，对标 db-k（Kotlin + Compose Desktop）。当前已完成：
编辑器换成 built-in `ui/editor`（A1）、控制台与工作区（可多控制台、标签/工作区、正文与光标持久化）、
连接管理与对象树、MySQL/SQLite 执行、**语句作用域补全**（关键字/函数/类型 + 表/视图 + 按 FROM/JOIN/CTE
/子查询的表补列，A2–A4）。

本路线图决定**接下来做什么、按什么顺序**。排序原则：**每天都用的能力优先**，工程化/安全收尾后置，
数据源广度最后；不做与当前定位无关的大件（Redis/ES、GUI 表设计器、打包分发）。

## 2. 已确认的决策（用户拍板）

| 决策点 | 结论 |
|---|---|
| 阶段顺序 | **1 日常查询闭环 → 2 写操作 → 3 数据源扩张**；密码加密最后 |
| 数据源范围 | **MySQL / SQLite / PostgreSQL / ClickHouse**；**不做 Redis / Elasticsearch** |
| 打包分发 | **不做**（`go build` 够用）；不做 AppImage/Deb/安装器 |
| 密码加密 | **最后做**（放在收尾阶段，等交互/数据源稳定） |
| 日志 | **提前做**（P0）：关键行为可排查，后续所有阶段都受益 |

## 3. 非目标（本路线图内明确不做）

- Redis、Elasticsearch 或其它非 JDBC/SQL 协议。
- GUI 表设计器、ER 图、可视化执行计划（`EXPLAIN` 只看原始结果表）。
- SSH 隧道、云同步、多窗口。
- 打包/安装器/自动更新（`go build` 即可）。
- i18n（界面保持中文）。
- 手动事务编辑器（`BEGIN/COMMIT` 手敲）：写操作沿用「提交时内部单事务」。
- 快捷键自定义、主题/字体设置（除非后面证明是痛点，排在密码加密同一档）。

## 4. 阶段划分

### P0：可观测性（日志/诊断）—— 提前

**目标**：出问题能自己查，不靠猜。

内容：
1. `app/log.go`：轻量结构化日志（无第三方依赖）。级别 `debug/info/warn/error`，输出一行一个 JSON
   （`ts, level, tag, msg, kv…`），写 `<configDir>/logs/app-YYYYMMDD.log`，权限 `0600`，按天新建、
   单文件超 5MB 滚动（`app-YYYYMMDD.1.log`）。
2. 记录的关键行为（tag 化）：
   - `startup`：版本、配置路径、加载连接数/控制台数、加载警告
   - `conn`：open/close/failed（**不含密码**）、dsn 脱敏
   - `exec`：连接名、耗时、行数/影响行数、是否截断、**完整 SQL**（本地文件，便于复现）、错误文本
   - `console`：create/close/rename/delete、正文落盘失败、光标落盘失败
   - `catalog`：补全目录/列加载失败与耗时（不含结果内容）
   - `export`/`import`（P1 起）：文件名、行数、耗时、错误
   - `panic`：`rootView` 每帧用 `defer recover()` 兜住 UI 构建 panic（记堆栈 + 在顶栏显示错误行，
     尽量让应用还能继续用；recover 不保证状态一致，只作为最后的排查手段）
3. 设置窗口：显示日志目录路径与「打开日志目录」按钮（`mygo.Shell.ShowItemInFolder`）、
   当前日志文件名、最近一条错误（点击可复制）。
4. 日志同时写 stderr（`go run` 时直接可见）。

验收：单测（级别过滤/滚动/脱敏：密码与 `password=` 字段不得出现；写入器与时钟可注入，测试用临时目录）；
手动：连续执行失败查询后日志文件里能复原「哪条 SQL、什么错、多久」。

风险：日志里写完整 SQL 可能含敏感数据 —— 文件本地 `0600`，并在设置窗口注明；后续可加开关。

### P1：日常查询闭环（主线 1）

**目标**：不出编辑器就能完成「查 → 看 → 拿出去」的完整动作。

1. **结果区交互**
   - 单元格/整行选中，`Ctrl+C` 复制（制表符分隔）；右键菜单：复制单元格/复制本行/复制本行→INSERT 语句
   - 长文本查看器（双击单元格弹窗，保留换行；JSON 自动美化）
   - 客户端**排序**（点列头，多列排序）与**每列筛选**（不重跑 SQL）；「快速过滤」输入框
   - **取更多**：`CapFetchMore` + 方言分页（MySQL/SQLite 先做），在已加载结果后追加；显示「已加载 N 行」。
     实现：把当前 SQL 包成派生表再 `LIMIT ? OFFSET ?`（`SELECT * FROM (<原 SQL>) AS dbg LIMIT … OFFSET …`）；
     只有单条 SELECT 可包裹，否则按钮禁用并提示。
   - **复制本行 → INSERT**：按数据源方言引用标识符与转义值（MySQL 反引号 + `\'`，SQLite 双引号）；
     值里的换行/NUL 走参数化风格转义，宁可不复制也不生成坏 SQL。
   - **导出**：当前结果导出 CSV / JSON / SQL INSERT；「全量导出」走新增可选接口
     `engine.RowStreamer`（游标流式写文件，不占内存），驱动不支持时退回「导出已加载结果」并提示。
     全量导出会**重新执行查询**（数据可能与当前看到的略有差异），导出前在 UI 说明一次。
2. **多语句执行**
   - `app/sqlsplit.go`：按 `;` 切语句（字符串/注释安全，复用 `sqlscope` 的词法思路），返回字节范围
   - 执行规则：**有选区 → 执行选区**（可含多条）；**无选区 → 执行光标所在的单条语句**
     （不是整个控制台 —— 保持现有「禁止一把跑整篇」的约定）。逐条执行，结果区按语句分 Tab
     （成功/失败/跳过 + 耗时 + 行数），gutter 标出语句起始行与状态；任一失败即停在该语句
3. **SQL 历史**
   - `~/.config/db-g/history.jsonl` 追加（`ts/connID/connName/sql/duration/ok/rows/err`），
     启动加载最近 2000 条，超 5MB 轮转
   - 侧栏或顶栏「历史」面板：列表（时间/连接/首行摘要/耗时/成功），双击弹窗看全文，
     「插入到光标处」
4. **对象树增强（小项）**
   - 双击表/视图 → 预览数据（`SELECT * … LIMIT 200`，新控制台或结果 Tab）
   - `Ctrl+Q`（或右键）→ 查看 DDL：engine 新增可选接口 `engine.ObjectDDL`（sqlite `sqlite_master`、
     mysql `SHOW CREATE TABLE`）
5. **SQL 格式化（本阶段最后一项，可裁剪）**
   - 轻量格式化：关键字大写、主语句换行缩进、保留原有语义 token 顺序；不做复杂重排
   - 失败/看不懂的结构保持原样（保守优先）

验收：单测（拆分器边界、分页 SQL 生成、导出转义）；UI headless（复制内容、排序/筛选、多语句 Tab）;
手动：一个 SELECT → 排序筛选 → 导出 CSV → 用 Excel 打开；一条多语句脚本逐 Tab 看结果。

依赖：`engine` 增加 `CapFetchMore` 实现、`RowStreamer`、`ObjectDDL`（均为可选接口，不破坏现有 Session）。

### P2：写操作（主线 2）

**目标**：能安全地改数据；宁可拒绝，不可写错。

1. 结果**可编辑判定**：结果列能映射到基表 + 主键（sqlite 用 `PRAGMA table_info` 已有列目录，
   mysql 用 `information_schema` 已实现；无主键/视图/聚合列 → 只读）
2. 单元格编辑（双击进编辑）→ 本地 overlay 暂存改动（显示脏标记）
3. 提交前弹窗：逐行参数化 `UPDATE ... WHERE pk = ?` 预览；确认后**单事务**提交；
   影响行数 ≠ 1 整体回滚并报告（对齐 db-k）
4. 增行/删行（主键定位）；「刷新」（重跑当前结果）、「撤销」本地改动
5. 能力门控：`CapEditableResult`（engine 已有位值），驱动未声明则结果区只读

验收：集成测试（sqlite 真库：改值/增/删/回滚/影响行数异常）；mysql 无容器时 skip；
手动：改三行提交成功，故意制造主键冲突验证回滚。

依赖：P1 的结果区交互（选中/单元格坐标）与列目录（A3 已有）。

### P3：数据源扩张（主线 3）

**目标**：MySQL/SQLite 之外补齐 PostgreSQL 与 ClickHouse；多源共用同一套 UI。

1. **方言层** `engine/dialect.go`：`QuoteIdent`、`LimitOffset`、`Placeholder`（`?` vs `$n`）、
   `ObjectsSQL` 差异；按 `Profile.Kind` 注册
2. **PostgreSQL**：`driver/postgres`（`github.com/jackc/pgx/v5/stdlib`）；对象树含 schema 层（多 schema），
   列目录、DDL、分页、写操作全链路打通
3. **ClickHouse**：`driver/clickhouse`（`github.com/ClickHouse/clickhouse-go/v2`）；只读优先：
   执行、对象树（库/表）、列目录、补全；写操作按能力位关闭
4. 对象树补 schema 分组与展开持久化（PG 必需，MySQL 受益）。
   `ObjectLister` 不扩展参数（避免破坏现有驱动）；新增可选 `engine.SchemaLister` +
   `ObjectsIn(ctx, schema, kind)`，PG 实现，MySQL/SQLite 走原接口（单 schema 视图）。
5. 补全目录支持 schema/库前缀（`db.table`、`schema.table`），仍是词法近似

验收：方言单测；PG 集成（容器或本地实例，无则 skip）；CK 只读集成；对象树多 schema 懒加载测试。

风险/需批准：**新增两个驱动依赖**（`github.com/jackc/pgx/v5`、`github.com/ClickHouse/clickhouse-go/v2`）。
Go 标准库没有 PG/CK 驱动，任何实现都需要第三方；若拒绝，P3 退化为「只做方言层 + 对象树 schema，
不接新数据源」。届时按批准确认版本。

### P4：安全与收尾

1. 密码 AES-256-GCM 本机加密（密钥文件 `<configDir>/secret.key`，`0600`；
   `LoadStore` 读到明文条目时自动加密回写一次并记日志），DSN 脱敏（也影响 P0 日志）
2. 连接档案 JSON 导出/导入（默认不含密码；含密码需二次确认；导入幂等合并）
3. （可选，视痛点）深浅主题、编辑器字号、快捷键自定义
4. 启动健康检查（连接可用性探测，徽标降级）

验收：加解密往返/迁移/权限测试；导入导出往返；日志中无明文密码。

## 5. 跨阶段基础设施决策（现在定，后面少返工）

| 主题 | 决策 | 理由 |
|---|---|---|
| 日志 | 一天一文件 JSON Lines，`0600`，stderr 同步 | 无依赖、可 grep、可轮转；JSON 便于后续做诊断面板 |
| SQL 历史 | `history.jsonl`（不是 app.db） | 追加写、无迁移；最近 2000 条足够；避免为历史引入 SQLite 元数据层 |
| 分页/导出 | 可选接口 `Session.Paginate` / `RowStreamer` | 不破坏现有 Session；驱动不支持时 UI 降级并提示 |
| 方言 | `Profile.Kind` → 方言注册表（P3 引入） | MySQL/SQLite 现在差异小，P3 再抽象，避免过早设计 |
| 语句拆分 | `app/sqlsplit.go` 纯函数，返回字节范围 | 与补全的 `ui/sqlscope.go` 共用词法思路但职责分离（一个切语句，一个取作用域） |
| SQL 格式化 | 自写保守格式化（不改语义），失败不动 | 引 parser 依赖代价高；保守实现可接受 |
| 结果上限 | 现有 500 行/16MB 保留为**首次加载**上限；取更多按页追加 | 保护内存；全量导出走流式 |

## 6. 阶段与验收总表

| 阶段 | 主题 | 关键交付 | 验收 |
|---|---|---|---|
| **P0** | 可观测性 | `app/log.go`、设置窗口日志入口、panic recover | 日志单测 + 手动复盘一次失败查询 |
| **P1** | 日常查询闭环 | 结果区交互/排序筛选/取更多/导出、多语句 Tab、SQL 历史、DDL、格式化 | 拆分/分页/导出单测；headless UI；手动「查→筛→导出」 |
| **P2** | 写操作 | 可编辑判定、单元格编辑、UPDATE 预览、单事务、增删行、刷新/撤销 | 真库集成测试（改/增/删/回滚）；只读降级 |
| **P3** | 数据源扩张 | 方言层、PostgreSQL、ClickHouse、schema 对象树、补全带 schema | 方言单测；PG/CK 集成（无实例 skip）；多 schema 树 |
| **P4** | 安全收尾 | 密码加密、导入导出、启动健康检查、（可选）主题/字体/快捷键 | 加解密/迁移/权限；导入导出往返 |

## 7. 工作方式

- 每个阶段单独出 writing-plans 计划（4–7 个任务），按 TDD 执行，阶段内可独立验收。
- 全局约束沿用：`CGO_ENABLED=0`、`go test -p 1 ./...`、基准（`TestEditor.*Perf`、结果网格）不回退、
  不新增依赖（**除 P3 两个驱动，需用户批准**）、提交信息 `taskN: …`。
- 每阶段结束更新 `Readme.md` 的「功能清单」与 `docs/spike-results.md`（若涉及性能）。

## 8. 需要用户在批准时确认的点

1. P3 的两个驱动依赖（pgx、clickhouse-go）是否接受？若拒绝，P3 只能做「对象树/方言 + 现有两源」，
   PostgreSQL 需换其它第三方驱动（同样要依赖）。
2. P0 日志里记录**完整 SQL**（本地 `0600`）是否接受？不接受就改为只记前 200 字符 + 哈希。
3. P1 的 SQL 格式化是否本期做（我建议做，但列为可裁剪项）。
