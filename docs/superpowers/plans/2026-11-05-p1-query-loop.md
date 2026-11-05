# P1：日常查询闭环 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 不出编辑器完成「查 → 看 → 拿出去」：多语句执行与结果 Tab、结果区选中/复制/长文本查看、客户端排序筛选与取更多、导出 CSV/JSON/SQL、SQL 历史、对象预览与 DDL、保守格式化。

**Architecture:** `app/sqlsplit.go` 提供语句切分（纯函数，字节范围）；`ui` 为一次执行按语句建运行时（`runtimeID = consoleID#i`，顺序执行、链式推进），结果区按语句分 Tab 并用 `LineBackgrounds` 标记语句行；结果区的排序/筛选是纯 UI 视图层（行顺序 + 过滤集合，不重跑 SQL），「取更多」用派生表包一层 LIMIT/OFFSET；导出走新增可选接口 `engine.RowStreamer`（全量流式）或当前结果；SQL 历史落 `history.jsonl`。

**Tech Stack:** Go 1.27.3、mygo（`ui.Table` 的 `CellSelect`/`CellMenu`/`HeaderMenu`、`c.WriteClipboard`、`mygo.Dialog.Save`、`editor.LineBackgrounds`）、`db-g/engine`、`db-g/app`。

**Spec:** `docs/superpowers/specs/2026-11-05-db-g-roadmap-v2-design.md`（P1 一节）

## Global Constraints

- 不新增依赖；`CGO_ENABLED=0`；全量 `go test -p 1 ./...`；编辑器/结果基准不回退。
- 「执行」语义：**有选区 → 执行选区**（可含多条）；**无选区 → 执行光标所在单条**（不跑整篇）。
- 排序/筛选/取更多不得重跑原 SQL（取更多除外，且用派生表包裹）；非单条 SELECT 禁用取更多并提示。
- 导出/历史/日志里的 SQL 与数据：本地文件 `0600`；导出默认不带密码；全量导出前提示「会重新执行查询」。
- 文案中文；提交信息 `taskN: …`。每任务收尾绿：`CGO_ENABLED=0 go test -p 1 ./...`、`go vet ./...`。

## Review Focus

1. **切分错误**：`;` 在字符串/注释/`BEGIN…END` 块里不能被当分隔符；空语句跳过；字节范围必须能 `Slice` 回原文。（Task 1）
2. **多语句顺序与失败停止**：必须按顺序执行，出错停在出错语句（后续标记「未执行」），不能并发打到同一连接。（Task 2）
3. **取更多的包裹安全**：SQL 必须整体作为派生表；末尾已有 `;`、`LIMIT`、注释都要处理；行数不重复不遗漏。（Task 4）
4. **导出转义**：CSV 的引号/换行/逗号，SQL INSERT 的引号与 `NULL`，JSON 的数字/字符串区分。（Task 5）
5. **历史与隐私**：只追加、超限轮转；面板插入时用字节光标位置；不把密码类字段写进历史。（Task 6）

---

### Task 1: `app/sqlsplit.go` —— 语句切分

**Files:** Create `app/sqlsplit.go`、`app/sqlsplit_test.go`

**Interfaces:**
```go
type SQLStatement struct{ Start, End int } // 字节范围，不含分隔的 ';' 与首尾空白
func SplitStatements(text string) []SQLStatement
func StatementAt(text string, offset int) (SQLStatement, bool) // 光标所在语句
```

- [ ] Step 1 tests：`select 1; select 2` → 2 段且 Slice 为 `select 1`/`select 2`；
  `select ';' as x; select 2` → 第一段含字符串分号；`-- a;\nselect 1` → 1 段；
  `/* ; */ select 1;` → 1 段；`;;; select 1;;` → 1 段；空文本 → 0 段；
  `select 1;\n\nselect 2` 第二段 Start 精确到 `select`；`StatementAt` 在第二段中间返回第二段。
