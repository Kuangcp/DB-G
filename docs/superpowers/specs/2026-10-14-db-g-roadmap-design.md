# db-g 功能对齐 db-k 路线图（Roadmap）设计

日期：2026-10-14
状态：已批准（四节设计逐节过审，用户确认「按你的推荐吧」「可以，继续」）

## 背景与目标

db-g（Go + mygo 自绘 GUI）已完成编辑器性能 spike：100k 行 × VV 策略单键中值 31.4ms，验证了长文本渲染编辑无性能问题（见 `docs/spike-results.md`）。但数据层完全空白——无驱动、无执行、无结果网格、无连接管理。

对标项目 db-k（Kotlin Compose Desktop JDBC 客户端，`/home/zk/Work/ai/db-k`，约 33k LOC，11 种数据源）提供完整的数据库客户端体验。本路线图规划在 db-g 中实现 db-k 的功能，要求：

1. **里程碑拆分带阶段性成果**，每个里程碑可演示、可测试、有性能守门（首个里程碑：操作 SQLite 执行 SQL 查看结果）；
2. **抽象设计**，便于后续扩展更多样的数据源（本期 SQLite → MySQL/PG，未来预留 Redis/ES 等）。

### 范围决策（已确认）

| 决策点 | 结论 |
|---|---|
| 总体范围 | **核心子集优先**：连接管理、执行/结果、结果编辑、对象树、补全、历史。Redis/ES、Excel 导出、多工作区、i18n 不在本期 |
| 数据源 | **SQLite → MySQL/Postgres**（database/sql 标准驱动），Redis/ES 不在本期但能力位预留 |
| 验收形式 | **演示 + 测试 + 性能基准**（spike 基准每里程碑必跑防回退） |
| Roadmap 粒度 | **全程粗粒度 + 近期详细**：M1–M7 全列，仅前 2 个里程碑做任务级拆分（任务拆分在后续 writing-plans 阶段完成） |
| 拆分思路 | **功能纵切**（每里程碑端到端可演示），吸收 db-k 的分层抽象（engine 契约先行），里程碑内部纪律为「数据层 → UI 接线 → 测试+基准」 |

### 被否决的替代方案

- **分层横切**（先做完全部数据层再做 UI）：数周无可见成果，违背阶段性验证目标，且 UI 风险推迟到最后集中爆发。
- **仿 db-k 模块顺序**（严格复制其源码结构）：db-k 的模块切分是 33k LOC 演化的结果，早期里程碑不需要完整 engine 契约会偏重；db-g 是 Go + mygo，不必逐模块对应。

## 第 1 节：架构与数据源抽象层

### 包结构

在现有扁平结构上引入分层（`highlight/` 子包模式已有先例）：

```
db-g/
├── engine/            # 协议无关契约 — 叶子包，不 import 驱动或 UI
│   ├── session.go     #   DataSource 接口 + 工厂注册表
│   ├── caps.go        #   能力位（仿 db-k BackendCapabilities）
│   └── model.go       #   QueryResult / QueryColumn / DbObject / ColumnMeta
├── driver/
│   ├── sqlite/        # M1：SQLite 实现（database/sql + modernc.org/sqlite，纯 Go 无 cgo）
│   ├── mysql/         # M4
│   └── postgres/      # M4
├── app/               # UI 无关的状态层（连接仓库、控制台状态、历史），仿 db-k ViewModel
├── ui/                # mygo 视图（现 main.go/editor.go 迁入）
└── highlight/         # 已有
```

### 核心接口

骨架在 M1 定义，M4 用第二种数据源验证：

```go
// engine/session.go — 叶子契约
type Session interface {
    Open(ctx context.Context) error
    Close() error
    RunStatement(ctx context.Context, sql string) (*QueryResult, error)
    Paginate(ctx context.Context, sql string, offset, limit int) (*QueryResult, error) // fetch-more，M5 启用
    Cancel() bool
    Caps() Capabilities
    // 元数据方法在 M2 加入接口（SQLite 实现后再扩展，避免 M1 过度设计）：
    // LoadNamespaces / LoadObjects / LoadColumns / ObjectDDL
}
type Factory func(profile Profile) (Session, error)
func Register(kind string, f Factory) // driver 包 init() 自注册
```

### 能力位（caps.go）

