# db-g 架构审视与调整方向

日期：2026-11-27
范围：`ui` / `app` / `engine` / `driver` / `ai` / `sync` 的分层、状态归属、并发、错误与本地化、
驱动契约、持久化与测试工程。只描述问题与方向，不含实现。

## 0. 基线

| 包 | 生产文件 | 生产行数 | 测试行数 | 备注 |
|---|---:|---:|---:|---|
| `ui` | 64 | 20,576 | 13,920 | 扁平包；`root` 277 个字段、547 个方法 |
| `app` | 35 | 7,717 | 4,824 | `conn.go` 单文件 1,307 行 |
| `ai` | 5 | 1,218 | 543 | 纯叶层，分层干净 |
| `engine` | 8 | 408 | 150 | 纯叶层，分层干净 |
| `sync` | 5 | 1,115 | 193 | 纯叶层，分层干净 |
| `driver/*` | 12 | 3,349 | — | 4 个驱动，方法面高度同构 |
| `locales` | 1（生成） | 9,835 | 87 | `catalog_gen.go`，生成物 |

整体判断：**跨包分层是守住的**（`app` 不 import `ui`；驱动只 import `engine`；`ai`/`sync`/`engine`
没有被反向污染）。主要问题集中在 **`ui` 包内部的组件边界、`root` 的状态归属、并发约定，
以及 4 个驱动之间的大量同构代码**。下面按风险排序。

---

## P0：正确性与并发

### ARCH-01 后台 goroutine 直接改 UI 状态（真实数据竞争）

**证据**
- `ui/profilexfer.go:54-68`：`go func()` 里调用 `a.exportProfiles(...)`（读 `a.store`）；
  `ui/profilexfer.go:73-87` 调用 `a.importProfiles(...)`，而后者在 `ui/profilexfer.go:44`
  调用 `a.reloadTree()`；`ui/objecttree.go:83-101` 的 `reloadTree` 直接清空/替换
  `a.treeData`、`a.treeCounts`、`a.treeLoading`、`a.treeSchemas`、`a.metaCache` 等 UI 字段。
- `ui/settings.go:750-768`：`go func()` 里调用 `a.importThemeFrom(...)`，后者经
  `ui/theme_prefs.go:38-62` 的 `setThemePrefs` 修改 `a.themePrefs/themeHex/themeWarn`，
  调用 `updateSettingsWindow()`、`win.SetBackgroundColor()` 和 `safeUpdate`。
- `ui/settings.go:776-788` 同样在后台线程走 `importThemeFrom`。

**影响**：违反 `AGENTS.md` 的「构建 UI 只在主线程」约定。`root` 字段无锁，后台写与视图读并发，
轻则状态丢失，重则 map 并发读写 panic。`-race` 能抓现行。

**调整方向**
1. 把这类操作拆成「纯工作」和「落地」两段：文件读写/解析返回数据，`safeUpdate` 回调里再改
   `store`/`root`。`importProfiles` 里的 `a.store.Add()` 可以留在后台（`ConnStore` 有锁），
   但 `reloadTree`/`setThemePrefs` 必须在 UI 线程。
2. 引入 `a.async(...)` 统一封装（见 ARCH-16），从根上禁止裸 `go func` 里出现 `a.<字段>`。
3. 加回归测试：用 `bgRun` 注入「工作直接同步执行、apply 延后」的模式，配合 `go test -race`。

**验收**：`go test -race ./ui` 通过；`rg 'go func' ui` 的闭包里不再出现 `a.reloadTree|a.setThemePrefs|a.<字段> =`。

---

### ARCH-02 `Session.Cancel` 是会话级单槽，取消可能打错语句；并发契约缺失

**证据**
- `engine/session.go:24-31`：`Session` 有 `RunStatement(ctx)` **和** `Cancel() bool` 两套取消机制。
- 四个驱动都是 `s.curCancel atomic.Pointer[context.CancelFunc]` 单槽，例如
  `driver/postgres/postgres.go:29,101-108,129-134`、`driver/mysql/mysql.go:29,108,130-135`、
  `driver/clickhouse/clickhouse.go:29,122,145-149`、`driver/sqlite/sqlite.go:31,87-93,102-105`。
- `RunStatement` 由 `s.mu` 串行化；不同控制台可以共享同一连接的同一个 `Session`
  （`ui/sessions.go:22-31` 的 `sessionFor`），于是 B 控制台排队时会把 `curCancel` 覆盖成 B 的；
  A 点取消会取消 B（或当前正在跑的那条），而不是 A 自己提交的语句。
- `engine` 没有写明「同一 Session 并发调用是否安全、Close 与运行中语句的关系」。

