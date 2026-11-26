# 表/视图临时浏览标签页设计（对标 dgopher 的 Table Tab）

日期：2026-11-27
状态：已实现（v1，见 §12 落地说明）
参考实现：`/home/kcp/Code/Go/dgopher` 的 `internal/ui/dataview`（TableTab / Viewer）

## 1. 背景与目标

TODO 里的诉求：像 DBeaver/Navicat 一样「先看数据、写条件、排序，不写 SQL」的临时查询，
并支持把生成的 SQL 复制走或直接带去控制台。

目标交互：**双击左侧对象树里的表/视图**，在控制台标签行新增一个**表视图标签**：

- 与 SQL 控制台并列，但**不持久化**：只存在内存，关应用即失，不写 `connections.json` / `consoles/`；
- 内含三个子页：**数据 / 结构 / DDL**；
- 数据页：网格预览，可写 WHERE、可按列排序、可分页，可复制 SQL、可在控制台打开；
- 结构页：字段表格（名称 / 类型 / 可空 / 主键）；
- DDL 页：只读 DDL（内容与 `Ctrl+Q` 弹窗一致），可复制、可在控制台打开。

## 2. dgopher 调研结论

| 模块 | 做法 | 本设计是否采用 |
|---|---|---|
| `dataview/table.go` `TableTab` | 一个标签承载 `PageData/PageStructure/PageDDL/Diagram`，顶部 `Segmented` 切页；`loadMeta` 后台一次读列/索引/外键/DDL，`metaGen` 轮次作废旧结果 | 采用「一标签三子页」+ 后台加载 + 轮次作废；不做 Diagram |
| `dataview/viewer.go` | WHERE 输入框（`whereIn` 输入中 / `where` 已应用 / `typedWhere`）、服务端 `ORDER BY`（列头排序）、`selectSQL()` 拼接、`COUNT(*)`、翻页、错误保留旧行 | 采用 WHERE 文本 + 列头排序 + LIMIT/OFFSET；COUNT 列后续 |
| `dataview/filterbuilder.go` | 可视化条件构建器（列 + 操作符 + 值，AND/OR） | v1 不做，后续可加 |
| `dataview/gensql.go` | `sqlGen` 生成 SELECT/INSERT/UPDATE/DELETE；`sqlDialog` 提供 **Copy** 与 **Open in Editor** | 采用「复制 SQL / 在控制台打开」；只生成 SELECT |
| `navchildren.go` / `navview.go` | 双击对象 `OpenTable(..., PageData)`；结构/DDL 也能从菜单直达 | 采用双击与右键直达 |
| `savedtabs` / `tablerestore` | 表标签随项目恢复 | **不采用**：db-g 要求临时、不落盘 |
| 行编辑 / 事务 / ER 图 / 图表 / 导出全量 | 功能很全 | v1 不做（只读预览） |

differences：dgopher 的 Tab 与连接、项目、持久化是强耦合的；db-g 用 transient 侧表实现更轻。

## 3. 范围

### 3.1 v1 要做

1. 入口
   - 树里表/视图**双击** → 打开（或激活已有）表视图标签；
   - 双击行为变更后，旧能力「插入预览 SQL」保留为右键菜单项；
   - 右键菜单新增「查看数据」，结构/DDL 子页也可从菜单直达。
2. 标签
   - 在现有 tabs 行渲染，表/视图图标 + 对象名；激活样式复用控制台标签；
   - 同一 `(connID, schema, name, kind)` 只一个标签，重复打开即激活；
   - 右键：关闭 / 刷新；不提供重命名、删除；
   - `Ctrl+Tab` 把表视图标签一并纳入最近使用序列；
   - 数据源被删除、连接重载、同步刷新后关闭对应标签。
3. 数据页
   - 查询：`SELECT * FROM <qualified> [WHERE …] [ORDER BY …] LIMIT n OFFSET m`；
   - WHERE：等宽输入框，Enter 应用，清空按钮；DB 报错原样显示并保留上一次成功结果；
   - 排序：工具栏「排序」菜单选列，点同一列循环 升序→降序→取消（v1 单列服务端 ORDER BY；
  列头点击会触发 mygo 的本地排序，与服务端排序叠加会更乱，故不采用）；
   - 分页：上一页/下一页/刷新，页大小取设置「结果每页行数」；
   - 操作：复制 SQL、在控制台打开（带 WHERE/ORDER BY，不带 LIMIT/OFFSET）。