`CapEditableResult / CapFetchMore / CapObjectDDL / CapSQLCompletion / CapSessionContext ...`——UI 按能力降级功能。这是 db-k 让 Redis/ES 与 JDBC 共享同一套 UI 的核心机制；本期只定义并用于 M6 门控（`CapEditableResult`），其余位值预留，不在本期依赖。

### 方言层

M4 引入 `Dialect` 接口（仿 db-k `DbDialect`），承载 quoteIdent / LIMIT 下推 / 元数据探测差异，经注册表 `DialectRegistry` 按数据源类型查表。**SQLite 时期不设方言**，避免过早抽象。

### mygo 关键 API 依据（v0.4.8 源码已核实）

| 需求 | API |
|---|---|
| 结果表格 | `ui.Table`（虚拟化、列拖拽/排序/固定、行级多选、`ListState.Columns` 持久化） |
| 对象树 | `ui.Outline` / `ui.OutlineTable`（虚拟化树） |
| 对话框 | `ui.Modal` + `ui.Form`；原生文件对话框 `mygo.Dialog.Open/Save`（任意协程可调） |
| 右键菜单 | `Element.ContextMenu(func(m *ui.Menu))` |
| 异步回投 | `win.Update(fn)`（任意协程安全，主线程执行并重绘）、`Invalidate()` |
| 补全 | `ui.Autocomplete` / `ui.Combobox` |
| headless 测试 | `ui.Tester` |
| 本地元数据存储 | mygo 自带 `plugins/sqlite`（cgo-free）用于 app.db |

## 第 2 节：里程碑划分与验收标准

每个里程碑 = 可演示功能 + 单元/集成测试 + 性能基准守门（`TestSpikeMatrix` 必须保持绿，编辑器延迟不回退）。

| # | 里程碑 | 内容 | 验收 |
|---|---|---|---|
| **M1** | **SQLite 执行 SQL 查看结果** | 文件对话框打开 SQLite；编辑器选中/当前语句执行（Ctrl+Enter）；异步执行（协程 + `win.Update`）+ Spinner + 可取消；`ui.Table` 结果渲染（列头、行号、错误横幅、500 行上限 + 截断提示） | 临时 SQLite 集成测试（建表→插入→查询断言）；headless UI 测试；结果网格基准（10 万行 × 50 列帧耗时）；spike 基准绿 |
| **M2** | 连接管理 + 对象树 | 连接 profile CRUD，持久化到本地 app.db；`ui.Outline` 对象树：库→表→列懒加载（先计数后展开）；右键菜单：预览数据/查看 DDL/打开控制台；对象双击插入编辑器 | 仓库层迁移测试；树懒加载集成测试 |
| **M3** | 控制台增强 | 多语句拆分（`;` + 字符范围锚点，仿 db-k `splitStatementRanges`）→ 语句级进度/结果 Tab；执行历史落库 + 历史面板插入光标；语句超时 30s + generation guard 防迟到结果 | 拆分器单测（字符串/注释边界）；历史持久化测试 |
| **M4** | **MySQL/PG 接入（抽象层验证）** | `driver/mysql`、`driver/postgres`；`Dialect` 方言注册表引入 | **核心：零 UI 改动**——同一套 UI 通过能力位与方言自动适配方言单测 + 容器化集成测试（无容器自动 skip） |
| **M5** | 结果增强 | 客户端多列排序/列筛选（不重跑 SQL）；fetch-more 分页追加（启用 `Paginate`）；导出 CSV/JSON（`Dialog.Save`） | 排序/筛选纯函数测试；分页驱动测试 |
| **M6** | 结果编辑 | PK 定位 → 单元格编辑 → 参数化 UPDATE 预览 → 单事务提交，影响行数 ≠1 全回滚（对齐 db-k RowUpdater 语义）；`CapEditableResult` 门控 | 编辑计划/回滚集成测试（含影响行数异常用例） |
| **M7** | 编辑器智能 | 补全（关键字/表/列）、SQL 格式化、Ctrl+/ 注释切换；依赖 M2/M4 元数据缓存 | 补全匹配单测；格式化幂等性测试 + spike 基准回归 |

### 依赖关系

```
M1 → M2 → M3（线性）
M4 依赖 M2（对象树展示多库元数据）
M5、M6 依赖 M1；M7 依赖 M2 + M4
M5 / M6 / M7 之间可并行调整顺序
```

### 隐藏风险点（提前暴露设计）

