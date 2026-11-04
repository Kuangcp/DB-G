# SQL 控制台 + 工作区（一个数据源 → 多个命名控制台；控制台可关闭/重开）设计

日期：2026-11-04（v2：加入工作区与关闭/重开语义）
状态：待批准（方案 B + 工作区已由用户选定；本文档待过审后进入 writing-plans）

## 背景

db-g 目前只有一条「执行运行态」：`app.Console`（会话、running、结果、取消），编辑器正文是
`ui.NewRoot` 里写死的 3k 行演示 SQL（`buildSQL(3_000)`）。连接只持久化了档案
（`~/.config/db-g/connections.json`）。没有任何「我的 SQL 存在哪、重启后还在不在、开着的标签有哪些」的概念。

对标 db-k（`/home/zk/Work/ai/db-k`，M4 + 工作区）：

- 控制台归属数据源，正文是一份真实 `.sql` 文件，元数据落库，编辑器缓冲区防抖回写，启动恢复正文与光标。
- **工作区是控制台的虚拟分组**：成员关系（`workspaceId → [consoleId]`）决定标签条上显示哪些控制台；
  正文/结果/光标只有一份，跨工作区共享。**关闭控制台 = 从当前工作区移出成员**（文件与元数据保留），
  从数据源侧栏可重新加入并激活。
- 每个工作区记住上次激活的控制台；上次激活的工作区跨重启持久化；零工作区时按需建「默认」工作区。

## 目标 / 非目标

**目标**

1. 每个控制台绑定一份本地 `.sql` 文件，正文即文件内容；重命名不换文件。
2. 控制台元数据（id / 归属数据源 / 名称 / 文件路径 / 顺序 / 创建时间 / 光标选区）持久化，老配置文件向后兼容。
3. 工作区：多个命名分组，成员关系持久化；标签条只显示当前工作区成员；工作区可新建/重命名/删除。
4. 关闭控制台 = 从当前工作区移出（文件保留、元数据保留、同会话内结果保留）；侧栏数据源下可重新打开并激活。
5. 启动时：读元数据 → 恢复上次激活的工作区 → 恢复该工作区上次激活的控制台 → 正文从文件加载 → 光标/选区回位。
6. 编辑自动防抖写回文件；切标签、关闭、执行前、Ctrl+S、退出时强制落盘。
7. 标签条跨数据源展示（带数据源名徽章），`＋` 新建、右键关闭/重命名/删除；标签条右侧工作区切换器。
8. 侧栏每个连接下列出其全部控制台（打开与否两种状态），可新建/打开/关闭/重命名/删除。
9. 每个控制台保留自己最近一次执行结果（切回来/当次会话内关-开结果还在），执行目标 = 该控制台归属的数据源。
10. 删除连接级联删除其控制台元数据、`.sql` 文件与成员关系。

**非目标（本期不做，明确不做）**

- 外部文件控制台（`external`：用户指定的任意路径、文件监听、冲突/缺失处理、另存为）。本期文件一律由应用托管。
- 控制台的执行目标库/schema（db-k 的 `target`，`USE`/`search_path` 前导）。
- 多语句拆分、结果 Tab、执行历史（属路线图 M3 剩余部分，不混入本次）。
- 多连接会话常驻（见「约束与取舍」第 3 条）。
- 文件系统监听（外部编辑器改动不实时同步；重新打开/激活时重新读盘即可）。
- 标签/成员拖拽排序、跨工作区移动控制台的专用 UI（关闭=移出当前工作区；到另一个工作区打开即可）。
- 工作区名字跟随语言（db-g 无 i18n，默认名固定「默认」）。

## 已确认的决策（用户）

| 决策点 | 结论 |
|---|---|
| 控制台粒度 | **方案 B**：一个数据源多个命名控制台 + 顶部标签条 |
| 文件位置 | `~/.config/db-g/consoles/<consoleID>.sql`（应用托管，不写进用户数据库目录） |
| 元数据存储 | 仍用现有 `connections.json`，新增 `consoles` / `workspaces` / `activeWorkspaceID`（不引入 app.db） |
| 工作区 | 要做（虚拟分组，控制台可关闭并从数据源重新打开） |

选区单位（已核实 mygo 源码）：`TextSelection()` / `SetTextSelection()` / `TextBuffer.Slice()` 的偏移
全部是 **rune 偏移**（`TextSnapshot.Slice` 内部走 `rope.ByteOfRune`），因此 `caretStart/caretEnd`
直接存 rune 偏移，与选区/`Slice` 同单位，无需换算。