4. 结构页：`ColumnLister` / `SchemaColumnLister` 读字段，表格展示名称/类型/可空/主键。
5. DDL 页：与 Ctrl+Q 弹窗同源取 DDL，只读 SQL 编辑器（高亮/选中复制），另提供复制与在控制台打开。
6. 键盘：`Ctrl+Enter`/`F5` 刷新数据页；`Ctrl+Q` 切到 DDL 子页；`Ctrl+Tab` 参与切换。
7. 并发：所有网络读都在后台协程 + 超时 + 每标签轮次（旧结果作废）；关标签取消在途请求。
8. 错误：连接失败沿用现有 `toastError` 浮窗；页内保留错误横幅与「重试」。

### 3.2 v1 不做（明确）

- 行编辑、增删行、事务（表视图只读）；
- ER 图、图表、全量导出；
- 索引 / 外键 / 检查约束（engine 目前没有对应可选接口，见 §7.4）；
- 标签持久化与恢复、标签拖拽重排、工作区归属；
- 可视化条件构建器（先文本 WHERE）。

## 4. 交互设计

### 4.1 入口与旧行为替换

| 操作 | 现在 | 之后 |
|---|---|---|
| 树里双击表/视图 | `previewObject`：往控制台追加 `SELECT * … LIMIT 200` | 打开/激活表视图标签（数据页） |
| 右键「预览数据」 | 同上 | 同上 |
| 右键「插入预览 SQL」 | 无 | **新增**，执行旧的追加行为 |
| 树里 Ctrl+点击 | DDL 弹窗 | 不变 |
| 右键「查看 DDL」/「查看统计」 | 弹窗 | 不变 |

### 4.2 标签行

- 位置：控制台标签之后追加（v1 不做与控制台交错排序；实现代价低、误拖风险小）。
- 外观：`iconTable`/`iconView` + 对象名；字号/高度/激活上边线与控制台标签一致。
- Tooltip：「数据源名 · schema.name」；视图带视图图标。
- 右键菜单：`关闭`、`刷新`（数据页）。
- 关闭激活的表视图后：回落到当前工作区激活的控制台；没有控制台则回空态。
- 标签行放不下时沿用 `ui.Toolbar` 的 More 收起行为（现有控制台标签同款）。

### 4.3 内容区

激活表视图时，中间内容区（编辑器 + 结果分屏）整体替换为 `viewerPane`：

```
┌ 页签行：控制台A │ 控制台B │ ▦ users …                         ┐
├ connBar：数据源 / schema / 工作区（执行、计划、格式化、AI 补全隐藏） ┤
├ viewer 标题栏：〔数据｜结构｜DDL〕            数据源 · schema.name ┤
├ 数据工具栏：[WHERE 图标] [条件输入………] ⟳  ⧉复制SQL  ↗在控制台打开  ├
├ 网格（列头可排序；只读）                                            │
└ 分页：‹ 上一页  第 1 页  ·  已读 500 行  › 下一页                  ┘
```

- 页签行、connBar 保留；connBar 中只对控制台生效的按钮隐藏（不是禁用，避免误解）。
- 数据页工具条顺序对齐 dgopher：过滤 → 刷新 → 复制/带走；右侧显示行数与页码。
- 结构页：标题「字段」+ 表格；列头 名称 / 类型 / 可空 / 主键（主键用钥匙图标）。
- DDL 页：顶部工具条 复制 / 在控制台打开；下方只读编辑器占满剩余高度。
- 空态文案：未连接（点按重试）、驱动不支持（说明原因）、加载中（Spinner）。
- 错误：数据页 DB 错误用页内 Danger 横幅 + 「编辑条件 / 清空条件 / 重试」；连接错误同时 toast。

### 4.4 键盘

| 键 | 表视图内行为 |
|---|---|
| `Ctrl+Enter` / `F5` | 重新读当前数据页（同一 WHERE/排序/页码归零） |
| `Ctrl+Q` | 切到 DDL 子页（等价于弹窗里看到的 DDL） |
| `Ctrl+Tab` | 最近使用的标签循环（含控制台与表视图） |
| `Ctrl+P` | 命令面板可搜到打开中的表视图 |
| 其他编辑器动作（保存/格式化/AI 补全/注释等） | 表视图内不生效（不吞键即可） |

## 5. 状态设计