**调整方向**
- 取消只保留一条路径：调用方持有 `context.CancelFunc`（`app.Console.RunTimeout` 已持有），
  删除 `Session.Cancel()`；若某些驱动需要服务端取消，改成 `RunStatement` 返回可取消的
  run handle（`Run(ctx) (Result, CancelFunc, error)` 之类），取消对象与语句一一对应。
- 在 `engine/session.go` 顶部写清并发契约，并用 fake session 测「同连接两个并发执行，取消 A
  不影响 B」「Close 等待运行中语句」。

**验收**：两个控制台共享连接时取消只影响目标语句；`rg '\.Cancel\(\)'` 不再命中 `engine.Session`。

---

### ARCH-03 结果预算放在 `engine` 全局原子变量

**证据**
- `engine/limits.go:13-31`：包级 `resultBudgetBytes atomic.Int64` + `SetResultBudgetMB`。
- `ui/root.go:355`（`newRoot` 内）每次构造 root 都把全局预算重置为默认值；
  `ui/root.go:388` 再按 `results.json` 设置一次；所有驱动在读取结果时调用
  `engine.ResultBudgetBytes()`。

**影响**：这是进程级可变状态。多个 root（测试、未来的多窗口/多工作区）会互相覆盖；
测试构造 root 会悄悄改掉别的测试正在用的预算；预算也无法按连接区分。

**调整方向**
- 复用已有的 per-query context 机制：`engine` 已有 `WithPageSize` / `WithProgress`
  （见 `app/console.go:132-134`），增加 `WithResultBudget(ctx, bytes)`；`app.Console.RunTimeout`
  写入本次预算，驱动从 ctx 读，默认值仍是常量。
- 删除 `SetResultBudgetMB` 与包级原子变量；`ui` 不再 import 一个可变全局。

**验收**：同进程两个不同预算的执行互不影响（fake driver 从 ctx 断言）；`rg 'SetResultBudgetMB'` 为空。

---

### ARCH-04 错误呈现通道混用：`consoleErr` 成了垃圾桶

**证据**
- `root.consoleErr` 有 44 处写入（`rg 'a\.consoleErr = ' ui`），包括设置窗口里的
  `ui/profilexfer.go:62,65,84,87`、主题导入、分页、计划、AI 补全等。
- 另有 12 个彼此独立的 `*Warn`/`*Err` 字段：`fontPrefsWarn`、`resultPrefsWarn`、
  `consolePrefsWarn`、`themeWarn`、`localeWarn`、`aiWarn`、`syncWarn`、`keymapWarn`、
  `connFormErr`、`installErr`、`exportStatus`、`connTestMsg`（`ui/root.go:50-101,135-169,257-260`）。
- `toastError` 只有 10 处调用。

**影响**：在设置窗口/新建连接等弹窗场景里，后台失败会写到**主窗口顶栏**（用户看不到或看到不相关
位置的错误），与 `AGENTS.md`「后台/非当前控制台的错误用 toast」相悖；同一个失败可能同时更新
`consoleErr` 和某个 `*Warn`。

**调整方向**
- 明确三层：当前控制台执行错误 → `consoleErr`；当前面板内操作 → 面板内联提示（沿用
  `connTestMsg` 模式）；跨上下文后台错误 → `toastError`。
- 用一个 helper `a.reportActionError(scope, err)` 统一决策；`*Warn` 收敛成
  `settingsWarn map[string]string` 或放进对应的子状态（见 ARCH-06）。
- 行为变化：设置窗口内的导入/导出失败只出现在设置窗口或 toast，顶栏不动。需要用户确认。

**验收**：headless 测试断言「设置窗口导入档案失败时顶栏文本不变、toast 出现」。

---

### ARCH-05 本地化边界：驱动/引擎里的中文错误与按字面量比较的标题

**证据**
- 用户可见中文错误散落在驱动与引擎：`driver/mysql/mysql.go:41,354,361,664,676,705`、
  `driver/postgres/postgres.go:444,492,526`、`driver/clickhouse/clickhouse.go:133,453,474,492`、
  `driver/sqlite/sqlite.go:79,139,524,529`、各 `stats.go`、`engine/params.go:21`。
- `ui/ai_chat.go:632`：`conv.Title == "新会话"` 判断是否自动命名；英文界面下默认标题是
  `New chat`，该判断失效；且持久化标题与界面语言耦合。

**影响**：`en` 界面会出现中文错误（违反 `AGENTS.md` 源语言 `en` 的约定）；中文硬编码标题比较
在切语言/英文构建下是逻辑 bug。