## 数据模型

```go
// app/conn.go
type ConsoleRecord struct {
	ID           string `json:"id"`            // 4 字节 hex；文件名与它绑定，改名不换文件
	ConnectionID string `json:"connectionID"`
	Name         string `json:"name"`
	FilePath     string `json:"filePath"`      // 绝对路径，派生自 consoles 目录 + id
	SortOrder    int    `json:"sortOrder"`     // 同数据源内顺序（侧栏列表用）
	CreatedAt    int64  `json:"createdAt"`     // Unix 毫秒；与 SortOrder 组成稳定排序
	UpdatedAt    int64  `json:"updatedAt"`     // 仅创建/改名/正文落盘时更新
	CaretStart   int    `json:"caretStart"`    // rune 偏移；0/0 = 从头
	CaretEnd     int    `json:"caretEnd"`
}

type WorkspaceRecord struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	AutoNamed           bool     `json:"autoNamed"`           // 内部标记（本期默认名「默认」）
	SortOrder           int      `json:"sortOrder"`
	CreatedAt           int64    `json:"createdAt"`
	LastActiveConsoleID string   `json:"lastActiveConsoleID"` // 重启回位
	Members             []string `json:"members"`             // 有序列 consoleID = 标签顺序（加入顺序）
}
```

配置文件格式（`connections.json`，老文件缺字段 → 零值，行为等价「还没建过控制台/工作区」）：

```json
{
  "conns": [...],
  "activeID": "连接id",
  "consoles": [ {...ConsoleRecord...} ],
  "workspaces": [ {...WorkspaceRecord...} ],
  "activeWorkspaceID": "工作区id"
}
```

不设全局 `activeConsoleID`：回位由「激活工作区 + 该工作区 `LastActiveConsoleID`」表达
（db-k 也是这个结构），避免两处真相不一致。指向已删除 id 的 `activeWorkspaceID` / `LastActiveConsoleID`
一律按无效处理并回落。写入仍是现有 `save()`：临时文件 + `rename`，失败回滚内存。

## 文件层（新增 `app/consolefiles.go`）

```go
type ConsoleFiles struct{ Dir string }

func (f ConsoleFiles) Path(id string) string          // Dir/<id>.sql
func (f ConsoleFiles) Read(id string) (string, error) // 不存在 → ""（首次/被外部删除）
func (f ConsoleFiles) Write(id, text string) error    // 同目录临时文件 + ATOMIC_MOVE，回落普通写
func (f ConsoleFiles) Delete(id string) error         // 不存在不算错
func (f ConsoleFiles) Exists(id string) bool

// TextSink 是 ConsoleManager 对正文文件的依赖面（唯一目的：测试注入写失败）。
type TextSink interface {
	Read(id string) (string, error)
	Write(id, text string) error
	Delete(id string) error
	Exists(id string) bool
}
```

- 目录 `0700`，文件 `0600`；`Write` 前 `MkdirAll`。
- 目录由 store 路径派生：`ConnStore.ConsolesDir()` = `filepath.Dir(storePath)/consoles`（测试天然隔离）。
- 不把文件 IO 塞进 `ConnStore` 的锁内（避免慢盘阻塞元数据操作）。

## 仓库 API（`app/conn.go` 扩展 `ConnStore`）

```go
// 控制台
func (s *ConnStore) ListConsoles(connID string) []ConsoleRecord  // 按 SortOrder, CreatedAt
func (s *ConnStore) GetConsole(id string) (ConsoleRecord, bool)
func (s *ConnStore) CreateConsole(connID, name string) (ConsoleRecord, error)
func (s *ConnStore) RenameConsole(id, name string) error
func (s *ConnStore) DeleteConsole(id string) error               // 元数据行 + 从所有工作区成员移除；文件由调用方删
func (s *ConnStore) SetConsoleCaret(id string, start, end int)   // 落盘失败记 warning，不中断编辑
func (s *ConnStore) TouchConsole(id string, at int64)            // 正文落盘后更新 UpdatedAt

// 工作区
func (s *ConnStore) ListWorkspaces() []WorkspaceRecord
func (s *ConnStore) GetWorkspace(id string) (WorkspaceRecord, bool)
func (s *ConnStore) CreateWorkspace(name string) (WorkspaceRecord, error) // name 空 → 「默认」
func (s *ConnStore) RenameWorkspace(id, name string) error
func (s *ConnStore) DeleteWorkspace(id string) error            // 控制台实体与文件不受影响
func (s *ConnStore) AddWorkspaceMember(wsID, consoleID string)  // 幂等，追加到末尾
func (s *ConnStore) RemoveWorkspaceMember(wsID, consoleID string)
func (s *ConnStore) SetWorkspaceLastActive(wsID, consoleID string)
func (s *ConnStore) ActiveWorkspaceID() string
func (s *ConnStore) SetActiveWorkspace(id string)                // 落盘失败回滚 + warning
```