- M1 即测结果网格基准：mygo `ui.Table` 在真实数据量（10 万行 × 50 列）下的帧耗时是关键不确定性，不推迟。
- `ui.Table` 无单元格级选择（仅行级）：M6 编辑先用行级选择 + 编辑对话框，单元格内联编辑按需自定义控件。
- modernc SQLite 性能若不达标：回退选项 mygo 自带 `plugins/sqlite`（需适配 database/sql 兼容层或双实现）。

## 第 3 节：执行模型与数据流

### 线程纪律（仿 db-k：UI 线程绝不碰驱动）

```
按键(UI主线程) → 语句拆分 → 提交执行
   └→ go 协程: session.RunStatement(ctx, sql)
        ├─ 每个 session = 专属串行执行器（单协程队列，串行化驱动调用，
        │   对齐 db-k 单线程 executor；SQLite 走同一路径 → M4 零特判）
        ├─ context.Context 贯穿：超时(30s)自动 cancel，手动停止即 cancel
        └→ 完成后 win.Update(func(){ app.updateResult(...) }) 回投主线程
             ├─ generation guard：取消/重跑后 generation+1，迟到结果丢弃
             ├─ 错误 → 结果区错误横幅（SQL 原文 + 驱动错误信息）
             └─ Invalidate() 触发重绘，Spinner 停止
```

### 关键规则

1. **状态所有权**：所有 `app` 状态只在主线程读写；后台协程只能经 `win.Update` 投递变更（`mygo vet` 拦截跨帧缓存 `ui.Element`）。
2. **结果内存预算**：单行超 1M 字符截断、单结果超 16M 字符降级（对齐 db-k QueryExecutor 字符预算），BLOB 只显示字节数。
3. **取消语义**：`Cancel()` 返回是否成功中断；无法中断的驱动调用靠 generation guard 防止迟到结果污染 UI。
4. **错误分层**：连接失败（对话框 + 树状态点变红）/ 执行失败（结果 Tab 内错误视图）/ 元数据失败（树节点错误占位，不弹窗）。
5. **连接打开/关闭不在 UI 线程**（SQLite 快但 MySQL/PG 网络握手会卡帧），走后台协程 + 进度状态点。

### 状态层切分（app/）

- `ConnectionsState`：profile 仓库 + 会话生命周期（懒打开；健康检查 M1–M3 只做执行前 isOpen 检查，完整健康检查按需推迟到 M4 后）
- `ConsoleState`：当前语句、运行槽（run slots）、结果 Tab、历史写入
- `TreeState`：树节点懒加载与缓存（列元数据缓存供 M7 复用）

## 第 4 节：测试与性能守门策略

### 四层测试

1. **纯函数单测**：语句拆分、补全匹配、格式化、方言 SQL 生成、编辑计划生成——无 UI 无驱动，秒级。
2. **数据层集成测试**：`driver/` + `app/` 仓库层，`t.TempDir()` 临时 SQLite 全链路；M4 的 MySQL/PG 用 docker 容器 + build tag 隔离，无容器环境自动 skip。
3. **Headless UI 测试**：沿用 `ui.Tester`——结果渲染、树展开、对话框交互无窗口验证。
4. **性能守门（每里程碑必跑）**：
   - `TestSpikeMatrix`：100k 行 VV 策略按键 p99 不得劣化超阈值（暂定 50ms，M1 实现计划中固化为具体数值）；
   - 结果网格基准：10 万行 × 50 列 `ui.Table` 帧耗时（M1 起）。

### 里程碑验收清单（收尾核对）

- [ ] 功能可演示（录屏或现场）
- [ ] 新增测试全绿 + 既有测试全绿（`go test ./...`）
- [ ] spike 基准无回退
- [ ] 更新 `docs/` 对应说明 + Readme 功能清单

### 风险预案

| 风险 | 对策 |
|---|---|
| `ui.Table` 无单元格选择 | M6 先用行级选择 + 编辑对话框；内联编辑按需自定义控件 |
| modernc SQLite 性能不达标 | 回退 mygo `plugins/sqlite`（适配兼容层或双实现） |
| 结果网格基准超标 | 降级：结果只读视图走 `ui.List` 自绘行，代价是放弃列拖拽 |
| mygo v0.4.x 个人维护框架风险 | 沿用 spike 结论：性能已验证，风险接受；engine/app 层不依赖 mygo，保持可迁移 |

## 交付物

- 本路线图设计文档（本文件）；
- 后续按 writing-plans 流程产出 M1、M2 的详细实现计划（任务级）；
- M3 之后里程碑在前一阶段完成时滚动细化。