**调整方向**
- `engine`/`driver` 只返回**哨兵错误或结构化错误码**（如 `engine.ErrNoDatabaseSelected`），
  由 `app`/`ui` 映射到 `locales`；`engine` 保持只依赖标准库。驱动对外部数据库返回的原始错误
  继续原样展示。
- 会话标题加 `TitleAuto bool`（或记录「用户是否手工改过」），不再与任何语言的默认文案比较。
- `tools/l10nscan -all` 输出作为迁移清单。

**验收**：`MYGO_LOCALE=en go test ./ui -run TestI18nEnglish` 覆盖错误路径；`l10nscan -all`
不再列出 `engine`/`driver` 的用户可见中文；标题在英文/中文切换后仍能自动更新。

---

## P1：结构性问题（决定后续改动成本）

### ARCH-06 `root` 是上帝对象：277 字段、547 方法、横跨十余个业务域

**已完成（2026-11-27）**：root 直接字段 277 → 42。分组：
`connForm`、`exportState`、`gitSync`、`install`、`cellView`、`tree`、`grid`（ARCH-07）、
`settings`、`tabs`、`ai`、`edit`、`modal`、`session`、`external`、`toasts`。
每组都用 gofmt 结构重写迁移引用，并有编译期的状态归组测试。
剩余 42 个是真·跨域单例（编辑器、窗口、存储、布局、会话管理器等），不再按业务域拆。

**证据**：`ui/root.go:22-304`。字段按前缀粗分：

| 领域 | 代表字段 |
|---|---|
| 新建/编辑连接表单 | `connForm*`（20+ 个） |
| 连接、对象树、元数据缓存 | `tree*`、`schema*`、`conn*`、`metaCache*` |
| 控制台/工作区/结果标签 | `consoles`、`activeConsole`、`consoleMRU`、`stmtRuns`、`runsByConsole` |
| 网格/结果/表视图 | `result*`、`lastResult`、`resView`、`widths*`、`grid*` 快照 |
| 编辑集/内联编辑 | `edit*`、`inlineEdit*` |
| 单元格查看器/JSON 树 | `cellView*`、`cellModal*`、`preview*` |
| 导出/安装/外部文件 | `export*`、`install*`、`ext*` |
| 设置/主题/字体/键位/语言 | `theme*`、`palette*`、`font*`、`resultPrefs*`、`consolePrefs*`、`locale*`、`chords` |
| AI | `aiChat`、`aiFix`、`aiCompletion`、`aiLint*`、`aiDDL`、`aiRelations`、`aiTokenScale` |
| Git 同步 | `sync*` |
| 弹窗/对话框 | `tabModal*`、`folderModal*`、`reload*`、`guard*`、`plan` |

一次性构造整个 root 的开销与耦合，导致任何 UI 测试都要拖上 Ai、同步、安装、主题等全部状态。

**调整方向（两步走，先机械后重构）**
1. **只做分组，不改行为**：把上述字段收进 `connFormState`、`treeState`、`consoleTabsState`、
   `gridState`、`cellViewState`、`exportState`、`settingsState`、`aiState`、`syncState`、
   `installState`、`extFileState` 等结构体，`root` 只保留这些子结构与少量跨域字段。
   这一阶段不移动文件、不改方法签名，风险最低。
2. 再把**无 mygo 依赖的逻辑**从这些子状态里抽到 `app`（见 ARCH-09），`ui` 只剩渲染与输入。

**验收**：每步 `go build ./... && go test -p 1 ./...` 通过；分组后 `root` 直接字段数降到 ~40。

---

### ARCH-07 网格状态靠 `stash/load` 在 root 与控制台/表视图之间搬

**已完成（2026-11-27）**：`gridSnap` 改为 `gridState`，控制台与每个 `viewerTab` 各持一份
`*gridState`；`root.grid` 指向当前展示的网格，切换标签只换指针；`stashGrid`/`loadGrid` 已删除。
新增 `TestGridStateSwitchingKeepsIndependentState` 覆盖独立状态与切回恢复。

**证据**
- `ui/grid.go:11-59`：`gridSnap` + `stashGrid`/`loadGrid` 逐字段复制 10 个 root 字段。
- 这些字段仍散落在 `root`：`resultState`、`resultSel`、`resultRow`、`resView`、`lastResult`、
  `widthsFor`、`widths`、`editKey`、`editPlan`、`readonlyReason`（`ui/root.go:31,211-225,284-285`）。
- `ui/grid.go:61-128` 的 `gridRuntime/gridSession/gridSQL/gridKind/gridConnID` 全部靠
  `activeViewerTab()` 分支。