- `CreateConsole` 校验：连接必须存在；`name` 去空白，空则回退「控制台 N」（N = 该数据源现有数 + 1，不报错）。
- `DeleteConsole` 清空引用它的 `LastActiveConsoleID`；若它是当前工作区最后成员，激活态回到「无控制台」。
- `Remove(connID)`（现删除连接）签名不变，内部级联删除该连接的控制台行与各工作区成员。

## 状态层（新增 `app/consolemgr.go`）

UI 薄、逻辑可测：控制台集合 + 激活 + 脏标记 + 防抖保存 + 光标 + 工作区成员编排都放 `app`。

```go
type ConsoleManager struct {
	store     *ConnStore
	sink      TextSink      // 生产实现 = ConsoleFiles
	saveDelay time.Duration // 默认 700ms；测试注入 10ms
	dirty     map[string]bool
	lastSaved map[string]string // 忽略自身写入、判断真脏
	cache     map[string]string // 非活动控制台正文缓存
}

func NewConsoleManager(store *ConnStore, sink TextSink) *ConsoleManager

// 工作区
func (m *ConsoleManager) Workspaces() []WorkspaceRecord
func (m *ConsoleManager) ActiveWorkspace() (WorkspaceRecord, bool)
func (m *ConsoleManager) EnsureWorkspace() WorkspaceRecord        // 零工作区时按需建「默认」
func (m *ConsoleManager) CreateWorkspace(name string) (WorkspaceRecord, error)
func (m *ConsoleManager) SetActiveWorkspace(id string) (WorkspaceRecord, bool)
func (m *ConsoleManager) RenameWorkspace(id, name string) error
func (m *ConsoleManager) DeleteWorkspace(id string) error         // 激活态删除 → 自动切到下一个（无则无工作区态）

// 控制台
func (m *ConsoleManager) Consoles() []ConsoleRecord               // 全部（侧栏用）
func (m *ConsoleManager) ConsolesOf(connID string) []ConsoleRecord
func (m *ConsoleManager) Open() []ConsoleRecord                   // 当前工作区成员，按成员顺序（标签条）
func (m *ConsoleManager) IsOpen(id string) bool
func (m *ConsoleManager) Active() (ConsoleRecord, bool)           // 当前展示的控制台
func (m *ConsoleManager) Create(connID, name string) (ConsoleRecord, error) // 建元数据+空文件+加入当前工作区+激活
func (m *ConsoleManager) OpenConsole(id string) (ConsoleRecord, bool)       // 缺则加入当前工作区（末尾）+激活
func (m *ConsoleManager) CloseConsole(id string)                  // 仅移出当前工作区；激活则切下一个成员
func (m *ConsoleManager) DeleteConsole(id string) error           // 元数据+文件+所有工作区成员
func (m *ConsoleManager) Rename(id, name string) error
func (m *ConsoleManager) Text(id string) (string, bool)           // 首次读盘并缓存
func (m *ConsoleManager) SetText(id, text string)                 // 置脏 + 重置防抖
func (m *ConsoleManager) Flush(id string) error                   // 立即写盘（切标签/关闭/执行前/退出）
func (m *ConsoleManager) FlushAll()
func (m *ConsoleManager) Dirty(id string) bool
func (m *ConsoleManager) Caret(id string) (int, int)              // 活动控制台以外读元数据行
func (m *ConsoleManager) SetCaret(id string, start, end int)      // 防抖 1.5s 落库 + 立即更新内存
func (m *ConsoleManager) OnConnectionDeleted(connID string)
```

**语义细节**