```go
// viewerPage 是表视图标签的三个子页。
type viewerPage int // viewerData / viewerStructure / viewerDDL

// viewerTab 是一个临时的表/视图浏览标签：不落盘，应用退出即丢。
type viewerTab struct {
    id     string // "vw:"+connID+"|"+schema+"|"+name+"|"+kindKey
    connID string
    schema string
    obj    engine.DBObject // Kind 为 Table 或 View

    page viewerPage

    // 数据页
    whereIn  string   // 输入中的条件
    where    string   // 已应用的条件
    sortBy   []ui.SortOrder
    rows     *engine.QueryResult
    loaded   int      // 已读行数（统计/展示）
    offset   int
    hasMore  bool
    loading  bool
    err      string
    seq      uint64 // 请求轮次：刷新/切页/关闭后旧结果作废
    cancel   context.CancelFunc

    // 结构页
    cols    []engine.Column
    colsErr string

    // DDL 页
    ddl      string
    ddlErr   string
    ddlView  *myeditor.State // 每标签独立滚动/选区，只读
}
```

root 新增：

```go
viewerTabs map[string]*viewerTab // 仅内存
viewerOrder []string             // 标签行顺序（创建顺序）
activeViewer string              // 非空 = 当前展示表视图
viewerMRU   []string             // Ctrl+Tab 用；可与 consoleMRU 合并成一个 tabMRU
```

激活语义（关键取舍，避免污染控制台状态）：

- `a.activeConsole` 仍表示「编辑器里装的是哪个控制台」，切换表视图**不**清空它，也不 flush 编辑器；
- 新增 `viewerActive() bool`；`activeConnID()` 在表视图激活时优先返回 `viewerTab.connID`
  （connBar、schema 选择器、状态栏显示该数据源）；
- 所有「对编辑器/控制台」的动作（执行、保存、格式化、AI 补全/改写、外部文件保存、钉住结果）
  在 `viewerActive()` 时直接不响应；`Ctrl+Enter` 改走数据页刷新。

## 6. 接线点（按文件）

| 文件 | 改动 |
|---|---|
| `ui/viewer.go`（新） | `viewerTab` 结构、打开/激活/关闭、`viewerPane`、数据/结构/DDL 三页、SQL 构建与执行 |
| `ui/tabs.go` | 标签行渲染（控制台后追加）、点击激活、右键关闭/刷新、`Ctrl+Tab` 合并 MRU、标签关闭回落 |
| `ui/root.go` | 新字段；`view()` 里按 `viewerActive()` 切换内容区；`activeConnID()` 调整；`removeConn` 关闭相关标签；`closeAll` 取消请求 |
| `ui/objecttree.go` | 双击改为 `openViewerTab`；菜单新增「插入预览 SQL」，旧 `previewObject` 保留给该项 |
| `ui/palette.go` | 命令面板列出已打开的表视图（激活） |
| `ui/shortcuts.go` / `ui/keymap.go` | 动作守卫；是否需要新动作 ID（如「关闭标签」）另定 |
| `ui/objecttree.go` | 抽出 `fetchObjectDDL(ctx, sess, obj)`（现在内嵌在 `loadObjectDDL`），弹窗与标签共用，避免两套 |
| `locales/en.json` + `zh-Hans.json` | 见 §9 |
| `ui/viewer_test.go`（新） | 见 §10 |
| `AGENTS.md` | 实施后补「表视图标签」约定（transient 状态、只读、接线点） |

## 7. 数据层

### 7.1 SQL 构建

```go
func (t *viewerTab) dataSQL(kind string, pageSize, offset int) string {
    q := "SELECT * FROM " + qualifiedName(kind, t.obj) // 复用现有引号规则
    if w := strings.TrimSpace(t.where); w != "" {
        q += " WHERE " + w
    }
    if len(t.sortBy) > 0 {
        // 列名必须来自当前列清单，统一 app.QuoteIdent；ASC/DESC
        q += " ORDER BY …"
    }
    return q + fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, offset)
}
```

- **只读约束**：只拼 SELECT；不提供任何写入口。
- **注入防线**：拼好的 SQL 交给 `app.SplitStatements`，必须恰好 1 条，否则拒绝并提示条件无效；
  这挡住 `;` 追加语句；`--`/`/*` 只会影响本语句，不构成跨语句执行。
