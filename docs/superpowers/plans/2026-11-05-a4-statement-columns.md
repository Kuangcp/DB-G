# A4：按语句上下文补列（JOIN / CTE / 子查询作用域） Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 SELECT 列表、GROUP BY、ORDER BY、WHERE 等任何位置，按**光标所在语句（及其所在子查询作用域）引用的表**补列；`FROM a JOIN b JOIN c` 补三张表的列；`WITH x AS (...)` 的 CTE 视为其 FROM 里的表；语句里没有任何表名时**不出列**（只给关键字/表名）。

**Architecture:** 新增 `ui/sqlscope.go`（纯函数 SQL 作用域扫描：语句切分、括号作用域、字符串/注释跳过、FROM/JOIN/UPDATE/INTO 关系提取、别名表、CTE 展开、派生表标记）。`ui/completion.go` 的候选优先级变为：点号上下文（表名/别名） → **语句表列** → 目录（表/视图+关键字） → 词表。列缓存与加载复用 A3 的 `catalogCache`/`ensureColumns`。

**Tech Stack:** Go 1.27.3、`ui/editor` 的 `CompletionRequest`、手写 SQL 词法扫描（不引入 parser 依赖）。

**Spec:** `docs/superpowers/specs/2026-11-05-editor-completions-design.md`（A3 的能力面扩展；本计划是 A4）

## Global Constraints

- 不新增依赖；所有命令 `CGO_ENABLED=0`；全量 `-p 1`。
- 补全的按键路径不得查库：列命中缓存时纯内存组装；未命中先给词表并后台加载（同 A2/A3）。
- 无表可推断时**不得**回退成「全库所有列」（用户明确否决：噪声太大）。
- 解析只做词法近似：不解析带引号的标识符作为补全前缀（`"tbl".` 的列首选仍走点号分支的表名匹配）、不处理 `USING`/`NATURAL` 的隐式连接列、不推断 `SELECT *` 的展开。
- 上限：单作用域最多 8 张表、候选总量不超过 2500 条；超出按出现顺序截断。
- 文案中文；提交信息 `taskN: …`。每任务收尾绿：`CGO_ENABLED=0 go test -p 1 ./...`、`go vet ./...`。

## Review Focus

1. **字符串/注释里的伪表名**：`WHERE note = 'from fake_table'`、`-- from x` 不能影响作用域。（Task 1）
2. **子查询作用域隔离**：`WHERE id IN (SELECT ... FROM b)` 里应只出 b 的列，外层 a 的列不能混进来；反之在外层不能出 b。（Task 1、Task 3）
3. **CTE 解析**：`WITH x AS (SELECT * FROM a) SELECT ... FROM x` 出 a 的列；CTE 引用链（`WITH x AS (... FROM a), y AS (... FROM x)`）要能展开；自引用不死循环。（Task 1）
4. **无表语句**：`select na|` 不出任何列（用户明确要求），且不因为「目录已加载」而退化成全库列。（Task 3）
5. **别名**：`FROM med_school m` 后 `m.` 出 med_school 的列；普通位置也出它的列（别名不改变列集）。（Task 1、Task 3）

---

### Task 1: `ui/sqlscope.go` —— 语句切分、作用域与关系提取（纯逻辑）

**Files:**
- Create: `ui/sqlscope.go`
- Test: `ui/sqlscope_test.go`

**Interfaces:**
- Consumes: 无（纯字符串处理）；后续 Task 2/3 消费。
- Produces:
  ```go
  // relationRef 是语句里出现的一张表；Alias 非空表示 FROM/JOIN 给了别名。
  type relationRef struct {
      Table string
      Alias string
  }

  // stmtScopeAt 返回光标所在的「作用域」文本：最内层包住光标的括号块（子查询），
  // 没有括号时是光标所在的整条语句（按 ; 切，跳过字符串/注释）。offset 为字节偏移。
  func stmtScopeAt(text string, offset int) string

  // relationsIn 从作用域文本里提取表：FROM 列表、JOIN、UPDATE、INTO；
  // CTE（WITH 名 AS (…)) 展开为其内部 FROM 的表（递归，深度上限 8，环保护）；
  // 派生表（FROM (SELECT …) s）登记为「未知」，不出现在结果里。
  func relationsIn(scope string) []relationRef

  // aliasMap 把别名映射到表名（供点号分支用）；同名表后出现的别名覆盖先出现的。
  func aliasMap(rels []relationRef) map[string]string
  ```
  内部辅助（同文件，私有）：`splitScope`（括号作用域）、`tokenizeSQL`（ident/number/punct/字符串跳过/注释跳过）、`cteDefs`。