- [ ] Step 2 RED；[ ] Step 3 实现（字符串 `'`/`"`/`` ` ``、行注释 `--`、块注释 `/* */`、括号深度不计入切分但跳过内部；`;` 后去掉空白，段内 trim 更新 Start/End）；[ ] Step 4 GREEN；[ ] Step 5 commit `task1: 语句切分（字符串/注释/空语句安全）`。

### Task 2: 多语句执行 + 结果 Tab + 语句标记

**Files:** Modify `app/console.go`（`Dur() time.Duration`）、`ui/run.go`、`ui/root.go`、`ui/result.go`

**Interfaces:**
```go
// app/console.go
func (c *Console) Dur() time.Duration // 最近一次执行的耗时
// ui/root.go
type stmtRun struct { stmt app.SQLStatement; runtimeID string }
// root 新字段
stmtRuns   []stmtRun   // 当前控制台本次执行的语句（按序）
activeStmt int
// ui/run.go
func (a *root) runSelected() // 取代 runSQL：算范围→逐条顺序执行
```
- 执行：`ranges := selection or StatementAt(cursor)`；为每条建 `a.runtime(consoleID+"#"+i)`；跑第 0 条；
  `OnDone`（已在 `execDoneLogger` 里）用 `win.Update` 推进下一条 —— 失败则停，后续不跑（Tab 显示「未执行」）。
- 结果区：`len(a.stmtRuns) > 1` 时表头显示 `语句 1 | 语句 2 …`（成功绿/失败红/未完灰），点击切 `activeStmt`；
  无多语句时与现在一致。
- 编辑器：每次 `Version()` 变化时重算 `LineBackgrounds`：当前执行语句区间浅色、失败语句红色底；删除旧标记。
- 测试：两条语句 → 2 Tab 且结果区显示第一条结果；第一条报错 → 第二条未执行（`"未执行"` 文案）；单条语句不显示 Tab；`Dur()>0`。

- [ ] 五步 TDD；commit `task2: 多语句执行与结果 Tab（顺序、失败即停、行标记）`

### Task 3: 结果区选中/复制/长文本查看

**Files:** Modify `ui/result.go`；Create `ui/resultcopy.go`、`ui/resultcopy_test.go`

**Interfaces:**
```go
func cellString(v any) string                      // NULL 显示与复制一致（"NULL"）
func rowsTSV(r *engine.QueryResult, rows []int, col int) string // 复制列/整行用
func (a *root) copySelection(c *ui.Context)        // 写剪贴板
func (a *root) openCellViewer(row, col int)        // 长文本/JSON 弹窗
```
- `resultState.CellSelect = true`（`Columns.Cells` 记录块）；`Ctrl+C` → 复制选中块（TSV，含列头可关）；
  `resultState.CellMenu` → 「复制单元格 / 复制本行 / 复制本行→INSERT / 查看完整内容」。
- 长文本弹窗：`ui.Modal` + `ui.TextAreaReadOnly`（或可选中文本），JSON 用 `json.Indent` 美化（解析失败显示原文）。
- 测试：`cellString(nil)=="NULL"`、`rowsTSV` 的制表符换行与 NULL；`copySelection` 单测用假剪贴板不可行 → 断言上层字符串；
  弹窗 headless `HasText` JSON 美化片段。

- [ ] 五步 TDD；commit `task3: 结果区选中/复制/右键菜单/长文本查看`

### Task 4: 客户端排序 + 每列筛选 + 取更多

**Files:** Modify `ui/result.go`；Create `ui/resultview.go`、`ui/resultview_test.go`

**Interfaces:**
```go
type resultView struct {
    order  []int          // 行下标顺序（排序后）
    filter map[int]string // 列 → 子串过滤
    hidden []bool         // 预留：列隐藏
}
func newResultView(n int) *resultView
func (v *resultView) sortBy(r *engine.QueryResult, col int, desc bool, types []bool)
func (v *resultView) apply(r *engine.QueryResult) []int   // 过滤后顺序
func fetchMoreSQL(sql string, offset, limit int) (string, bool) // 派生表包裹
```
- 表头点击（`HeaderMenu` 与 `ListState.Sort`）：排序按列类型（数值/字符串/时间/NULL 最后），多列按 Shift；
  过滤条：每列一个输入框（表头右键菜单「筛选…」→ 顶部输入行）；渲染行时按 `order` 取 `r.Rows[i]`。
- 取更多：记录 `lastExecSQL` 与已加载行数；按钮 `取更多`（`CapFetchMore` 且单条 SELECT）；执行
  `fetchMoreSQL`，追加到当前结果（`Truncated`/`合计数` 更新）；派生表包裹失败（驱动报错）→ 提示并禁用。
- 测试：`fetchMoreSQL`（含尾 `;`/`LIMIT`/注释）输出预期；排序数值 vs 字符串 vs NULL 顺序；
  过滤大小写不敏感与空过滤；取更多追加后行数不重复。

- [ ] 五步 TDD；commit `task4: 结果排序/筛选/取更多（不重跑原 SQL）`

### Task 5: 导出 CSV / JSON / SQL INSERT（含全量流式）

**Files:** Modify `engine/session.go`（可选接口）、`driver/sqlite/sqlite.go`、`driver/mysql/mysql.go`；Create `app/export.go`、`app/export_test.go`；Modify `ui/result.go`

**Interfaces:**
```go
// engine/session.go
type RowStreamer interface {
    Stream(ctx context.Context, sql string, fn func(row []any) error) error
}
// app/export.go
func WriteCSV(w io.Writer, cols []string, rows [][]any) error
func WriteJSON(w io.Writer, cols []string, rows [][]any) error
func WriteInsert(w io.Writer, table string, cols []string, rows [][]any) error
func ExportTo(path string, format string, cols []string, rows [][]any) error
```
- UI：结果区「导出…」按钮 → 菜单（当前结果 CSV/JSON/SQL INSERT；全量 → 三种之一，需 `RowStreamer`）；
  `mygo.Dialog.Save` 取路径；导出在 goroutine，完成后 `win.Update` 提示行数；写文件 `0600`。
- 驱动实现 `Stream`：`db.QueryContext` 逐行 scan（复用 RunStatement 的 []byte→文本归一化），不设 500 行限制，
  但保留单格 1MB/总量 16MB 的**保护**改为「单格截断 + 继续」。
- 测试：三种格式的转义（逗号/引号/换行/NULL/中文）、JSON 数字与字符串、SQL INSERT 的表名引用；
  驱动 Stream 用 sqlite 真库断言逐行回调与错误传播。

- [ ] 五步 TDD；commit `task5: 导出 CSV/JSON/SQL INSERT + 驱动流式接口`

### Task 6: SQL 历史

**Files:** Create `app/history.go`、`app/history_test.go`；Modify `ui`（历史面板）

**Interfaces:**
```go
type HistoryEntry struct {
    Time time.Time; ConnID, ConnName, SQL string
    DurMS int64; OK bool; Rows int; Err string
}
func AppendHistory(path string, e HistoryEntry) error // JSONL，0600
func LoadHistory(path string, limit int) ([]HistoryEntry, error) // 最近 N 条（新的在前）
```
- 路径 `~/.config/db-g/history.jsonl`；>5MB 轮转为 `history.1.jsonl`；读取时容忍坏行（跳过）。
- UI：顶栏「历史」按钮 → 侧栏/弹窗列表（时间/连接/首行摘要/耗时/状态）；双击看全文；「插入到光标」。
- 记录点：`execDoneLogger`（成功/失败都记，带 SQL 全文）。
- 测试：追加/读取/轮转/坏行跳过/limit；UI headless 列表出现条目与插入后编辑器文本变化。

- [ ] 五步 TDD；commit `task6: SQL 历史（JSONL 落盘 + 面板 + 插入光标）`

### Task 7: 对象树预览 + DDL

**Files:** Modify `engine/session.go`（可选 `ObjectDDL`）、`driver/*`、`ui/objecttree.go`、`ui/run.go`

**Interfaces:**
```go
type ObjectDDLer interface { ObjectDDL(ctx context.Context, kind ObjectKind, name string) (string, error) }
```
- 双击表/视图 → 新控制台里 `SELECT * FROM <quote> LIMIT 200` 并执行（走 Task 2 的执行路径）。
- 右键「查看 DDL」→ 弹窗（只读、可复制）；sqlite `SELECT sql FROM sqlite_master WHERE name=?`，
  mysql 用 `SHOW CREATE TABLE`（视图 `SHOW CREATE VIEW`）。
- 测试：sqlite DDL 往返；对象树双击后控制台文本与执行结果；headless HasText。

- [ ] 五步 TDD；commit `task7: 对象预览与 DDL 查看`

### Task 8: 保守 SQL 格式化（可裁剪）

**Files:** Create `app/sqlformat.go`、`app/sqlformat_test.go`；Modify `ui`（按钮/快捷键）

**Interfaces:**
```go
func FormatSQL(sql string) (string, error) // 只做空白与关键字大小写；结构看不懂就返回原文
```
- 规则：主语句关键字（SELECT/FROM/WHERE/GROUP BY/ORDER BY/HAVING/LIMIT/INSERT/UPDATE/DELETE/JOIN…）前换行并大写；
  顶层逗号后换行（SELECT/JOIN 列表）；条件内不重排；字符串/注释原样保留；幂等。
- 测试：幂等（`FormatSQL(FormatSQL(x)) == FormatSQL(x)`）、字符串/注释不变、关键字大写、错误 SQL 返回原文。

- [ ] 五步 TDD；commit `task8: 保守 SQL 格式化`

## 升级条件（何时停下来问）

- 多语句顺序执行需要改动 `app.Console` 的 Run 签名才能串起来 → 先汇报，不擅自破坏既有 API。
- `ui.Table` 的 `CellSelect` 与现有 `ListState`（列宽持久化）冲突或崩溃 → 停下来评估是否退回「整行选择 + 复制」。
- 全量流式导出在 MySQL 上内存/连接行为异常（长时间游标）→ 汇报后改为「分页批量导出」。
- 格式化在中途改变语义（测试或人工发现）→ 立即回退该规则，必要时把 Task 8 移出 P1。