**影响**：切换标签是「搬指针 + 复制值」的隐式单例；新增网格状态必须同时改 `root`、`gridSnap`、
`stash/load`，容易漏字段；编辑集按 `gridRuntimeID` 分桶的机制与网格状态分离。

**调整方向**：把网格的全部可持久 UI 状态收成一个 `gridState`，控制台运行态与 `viewerTab` 各持有
一份，`root` 只保留 `activeGrid *gridState`（或一个解析函数）。`stash/load` 退化为改一个指针；
`grid*` 访问器变成 `activeGrid` 上的方法。与 ARCH-06 的分组一起做。

**验收**：切换控制台/表视图后排序、过滤、列宽、编辑集、选区都不丢；`rg 'stashGrid|loadGrid'` 为空。

---

### ARCH-08 `view()` 在渲染过程中改状态，`pending*` 字段是临时补丁

**进度（2026-11-27）**：已完成 `frameStart()` + `deferred []func()` 队列：
`pendingClose/pendingSwitchWS/pendingActivate/pendingActivateViewer/pendingAppend/pendingExec`
与 `execPending` 全部删除，调用方改用 `deferUI`/`deferActivateConsole`/`deferActivateViewer`/
`deferCloseConsole`/`deferSwitchWorkspace`；`view` 的 editor 闭包不再执行 `runSQL`。
队列有 FIFO + 执行中追加排空的单测。弹窗改为 `modalRegistry`：`newRoot` 按原顺序注册，
`view` 只调 `a.modals.draw(c)`；新增弹窗只需注册，不再改 `view`。

**证据**：`ui/root.go:826-900`：每帧开头执行 `maybeOpenSettings`、`syncActiveBuffer`、
`applyEditorLanguage`、`pollExternalFiles`，并依次消费 `pendingClose`、`pendingSwitchWS`、
`pendingActivate`、`pendingActivateViewer`、`pendingAppend`、`pendingExec`；
`a.execPending` 甚至在 `editor` 渲染闭包里执行 `a.runSQL()`（`ui/root.go:918-921`）。
此外每帧无条件调用 ~30 个 `xxxModal(c)` 来问「要不要画」。

**影响**：视图构建与状态迁移耦合，任何「不能在构建中途改」的操作都要新加一个 `pending*` 字段；
测试只能通过整帧驱动，难以单测状态迁移；`rootView` 的 panic 兜底只能掩盖问题。

**调整方向**
- 保留一个显式的 `frameStart()`，把上述动作集中进去；把 `pending*` 合并成一个
  `deferred []func()` + `a.deferUI(fn)`（帧首统一执行、可断言顺序），不再一操作一字段。
- 弹窗改为注册表 `[]func(*ui.Context)`（或带 `active bool` 的结构），避免每加一个弹窗就改 `view`。
- 不要求 `view` 变成纯函数；目标是「所有状态迁移只发生在帧首/事件回调，渲染分支只读」。

**验收**：新增一个「下一帧激活某标签」的需求时不需要新的 root 字段；`ui/root.go` 的 `view` 降到
结构布局 + 弹窗注册。

---

### ARCH-09 `ui` 扁平包把纯 SQL 逻辑和视图绑死

**进度（2026-11-27）**：新增纯叶包 `sql/`（只用标准库）：
- `scope.go`：Tokenize/StatementScope/Relations/AliasMap/RelationPositionAt（含原 243 行测试）。
- `ident.go`：IsIdentStart/IsIdentPart/FirstWord/IsSQLWord。
- `dotident.go`：DotIdent（原 `completioncontext.go`）。
- `words.go`：通用/各方言词表、DialectTermMap/DialectTerms、IsExtraKeyword。
- `preset.go`：BuildSQL（原 `ui/preset.go`）。
`ui/sqlwords.go`/`sqldialect.go`/`sqlhighlight.go` 只保留 mygo 类型映射与语言包装；
`AGENTS.md` 分层表已加 `sql/`。
**剩余**：`metrics`（通用采样器，不属于 SQL）、`resultview` 分页 SQL 生成（依赖 app/engine）
可按后续需要再下沉；mygo tokenizer 包装必须留在 ui。

**证据**：`ui` 下 132 个 Go 文件（64 生产 + 68 测试）同属一个包；其中 11 个**完全不 import mygo**：
`ai_context.go`、`ai_request.go`、`ai_selfcheck.go`、`completioncontext.go`、`metacache.go`、
`metrics.go`、`preset.go`、`resultcopy.go`、`resultview.go`、`run.go`、`sqlscope.go`。
`ui/sqlscope.go` 731 行、`ui/sqlhighlight.go`、`ui/sqlwords.go`、`ui/sqldialect.go` 都是纯词法/
作用域/方言逻辑，却只能用 `ui` 包测试（会拖入 mygo 与 `root`）。

