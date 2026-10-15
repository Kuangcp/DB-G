# db-g M2 设计：布局骨架对齐 db-k + 连接管理与对象树

日期：2026-10-15 · 状态：已批准（逐节过审）
上游：`2026-10-14-db-g-roadmap-design.md`（Roadmap M2 条目）· `2026-10-14-m1-sqlite-execution.md`（M1 实现）

## 1. 背景与范围

M1 交付了 SQLite 执行闭环（engine/driver/app/ui 四层、结果面板、性能守门）。M2 按 Roadmap 为「连接管理 + 对象树」；用户要求主页面布局先对齐 db-k。本 spec 为**单份设计**覆盖布局骨架与 M2 功能（方案一），对象树数据接口采用**可选接口 + 能力位**（方案 A，不破坏既有 Session 实现与测试桩）。

**范围内**：主页面骨架重排（侧栏/顶栏/工具行）、连接 profile 的增删改查与 JSON 持久化、SQLite 对象树（懒加载）、切换连接即切会话。
**范围外**（对应后续里程碑）：多控制台标签（M3）、连接搜索/右键菜单/文件夹分组/拖拽/导入导出、双击表预览、密码与非 SQLite 方言的树实现（M4/M5）、无边框自绘窗口标题栏、快捷键显隐侧栏、查找浮动条。

### 与已确认决策的对照

| 决策 | 结论 |
|---|---|
| 布局范围 | 骨架对齐：保留系统窗口边框，尺寸对齐 db-k（侧栏默认 280 可拖），不做无边框标题栏/窗口控制钮 |
| spike 工具栏 | 保留，合并为顶栏下**一行**（HUD + 3k/12k/100k + FF/FV/VV + 查找输入 + Find/Clear） |
| M2 范围 | 核心子集：连接增删改 + JSON 持久化；树 = 连接→表/视图/索引；工具条仅「新建连接/刷新」 |
| 顶栏 | db-k 骨架 + 现有按钮：连接状态 + 连接名 … spacer … [执行]（无连接/运行中禁用）；停止钮在结果面板 |
| 「打开…」按钮 | **移除**；数据库只能从侧栏新建连接进入（对齐 db-k） |
| spec 粒度 | 单份 spec（方案一）；对象树接口 = 可选接口（方案 A） |

## 2. 布局骨架（`ui/root.go` rootView）

```
rootView
└── ui.Split(&a.sidebarWidth, 左侧栏, 右侧).Fill()     // 水平可拖分栏，默认 280

左侧栏 Column（Fill）
├── 工具行：[新建连接] [刷新]
└── ui.Tree：连接 → 表/视图/索引 分组 → 叶子；空态「新建连接开始」

右侧 Column（Fill）
├── 顶栏一行：● 状态点 + 活动连接名   …spacer…   [执行]
├── 工具行一行：HUD | 3k 12k 100k | FF FV VV | [find 输入] Find Clear
└── ui.SplitVertical（M1 现状）：编辑器（gutter/find 逻辑不变） | 结果面板（M1 四态）
```

- 顶层水平分栏用 `ui.Split`（首个 `*float32` 为宽度），纵向沿用 `ui.SplitVertical`（`resultsHeight` 默认 260 不变）。
- 快捷键 Ctrl+Enter / F5 保留（快捷键在 `rootView` 入口处，不因分栏移动而失效）。
- 移除「打开…」按钮与 `openDatabase()`；`root` 字段 `dbName/openErr` 更名为 `connName/connErr`，语义改为活动连接驱动。
- 工具行元素紧凑单行排布；HUD 文本过长时截断（`SingleLine`），不换行。
- 布局变更后必须跑 spike 门槛与结果网格基准（性能守门，见 §6）。

## 3. 连接管理（`app/conn.go`）

```go
type ConnProfile struct {
    ID   string // 短随机唯一，持久化稳定引用
    Name string // 展示名，非空
    Kind string // M2 仅 "sqlite"
    Path string // 数据库文件路径
}

type ConnStore struct { /* mu, path, conns []ConnProfile, activeID */ }
func LoadStore(path string) *ConnStore   // 缺失→空；损坏→改名 .bak 后空表，返回警告串
func (s *ConnStore) List() []ConnProfile
func (s *ConnStore) Add(p ConnProfile) error     // 校验 Name 非空、Path 存在、ID 唯一
func (s *ConnStore) Update(p ConnProfile) error  // 按 ID，改自身
func (s *ConnStore) Remove(id string) error      // 按 ID；活动连接被删→activeID 清空
func (s *ConnStore) Active() (ConnProfile, bool)
func (s *ConnStore) SetActive(id string)
// 所有变更同步落盘：写临时文件 + rename 原子替换
```