- [ ] **Step 1: Write the failing tests**

```go
func TestStmtScopeAt(t *testing.T) {
	text := "select * from a; select id, na from b where id in (select x from c); -- tail"
	// 第二条语句里的子查询
	inner := stmtScopeAt(text, strings.Index(text, "select x"))
	if !strings.HasPrefix(inner, "select x from c") {
		t.Fatalf("inner scope: %q", inner)
	}
	outer := stmtScopeAt(text, strings.Index(text, "select id"))
	if strings.Contains(outer, "select x") {
		t.Fatalf("outer scope must stop at the parenthesis: %q", outer)
	}
	if got := stmtScopeAt("select 1; select 2", strings.Index("select 1; select 2", "select 2")); got != "select 2" {
		t.Fatalf("statement split: %q", got)
	}
	if got := stmtScopeAt("select ';' from a", 10); !strings.Contains(got, "select ';' from a") {
		t.Fatalf("semicolon in string must not split: %q", got)
	}
}

func TestRelationsBasicClauses(t *testing.T) {
	for _, c := range []struct {
		sql  string
		want []string
	}{
		{"select id, name from med_school", []string{"med_school"}},
		{"select * from a join b on a.id = b.id join c on c.id = b.id", []string{"a", "b", "c"}},
		{"select * from a, b where a.id = b.id", []string{"a", "b"}},
		{"select * from a left outer join b using (id)", []string{"a", "b"}},
		{"update med_school set name = 1 where id = 2", []string{"med_school"}},
		{"insert into med_school (name) values ('x')", []string{"med_school"}},
		{"select * from app.med_school", []string{"med_school"}},
		{"select * from `med_school`", []string{"med_school"}},
	} {
		got := relationsOf(c.sql)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("%q → %v want %v", c.sql, got, c.want)
		}
	}
}

func TestRelationsStringsAndCommentsIgnored(t *testing.T) {
	sql := `select * from real_t where note = 'from fake_t' -- from also_fake
	/* from block_fake */ and x = 1`
	got := relationsOf(sql)
	if strings.Join(got, ",") != "real_t" {
		t.Fatalf("got %v", got)
	}
}

func TestRelationsCTE(t *testing.T) {
	sql := `with x as (select * from a), y as (select * from x join b on true)
	select * from y`
	got := relationsOf(sql)
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("cte chain: %v", got)
	}
	// 自引用不得死循环
	_ = relationsOf(`with r as (select * from r) select * from r`)
}

func TestRelationsDerivedTableSkipped(t *testing.T) {
	got := relationsOf(`select * from (select * from inner_t) d join real_t on true`)
	if strings.Join(got, ",") != "real_t" {
		t.Fatalf("got %v", got)
	}
}

func TestAliasMap(t *testing.T) {
	rels := relationsOfRefs("select * from med_school m join b on true join c")
	m := aliasMap(rels)
	if m["m"] != "med_school" || m["b"] != "b" || m["c"] != "c" {
		t.Fatalf("alias map: %v", m)
	}
}

// 测试辅助
func relationsOf(sql string) []string {
	var out []string
	for _, r := range relationsIn(sql) {
		out = append(out, r.Table)
	}
	return out
}

func relationsOfRefs(sql string) []relationRef { return relationsIn(sql) }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'StmtScopeAt|Relations|AliasMap' -v`
Expected: 编译失败 `undefined: relationsIn` 等。

- [ ] **Step 3: Implement**