**影响**：补全/高亮/分页/结果视图这些最容易出错、最值得表驱动测试的逻辑，被锁在 UI 包里；
`ui` 包体积也让编译与测试反馈变慢。

**调整方向**
- 新增纯叶包 `sql/`（只用标准库，**不 import mygo/locales/app/engine**），先迁真正无状态的
  `sqlscope`、`sqlwords`、`sqlhighlight` 纯部分、`resultview` 的分页 SQL 生成、
  `completioncontext`、`resultcopy`、`metrics`；`sqldialect` 返回语言无关的 kind 常量，
  文案由 `ui` 映射到 `locales`。
- `run.go`/`metacache.go`/`ai_context.go` 虽然不 import mygo，但方法都挂在 `root` 上，
  要等 ARCH-06 的状态分组完成后再迁，不能先动。
- 迁移顺序：先把源码复制到新包并补表驱动测试，再让 `ui` 调新包，最后删旧实现（不留别名）。
- 需要同步更新 `AGENTS.md` 的分层表（新增叶子包）。

**验收**：`sql` 包的覆盖率与用例数明显高于原 ui 内测试；`ui` 不再包含词法扫描实现。

---

### ARCH-10 `app/conn.go` 一个文件混了四个聚合

**已完成（2026-11-27）**：`app/conn.go` 按聚合拆成 5 个文件（零 API 变化）：
`conn_store.go`（类型/加载/落盘/快照，353 行）、`conn_profiles.go`（159）、
`conn_folders.go`（258）、`conn_consoles.go`（303）、`conn_workspaces.go`（274）。
全部 < 400 行；`go test ./app` 不变。后续若需要再拆成独立 Repo 类型。

**证据**：`app/conn.go:20-1307`：`ConnProfile`/`ConnTag`、`ConnStore`、文件夹树、
控制台元数据、工作区，以及落盘、回滚快照、加密迁移全部在一个文件、一个
`ConnStore` 类型上（60+ 方法）。

**影响**：文件级导航困难；任何控制台改动都要在 1,300 行里找；`ConnStore` 的锁粒度把
连接/控制台/工作区所有读写绑在一起（目前可接受，但无法独立演进）。

**调整方向**
- 先做**文件级拆分**（零 API 变化）：`conn_store.go`（加载/保存/快照/回滚）、
  `conn_profiles.go`（连接与标签）、`conn_folders.go`、`conn_consoles.go`、`conn_workspaces.go`。
- 后续再评估是否拆成 `Store` + `ConsoleRepo` + `WorkspaceRepo`；在 `app` 内部拆类型不影响
  `ui` 调用点（由 `ConsoleManager` 收口）。

**验收**：拆分后 `go test ./app` 不变；单文件 < 400 行。

---

### ARCH-11 数据源元数据散在 8 处 `switch kind`

| 元数据 | 位置 |
|---|---|
| 类型列表/顺序 | `ui/sidebar.go:879` |
| 默认端口 | `ui/sidebar.go:1061-1068,1157-1167` |
| 连接参数提示 | `ui/sidebar.go:1089-1099` |
| 图标/品牌色 | `ui/dbicons.go:51-78` |
| 方言关键词/类型/函数 | `ui/sqldialect.go:21+` |
| URL/JDBC 解析 | `app/connurl.go:12+` |
| 标识符引用/字面量 | `app/export.go:17-40` + 各驱动 `quoteIdent` |
| 分页方言（`count()`） | `ui/resultview.go:38-50` |

新增一个数据源要改 5+ 个文件（`AGENTS.md` 已列出其中 4 步），且没有编译期守卫。

**调整方向**
- 扩展注册表：`engine.Register(kind, engine.DriverMeta{Label, DefaultPort, Protocols,
  ParamsHelpKey, QuoteIdent, Dialect, SupportsPagingCount...}, factory)`。
  `ui` 从 `engine` 读取 label/端口/参数提示/方言；图标与品牌色仍留在 `ui`（纯视觉）。
- `app/connurl.go` 的 URL 解析保留在 app，但默认端口/协议从 `DriverMeta` 取。
- `main_test.go` 的 `TestAllConnKindsLinked` 可扩展为「每个 kind 都有 Meta 且端口/提示完整」。

**验收**：新增一个假驱动时，除图标/颜色外不需要改 `sidebar.go`/`sqldialect.go`/`resultview.go`。

---

### ARCH-12 标识符引用与字面量转义重复实现

