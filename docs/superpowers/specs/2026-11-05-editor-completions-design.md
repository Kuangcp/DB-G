# 迁移到 ui/editor + SQL 自动补全 设计

日期：2026-11-05
状态：待批准（方案 A + 三阶段推进已由用户选定；本文档待过审后进入 writing-plans）

## 背景

db-g 的编辑器是自制的：`ui.TextAreaBuffer` + 自写 `highlight/` tokenizer + 手绘行号 + 手写查找
（`ui/findmatch.go`）+ 一把绑在 `TextBuffer` 上的性能基准（`ui/bench_test.go` 的 spike matrix）。

用户要自动补全。mygo 官方方案是 `ui/editor` 包（仿 Zed）：内置 SQL 语言高亮、补全协议
（`OnCompletion` / `ShowCompletions`，弹层定位、前缀过滤与模糊排序、↑↓/Enter/Tab 接受、Ctrl+Space
显式触发都由库负责）、查找替换、折叠、多光标、行号与 gutter、`LineBackgrounds`（将来高亮当前执行语句）、
`OnInput`（接管键位）、视口级布局（百万行可用）。参照实现 `examples/code-editor`。

自制编辑器拿不到光标屏幕坐标，自己叠补全弹层等于重造库功能 —— 因此选定**迁移**，分三阶段。

已核实的库接口（本地 `github.com/egoist/mygo` HEAD，比 v0.7.4 新）：

| 需求 | API |
|---|---|
| 编辑器视图 | `editor.View(c, st).Grow(1).FillWidth()`，`State{Buffer, Placeholder, Theme?, Bindings, TabSize, Indent, LineNumbers, LineBackgrounds, OnCompletion, OnInput, …}`（字段名已核实） |
| 语言 | `editor.LanguageNamed("SQL")`（大小写不敏感）；SQL 语言默认 `TabSize: 2`、`--` 与 `/* */` 注释 |
| 文本 | `Buffer{String, Len, Line, Slice, Set, Reset, Version, Edit, Undo/Redo, Language, SetLanguage}` |
| 光标/选区 | `State.Selections() []Selection{Anchor,Head}`（**字节偏移**）、`Select(start,end)`、`Cursor()`、`CursorPos() Pos{Line,Col}`、`SelectedText()` |
| 补全 | `State.OnCompletion = func(CompletionRequest{Offset, Prefix, Version, Explicit})`、`ShowCompletions(start int, []Completion{Label,Detail,Kind,Insert,Documentation,FilterText})`、`Ctrl+Space` 内建 |
| 查找 | `State.Find(query)`、`Run(editor.Find / FindReplace / FindNext / …)`，键位 Ctrl+F / Ctrl+H |
| 键位 | `State.Bindings []Binding`，`{Mods, Key, Command: editor.NoCommand}` 可把键从编辑器拿走 |
| 其他 | `OnInput(func(ui.InputEvent) bool)` 接管按键、`LineBackgrounds`、`GutterMarks`、`GoTo`、`ScrollTo`、`SetDiagnostics`、`ShowHover` |

关键冲突：非 mac 键位表里 **Ctrl+Enter = NewlineBelow**（`ui/editor/keymap.go`），与我们的「执行选中」冲突；
Ctrl+S / F5 未被占用。Ctrl+Space 内建为显示补全。

## 目标 / 非目标

**目标**

1. **A1**：主编辑器换成 `ui/editor`。SQL 高亮、行号、查找替换、折叠、多光标来自库；「仅执行选中 SQL」、
   控制台正文/光标持久化、工作区标签切换、结果区等既有行为**完全不变**。
2. **A2**：补全接入 —— SQL 关键字/函数/类型（静态词表）+ 当前激活控制台数据源的表/视图名（复用引擎
   `Objects` 枚举，带缓存），打字自动弹、Ctrl+Space 显式弹。