- 持久化路径：`~/.config/db-g/connections.json`（`os.UserConfigDir()` + `db-g/`）；测试注入 `t.TempDir()`。
- 无密码字段（SQLite 无密码）。
- UI 侧（`ui/conn_ui.go`）：`ui.Modal` 表单——名称输入 + 路径输入 + 「浏览…」（`mygo.Dialog.Open` 文件选择器，过滤 db/sqlite/sqlite3）；提交前本地校验（名称非空、路径存在）。
- 打开/切换：双击连接节点 → `engine.Open(Profile{Kind,Name:Name,DSN:Path})` → `sess.Open(5s ctx)` → `con.SwapSession(sess)` + `SetActive`；失败 → 该节点下红字错误行 + 状态点红，不影响当前活动连接。
- 删除：确认 Modal；删除的是活动连接则 `con.SwapSession(nil)`，状态点回灰。
- 状态点：绿=活动已连接、灰=无连接、红=最近一次打开失败（`connErr` 非空）。
- 单击仅选中/展开，不连库。

## 4. 对象树

### 4.1 engine 扩展（可选接口）

```go
// engine/object.go
const CapObjects Capabilities = 1 << 3   // 接在既有 4 个能力位之后

type ObjectKind int   // ObjectTable | ObjectView | ObjectIndex

type DBObject struct {
    Kind ObjectKind
    Name string
}

// ObjectLister 是可选能力；UI 经类型断言 + Caps 判定使用。
type ObjectLister interface {
    Objects(ctx context.Context, kind ObjectKind) ([]DBObject, error)
}
```

不修改 `Session` 方法集；既有 fake/stub 实现零改动。

### 4.2 SQLite 实现（`driver/sqlite`）

- `session` 实现 `ObjectLister`：`SELECT name FROM sqlite_master WHERE type = ? AND name NOT LIKE 'sqlite_%' ORDER BY name`（kind→type 映射：table/view/index）。
- `Caps()` 返回 `engine.CapObjects`。
- 查询走既有 `mu` 串行与 ctx 取消语义。

### 4.3 树 UI（`ui/tree.go`，基于 `ui.Tree/TreeItem`）

```
连接A（●活动）
├ 表 (n)      ← 分组节点，展开时懒加载
│  ├ users    ← 叶子，单击选中（M2 不做预览）
├ 视图 (n)
└ 索引 (n)
连接B         ← 未连接：分组骨架先显示「表/视图/索引」无计数，首次展开才查询
```

- 懒加载：`root.treeData map[connID][3][]DBObject` 缓存；展开分组且无缓存 → 拉取（5s ctx），成功回填计数，失败 → 分组下红字一行，再次展开重试。
- 分组计数：拉取后显示 `(n)`；未拉取显示无计数骨架。
- 「刷新」：清空 `treeData` 缓存 + 重读 store（连接列表变更后树同步）。
- 能力降级：`CapObjects` 缺位 → 连接节点下显示「该数据源不支持对象树」一行。
- 空态：无连接 → 「左上角新建连接」；树容器带滚动（连接多时）。

## 5. 错误处理汇总

| 场景 | 行为 |
|---|---|
| connections.json 损坏 | 改名 `.bak`，空表启动，侧栏顶部红字警告一次 |
| 新建连接：名称空/路径不存在 | 表单内红字，Modal 不关 |
| 打开连接失败 | 连接节点下红字行 + 状态点红；不动活动连接 |
| Objects() 失败/超时 | 对应分组下红字行，展开可重试 |
| 删除活动连接 | 确认后 `SwapSession(nil)`，状态点灰 |
| 执行/取消/超时 | M1 Console 语义不变 |

## 6. 测试与验收

**测试四层**
1. 单测 `app/conn_test.go`：Add/Update/Remove、JSON round-trip、损坏文件恢复、Active 语义、原子写。
2. 驱动集成 `driver/sqlite`：`TestObjects`——临时库建 2 表 1 视图 1 索引 + 1 张 `sqlite_temp` 类系统表，断言枚举与过滤。
3. headless UI `ui/sidebar_test.go`：空态；注入 store 渲染连接名；Modal 新建全流程（落盘+树出现）；双击真实 SQLite 连接 → 状态点绿 + 端到端执行；展开懒加载表名渲染；注入失败 lister → 红字行。
4. 守门回归：spike 门槛（VV p99 8/15/50ms）、结果网格 p50≤16ms、全量 `go test ./...`、`go tool mygo vet .`。

**M2 验收清单**
- [ ] 连接新建/编辑/删除，重启进程后仍在（持久化 round-trip）
- [ ] 对象树按连接懒加载枚举表/视图/索引（过滤 `sqlite_%`）
- [ ] 双击切换连接即换会话，SELECT 端到端仍绿
- [ ] 主页面骨架与 §2 结构一致（侧栏 280 默认可拖；工具行为单行；无「打开…」）
- [ ] spike/网格基准无回退
- [ ] Readme 勾选 M2

## 7. 已知取舍

- 查找行并入工具行后单行拥挤：接受，M3 移入编辑器浮动条后释放。
- 树叶子双击预览、右键菜单、搜索：明确推迟（范围外）。
- 侧栏显隐快捷键、分栏宽度持久化：db-k 有（`window.properties`），M2 不做，`sidebarWidth` 仅进程内记忆。
- 连接数预期个位数到几十：`ui.Tree` 全构建即可，不引入 `ui.Outline`；超过数百对象再评估。