**证据**：`app/export.go:18` 的 `QuoteIdent(kind, name)` 与四个驱动的
`quoteIdent`（`driver/sqlite/sqlite.go:313`、`driver/mysql/mysql.go:263`、
`driver/postgres/postgres.go:558`、`driver/clickhouse/clickhouse.go:495`）是同一规则的两套；
`app.QuoteValueFor` 与各驱动的 `*Literal` 也是同一类逻辑。

**影响**：方言规则（反引号/双引号、反斜杠转义）改一处漏一处；导出、编辑集、表视图、驱动内部
DDL 生成各说各话。

**调整方向**：把「如何引用标识符/字面量」变成驱动提供的纯函数（与 ARCH-11 的 `DriverMeta` 同处），
`app` 与 `ui` 通过 `kind` 查表调用；驱动内部改调自己的同一实现，不再各自维护 `quoteIdent`。

**验收**：四个驱动的 `quoteIdent` 只剩 1 份（或由共享函数生成）；`app.QuoteIdent` 有对照测试。

---

### ARCH-13 SQL 词法扫描写了 4 遍

**证据**
- `app/sqlsplit.go:16-176`（切语句 + `skipSQLQuoted` + `hasSQLCode`）。
- `app/sqlformat.go:44-240`（格式化时再扫一遍引号/注释）。
- `app/guard.go:96-125`（高危 SQL 扫描又扫一遍）。
- `ui/sqlscope.go:25-120`（补全用 `tokenizeSQL`，自己的 token 化）。

**影响**：四套实现对“注释/字符串/双写引号/$$ 美元引用/反引号”的处理迟早不一致；
补全作用域与执行切分对同一条 SQL 的边界判断可能不同（已有相关 TODO 在 `TODO.md`）。

**调整方向**：新增 `sql/token.go`（ARCH-09 的同一个包），输出带字节范围的 token；
`SplitStatements`、`FormatSQL`、`risk scan`、补全作用域共用 token 流；各自的“语句级”规则
（触发器 BEGIN…END、软切分）留在调用方。先补现有测试作为行为基线，再替换。

**验收**：四处的引号/注释用例共用一份表驱动测试；`rg 'skipSQLQuoted|tokenizeSQL'` 只剩新包。

---

### ARCH-14 七个偏好文件各自重复实现 Load/Save

**证据**：`app/fontprefs.go`、`app/resultprefs.go`、`app/consoleprefs.go`、`app/themeprefs.go`、
`app/locale.go`、`app/syncprefs.go`、`app/ai.go` 各自重复「ReadFile → 不存在用默认 →
Unmarshal → 本地化错误 → Normalize → MkdirAll → MarshalIndent → 写 `.tmp` → `0600` → Rename」。
`root` 对应地持有 7 组 `path/warn` 字段（`ui/root.go:73-101`）。

**影响**：修一次原子写/错误文案要改 7 处；新增一个偏好就是再抄一份；`ui` 的构造函数因此膨胀。
另外路径来源不一致：`fonts.json`/`theme.json` 恒用 `app.Default*Path()`（真实用户配置目录），
而 `results/console/locale/ai/sync` 用 `filepath.Dir(storePath)`（`ui/root.go:383-400`）。

**调整方向**
- 在 `app` 增加一个泛型/通用 `PrefFile[T]`：`Load() (T, string)`、`Save(T) error`，
  可注入默认值、Normalize、错误文案；AI/Sync 的加密字段保留各自 hook。
- 增加 `app.ConfigPaths`（根目录 + 文件名），`NewRoot`/`newRootWithStore` 从同一个根派生全部路径；
  测试注入临时根即可（同时解决 ARCH-18 的真实配置泄漏）。

**验收**：新增一个偏好文件不超过 20 行；`rg 'os.Rename\(tmp' app` 只剩 1 处。

---

### ARCH-15 可选接口成对出现（scoped/flat），UI 侧 38 处类型断言

**证据**
- `engine` 有 10 对「单 schema / 带 schema」接口：`ObjectLister`/`SchemaLister`、
  `ColumnLister`/`SchemaColumnLister`、`IndexLister`/`SchemaIndexLister`、
  `ObjectDDLer`/`SchemaObjectDDLer`、`ObjectStatter`/`SchemaObjectStatter`
  （`engine/object.go:23-44`、`engine/session.go:55-126`、`engine/stats.go:25-33`）。
- `ui` 非测试代码有 38 处 `sess.(engine.Xxx)`，其中大量成对判断
  （`ui/completion.go:235-263`、`ui/objecttree.go:161-168,689-690`、`ui/stats.go:47-48`、
  `ui/viewer.go:349-350,403-404`、`ui/ai_context.go:379-380`）。