- **排序来源**：排序键只能从 `t.cols`（列清单）里选，界面通过列头点击产生，不解析用户文本。
- **视图**：与表同一路径（SELECT + 列 + DDL）；不支持时按能力位降级。

### 7.2 执行与生命周期

- 读数据：`session.RunStatement(ctx, sql)`，`ctx` 由连接配置的查询超时决定（`queryTimeoutFor`，默认 30s），
  后台 goroutine，`win.Update` 回投；headless（无窗口）同步执行便于测试。
- 每标签 `seq++`，回投时 `if seq != t.seq { return }`；刷新/切页/关闭先 `cancel()`。
- 行数预算沿用 `engine` 的结果预算；超出时结果带 `Truncated`，页内显示徽标。
- `hasMore := len(rows) == pageSize`；下一页 `offset += pageSize`；上一页 `offset -= pageSize`（夹到 0）。
- 结构/DDL：同样后台 + 超时 + 轮次；只在该子页第一次打开时读，刷新按钮可强制重读。
- 不新建 `app.Console` 运行态：表视图是无控制台语义的只读查询，避免和 `stmtRuns`/`pin`/编辑状态缠绕。

### 7.3 网格渲染

- 用 mygo `ui.Table` + 现有单元格格式化辅助（`cellStringFor`/`cellAt`/`columnWidths` 的纯函数部分）。
- 只读：不设置编辑、不挂 `CellSelect` 编辑路径；保留行号列、列宽拖拽、单元格双击查看（如果成本低）。
- 排序：v1 用工具栏「排序」菜单维护 `[]ui.SortOrder`（单列），变更即重新查询（offset=0）。
  不用 mygo 的 `ListState.SortBy`：它会在本地再排一次，数字列按显示文本排会打乱服务端顺序。
- 行数展示：`已读 N 行`；不显示假总数（COUNT 作为后续增强）。

### 7.4 能力位与降级

| 子页 | 依赖 | 不满足时 |
|---|---|---|
| 数据 | `Session.RunStatement`（总是有） | — |
| 结构 | `CapColumns` + `ColumnLister`/`SchemaColumnLister` | 显示「该数据源不提供字段信息」 |
| DDL | `CapObjectDDL` + `ObjectDDLer`/`SchemaObjectDDLer` | 显示「该数据源不提供 DDL」 |
| 复制/带走 SQL | 无 | — |

索引、外键、默认值、注释本版不展示；后续若要，在 `engine` 加可选接口（`IndexLister` 等）再补。

## 8. 国际化文案（新增 key 草案）

`viewerData` / `viewerStructure` / `viewerDDL`、`viewerWherePlaceholder`、`viewerWhereLabel`、
`viewerClearFilter`、`viewerRefresh`、`viewerCopySQL`、`viewerOpenInConsole`、`viewerPrevPage`、
`viewerNextPage`、`viewerPageInfo`、`viewerRowsRead`、`viewerColumnsTitle`、
`viewerColumnsUnsupported`、`viewerDDLUnsupported`、`viewerLoadFailed`、`viewerReadonlyBadge`、
`viewerFilterInvalid`、`viewerTabTooltip`（数据源 · schema.对象）、`viewerRequestBody`。

英文为源语言，中文补 `zh-Hans`，走 `go tool mygo generate`。

## 9. 测试计划（headless）

1. 双击对象树表 → 生成一个表视图标签（`viewerTabs` 长度 1），标签行出现对象名；再双击同对象只激活不新增。
2. 数据页：fake session 记录收到的 SQL，断言 WHERE/ORDER BY/LIMIT/OFFSET 拼接正确；
   WHERE 写 `1=1; DROP …` 被拒绝且不执行。
3. 排序：点击列头后重新查询，SQL 带 `ORDER BY "<col>" ASC`（再点变 DESC，第三下取消）。
4. 分页：下一页 offset 增加、上一页回退；`hasMore` 依据返回行数。
5. 复制 SQL：`tt.Clipboard()` 与最新查询一致（带 WHERE/ORDER BY，不带 LIMIT/OFFSET）。
6. 在控制台打开：创建/复用该数据源控制台，正文含 SQL，标签切回控制台。
7. 结构页：fake `ColumnLister` 的字段按表格渲染；不支持时显示说明。
8. DDL 页：与 `Ctrl+Q` 弹窗内容一致；复制按钮可用；不支持时说明。
9. 标签生命周期：关闭激活的表视图回落到控制台；删除数据源时对应标签关闭；关应用不落盘（无文件写入断言）。
10. 快捷键：`Ctrl+Enter` 刷新数据页、`Ctrl+Q` 切 DDL、`Ctrl+Tab` 能在控制台与表视图之间切换。
11. 并发：先发请求后立刻改条件/刷新，旧结果因 `seq` 被丢弃（fake session 延迟返回）。