- `CloseConsole`：先 `Flush` + `SetCaret`（落盘），再从当前工作区移除成员；若关的是激活控制台 →
  激活同工作区下一个成员（没有则「无激活控制台」态）。**正文、元数据、光标、同会话结果全部保留**。
- `OpenConsole`：已在当前工作区 → 直接激活；否则追加到成员末尾并激活，正文从文件**重新读盘**
  （覆盖缓存，避免外部改动看不到）。
- `DeleteWorkspace`：只删分组与成员关系；控制台变回「两侧都没打开」状态，文件保留。
- `SetActiveWorkspace`：切工作区后按该工作区 `LastActiveConsoleID`（无效则成员最后一个）自动激活控制台；无成员则空态。
- 删除当前工作区且有其它工作区 → 自动切到下一个工作区，并按上一条恢复它的激活控制台。
- 写盘失败不丢内容：`Flush` 失败保留脏标记并返回错误，UI 顶栏红字提示，下次编辑/切换重试。

## 与现有执行态的关系（含一次明确重构）

`app.Console`（运行态）**每控制台一个实例**：`root.consoles map[string]*app.Console`（懒创建），
按 consoleID 保存各自最近一次结果 —— 关闭/重开同一控制台（本次会话内）结果仍在，重启后没有（结果不落盘）。

会话与运行态解耦（重构，必须做，否则「哪个 Console 持有会话」自相矛盾）：

- **会话上提到 root**：新增 `root.sess engine.Session` + `root.sessConnID string`；
  `app.Console` 删除 `sess`/`UseSession`/`SwapSession`/`Session()`，改为
  `Run(sess engine.Session, query string) bool`（无会话返回 false）。
- 现 `a.con.Session()` 的 8 处调用点（root 执行按钮/结果区、sidebar 状态点/连接行、objecttree 展开加载）
  改为 `a.sess != nil && a.sessConnID == p.ID`；`runSQL` 传 `a.sess`。
- 影响测试：`app/console_test.go`、`ui/run_test.go`、`ui/sidebar_test.go` 及 objecttree 相关断言同步更新。
- 标签切换/工作区切换**不**触发连接动作（与左侧树选中解耦，db-k 同语义）。
- 执行时若目标控制台的数据源 ≠ `sessConnID`：`openConn(...)`（关闭旧会话，失败则结果区报连接错误且不执行）；
  执行前先 `Flush(activeID)`。
- `runSQL` 的「必须选区」语义不变。

## 编辑器与逐帧成本（性能守门相关）

编辑器仍**只有一个** `*ui.TextBuffer`；控制台切换 = `ed.Buf.Set(text)` + `ed.dirty = true` + `ed.scroll` 归零，
再由一次性 `SetTextSelection(caretStart, caretEnd)` 回位（自带 scroll-into-view）。

逐帧只做常数级工作，维持 spike 性能包线（100k 行单键中值 31.4ms）：

- `SetText(activeID, text)` **仅在 `ed.Buf.Version()` 变化时**调用（`Buf.String()` 是 O(n) 拷贝，
  每帧无条件调用会直接打穿性能包线）；root 按控制台记 `lastVer`。
- `SetCaret` 仅在 `(selStart, selEnd)` 变化时调用（选区读取 O(1)）。
- 管理器缓存非活动控制台正文；活动控制台**以 TextBuffer 为权威**。

## UI 变更（`ui/`）

**标签条**（编辑器上方，新增 `ui/tabs.go`）：

```
[ ● 控制台 1 × ] [ 查询示例 ] [ + ]                    [ 工作区：默认 ▾ ]
```

- 标签 = 当前工作区成员（跨数据源），脏时名称前置 `●`；数据源徽章（小字、TextMuted，跨数据源时显示）。
- 点击 → `OpenConsole` + 激活；切换前 `Flush(旧)` + `SetCaret(旧, selStart, selEnd)`；
  激活后换 buffer + 回位光标。
- 标签右键菜单：`关闭`（移出工作区，不删文件）/ `重命名…` / `删除…`（确认：删元数据与 `.sql` 文件）。
  标签上 `×` = 关闭（与右键「关闭」同义，非破坏性，因此这次提供 `×`）。
