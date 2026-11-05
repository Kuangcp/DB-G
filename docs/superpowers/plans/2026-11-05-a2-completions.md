# A2：关键字 + 表/视图补全 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `ui/editor` 的补全接口后面接上 db-g 自己的候选：SQL 关键字/函数/类型（静态词表）+ 当前控制台数据源的表/视图（引擎 `Objects` 枚举，带缓存）。打字自动弹、`Ctrl+Space` 显式弹。

**Architecture:** 静态词表在 `ui/sqlwords.go` 构建一次；`ui/catalog.go` 用 `catalogCache` 按连接缓存表/视图并组装成不可变的 `[]myeditor.Completion`（每连接一份，命中即复用，不逐键重建）；`ui/completion.go` 把 `editor.State.OnCompletion` 接到缓存上，未命中时先给关键字、后台加载完再回投一次。

**Tech Stack:** Go 1.27.3、mygo `ui/editor`（`CompletionRequest`/`ShowCompletions`/`Completion{Label,Detail,Kind}`）、`db-g/engine`（`ObjectLister` + `CapObjects`）。

**Spec:** `docs/superpowers/specs/2026-11-05-editor-completions-design.md`（阶段 A2）

## Global Constraints

- `go.mod` 版本下限 `1.27.3`；模块 `db-g`；**不新增依赖**。
- 所有命令加 `CGO_ENABLED=0`；全量测试 `-p 1`。
- 补全**必须**在常数时间内返回：缓存命中时纯内存拼接；未命中时立即返回静态词表并触发一次后台加载，**不得**在按键路径上做数据库查询。
- 缓存以 `consoleID → connectionID` 为键，跨连接不得串数据；`openConn` 成功后失效该连接缓存；`removeConn` 删除缓存。
- 候选的 `Detail` 用中文（`表` / `视图`）；文案风格与现有 UI 一致；提交信息 `taskN: …`。
- 每个任务收尾必须绿：`CGO_ENABLED=0 go test -p 1 ./...`、`CGO_ENABLED=0 go vet ./...`。

## Review Focus

补全最容易被用户撞上的五类情况，按可能性排序；每条在括注任务的测试里钉住：

1. **没有连接 / 数据源不支持对象枚举**：`Ctrl+Space` 只能出关键字，不报错、不阻塞按键。（Task 2）
2. **目录加载失败或超时**：补全退化为关键字，并且**下次请求要能重试**（失败不写进缓存）。（Task 1）
3. **按键路径的延迟**：命中缓存时一次 `OnCompletion` 不查库、不重建切片；几千张表也不能卡帧。（Task 1）
4. **连接切换**：切到另一个数据源后补全只出它的表；删连接后缓存不泄漏、不串到同名表。（Task 2）
5. **`Ctrl+Space` 时目录还没加载**：立刻给关键字，加载完成后自动再弹一次（光标仍在该词上时）。（Task 2）

---

### Task 1: SQL 词表 + 目录缓存 + 候选组装（纯逻辑）

**Files:**
- Create: `ui/sqlwords.go`、`ui/catalog.go`
- Test: `ui/sqlwords_test.go`、`ui/catalog_test.go`