## 10. 分阶段实施

- **S1 标签骨架 + DDL 页**：transient 标签、标签行接线、激活/关闭/回落、DDL 读取（复用 Ctrl+Q 逻辑）、
  复制/在控制台打开。验收：双击表出现标签，DDL 页内容与弹窗一致，关标签回落控制台。
- **S2 数据页**：SQL 构建、后台执行、WHERE、列头排序、分页、刷新、复制/带走；安全校验与错误保留。
- **S3 结构页 + 打磨**：字段表、空态/错误、快捷键（Ctrl+Enter/Ctrl+Q/Ctrl+Tab）、命令面板、
  删除数据源时清理、文档（AGENTS/Readme/CHANGELOG）。

每阶段都按仓库约定跑 `go build ./... && go vet ./... && go test -p 1 ./... && go tool mygo vet . && gofmt -l .`。

## 11. 决策记录（实施前拍板）

| # | 决策 | 推荐 | 备选 |
|---|---|---|---|
| D1 | 双击表/视图替换旧的「插入预览 SQL」 | **是**，旧行为移到右键「插入预览 SQL」 | 保留双击旧行为，仅右键打开表视图 |
| D2 | 数据页网格实现 | **共享网格组件**：抽出 `gridTable` + 编辑/选择/复制实现，控制台与表视图共用；表视图保留编辑能力，**不提供钉住、全量导出** | 独立网格（要复制一套编辑逻辑） |
| D3 | WHERE 输入 | **自由 SQL 片段 + 单语句校验** | v1 就做 dgopher 式条件构建器（成本高） |
| D4 | 表视图标签归属 | **全局、跨工作区常驻内存**（不落盘） | 归属当前工作区，切工作区即隐藏 |
| D5 | 表视图内 `Ctrl+Enter` | **刷新数据页** | 不绑定 |
| D6 | 多个标签顺序 | 控制台在前、表视图在后（v1 不交错） | 按最近创建/激活交错排序（需要统一 tab 列表） |

若无异议，按推荐执行，先出 `docs/superpowers/plans/2026-11-27-table-viewer-tab.md` 再动代码。

## 12. 落地说明（2026-11-27）

按评审意见实现，与初稿的差异：

- **一个对象一个标签**：`viewerTabID = connID|schema|name|kind`，重复双击只激活；标签只存在内存。
- **共享网格**：`ui/grid.go` 定义 `gridSnap`（ListState/选区/列宽/编辑计划/SQL 等）与
  `gridRuntime/gridSession/gridKind/gridConnID/gridSQL/gridErr` 访问器；`ui/result.go` 的
  `resultTable` 拆成 `gridTable(c, r, mode)`，控制台与表视图共用网格、单元格菜单、
  编辑计划、增删行、提交、内联/弹窗编辑；表视图模式隐藏钉住、重跑与导出工具条。
- **状态切换**：控制台与表视图各自持有 `gridSnap`，切标签时 `stashGrid/loadGrid` 搬一次；
  编辑集仍按 `gridRuntimeID`（控制台运行态 id / `vw:<viewerID>`）分桶，互不污染。
- **不持久化**：不写 `connections.json`/`consoles/`；关闭应用即丢；不恢复上次标签。
- **排序**：工具栏「排序」菜单，服务端单列 ORDER BY（见 §7.3 修订）。
- **执行安全**：多语句执行途中切到表视图时，后续语句按运行态所属控制台取会话，
  不会跑到表视图的数据源上（`runtimeConnID` / `queryTimeoutForConn`）。
- **Ctrl+Tab**：最近使用序列里控制台 id 与 `vw:<id>` 混排；两个方向都能切。
- **包内文件**：`ui/viewer.go`（标签与三个子页）、`ui/grid.go`（共享网格状态/访问器）、
  `ui/viewer_test.go`（headless 覆盖双击去重、WHERE/排序/分页、注入拒绝、编辑提交、
  结构/DDL、复制 SQL/带走、标签关闭回落、跨标签状态隔离、快捷键）。