3. **A3**：列名补全 —— `表名.` 之后列出该表的列（engine 新增列目录能力；sqlite `PRAGMA table_info`，
   mysql `information_schema.columns`）。
4. 设置窗口保留 `3k/12k/100k` 演示预设（性能自测），去掉已无意义的高亮策略与 HUD，改为编辑器信息。
5. 重建性能基线：旧的 spike matrix（FF/FV/VV 策略）随自制 tokenizer 一起退役，改测 `ui/editor` 在
   3k/12k/100k 行下的键入/滚动/首帧/补全弹层开销，限值先测后定。

**非目标（本期明确不做）**

- SQL 语义分析：别名（`FROM t x` → `x.col`）、CTE、子查询作用域、`*` 展开、JOIN 推断。
  A3 只做「光标前的 `标识符.` 恰好等于已知表/视图名」。
- 补全排序智能（相关度）——交给编辑器的模糊打分；我们只给候选与 `Kind`。
- hover / diagnostics / LSP / AI 建议 / 格式化（M7 其余项）。
- 编辑器的 `FindReplace` UI 定制、主题定制（用默认）。
- 撤销历史跨重启保留（仍按控制台内存内）。

## 方案与被否决项

- **A（选定）**：迁移 `ui/editor`，三阶段：A1 编辑器、A2 关键字+表、A3 列名。
- **B 保留自制编辑器，自叠补全弹层**：否决。`ui.TextArea` 不暴露光标屏幕坐标，弹层位置只能靠
  行高/字宽估算（换行、CJK、缩放即错位）；过滤、接受键、Esc、粘贴边界全要自写，成本接近 A 而质量更低。
- **C 只做关键字补全**：否决。仍需要弹层，等于 B 的子集。

## A1：编辑器迁移

### 新增 `ui/code.go`（编辑器封装层）

```go
// codeEditor 包住 ui/editor，负责「每控制台一个 Buffer」与 rune/字节换算。
type codeEditor struct {
    state   *editor.State
    buffers map[string]*editor.Buffer // consoleID → Buffer（撤销历史各自保留）
    active  string
}

func newCodeEditor() *codeEditor
func (c *codeEditor) SetActive(consoleID, text string) // 取/建 Buffer（Reset 到 text）、SetLanguage(SQL)、state.Buffer = 它
func (c *codeEditor) ActiveBuffer() *editor.Buffer
func (c *codeEditor) Text() string                     // Buffer.String()（控制台惰性来源用）
func (c *codeEditor) SetText(text string)              // Reset（不用 Set：切换控制台不产生撤销步）
func (c *codeEditor) Version() uint64
func (c *codeEditor) Selection() (start, end int, hasSel bool) // 字节偏移
func (c *codeEditor) SetCaret(startRune, endRune int)          // rune → 字节后 Select()
func (c *codeEditor) CaretRunes() (start, end int)             // 字节 → rune（持久化用）
func (c *codeEditor) Insert(text string)
func (c *codeEditor) SelectedText() (string, bool)
func (c *codeEditor) Find()  // 打开查找栏
```

- 语言：`editor.LanguageNamed("SQL")`（库内建 SQL tokenizer 含关键字/类型/字符串/注释，`--` 与 `/* */`）。
- rune↔字节换算：`Buffer.Offset(Pos{...})` 不可直接换算 rune；实现 `runeOffset(text, runeIdx) int` 与
  `runeIndex(text, byteOff) int`（前者遍历前缀，后者用 `utf8.RuneCountInString(text[:byteOff])`）。
  光标在每次激活/切换时换算一次，不逐帧。
- 每控制台一个 `*editor.Buffer`：切换零拷贝，撤销历史互不串。控制台删除时丢弃对应 Buffer。

### 键位与执行

`State.Bindings`：

```go
{Mods: ui.Ctrl, Key: ui.KeyEnter, Command: editor.NoCommand}, // 交还窗口做「执行选中」
```