**Interfaces:**
- Consumes: `db-g/engine`（`ObjectLister`、`DBObject`、`ObjectTable`/`ObjectView`、`Capabilities.Has`）、`myeditor "github.com/egoist/mygo/ui/editor"`。
- Produces（Task 2 依赖）:
  ```go
  // ui/sqlwords.go
  func sqlWordItems() []myeditor.Completion   // 关键字/函数/类型/常量，已去重，初始化一次

  // ui/catalog.go
  type catalogEntry struct {
      tables, views []string
      items         []myeditor.Completion // = 词表 + 表 + 视图（不可变，命中即复用）
      loadedAt      time.Time
  }
  type catalogCache struct {
      ttl      time.Duration // 默认 5 分钟
      now      func() time.Time
      entries  map[string]catalogEntry
      inflight map[string]bool
  }
  func newCatalogCache() *catalogCache
  func (c *catalogCache) get(connID string) ([]myeditor.Completion, bool) // 命中且未过期
  func (c *catalogCache) begin(connID string) bool  // false = 有缓存或已在加载（不重复发起）
  func (c *catalogCache) finish(connID string, tables, views []string)
  func (c *catalogCache) fail(connID string)        // 清 inflight，不写缓存（下次可重试）
  func (c *catalogCache) invalidate(connID string)
  func buildCatalogItems(tables, views []string) []myeditor.Completion
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestSQLWordItems(t *testing.T) {
	items := sqlWordItems()
	if len(items) < 80 {
		t.Fatalf("词表太小: %d", len(items))
	}
	seen := map[string]bool{}
	byLabel := map[string]myeditor.CompletionKind{}
	for _, it := range items {
		if it.Label == "" {
			t.Fatal("empty label")
		}
		if seen[it.Label] {
			t.Fatalf("重复词条: %q", it.Label)
		}
		seen[it.Label] = true
		byLabel[it.Label] = it.Kind
	}
	for _, want := range []string{"SELECT", "select", "FROM", "WHERE", "JOIN", "NULL"} {
		if !seen[want] {
			t.Fatalf("缺少 %q", want)
		}
	}
	if byLabel["count"] != myeditor.CompletionFunction && byLabel["COUNT"] != myeditor.CompletionFunction {
		t.Fatalf("count 应为函数: %v", byLabel["COUNT"])
	}
	if byLabel["varchar"] != myeditor.CompletionType && byLabel["VARCHAR"] != myeditor.CompletionType {
		t.Fatalf("varchar 应为类型: %v", byLabel["VARCHAR"])
	}
}

func TestCatalogCacheHitAndTTL(t *testing.T) {
	c := newCatalogCache()
	clock := time.Unix(1000, 0)
	c.now = func() time.Time { return clock }
	if !c.begin("conn") {
		t.Fatal("first load must start")
	}
	if c.begin("conn") {
		t.Fatal("concurrent load must not start twice")
	}
	c.finish("conn", []string{"orders"}, []string{"v_orders"})
	items, ok := c.get("conn")
	if !ok || len(items) == 0 {
		t.Fatalf("cache miss after finish: %v", ok)
	}
	var hasOrders, hasView bool
	for _, it := range items {
		if it.Label == "orders" && it.Detail == "表" {
			hasOrders = true
		}
		if it.Label == "v_orders" && it.Detail == "视图" {
			hasView = true
		}
	}
	if !hasOrders || !hasView {
		t.Fatalf("items: %+v", items)
	}
	clock = clock.Add(6 * time.Minute) // 超过 5 分钟 TTL
	if _, ok := c.get("conn"); ok {
		t.Fatal("expired entry must miss")
	}
	if !c.begin("conn") {
		t.Fatal("expired entry must allow a reload")
	}
}

func TestCatalogFailureAllowsRetry(t *testing.T) {
	c := newCatalogCache()
	if !c.begin("x") {
		t.Fatal("begin")
	}
	c.fail("x")
	if _, ok := c.get("x"); ok {
		t.Fatal("failure must not be cached")
	}
	if !c.begin("x") {
		t.Fatal("failure must allow retry")
	}
}

func TestCatalogIsolatesConnections(t *testing.T) {
	c := newCatalogCache()
	c.begin("a")
	c.finish("a", []string{"only_a"}, nil)
	if _, ok := c.get("b"); ok {
		t.Fatal("connections must not share a catalog")
	}
	items, _ := c.get("a")
	found := false
	for _, it := range items {
		if it.Label == "only_a" {
			found = true
		}
	}
	if !found {
		t.Fatal("conn a lost its tables")
	}
}

func TestCatalogInvalidate(t *testing.T) {
	c := newCatalogCache()
	c.begin("a")
	c.finish("a", []string{"t"}, nil)
	c.invalidate("a")
	if _, ok := c.get("a"); ok {
		t.Fatal("invalidate must drop the entry")
	}
}

func TestBuildCatalogItemsKeepsWordsFirst(t *testing.T) {
	items := buildCatalogItems([]string{"orders"}, []string{"v"})
	if len(items) <= len(sqlWordItems()) {
		t.Fatalf("tables/views missing: %d", len(items))
	}
	if items[0].Label != sqlWordItems()[0].Label {
		t.Fatal("words must come first")
	}
	last := items[len(items)-1]
	if last.Label != "v" || last.Detail != "视图" {
		t.Fatalf("last item: %+v", last)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'SQLWord|Catalog|BuildCatalog' -v`
