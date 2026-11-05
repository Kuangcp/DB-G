# A3：列名补全 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `表名.` 之后补全该表的列。engine 新增可选能力 `ColumnLister`/`CapColumns`（sqlite `PRAGMA table_info`、mysql `information_schema.columns`），补全层按（连接, 表）缓存列并复用 A2 的后台加载/重弹机制。

**Architecture:** 能力位 + 可选接口扩展 `engine`（不破坏现有 `Session`）；两个驱动实现列目录；`ui/catalog.go` 增加「表/视图是否存在」与「列候选缓存」；`ui/completion.go` 在 `OnCompletion` 里先判点号上下文（`ident.`），命中已知表/视图才走列补全，否则维持 A2 行为。

**Tech Stack:** Go 1.27.3、mygo `ui/editor`、`database/sql`（sqlite `modernc.org/sqlite`、mysql `go-sql-driver/mysql`）、`db-g/engine`。

**Spec:** `docs/superpowers/specs/2026-11-05-editor-completions-design.md`（阶段 A3）

## Global Constraints

- `go.mod` 版本下限 `1.27.3`；模块 `db-g`；**不新增依赖**。
- 所有命令加 `CGO_ENABLED=0`；全量测试 `-p 1`。
- `Session` 接口**不得**新增必须实现的方法（老驱动/测试桩会编译失败）；列目录一律走可选接口
  `engine.ColumnLister` + 能力位 `engine.CapColumns`。
- 按键路径不得查库：命中缓存纯内存；未命中先给词表并后台加载（同 A2）。
- 列缓存与 A2 目录同为 5 分钟 TTL、失败不缓存、按连接隔离；重连/删连接/「刷新补全缓存」一并失效。
- 明确不做：别名解析（`FROM t x` → `x.col`）、CTE、子查询作用域、`*` 展开（见 spec 非目标）。
- 文案中文；提交信息 `taskN: …`。每个任务收尾必须绿：`CGO_ENABLED=0 go test -p 1 ./...`、`CGO_ENABLED=0 go vet ./...`。

## Review Focus

1. **表名里的引号/保留字**：``PRAGMA table_info("weird""name")`` 这类表名不能拼错、不能被注入。（Task 1）
2. **点号上下文的边界**：`a.b.`、`"tbl".`、`1.5`、注释/字符串里的 `x.` 不能误判成表前缀。（Task 2）
3. **能力位缺失**：会话没有 `CapColumns`（或不是 `ColumnLister`）时只能出关键字+表名，不报错、不卡键。（Task 3）
4. **列缓存与目录的一致性**：表被重命名/重建后，缓存 TTL 内可能给出旧列；重连与「刷新补全缓存」必须清掉。（Task 2）
5. **未知前缀**：`nosuchtable.` 不能只出一堆关键字而让人以为补全坏了 —— 仍给关键字+表/视图（与 A2 一致）。（Task 2、Task 3）

---

### Task 1: engine 列目录契约 + sqlite/mysql 驱动实现

**Files:**
- Modify: `engine/session.go`（追加类型与可选接口）、`engine/caps.go`（追加 `CapColumns`）、`driver/sqlite/sqlite.go`（`Caps` + `Columns`）、`driver/mysql/mysql.go`（同）
- Test: `engine/session_test.go`（能力位不冲突）、`driver/sqlite/sqlite_test.go`、`driver/mysql/mysql_test.go`

**Interfaces:**
- Consumes: 现有 `engine.Session`/`Capabilities`/`ObjectLister` 模式；sqlite `session.db`；mysql `session.cfg.DBName`。
- Produces（Task 2/3 依赖）:
  ```go
  // engine/session.go
  type Column struct {
      Name       string
      Type       string
      Nullable   bool
      PrimaryKey bool
  }
  type ColumnLister interface {
      Columns(ctx context.Context, table string) ([]Column, error)
  }
  // engine/caps.go
  const CapColumns Capabilities = ... // 追加到 iota 末尾，不影响既有位值
  ```
  驱动：`Caps()` 返回 `CapObjects | CapColumns`；`Columns` 未打开会话报错、未知表返回空切片且 `nil` 错误。

- [ ] **Step 1: Write the failing tests**

`engine/session_test.go` 追加：