- `tokenizeSQL`：只产出 `ident`（含 `` ` ``/`"` 包裹时去掉包裹）、`punct`（`,`、`(`、`)`、`.`、`;`）与关键字（统一大写比较）；字符串字面量整体跳过；`--` 到行尾、`/*…*/` 跳过。
- `stmtScopeAt`：先用 tokenizer/扫描找到光标所在的 `;` 段（跳过字符串/注释）；再找包住光标的最内层 `(`/`)`；返回该括号内的文本（不含括号）。
- `relationsIn`：在 scope 上扫描 token 流：
  - `FROM` 后按逗号列表解析 `relation := ident ['.' ident] [[AS] ident]`，遇到 `(` 开头 → 派生表：跳到匹配 `)`，登记一个「未知别名」到 `aliasMap` 的占位（表名空）。
  - `JOIN` 后解析一个 relation；`UPDATE`/`INTO` 后解析一个 relation。
  - scope 以 `WITH` 开头：解析 `name AS ( … )` 序列，递归 `relationsIn` 括号体并存入 `cte`；之后 FROM 里遇到 CTE 名 → 展开为它的表（递归深度 ≤8，带 visiting 集合防环）。CTE 自身的 relations 也返回（所以 `WITH x AS (FROM a) FROM x` 得到 a）。
- `aliasMap`：跳过 `Table == ""` 的派生表别名；其余 `Alias`→`Table`；无别名时 `Table`→`Table` 也登记（点号 `med_school.` 仍可用）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'StmtScopeAt|Relations|AliasMap' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add ui/sqlscope.go ui/sqlscope_test.go
git commit -m "task1: SQL 作用域扫描（语句/括号/CTE/别名/派生表）"
```

---

### Task 2: 多表列候选组装与缓存（纯逻辑）

**Files:**
- Modify: `ui/catalog.go`
- Test: `ui/catalog_test.go`

**Interfaces:**
- Consumes: Task 1 的 `relationRef`；A3 的 `getColumns`/`buildColumnItems`。
- Produces:
  ```go
  // buildStatementItems 组装「多表列 + 词表」：按 rels 顺序出列（Detail 多表时为
  // "表 · 类型"，单表时为类型），去重 (label, detail)，然后接词表。
  // cols[i] 为 nil 表示该表列尚未加载。
  func buildStatementItems(rels []string, cols [][]engine.Column) []myeditor.Completion

  // catalogCache.getColumnsMany 返回各表列与「是否全部命中」。
  func (c *catalogCache) getColumnsMany(connID string, tables []string) ([][]engine.Column, bool)
  ```
  `catalogEntry` 需额外保存原始列（`cols []engine.Column`），供跨表组装；`getColumns` 仍返回组装好的单表候选。

- [ ] **Step 1: Write the failing tests**

```go
func TestBuildStatementItemsMultiTable(t *testing.T) {
	items := buildStatementItems([]string{"a", "b"}, [][]engine.Column{
		{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}},
		{{Name: "id", Type: "INTEGER"}, {Name: "bcol", Type: "TEXT"}},
	})
	var name, bcol, idDetail []string
	for _, it := range items {
		switch it.Label {
		case "name":
			name = append(name, it.Detail)
		case "bcol":
			bcol = append(bcol, it.Detail)
		case "id":
			idDetail = append(idDetail, it.Detail)
		}
	}
	if len(name) != 1 || !strings.Contains(name[0], "a") {
		t.Fatalf("name detail: %v", name)
	}
	if len(bcol) != 1 || !strings.Contains(bcol[0], "b") {
		t.Fatalf("bcol detail: %v", bcol)
	}
	if len(idDetail) != 2 {
		t.Fatalf("same column in two tables must show both: %v", idDetail)
	}
	if items[0].Kind != myeditor.CompletionField {
		t.Fatalf("columns must come first: %+v", items[0])
	}
}

func TestBuildStatementItemsSingleTableDetail(t *testing.T) {
	items := buildStatementItems([]string{"a"}, [][]engine.Column{{{Name: "name", Type: "TEXT"}}})
	if items[0].Label != "name" || items[0].Detail != "TEXT" {
		t.Fatalf("single table detail must be the type: %+v", items[0])
	}
}