- `+`：在当前已连接的数据源下 `Create`（无活动连接时禁用 + 提示「先连接数据源」；无工作区时先 `EnsureWorkspace()`）。
- 标签条用 `ui.ScrollHorizontal` + 行布局（数量增长不挤压编辑器）；`ui.Tabs` 不支持内容自定义，不用。
- 工作区切换器：`ui.MenuButton(c, activeName, ...)`，菜单项 = 全部工作区（当前项带 ✓，后面显示成员数）、
  分隔线、`新建工作区…`（Modal + TextInput）、`重命名当前工作区…`、`删除当前工作区…`（确认：
  仅删分组，控制台与文件保留）。零工作区时按钮文案「无工作区 ▾」，菜单只有新建项。

**侧栏**（`ui/sidebar.go`）：

- 连接行展开区新增「控制台」小节（位于对象树之上）：
  - 列出该数据源**全部**控制台：已打开（在当前工作区）加粗并带 ●/✓，未打开的用 TextMuted；
    点击：未打开 → `OpenConsole`（加入当前工作区 + 激活），已打开 → 激活。
  - 右键菜单：`关闭`（仅已打开）/ `重命名…` / `删除…`。
  - `＋ 新建控制台` 按钮：**始终可用**（建元数据+空文件不需要连接），创建后加入当前工作区并激活；
    零工作区时先 `EnsureWorkspace()`。
- 删除连接确认文案追加：「将同时删除该数据源的 N 个控制台及其 .sql 文件」。
- 级联实现**不改 `Remove(connID)` 签名**：`recs := store.ListConsoles(id)` → `store.Remove(id)` →
  `mgr` 逐个删文件；删文件失败只记 warning（孤儿 .sql 无害）。

**编辑器 / 顶栏**：

- 编辑器初始内容不再是 `buildSQL(3000)`：无激活控制台时显示空编辑器 + 占位提示
  （「在左侧数据源下新建或打开控制台」）。无工作区时提示可先新建工作区。
- 演示数据按钮（3k/12k/100k）与性能 HUD 保留（spike 回归用），仅在有激活控制台时可用；
  点击会改写该控制台正文并自动落盘（行为变化记入 Readme）。
- 顶栏连接信息不变；新增控制台错误行（写盘失败/创建失败）红字展示。

**快捷键**：`Ctrl+S` = `Flush(active)`（不等防抖）。

## 启动与退出

启动（`ui.NewRoot`）：

1. `store := app.LoadStore(app.DefaultStorePath())`（现有）。
2. `mgr := app.NewConsoleManager(store, app.ConsoleFiles{Dir: store.ConsolesDir()})`。
3. 激活工作区：`activeWorkspaceID` 有效则用之，否则第一个；**都没有则不建**（惰性，等首次新建控制台时 `EnsureWorkspace`）。
4. 激活控制台：`LastActiveConsoleID` 是该工作区成员则用之，否则成员最后一个；都没有 → 空态。
5. 正文 = `mgr.Text(id)`（文件不存在 → 空串，首次编辑时创建）；`caret = mgr.Caret(id)`。
6. 只加载**活动控制台**正文进编辑器；其他控制台在打开/激活时读盘（懒加载）。

退出（`main.go`）：`mygo.App.Run()` 返回后调用 `r.FlushAll()`（`*root` 的导出方法）。

## 约束与取舍

1. **成员关系内嵌在 JSON**（`WorkspaceRecord.Members`）而非独立表：数据量小、一次原子写、零迁移。
   代价：关闭/打开控制台要重写整个配置文件。可接受（每控制台约 200 字节）。
2. **关闭不删文件、删除才删文件**：对齐 db-k「工作区接管关闭语义」（其 `consoles.closed` 列已在 v11 退役）。
   `×` 因此是安全操作；误删草稿只可能来自右键「删除…」（带确认）。
3. **单活动会话**：切换不同数据源的控制台后执行会重连（SQLite 几乎无感；MySQL 重建连接）。
   真·多会话常驻需要把会话上提为 `map[connID]Session` 并重做树的所有权判定，属独立重构，不在本次。
4. **结果不跨重启**：运行态（结果/耗时）只在内存，关闭后同会话重开仍在，重启丢失。对齐 db-k（runSlots 不落盘）。
5. **工作区删除不可撤销但不伤数据**：只删分组，控制台可从侧栏重新打开。
6. **脏标记是内存态**：崩溃丢的只是上次防抖窗口（≤700ms）内的编辑；文件写入是原子替换，不会半截。

## 需要落地的测试（TDD，先写测试）

`app`（纯逻辑，无 UI）：