窗口层仍用 `c.Shortcut(ui.Ctrl, ui.KeyEnter)` / `KeyF5` 触发 `execPending`；因为编辑器已把该键拿走，
不会插入换行。`Ctrl+S`（编辑器未占用）→ `flushActive()`。

「仅执行选中」：`SelectedText()` 为空则按钮禁用且快捷键不执行（语义同现在）。

### root 更换接线

- 删字段：`ed *editor`（自制）、`findQuery/findActive`、`sampler`（`Sampler` 已无人用，随 `metrics.go` 删除）。
- 新增：`code *codeEditor`。逐帧：`a.selStart, a.selEnd = a.code.Selection()`（字节）→ 执行按钮的
  「有选区」判定用 `hasSel`。
- `activateConsole`：`a.code.SetActive(rec.ID, text)` + `a.code.SetCaret(caretStart, caretEnd)`（rune）+ `ScrollTo(caret 所在行)`。
- `syncActiveBuffer`：`ver := a.code.Version()`；`SetTextSource(func() string { return a.code.Text() })`（不变）。
- 设置窗口：删除 HUD/策略；显示 `行数=N 字节=M 版本=V`；预设按钮改为 `a.code.SetText(buildSQL(n))` + flush。
- 工具行：「查找」按钮 → `a.code.Find()`（保留「查找」，`清除` 删除 —— 查找栏自带清除）。

### 删除（A1 一并做）

`ui/editor.go`、`ui/findmatch.go`、`ui/metrics.go`、`highlight/` 整包（含测试与基准）、
`ui/bench_test.go`、`ui/editor_test.go`、`ui/findmatch_test.go`、`ui/fixtures_test.go`、`ui/metrics_test.go`。

**保留 `buildSQL`**：它现在住在 `ui/fixtures.go` 里，但设置窗口的 3k/12k/100k 预设还要用 —— 移到新的
`ui/preset.go`（只留 `buildSQL`），随 `fixtures.go` 一起删掉的部分只有测试假数据。

### 测试与基准策略

- 既有行为测试：把对自制编辑器的操作换成新 API（`a.ed.Buf.Set(text)` → `a.setText(text)`；
  `a.selStart/selEnd` 赋值 → `a.setSelection(startRune, endRune)`），**断言不变**；13 个测试文件受影响，
  A1 内一次改完。
- 新增 `ui/code_test.go`：rune↔字节换算（ASCII/中文/emoji/换行）、每控制台 Buffer 互不干扰、
  `Ctrl+Enter` 触发执行而不是换行（headless 键事件断言）、`SetText` 不产生撤销步而打字产生。
- 新增 `ui/perf_test.go`：3k/12k/100k 行下「键入字符」「滚动一屏」「首帧构建」「补全弹层打开时键入」
  四项 p50/p99；限值用**实测值 ×1.5** 写入常量，并把实测数字记到 `docs/spike-results.md` 新章节
  （旧的 FF/FV/VV 章节标注「自制编辑器时代，已退役」）。
  测「键入」时把 `State.OnCompletion` 置 nil（隔离补全开销）；「补全弹层」一项才接空回调。

## A2：关键字 + 表/视图补全

### 数据

- 静态词表 `ui/sqlwords.go`：关键字（SELECT/FROM/WHERE/JOIN/…）、函数（COUNT/SUM/NOW/…）、
  类型（INT/TEXT/VARCHAR/…），每条带 `editor.CompletionKind`；启动时构建一次，按 Label 去重。
- 目录缓存 `ui/catalog.go`：

```go
type connCatalog struct {
    loadedAt time.Time
    tables   []string
    views    []string
    err      string
}
type catalogCache struct {
    mu      sync.Mutex
    byConn  map[string]*connCatalog // connID → 目录
    loading map[string]bool
}
```