```go
func TestCapColumns(t *testing.T) {
	var c Capabilities = CapColumns
	if !c.Has(CapColumns) {
		t.Fatal("CapColumns must be settable and readable")
	}
	for _, other := range []Capabilities{CapEditableResult, CapFetchMore, CapObjectDDL, CapSQLCompletion, CapObjects} {
		if CapColumns == other {
			t.Fatal("CapColumns must not collide with existing bits")
		}
	}
}
```

`driver/sqlite/sqlite_test.go` 追加：

```go
func TestColumns(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, score REAL)`); err != nil {
		t.Fatal(err)
	}
	lister, ok := s.(engine.ColumnLister)
	if !ok || !s.Caps().Has(engine.CapColumns) {
		t.Fatal("sqlite must list columns")
	}
	cols, err := lister.Columns(ctx, "users")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 {
		t.Fatalf("cols: %+v", cols)
	}
	if cols[0].Name != "id" || !cols[0].PrimaryKey || cols[0].Type == "" {
		t.Fatalf("pk column: %+v", cols[0])
	}
	if cols[1].Name != "name" || cols[1].Nullable {
		t.Fatalf("not-null column: %+v", cols[1])
	}
}

func TestColumnsQuotesIdentifier(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	// 表名含双引号与空格：拼错或未转义都会失败。
	if _, err := s.RunStatement(ctx, `CREATE TABLE "weird""name" (c TEXT)`); err != nil {
		t.Fatal(err)
	}
	cols, err := s.(engine.ColumnLister).Columns(ctx, `weird"name`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || cols[0].Name != "c" {
		t.Fatalf("cols: %+v", cols)
	}
}

func TestColumnsUnknownTable(t *testing.T) {
	cols, err := newMemSession(t).(engine.ColumnLister).Columns(context.Background(), "nope")
	if err != nil {
		t.Fatal("unknown table must not error:", err)
	}
	if len(cols) != 0 {
		t.Fatalf("cols: %+v", cols)
	}
}
```

`driver/mysql/mysql_test.go` 追加（沿用现有 `DB_G_MYSQL_HOST` 跳过约定）：

```go
func TestColumnsRequiresOpenSession(t *testing.T) {
	s, err := openSession(engine.Profile{Kind: "mysql", Host: "h", Port: 3306, User: "u"})
	if err != nil {
		t.Fatal(err)
	}
	// 未 Open（或未选库）都必须报错，而不是返回空列表假装成功。
	if _, err := s.(engine.ColumnLister).Columns(context.Background(), "users"); err == nil {
		t.Fatal("listing columns without an open session must fail")
	}
}

func TestColumnsIntegration(t *testing.T) {
	host := os.Getenv("DB_G_MYSQL_HOST")
	if host == "" {
		t.Skip("DB_G_MYSQL_HOST 未设置")
	}
	// …沿用文件里既有的 DSN 构造方式，连接后：
	// CREATE TABLE IF NOT EXISTS a3_cols (id INT PRIMARY KEY, label VARCHAR(32) NOT NULL)
	// 断言 Columns("a3_cols") 的 Name/Type/PrimaryKey/Nullable。
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./engine ./driver/sqlite ./driver/mysql -run 'CapColumns|Columns' -v`
Expected: 编译失败 `undefined: Column` / `undefined: CapColumns` / `engine.ColumnLister`。

- [ ] **Step 3: Implement**

- `engine/session.go`：加 `Column`、`ColumnLister`（注释说明是可选能力）。
- `engine/caps.go`：`CapColumns` 追加在 `CapObjects` 之后。
- sqlite：
  ```go
  func (s *session) Caps() engine.Capabilities { return engine.CapObjects | engine.CapColumns }
  func (s *session) Columns(ctx context.Context, table string) ([]engine.Column, error) {
      s.mu.Lock(); defer s.mu.Unlock()
      if s.db == nil { return nil, errors.New("sqlite: session not open") }
      q := `PRAGMA table_info(` + quoteIdent(table) + `)`
      // 列：cid, name, type, notnull, dflt_value, pk
  }
  func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
  ```
  `Nullability = notnull == 0`；`PrimaryKey = pk > 0`。
- mysql：
  ```go
  func (s *session) Caps() engine.Capabilities { return engine.CapObjects | engine.CapColumns }
  func (s *session) Columns(ctx context.Context, table string) ([]engine.Column, error) {
      s.mu.Lock(); defer s.mu.Unlock()
      if s.db == nil { return nil, errors.New("mysql: session not open") }
      if s.cfg.DBName == "" { return nil, errors.New("mysql: 未选择数据库") }
      const q = `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY FROM information_schema.COLUMNS
                 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`
      // Nullable = IS_NULLABLE == "YES"；PrimaryKey = COLUMN_KEY == "PRI"
  }
  ```
- 两个驱动都要保持 `Objects` 现有行为不变。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./engine ./driver/sqlite ./driver/mysql -v`（mysql 集成用例无 env 时 skip）
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add engine/session.go engine/caps.go engine/session_test.go \
        driver/sqlite/sqlite.go driver/sqlite/sqlite_test.go \
        driver/mysql/mysql.go driver/mysql/mysql_test.go
git commit -m "task1: engine 列目录能力（ColumnLister/CapColumns）+ sqlite/mysql 实现"
```

---

### Task 2: 列缓存 + 点号上下文解析（纯逻辑）

**Files:**
- Modify: `ui/catalog.go`（列缓存与关系查询）
- Create: `ui/completioncontext.go`
- Test: `ui/catalog_test.go`、`ui/completioncontext_test.go`

**Interfaces:**
- Consumes: Task 1 的 `engine.Column`；A2 的 `catalogCache`（`entries` 里已有 `tables`/`views`）。
- Produces（Task 3 依赖）:
  ```go
  // ui/catalog.go
  func (c *catalogCache) hasRelation(connID, name string) bool  // 表或视图（EqualFold：SQL 标识符大小写不敏感）
  func (c *catalogCache) getColumns(connID, table string) ([]myeditor.Completion, bool)
  func (c *catalogCache) beginColumns(connID, table string) bool
  func (c *catalogCache) finishColumns(connID, table string, cols []engine.Column)
  func (c *catalogCache) failColumns(connID, table string)
  func buildColumnItems(cols []engine.Column) []myeditor.Completion // 词表 + 列（Detail = 类型，Kind = Field）

  // ui/completioncontext.go
  func dotIdent(text string, offset int) (string, bool) // 光标前紧邻的 `ident.` 的 ident（未加引号的 [A-Za-z_][A-Za-z0-9_$]*）
  ```
  内部：`catalogCache` 增 `cols map[string]map[string]columnEntry`、`colsInflight map[string]bool`，
  键为 `connID + "\x00" + table`；`invalidate` 同时清该连接的全部列。

- [ ] **Step 1: Write the failing tests**

```go
func TestDotIdent(t *testing.T) {
	cases := []struct {
		text   string
		offset int
		want   string
		ok     bool
	}{
		{"select * from users.", 20, "users", true},
		{"select users.", 13, "users", true},
		{"SELECT a.", 9, "a", true},
		{"select 1.5", 10, "", false},   // 数字不是标识符
		{"select 'abc.", 12, "", false}, // 单引号字符串里
		{"select 1 -- x.", 14, "", false}, // 行注释里
		{"a.b.", 4, "b", true},          // 最后一个点号前是 b
		{"users.", 0, "", false},        // 光标在行首
		{"", 0, "", false},
	}
	for _, c := range cases {
		got, ok := dotIdent(c.text, c.offset)
		if ok != c.ok || got != c.want {
			t.Fatalf("dotIdent(%q,%d) = %q,%v want %q,%v", c.text, c.offset, got, ok, c.want, c.ok)
		}
	}
}

func TestDotIdentQuotedPrefix(t *testing.T) {
	// 引号标识符写作 `"tbl".`：v1 不解析（返回 false），退化为 A2 行为。
	if _, ok := dotIdent(`SELECT "tbl".`, 13); ok {
		t.Fatal("quoted identifiers are out of scope in v1")
	}
}

func TestCatalogHasRelationAndColumns(t *testing.T) {
	c := newCatalogCache()
	clock := time.Unix(2000, 0)
	c.now = func() time.Time { return clock }
	c.begin("conn")
	c.finish("conn", []string{"users"}, []string{"v_users"})
	if !c.hasRelation("conn", "users") || !c.hasRelation("conn", "v_users") {
		t.Fatal("tables and views must be relations")
	}
	if c.hasRelation("conn", "nope") || c.hasRelation("other", "users") {
		t.Fatal("unknown relation or connection must miss")
	}
	if !c.hasRelation("conn", "USERS") {
		t.Fatal("relation lookup must ignore case, as SQL identifiers do")
	}
	if !c.beginColumns("conn", "users") {
		t.Fatal("first column load must start")
	}
	if c.beginColumns("conn", "users") {
		t.Fatal("column load must dedupe")
	}
	c.finishColumns("conn", "users", []engine.Column{{Name: "id", Type: "INTEGER", PrimaryKey: true}, {Name: "name", Type: "TEXT", Nullable: true}})
	items, ok := c.getColumns("conn", "users")
	if !ok {
		t.Fatal("columns must be cached")
	}
	var id, name bool
	for _, it := range items {
		if it.Label == "id" && it.Detail == "INTEGER" && it.Kind == myeditor.CompletionField {
			id = true
		}
		if it.Label == "name" {
			name = true
		}
	}
	if !id || !name {
		t.Fatalf("items: %+v", items)
	}
	// 列缓存也随 TTL 过期
	clock = clock.Add(6 * time.Minute)
	if _, ok := c.getColumns("conn", "users"); ok {
		t.Fatal("expired columns must miss")
	}
}

func TestCatalogColumnFailureAllowsRetry(t *testing.T) {
	c := newCatalogCache()
	c.beginColumns("c", "t")
	c.failColumns("c", "t")
	if _, ok := c.getColumns("c", "t"); ok {
		t.Fatal("failure must not be cached")
	}
	if !c.beginColumns("c", "t") {
		t.Fatal("failure must allow retry")
	}
}

func TestCatalogInvalidateDropsColumns(t *testing.T) {
	c := newCatalogCache()
	c.beginColumns("c", "t")
	c.finishColumns("c", "t", []engine.Column{{Name: "x"}})
	c.invalidate("c")
	if _, ok := c.getColumns("c", "t"); ok {
		t.Fatal("invalidate must drop columns too")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'DotIdent|CatalogHasRelation|CatalogColumn|InvalidateDrops' -v`
Expected: 编译失败 `undefined: dotIdent` / `hasRelation` / `beginColumns`。

- [ ] **Step 3: Implement**

- `dotIdent`：取 `text[:offset]`（offset 越界夹紧），要求以 `.` 结尾；向前扫 `[A-Za-z0-9_$]`，首个必须 `[A-Za-z_]`；
  遇到 `"`、`` ` ``、`'` 等直接返回 false（v1 不解析引号标识符）。返回 **点号前** 的 ident。
  两个廉价守卫（防误判，见 Review Focus 2）：当前行在光标前有**奇数个** `'` → 在字符串里 → false；
  当前行在 ident 之前出现 `--` → 在行注释里 → false。
- `ui/catalog.go`：加 `columnEntry{items []myeditor.Completion; loadedAt time.Time}`；`buildColumnItems`
  = `buildCatalogItems(nil, nil)`（词表）再追加列（`Detail: c.Type`，空类型时 "列"；`Kind: CompletionField`），
  列名去重排序；`invalidate` 连带 `delete(c.cols, connID)` 与清该连接所有 `colsInflight`。
- 关系判断：`hasRelation` 在 `entries[connID]` 的 tables+views 里用 `strings.EqualFold` 找（未加载 → false）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'DotIdent|Catalog' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add ui/catalog.go ui/catalog_test.go ui/completioncontext.go ui/completioncontext_test.go
git commit -m "task2: 列候选缓存与点号上下文解析"
```

---

### Task 3: 接线（`表名.` 出列名）+ 文档

**Files:**
- Modify: `ui/completion.go`（`onCompletion` 分支、`ensureColumns`、`retrigger` 复用）、`Readme.md`
- Test: `ui/completion_test.go`

**Interfaces:**
- Consumes: Task 1/2 全部；A2 的 `onCompletion`/`ensureCatalog`/`retriggerCompletions`。
- Produces:
  ```go
  func (a *root) columnsOf(sess engine.Session, table string) ([]engine.Column, error) // 纯函数，后台协程可调
  func (a *root) ensureColumns(connID, table string)
  func (a *root) showCompletionsAtCursor() // 重算上下文后 ShowCompletions(-1, items)；catalog/columns 共用
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestCompletionColumnsAfterDot(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	// 造一张带特征列名的表，避免与关键字/表名撞车。
	if _, err := a.sess.RunStatement(t.Context(), `CREATE TABLE a3_t (id INTEGER PRIMARY KEY, zz_col TEXT)`); err != nil {
		t.Fatal(err)
	}
	a.catalog.invalidate(prof.ID)
	rec, err := a.consoleMgr().Create(prof.ID, "列补全")
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
	for _, ch := range "select * from a3_t." {
		tt.TypeKey(0, 0, string(ch))
	}
	for range 6 {
		tt.Frame()
	}
	if !tt.HasText("zz_col") {
		t.Fatalf("column completion missing: %q", tt.Texts())
	}
}

func TestCompletionUnknownTableAfterDotKeepsKeywords(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	a.catalog.invalidate(prof.ID)
	rec, _ := a.consoleMgr().Create(prof.ID, "列补全")
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Click("编辑器")
	tt.Frame()
	for _, ch := range "select * from nosuch." {
		tt.TypeKey(0, 0, string(ch))
	}
	for range 6 {
		tt.Frame()
	}
	if !tt.HasText("select") && !tt.HasText("SELECT") {
		t.Fatalf("keywords must remain for unknown relations: %q", tt.Texts())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'CompletionColumnsAfterDot|UnknownTableAfterDot' -v`
Expected: `zz_col` 缺失 → FAIL（`unknown` 用例此时可能已过）。

- [ ] **Step 3: Implement**

- `onCompletion`：先 `text := a.code.Text(); ident, ok := dotIdent(text, r.Offset)`；
  `connID := a.activeConsoleConnID()`；
  `if ok && a.catalog.hasRelation(connID, ident)`：
  `if items, hit := a.catalog.getColumns(connID, ident); hit { a.code.state.ShowCompletions(r.Offset-len(r.Prefix), items); return }`；
  否则 `ShowCompletions(词表)` + `a.ensureColumns(connID, ident)`，return。
  `hasRelation` 未命中时保持 A2 分支（catalog 命中 → 表/视图+词；否则词 + 加载目录）。
- `ensureColumns`：`if !a.catalog.beginColumns(connID, ident)` → return；主线程捕获 `sess := a.sess`；
  `if sess == nil || a.sessConnID != connID || !sess.Caps().Has(engine.CapColumns) { a.catalog.failColumns(...); return }`；
  `run := func(){ cols, err := columnsOf(sess, ident); if err != nil { fail } else { finishColumns }; if a.win != nil { win.Update(a.showCompletionsAtCursor) } }`；
  无窗同步执行后直接 `a.showCompletionsAtCursor()`。
- `columnsOf`：类型断言 `engine.ColumnLister`；`context.WithTimeout(5s)`。
- `showCompletionsAtCursor`：重算上下文（同 `onCompletion` 的前半段但不再发起加载），
  命中则 `a.code.state.ShowCompletions(-1, items)`；把 A2 的 `retriggerCompletions` 改成**委托**它
  （`func (a *root) retriggerCompletions() { a.showCompletionsAtCursor() }`），目录与列共用，A2 的调用点不动。
- `Readme.md`：功能清单补一句「`表名.` 后补全该表的列（不做别名解析）」；M7 一行保持「列名 A3」已勾。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'Completion' -v` 然后 `CGO_ENABLED=0 go test -p 1 ./...`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add ui/completion.go ui/completion_test.go Readme.md
git commit -m "task3: 表名后的列补全 + Readme"
```

## 升级条件（何时停下来问）

- sqlite 上 `PRAGMA table_info` 对某类表（视图/temp/attached）不可用，导致列补全静默失败 → 汇报现象再定是否加回退（`SELECT * ... LIMIT 0` 读列名）。
- mysql `information_schema.columns` 在有大量表时超过 2s → 停下来讨论按需（只在点号后查该表，已是按表查）是否还需要缓存以外的手段。
- 候选里出现列名与关键字同名（如 `name`/`date`）导致排序混乱 → 停下来讨论是否给列更高优先级（编辑器只做模糊打分，我们只能靠顺序）。