func TestGetColumnsMany(t *testing.T) {
	c := newCatalogCache()
	c.finishColumns("c", "a", []engine.Column{{Name: "x"}})
	cols, ok := c.getColumnsMany("c", []string{"a"})
	if !ok || len(cols) != 1 || cols[0][0].Name != "x" {
		t.Fatalf("cols=%v ok=%v", cols, ok)
	}
	if _, ok := c.getColumnsMany("c", []string{"a", "b"}); ok {
		t.Fatal("missing table must report not-all-hit")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'BuildStatementItems|GetColumnsMany' -v`
Expected: 编译失败 `undefined: buildStatementItems`。

- [ ] **Step 3: Implement**

- `columnEntry` 增加 `cols []engine.Column`。
- `getColumnsMany`：逐表查 `cols[connID][table]`（同一 TTL 判定），任一缺失 → `ok=false` 但**仍返回已命中的那些**（调用方据此决定是否等待加载）。
- `buildStatementItems`：`rels` 顺序遍历；`cols[i]` 为 nil 跳过；多表（`len(rels) > 1`）时 `Detail = rels[i] + " · " + type`，单表用 type（空类型 "列"）；去重键 `label+"\x00"+detail`；列在前，词表在后（复用 `sqlWordItems`）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'BuildStatementItems|GetColumnsMany|Catalog' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add ui/catalog.go ui/catalog_test.go
git commit -m "task2: 多表列候选组装与批量缓存读取"
```

---

### Task 3: 接线（语句列优先）+ UI 测试 + 文档

**Files:**
- Modify: `ui/completion.go`（候选优先级、`ensureColumnsFor`、memo）、`Readme.md`
- Test: `ui/completion_test.go`

**Interfaces:**
- Consumes: Task 1/2 全部；A3 的 `ensureColumns`。
- Produces:
  ```go
  func (a *root) statementRels() []relationRef       // 基于光标所在作用域
  func (a *root) columnsForStatement(connID string) ([]myeditor.Completion, bool) // 命中全表缓存
  func (a *root) ensureStatementColumns(connID string) // 为缺失的表逐个发起加载
  ```
  不做额外 memo：列已在 `catalogCache` 里按表缓存，组装是 O(表数 × 列数) 的纯内存拼接（≤8 表）。

- [ ] **Step 1: Write the failing tests**

```go
func setupA4(t *testing.T) (*root, app.ConnProfile) {
	t.Helper()
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE med_school (id INTEGER PRIMARY KEY, name TEXT, zz_col TEXT)`,
		`CREATE TABLE b (id INTEGER PRIMARY KEY, bcol TEXT)`,
		`CREATE TABLE inner_t (zz_inner TEXT)`,
	} {
		if _, err := a.sess.RunStatement(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	a.catalog.invalidate(prof.ID)
	rec, err := a.consoleMgr().Create(prof.ID, "语句列")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	return a, prof
}

// 把 sql 写进编辑器，光标放在 marker（"|"）处，敲一个字母触发补全，返回菜单文本
func menuAt(t *testing.T, a *root, sql string, typed string) []string {
	t.Helper()
	marker := strings.Index(sql, "|")
	if marker < 0 {
		t.Fatal("missing marker")
	}
	text := strings.Replace(sql, "|", "", 1)
	a.setActiveText(text)
	caret := len([]rune(text[:marker]))
	a.code.SetSelection(caret, caret)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Click("编辑器")
	tt.Frame()
	a.code.SetSelection(caret, caret)
	if typed != "" {
		tt.TypeKey(0, 0, typed)
	}
	for range 8 {
		tt.Frame()
	}
	return tt.Texts()
}

func TestStatementColumnsInSelectList(t *testing.T) {
	a, _ := setupA4(t)
	texts := menuAt(t, a, "select id, na| from med_school;", "m")
	if !hasText(texts, "name") {
		t.Fatalf("select-list column missing: %v", texts)
	}
}

func TestStatementColumnsInGroupAndOrderBy(t *testing.T) {
	a, _ := setupA4(t)
	if texts := menuAt(t, a, "select id from med_school group by na|", "m"); !hasText(texts, "name") {
		t.Fatalf("group by column missing: %v", texts)
	}
	if texts := menuAt(t, a, "select id from med_school order by zz|", "z"); !hasText(texts, "zz_col") {
		t.Fatalf("order by column missing: %v", texts)
	}
}

func TestStatementColumnsJoinMultipleTables(t *testing.T) {
	a, _ := setupA4(t)
	texts := menuAt(t, a, "select * from med_school m join b on m.id = b.id where bc|", "b")
	if !hasText(texts, "bcol") || !hasText(texts, "name") {
		t.Fatalf("join must offer both tables' columns: %v", texts)
	}
}

func TestStatementColumnsCTE(t *testing.T) {
	a, _ := setupA4(t)
	texts := menuAt(t, a, "with x as (select * from med_school) select na| from x", "m")
	if !hasText(texts, "name") {
		t.Fatalf("cte column missing: %v", texts)
	}
}

func TestStatementColumnsSubqueryScope(t *testing.T) {
	a, _ := setupA4(t)
	inner := menuAt(t, a, "select * from med_school where id in (select zz| from inner_t)", "z")
	if !hasText(inner, "zz_inner") {
		t.Fatalf("inner scope column missing: %v", inner)
	}
	outer := menuAt(t, a, "select * from med_school where id in (select zz_inner from inner_t) and na|", "m")
	if !hasText(outer, "name") {
		t.Fatalf("outer scope column missing: %v", outer)
	}
}

func TestNoTableNoColumns(t *testing.T) {
	a, _ := setupA4(t)
	texts := menuAt(t, a, "select na|", "m")
	if hasText(texts, "name") || hasText(texts, "zz_col") {
		t.Fatalf("no table mentioned → no columns: %v", texts)
	}
}

func TestAliasDotColumns(t *testing.T) {
	a, _ := setupA4(t)
	texts := menuAt(t, a, "select m.na| from med_school m", "m")
	if !hasText(texts, "name") {
		t.Fatalf("alias dot column missing: %v", texts)
	}
}
```

```go
func hasText(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'StatementColumns|NoTableNoColumns|AliasDotColumns' -v`
Expected: 多数 FAIL（列缺失）；`NoTableNoColumns` 可能已过。

- [ ] **Step 3: Implement**

- 候选优先级函数 `a.completionsAt(offset int) []myeditor.Completion`（供 `onCompletion` 与 `showCompletionsAtCursor` 共用）：
  1. 点号上下文：`ident` → `aliasMap(rels)` 或目录关系 → 该表列（A3 语义，别名也认）。
  2. 语句列：`rels := a.statementRels()`；`if len(rels) > 0 { if items, ok := a.columnsForStatement(connID); ok { return items } }`
  3. 目录命中 → 表/视图+词；否则词表。
  返回 `(items, needsLoad bool)`：`needsLoad` 时调用方先 `ShowCompletions` 现有 items，再 `ensureStatementColumns`/`ensureColumns`。
- `statementRels`：`scope := stmtScopeAt(a.code.Text(), a.code.state.Cursor())`；`relationsIn(scope)`（原样返回，大小写保留）。
  `columnsForStatement`：先用 `hasRelation` 过滤（大小写不敏感）——**目录还没加载时**（过滤后为空）调 `a.ensureCatalog(connID)` 并当作「暂无列」，等目录就绪后重弹；绝不因为目录已加载就把全库列塞进来。
- `ensureStatementColumns`：对 `rels` 里列未命中的表逐个 `a.ensureColumns(connID, table)`（每表一次，已有 inflight 去重）。
- `onCompletion`/`showCompletionsAtCursor` 改为调用 `a.completionsAt(...)`；点号分支保持 A3 行为。
- `Readme.md`：「列补全」一行补充「按当前语句/子查询里的 FROM·JOIN·UPDATE·INTO 表提示，支持 CTE 与多表 JOIN；语句里没有表时不提示列」。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'Completion|StatementColumns' -v` 然后 `CGO_ENABLED=0 go test -p 1 ./...`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add ui/completion.go ui/completion_test.go Readme.md
git commit -m "task3: 按语句上下文补列（多表/CTE/子查询作用域）+ Readme"
```

## 升级条件（何时停下来问）

- 某类写法解析错得离谱（例如 `FROM` 后的表名带 schema 引号、`JOIN LATERAL`、`MERGE`）→ 记录下来问是否值得支持，不硬塞。
- 语句列让弹层在 100k 行文件上键入 p99 超过 A1 的 15ms 预算 → 停下来汇报数字（候选过多时改为按前缀裁剪）。
- CTE/派生表需求超出「v1 只展开 CTE 的 FROM 表」的范围（例如派生表别名要出列）→ 停下来定边界。