- 取数：`sess.Objects(ctx, engine.ObjectTable/ObjectView)`（引擎已有；会话是当前唯一活动会话，
  第一次需要时若未连接则不取，只给关键字）。后台 goroutine + `win.Update` 回来；同一连接同时只拉一次。
- 失效：TTL 5 分钟；`openConn` 成功后清该连接缓存；`removeConn` 删缓存。

### 接线

```go
a.code.state.OnCompletion = func(r editor.CompletionRequest) {
    items := a.completions(r)           // 静态词 + 缓存目录（不含慢查询）
    if len(items) > 0 {
        a.code.state.ShowCompletions(r.Offset-len(r.Prefix), items)
    }
}
```

- `completions` 同步组装（缓存命中时纯内存）；缓存未命中 → 返回静态词 + 触发后台加载（下一次按键就有表名）。
- **组装结果按（connID, 目录版本）缓存**：每次按键都重建 `[]Completion` 会在表/列多时产生可观的分配；
  缓存一个不可变切片复用，目录刷新（TTL/重连）时递增版本重建。
- `Prefix` 由编辑器过滤/排序；我们返回全量候选（表/视图通常几十到几百，量级安全）。
- `Detail`：关键字留空；表 `Detail: "表"`、视图 `"视图"`；`Kind` 分别用 `CompletionModule`/`CompletionType`。
- 显式 `Ctrl+Space`（`Explicit`）时若目录未加载，同样先给关键字并后台加载，加载完 `win.Update` 里
  再触发一次 `ShowCompletions`（光标位置用当前 `Cursor()` 重算前缀）。

### 测试

- 词表：内容非空、无重复 Label、kind 正确。
- 目录：假会话返回表/视图 → 补全列表包含；缓存命中不再调用会话；TTL 过期重新拉；连接未打开只给关键字；
  会话报错不弹错（只给关键字）。
- UI：headless 断言 `ShowCompletions` 后完成列表里出现表名（用 `ui.Tester` 的 `Texts()`）；Ctrl+Space 显式触发。

## A3：列名补全

### engine 扩展（可选能力）

```go
// engine/session.go
type Column struct {
    Name     string
    Type     string
    Nullable bool
    PrimaryKey bool
}
// ColumnLister 是可选能力：按表名列出一个表的列。
type ColumnLister interface {
    Columns(ctx context.Context, table string) ([]Column, error)
}
const CapColumns Capabilities = 1 << iota // caps.go 追加
```

- sqlite：`PRAGMA table_info(<quoted table>)`（`name/type/notnull/pk`）。
- mysql：`SELECT column_name, column_type, is_nullable, column_key FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ordinal_position`。
- 驱动在 `Caps()` 里带 `CapColumns`。

### 补全上下文

- 从光标往前取 `ident.`（`[A-Za-z0-9_$"\`]`）：命中已知表/视图名 → 只给该表列（`Kind: CompletionField`,
  `Detail: 列类型`）+ 关键字；`ident` 不认识 → 关键字 + 表/视图名。
- 列缓存：`byConn[conn].columns[table]`，懒加载 + 同一个 TTL；`loading` 去重。
- 明确不做别名解析（非目标），文档里写明。

### 测试

- driver：sqlite 建表 → `Columns` 断言名称/类型/主键；mysql 无容器则 `t.Skip`。
- 上下文解析：`SELECT * FROM users WHERE users.` → 列补全；`foo.` → 表名补全；无 `.` → 关键字+表名。
- 能力位降级：不带 `CapColumns` 的会话 → 不请求列、只给关键字+表名。

## 风险与缓解

1. **Ctrl+Enter 冲突**：编辑器默认 `NewlineBelow`。缓解：`NoCommand` + 窗口级快捷键，并有 headless 测试断言
   「按 Ctrl+Enter 产生执行、正文不变」。
2. **光标 rune↔字节**：这是已上线持久化数据的语义，换算错会跳错位置。缓解：专门单测（中文/emoji/换行/越界夹紧），
   且只在激活时换算一次。