- `AGENTS.md` 要求「UI 靠 Capabilities 位降级」，实际是能力位 + 接口断言混合；
  能力位里还有未写入约定表的 `CapIndexes`。

**影响**：每个驱动要实现两套方法（MySQL/Postgres/ClickHouse 的 flat 版本基本只是
`schemaLocked` 后调用 scoped 版本）；UI 每加一个功能都要写一遍「先试 scoped 再试 flat」；
新增驱动忘实现其中之一时，功能静默消失。

**调整方向**
- 统一为 schema 感知的单接口，`schema == ""` 表示当前 schema；`engine` 提供
  `engine.Columns(sess, schema, table)` 这类解析函数（兼容旧实现，内部回退到 flat 接口）。
- 能力位与接口的对应关系显式化（`CapColumns → ColumnLister` 等），保留 `Capabilities` 作为
  UI 降级依据，类型断言只出现在 `engine` 的适配层。
- 四个驱动删掉重复的 flat 包装方法。

**验收**：`ui` 里的 `.(engine.` 断言降到个位数（只留确实可选的 Stream/Explain/ServerInfo 等）；
新增驱动只实现一套元数据方法。

---

### ARCH-16 异步执行有三套写法，headless 分支 37 处

**证据**
- `rg 'a\.win == nil' ui`（非测试）= 37 处；`rg 'go func' ui` = 40 处；
  `safeUpdate` = 59 处调用（另有定义），`win.Update` 直调 7 处。
- 已有 `root.bgRun` 注入点，但只被 `ui/sessions.go:302` 使用；其余模块各自 `if a.win == nil { ... }`
  + `go func` + `safeUpdate`。

**影响**：每个新异步操作都要复制「headless 同步 / 有窗口异步 / panic 兜底 / 过期结果丢弃」四件事；
漏一处就是测试不可断言或状态竞争（ARCH-01 就是漏了这一层）。

**调整方向**：提供 `a.async(key string, work func() (T, error), apply func(T, error))`：
- headless 时同步执行 `apply`（测试可断言）；
- 有窗口时起 goroutine，`safeUpdate` 回投；
- 内建 `key + seq` 过期丢弃与 `recover`；成功/失败统一进 ARCH-04 的错误通道。

先从连接测试、元数据加载、档案导入、主题导入这四个模块迁移，再逐步替换其余 `a.win == nil`。

**验收**：`rg 'a\.win == nil' ui` 降到个位数；所有 `go func` 都在 helper 内。

---

## P2：工程与验证

### ARCH-17 CI 与本地验证口径不一致

**证据**：`.github/workflows/ci.yml` 跑 `go test -short -p 1 ./...`；`Makefile` 的 `make test`
跑全量；性能基准依赖 `-p 1`（`Makefile` 注释与 `ui/perf_test.go` 的预算）。CI 没有 `-race`。

**调整方向**
- 增加 `make ci`（gofmt + vet + mygo vet + l10ncheck + `-short -p 1`），文档统一入口。
- 增加一个轻量 `-race` job，只跑 `ui` 的并发相关测试（ARCH-01/02/16 的回归）。
- 性能基准保持本地跑；CI 只跑功能测试，避免共享 runner 抖动造成假红灯（现状已如此，写清楚）。

---

### ARCH-18 测试隔离：`newRootWithStore` 仍读真实用户配置

**证据**：`ui/root.go:378-430` 的 `newRootWithStore(storePath)`：
- `keymapPath = app.DefaultKeymapPath()`（`:380`）、`fontPrefsPath = app.DefaultFontPrefsPath()`
  （`:383`）、`themePath = app.DefaultThemePrefsPath()`（`:391`）、
  `history = app.OpenHistory(app.DefaultHistoryDir())`（`:414`）—— 全部是真实配置目录，
  与传入的临时 `storePath` 无关；
- `results/console/locale/ai/sync` 才用 `filepath.Dir(storePath)`。
- `ui/restore_test.go:35,57,75`、`ui/sessions_test.go:120` 使用它，因此测试会读开发机的
  `fonts.json`/`theme.json`/`keymap.json`/历史目录；任何断言布局/颜色/字体的测试都可能随
  开发者本地配置变化，甚至触发迁移写盘。

**调整方向**：让 `newRootWithStore` 接收 `app.ConfigPaths`（或配置根目录），所有偏好、历史、
键位、元数据、控制台都从该根派生；测试传 `t.TempDir()`。同时保留 `NewRoot()` 用真实根。
这与 ARCH-14 的路径收敛是同一件事。