Expected: 编译失败 `undefined: sqlWordItems` / `catalogCache`。

- [ ] **Step 3: Implement**

- `ui/sqlwords.go`：`sqlWordItems()` 用 `sync.Once` 构建一次；词条来源可直接抄 `mygo/ui/editor/languages.go` 里 SQL lexer 的关键字/类型/常量列表（`words(TokenKeyword, …)`、`words(TokenType, …)`、`words(TokenConstant, …)`），再补常用函数（`count sum avg min max coalesce ifnull nullif now date time length lower upper substr replace round abs` 等，`CompletionFunction`）。输出时对每个词生成两种大小写形式（`SELECT` 与 `select`），按 Label 升序去重。
- `ui/catalog.go`：
  - `catalogCache` 用 `sync.Mutex` 保护三个 map；`get` 检查 `now().Sub(loadedAt) < ttl`。
  - `begin`：有未过期条目 → false；`inflight[connID]` → false；否则置 true 返回 true。
  - `finish`：`entries[connID] = catalogEntry{tables,views,items: buildCatalogItems(tables,views), loadedAt: now()}`，清 inflight。
  - `fail`：清 inflight（不写 entries）。
  - `buildCatalogItems`：`append(sqlWordItems(), tables…, views…)`；表 `Detail:"表"`/`Kind: CompletionModule`，视图 `Detail:"视图"`/`Kind: CompletionType`；为控制候选规模，表/视图各自去重并按名字排序。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'SQLWord|Catalog|BuildCatalog' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add ui/sqlwords.go ui/sqlwords_test.go ui/catalog.go ui/catalog_test.go
git commit -m "task1: SQL 词表与补全目录缓存（按连接、TTL、失败可重试）"
```

---

### Task 2: 接线到编辑器 + 刷新缓存

**Files:**
- Create: `ui/completion.go`
- Modify: `ui/root.go`（`root` 加 `catalog *catalogCache`）、`ui/run.go`（`openConn` 成功后失效缓存、`removeConn` 清除）、`ui/settings.go`（加「刷新补全缓存」按钮）
- Test: `ui/completion_test.go`、`ui/settings_test.go`（补一条）

**Interfaces:**
- Consumes: Task 1 的 `catalogCache`、`sqlWordItems`、`buildCatalogItems`；`root.code.state`（A1）、`root.sess/sessConnID`、`engine.ObjectLister`。
- Produces:
  ```go
  // ui/completion.go
  func (a *root) onCompletion(r myeditor.CompletionRequest)      // 编辑器回调
  func (a *root) completionItems(connID string) []myeditor.Completion // 缓存命中→缓存项；否则→仅词表（未知连接也返回词表）
  func (a *root) ensureCatalog(connID string)                    // 后台加载一次（headless 同步）
  func (a *root) loadCatalog(connID string) (tables, views []string, err error) // 用活动会话枚举
  func (a *root) refreshCompletions()                            // 失效当前连接缓存并重新加载
  ```
  `root` 新增字段：`catalog *catalogCache`（`newRoot` 里 `newCatalogCache()`）。

- [ ] **Step 1: Write the failing tests**

```go
// completionLabels 取某连接的候选标签（completionItems 对未知连接也返回词表）。
func completionLabels(t *testing.T, a *root) []string {
	t.Helper()
	var out []string
	for _, it := range a.completionItems(a.sessConnID) {
		out = append(out, it.Label)
	}
	return out
}

func TestCompletionWithoutSessionHasOnlyWords(t *testing.T) {
	a := newConsoleRoot(t)
	labels := completionLabels(t, a)
	if len(labels) == 0 {
		t.Fatal("keywords must always be available")
	}
	for _, l := range labels {
		if l == "users" {
			t.Fatal("no session → no tables")
		}
	}
}