3. **性能基线重定**：`ui/editor` 的布局与 tokenizer 与旧实现不同，首帧可能更慢（一次性 tokenize 视口）。
   缓解：先测后定限值，记录原始数字；不为了「好看」把限值放宽到无意义。
4. **测试面大**：13 个测试文件引用旧编辑器。缓解：A1 一次改完，评审按「行为不变」验收；机械替换集中在
   `setText/setSelection` 两个 helper。
5. **补全数据陈旧**：目录缓存 5 分钟 TTL，新建表要等过期或重开连接才出现。缓解：`openConn` 清缓存 +
   A2 在设置窗口加「刷新补全缓存」按钮（一行代码，顺手做）。
6. **列名补全噪声**：无别名解析时 `t.` 才出列名，用户可能期望 `x.` 也出（如果写了别名）。缓解：文档写明限制，
   后续再做别名/语句级解析。

## 分阶段验收

| 阶段 | 交付 | 验收 |
|---|---|---|
| **A1** | 主编辑器换成 `ui/editor`；旧 highlight/自制编辑器删除；设置窗口去 HUD/策略 | 既有全会话/控制台/结果测试改造后全绿；runes↔字节与新基线测试绿；`docs/spike-results.md` 记录新数字与限值 |
| **A2** | 关键字+表/视图补全、缓存、Ctrl+Space、设置窗口「刷新补全缓存」 | 打字与 Ctrl+Space 都能看到候选；跨连接/重连缓存正确；无连接只有关键字 |
| **A3** | engine 列目录 + `表名.` 列补全 | sqlite 真库测试；mysql 无容器 skip；`users.` 出列名，`foo.` 出表名 |

## 对现有代码的改动面

| 文件 | A1 | A2 | A3 |
|---|---|---|---|
| `ui/code.go` | 新增（封装/换算/键位） | 接 `OnCompletion` | 列上下文 |
| `ui/root.go` | 换编辑器接线、删字段、`selStart/selEnd` 来源 | — | — |
| `ui/settings.go` | 去 HUD/策略、加信息与预设改写 | 加「刷新补全缓存」 | — |
| `ui/sqlwords.go` / `ui/catalog.go` / `ui/completion.go` | — | 新增 | 扩展 |
| `ui/editor.go`、`ui/findmatch.go`、`ui/fixtures.go`、`ui/metrics.go`、`highlight/*`、旧测试与基准 | 删除 | — | — |
| `ui/*_test.go`（13 个） | 机械改造 | 新增补全测试 | 新增列测试 |
| `engine/session.go`、`engine/caps.go`、`driver/sqlite/sqlite.go`、`driver/mysql/mysql.go` | — | — | 新增 `ColumnLister`/`CapColumns` |
| `docs/spike-results.md`、`Readme.md` | 新基准与功能清单 | 补全说明 | 列补全说明 |

## 留给实现者的细节（本人按此定，评审时可改）

- 每控制台一个 `*editor.Buffer`（切换零拷贝、撤销独立）；控制台删除时丢弃。
- 切换控制台用 `Buffer.Reset(text)`（不产生撤销步）；用户打字才进撤销栈。
- 光标持久化仍存 **rune** 偏移（兼容已上线数据），边界换算；超出长度夹紧。
- `State` 的 `LineNumbers`/`TabSize`/`Indent`/`Theme` 都留零值（库默认：绝对行号、SQL 的 2 空格、跟随窗口主题）。
- 补全列表上限不做硬截断，靠编辑器模糊过滤；若目录很大导致卡顿再引入按前缀预算（记入风险）。
- 目录 TTL 5 分钟；`openConn` 成功后清该连接缓存。
- 设置窗口信息行：`行数=N 字节=M 版本=V`，预设按钮保留 `3k/12k/100k`。
- 不保留 `highlight/` 的兼容层；`go.mod` 不新增依赖。