**验收**：`newRootWithStore(t.TempDir()+"/connections.json")` 不再访问 `os.UserConfigDir()`
（可用 `HOME`/`XDG_CONFIG_HOME` 指向临时目录后跑测试验证）。

---

### ARCH-19 生成物与契约文档漂移

**证据**：`locales/catalog_gen.go`（9,835 行）、`ui/icons_gen.go`、`ui/lobe/tokens_gen.go`
都是生成物；`AGENTS.md` 的能力位表缺 `CapIndexes`；`engine` 可选接口列表与文档描述不完全一致。

**调整方向**
- CI 增加生成物校验：`go tool mygo generate`、图标生成器重跑后 `git diff --exit-code`；
- 能力位表与代码用同一个测试守住（遍历常量，要求文档/元数据覆盖）。

---

## 建议执行顺序

| 波次 | 内容 | 前置 | 风险 |
|---|---|---|---|
| 1 | ARCH-01、02、03、05 | 无 | 低-中（修真实 bug） |
| 2 | ARCH-04、06、07、08 | 波次 1 | 中（行为微调 + 大范围机械改） |
| 3 | ARCH-09、10、14、16、18 | 波次 2 | 中（抽包/路径收敛） |
| 4 | ARCH-11、12、13、15 | 波次 3，需拍板新包与注册表 | 中-高（跨驱动） |
| 5 | ARCH-17、19 | 无 | 低 |

每波完成后跑：

```bash
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -p 1 ./...
go tool mygo vet . && gofmt -l .
```

## 已经合理、建议保持的部分

- 跨包依赖方向：`app` 不 import `ui`；驱动只 import `engine`；`engine`/`ai`/`sync` 是纯叶层。
- `engine.Register` + `main_test.TestAllConnKindsLinked` 的漏链接守卫。
- 快捷键单表（`ui/keymap.go`）与作用域判断，避免散写 `c.Shortcut`。
- 设置窗口分区表（`settingsSections`）与 `sideNavRow` 的统一导航。
- 会话 LRU（`maxOpenSessions`）、`connectAsync` 单飞、启动不预连；除 `Cancel` 语义（ARCH-02）外设计合理。
- 密码加密、日志脱敏、同步 KCV、外部文件一致性校验等安全/可靠性设计。
- headless tester 的验证策略与性能基准预算。
- `locales` 源语言 `en` + `zh-Hans` 的 catalog 机制本身。

## 决策记录（2026-11-27 已拍板）

1. **新增叶子包**：允许 `sql/`，实施时同步更新 `AGENTS.md` 分层表。
2. **`ui` 拆分边界**：只抽纯逻辑（ARCH-09），不把侧栏/结果/设置拆成子包。
3. **取消语义**：删除 `Session.Cancel`，完全用 ctx；4 个驱动与 UI 调用点一起改。
4. **错误呈现**：设置窗口/后台操作失败改 toast 或面板内联，不再写主窗口顶栏。
5. **驱动注册表**：允许 `engine.Register(kind, meta, factory)`，4 个驱动 `init()` 一并改。
6. **执行节奏**：波次 1 先补失败测试再开工。

**波次 2 进度（2026-11-27）**：

- ARCH-04 完成：设置窗口/偏好错误统一走 `settingsNotice` + toast，不再写 `consoleErr`；
  `keymapWarn`、`fontPrefsWarn`、`resultPrefsWarn`、`consolePrefsWarn`、`themeWarn`、`localeWarn`
  收敛为 `settingsNotice map[string]settingsNotice`；对象预览/建连、控制台创建、标签关闭失败改 toast。
  `consoleErr` 只保留当前控制台的执行/分页/计划/AI 前置错误。

**波次 1 完成（2026-11-27）**：

- ARCH-01：档案/主题导入导出拆成「纯文件工作 + UI 线程落地」；`importProfilesFrom` /
  `importThemeFile` 可单测，后台 goroutine 不再调用 `reloadTree`/`setThemePrefs`。
- ARCH-02：`engine.Session.Cancel()` 已删除，取消只走每条语句的 context；驱动删除
  `curCancel` 单槽，UI 断开前用 `cancelConnRuns` 取消该连接上的运行态。
- ARCH-03：`engine.SetResultBudgetMB`/全局原子变量已删除，预算改为
  `engine.WithResultBudget(ctx, mb)`；`app.Console` 持有预算并在 `RunTimeout` 写入 ctx。
- ARCH-05：新增 `engine.UserError` 错误码 + `app.ErrorText` 本地化映射；驱动/引擎不再拼
  中文用户文案；AI 会话标题改为 `TitleLocked` 标记，加载时迁移旧默认标题，不再比较语言字面量。