func TestCompletionIncludesTablesAndViews(t *testing.T) {
	a := newConsoleRoot(t)
	if err := a.openConn(app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}); err != nil {
		t.Fatal(err)
	}
	tables, views, err := a.loadCatalog(a.sessConnID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 || tables[0] != "users" {
		t.Fatalf("tables: %v", tables)
	}
	_ = views
	a.ensureCatalog(a.sessConnID) // headless：同步加载，便于断言
	labels := completionLabels(t, a)
	found := false
	for _, l := range labels {
		if l == "users" {
			found = true
		}
	}
	if !found {
		t.Fatal("users must appear after the catalog loads")
	}
}

func TestCompletionMenuShowsTable(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地") // 已存在：写入 store 并返回记录
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "补全")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Click("编辑器")
	tt.Frame()
	tt.TypeKey(0, ui.KeyU, "u")
	tt.TypeKey(0, ui.KeyS, "s")
	for range 5 {
		tt.Frame()
	}
	if !tt.HasText("users") {
		t.Fatalf("completion menu must list the table: %q", tt.Texts())
	}
}

func TestRefreshCompletionsInvalidatesCache(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.loadCatalog(prof.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.catalog.get(prof.ID); !ok {
		t.Fatal("catalog must be cached after a load")
	}
	a.refreshCompletions()
	if _, ok := a.catalog.get(prof.ID); !ok {
		t.Fatal("refresh must repopulate the cache")
	}
}

func TestOpenConnInvalidatesCatalog(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	a.catalog.begin(prof.ID)
	a.catalog.finish(prof.ID, []string{"stale"}, nil)
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.catalog.get(prof.ID); ok {
		t.Fatal("openConn must invalidate the connection's catalog")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'Completion|RefreshCompletions|OpenConnInvalidates' -v`
Expected: 编译失败 `a.completionItems` / `a.catalog` / `a.refreshCompletions` 未定义。

- [ ] **Step 3: Implement**

- `newRoot`：`catalog: newCatalogCache()`。
- `activateConsole` 末尾：`a.code.state.OnCompletion = a.onCompletion`（只需一次；放在 `newRoot` 里更省：`a.code.state.OnCompletion = a.onCompletion`，但 `a` 尚未构造完 → 在 `newRoot` 返回前赋值）。
- `onCompletion(r)`：`connID := ""；if rec, ok := a.consoleMgr().Active(); ok { connID = rec.ConnectionID }`；
  `if items, ok := a.catalog.get(connID); ok { a.code.state.ShowCompletions(r.Offset-len(r.Prefix), items); return }`；
  `items := sqlWordItems()`（命中前先给关键字）→ `a.code.state.ShowCompletions(...)`；`a.ensureCatalog(connID)`。
  注意：`connID == ""` 或 `a.sess == nil` 或 `!a.sess.Caps().Has(engine.CapObjects)` 时**只**给关键字，不触发加载。
- `ensureCatalog(connID)`（主线程调用）：`if connID == "" || !a.catalog.begin(connID) { return }`；
  `sess := a.sess`（**在主线程捕获**，避免后台读 `a.sess` 产生数据竞争）；
  `if sess == nil || a.sessConnID != connID { a.catalog.fail(connID); return }`；
  `run := func() { tables, views, err := objectsOf(sess); if err != nil { a.catalog.fail(connID); return }; a.catalog.finish(connID, tables, views); if a.win != nil { win := a.win; win.Update(a.retriggerCompletions) } }`；
  `if a.win == nil { run() } else { go run() }`（headless 同步便于断言；有窗口时后台，会话对象在别的协程用是安全的 —— `app.Console.Run` 本来就这样用）。
- `objectsOf(sess engine.Session) (tables, views []string, err error)`（纯函数，可被后台协程安全调用）：
  `lister, ok := sess.(engine.ObjectLister)`；`!ok` 或 `!sess.Caps().Has(engine.CapObjects)` → 返回空 + nil（不是错误，降级为只有关键字）；
  `ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)`；分别 `Objects(ctx, engine.ObjectTable/ObjectView)`，取名字、排序去重。
- `loadCatalog(connID)`（主线程用，测试直接调）：`if a.sess == nil || a.sessConnID != connID { return nil, nil, errors.New("连接未打开") }`；否则 `return objectsOf(a.sess)`。
- `retriggerCompletions()`（主线程）：`if a.win == nil || !a.code.state.Focused() { return }`；`a.code.state.Run(myeditor.ShowCompletions)`（编辑器内建命令，等价 Ctrl+Space，会重新请求候选；此时缓存已就绪）。
- `refreshCompletions()`：`if rec, ok := a.consoleMgr().Active(); ok { a.catalog.invalidate(rec.ConnectionID); a.ensureCatalog(rec.ConnectionID) }`；`a.updateSettingsWindow()`。
- `openConn` 成功 `a.setSession(...)` 之后：`if a.catalog != nil { a.catalog.invalidate(p.ID) }`。
- `removeConn`：`if a.catalog != nil { a.catalog.invalidate(id) }`。
- `settings.go`：在预设下面加
  ```go
  if ui.Button(c, "刷新补全缓存").Clicked() {
      a.refreshCompletions()
  }
  ```
  并显示当前缓存的表数量（`if items, ok := a.catalog.get(connID); ok { ui.Text(c, fmt.Sprintf("补全候选 %d 条（含表/视图）", len(items))) }`，非必需）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'Completion|RefreshCompletions|OpenConnInvalidates|Settings' -v` 然后 `CGO_ENABLED=0 go test -p 1 ./ui`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add ui/completion.go ui/root.go ui/run.go ui/settings.go ui/completion_test.go ui/settings_test.go
git commit -m "task2: 编辑器补全接上关键字与表/视图（缓存、后台加载、刷新按钮）"
```

---

### Task 3: 文档与手工验收

**Files:**
- Modify: `Readme.md`（功能清单：补全；`Ctrl+Space` 说明）

**Interfaces:**
- Consumes: Task 1/2 的成品。
- Produces: 无。

- [ ] **Step 1: 更新 Readme 功能清单**

```markdown
- [x] 自动补全（A2）：SQL 关键字/函数/类型 + 当前数据源的表/视图；
      打字自动弹，`Ctrl+Space` 显式触发；表/视图目录按连接缓存（TTL 5 分钟，
      顶栏「设置」里可手动「刷新补全缓存」）
```

并把 M7 一行改为 `- [ ] M7：编辑器补全（列名 A3）与格式化`。

- [ ] **Step 2: 手工验收（真实窗口，用户执行）**

`CGO_ENABLED=0 go run .`：
1. 连接 SQLite（含 `users` 表）→ 新建控制台 → 输入 `SELECT * FROM us` → 候选里出现 `users`，Enter/Tab 接受。
2. `Ctrl+Space` 在空行 → 出关键字列表；输入 `s` → 列表过滤到 `select` 等。
3. 断开/未连接的数据源上 `Ctrl+Space` → 只有关键字，不报错。
4. 设置窗口「刷新补全缓存」→ 不崩，重新加载。

- [ ] **Step 3: 全量验证并提交**

Run: `CGO_ENABLED=0 go test -p 1 -count=1 ./...`、`CGO_ENABLED=0 go vet ./...`、`CGO_ENABLED=0 go build ./...`
Expected: 全绿 / 无输出 / 无输出。

```bash
git add Readme.md
git commit -m "task3: Readme 补全说明"
```

## 升级条件（何时停下来问）

- 补全弹层在 100k 行文件上让键入 p99 超过 A1 的 15ms 预算 → 停下来汇报数字（可能是候选过多）。
- `engine.Objects` 在 MySQL 上很慢（>2s）导致「加载完自动弹」体验突兀 → 停下来讨论是否改为按前缀加载或降级为仅关键字。
- 编辑器 `ShowCompletions` 的过滤与我们的全量候选在几千表时出现可感知卡顿 → 停下来讨论是否改为服务端/前缀裁剪。