1. 老配置兼容：无 `consoles`/`workspaces` 字段的 JSON 载入成功、列表为空、激活 id 为空。
2. 控制台 CRUD 落盘往返：创建 → 重新 `LoadStore` → 名称/归属/顺序/路径一致；`SortOrder` 递增。
3. 工作区 CRUD 落盘往返 + 成员幂等追加/移除 + `LastActiveConsoleID` 回位。
4. 删除控制台：从所有工作区移除成员、清 `LastActiveConsoleID`；删除连接级联（控制台行 + 文件 + 成员）。
5. 删除工作区：控制台与文件保留。
6. `ConsoleFiles`：不存在读空串、写后读回一致、原子替换（写前存在旧内容）、删除幂等。
7. `ConsoleManager`：`SetText` 后脏、防抖到点落盘、`Flush` 立即落盘、写失败保留脏标记并返回错误（注入失败 `TextSink`）。
8. `CloseConsole` 保留文件/元数据/光标，激活切到下一个成员；`OpenConsole` 重新加入并重读盘；`DeleteWorkspace` 后控制台可重新打开。
9. 启动回落：无效 `activeWorkspaceID` → 取第一个；无效 `LastActiveConsoleID` → 取成员最后一个；零工作区 → 空态且不写盘。

`ui`（headless，`ui.Tester`）：

10. 标签条渲染当前工作区成员、脏标记出现/消失、点击切换后编辑器文本与光标变化。
11. `＋` 新建后标签增加且激活、文件创建；无连接时禁用。
12. 标签右键：关闭（标签消失、文件仍存在、侧栏该控制台变为未打开）→ 侧栏点击重新打开（标签回来、正文为磁盘内容）。
13. 工作区切换器：新建/切换后标签条只显示该工作区成员；重命名生效；删除后进入空态且控制台仍可从侧栏打开。
14. 启动：预置 store + 文件 → `NewRoot` 后编辑器正文、选区、激活工作区与标签条等于持久化值。
15. 删除连接 → 其控制台标签消失、`.sql` 文件被删。
16. 性能：`TestSpikeMatrix` 与结果网格基准保持绿。

## 对现有代码的改动面

| 文件 | 改动 |
|---|---|
| `app/conn.go` | 加 `ConsoleRecord`/`WorkspaceRecord`、控制台与工作区 CRUD、`ConsolesDir()`、删连接级联 |
| `app/consolefiles.go` | 新增：正文文件读写（原子）+ `TextSink` |
| `app/consolemgr.go` | 新增：`ConsoleManager`（激活/工作区成员/脏/防抖/光标/懒加载） |
| `app/console.go` | 去掉会话所有权，`Run(sess, query)` |
| `ui/root.go` | 去演示初始文本；接 `ConsoleManager`；会话字段；`consoles map[string]*app.Console` |
| `ui/tabs.go` | 新增：标签条（关闭/重命名/删除）+ 工作区切换器 |
| `ui/sidebar.go` | 连接下控制台小节（打开/关闭/新建/重命名/删除）；删连接文案与级联 |
| `ui/run.go` | 执行前 Flush、按控制台数据源自动连接、会话归属调整 |
| `ui/result.go` | 读「当前控制台」的运行态（其余不变） |
| `main.go` | 退出时 `FlushAll` |
| `Readme.md` | 功能清单（控制台/工作区）+ 演示按钮行为变化说明 |

## 留给实现者的细节（本人已按此定，评审时若不同意可改）

- 控制台/工作区 id 复用现有 `genID()`（4 字节 hex）；正文文件名 `<id>.sql`。
- 排序：控制台按 `SortOrder, CreatedAt`；工作区按 `SortOrder, CreatedAt`；标签顺序 = `Members` 数组顺序。
- `UpdatedAt` 只在创建/改名/`Flush` 时更新（光标落库不更新，避免回落排序被光标移动带跑）。
- 工作区默认名「默认」；重名允许（用 id 区分）；空名回退默认名。
- 零工作区时「新建控制台」先 `EnsureWorkspace()`；启动时不主动建工作区（不写盘、不产生空分组噪音）。
- 关闭最后一个成员 → 进入「无激活控制台」空态，标签条仍显示工作区与 `+`。
- 删除工作区为当前工作区且有其它工作区 → 自动切到下一个；无其它工作区 → 无工作区态。
- `Ctrl+S` 立即落盘当前控制台（取消防抖）。
