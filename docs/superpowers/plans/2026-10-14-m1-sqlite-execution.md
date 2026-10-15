# M1 · SQLite 执行 SQL 查看结果 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 db-g 中打通「打开 SQLite 文件 → 编辑器写 SQL → 异步执行 → 虚拟化结果表格查看」的端到端链路，同时落地 `engine`/`driver`/`app` 分层骨架，为 M2+ 的数据源扩展与 UI 功能打基础。

**Architecture:** 三层分离——`engine`（协议无关契约 + 注册表，叶子包）、`driver/sqlite`（database/sql + modernc 实现：串行执行 + 行数/字符预算 + ctx 取消）、`app`（Console 状态机：互斥锁保护 + generation 防迟到 + 30s 超时 + publish 回投）；原 package main 迁入 `ui` 包，视图经 `ui.SplitVertical` 分为编辑器/结果两半，后台协程经 `win.Update` 回投重绘。spec：`docs/superpowers/specs/2026-10-14-db-g-roadmap-design.md`。

**Tech Stack:** Go 1.27.1（`/home/zk/Code/go/bin/go1.27.1`）、mygo v0.4.8（自绘 GUI：`ui.Table`/`ui.SplitVertical`/`ui.Tester`）、modernc.org/sqlite v1.33.1（纯 Go 无 cgo 的 database/sql 驱动，已验证支持 ctx 中断）。

## Global Constraints

- Go 工具链必须用 `/home/zk/Code/go/bin/go1.27.1`（本机默认 go 为 1.26.5，不满足 go.mod）。先在 shell 执行 `export GO=/home/zk/Code/go/bin/go1.27.1`，下文以 `$GO` 引用。
- 模块名 `db-g`；本计划唯一新依赖 `modernc.org/sqlite v1.33.1`。
- **执行语义（spec 偏离声明）**：mygo v0.4.8 的 `TextAreaBuffer` 选区/光标为包内私有（`ed.selection()` 未导出），M1 执行**整个编辑器缓冲区**；选区/当前语句定位归入 M3。
- 结果预算：行上限 **500**（读到第 501 行 → `Truncated=true`）、单元格 **1MB** 字节（rune 安全截断）、单结果 **16MB** 字节（超限停止追加行）。
- 执行超时 **30s**（`context.WithTimeout`）；UI 线程不碰驱动；`app` 状态由互斥锁保护，重绘经 `publish`（生产 = `win.Update` 包装，测试 = 直调）。
- 性能门槛（spec 固化，Task 8 断言）：spike 单键 p99 VV 策略——3k ≤ 8ms、12k ≤ 15ms、100k ≤ 50ms（基线 3.9/7.4/40.1ms）；结果网格 100k 行 × 50 列帧 p50 ≤ 16ms。
- UI 生命周期：`ui.Element` 不得跨帧/进协程存储（`ListState`/`Handle`/`*mygo.Window` 除外）；Task 4 起每任务收尾跑 `$GO tool mygo vet .`。
- 每任务一次 commit，message 格式 `taskN: <内容>`（N = 本计划任务号）。
- 用户可见文案用中文（沿用「编辑器」既有惯例）；代码不写行内注释，仅导出 API 写 doc comment。

## File Structure

```
db-g/
├── main.go                     # package main：薄入口（Task 4 重写）
├── engine/                     # Task 1
│   ├── model.go                #   QueryResult
│   ├── caps.go                 #   Capabilities 能力位
│   ├── session.go              #   Session/Profile/Factory/Register/Open
│   └── session_test.go
├── driver/sqlite/              # Task 2
│   ├── sqlite.go               #   串行执行 + 预算 + Cancel
│   └── sqlite_test.go          #   :memory: 集成测试
├── app/                        # Task 3
│   ├── console.go              #   执行状态机
│   └── console_test.go
├── ui/                         # Task 4 起：原 package main 全部迁入
│   ├── root.go                 #   root 状态 + rootView + NewRoot/Attach（Task 4）
│   ├── editor.go / findmatch.go / fixtures.go / metrics.go   # Task 4 迁入
│   ├── result.go               #   结果面板（Task 5）
│   ├── run.go                  #   打开文件 + 执行接线（Task 6）
│   ├── result_bench_test.go    #   结果网格基准（Task 7）
│   └── *_test.go               #   原测试迁入 + 新增
└── highlight/                  # 不动
```

依赖方向：`ui → app → engine ← driver/sqlite`；根 `main → ui` + 空白导入 `driver/sqlite`（触发注册）。

---

### Task 1: engine 契约包

**Files:**
- Create: `engine/model.go`
- Create: `engine/caps.go`
- Create: `engine/session.go`
- Test: `engine/session_test.go`

**Interfaces:**
- Consumes: 无（叶子包）。
- Produces（后续任务依赖的确切签名）:
  - `type QueryResult struct { Columns []string; Rows [][]any; Truncated bool; IsQuery bool; Affected int64 }`
  - `type Capabilities uint32`；常量 `CapEditableResult / CapFetchMore / CapObjectDDL / CapSQLCompletion`；方法 `Has(Capabilities) bool`
  - `type Profile struct { Kind, Name, DSN string }`
  - `type Session interface { Open(context.Context) error; Close() error; RunStatement(context.Context, string) (*QueryResult, error); Cancel() bool; Caps() Capabilities }`
  - `type Factory func(Profile) (Session, error)`；`Register(kind string, f Factory)`（重复注册 panic）；`Open(Profile) (Session, error)`（未知 Kind 返回错误）

- [ ] **Step 1: 写失败测试**

创建 `engine/session_test.go`：

```go
package engine

import (
	"context"
	"testing"
)

type stubSession struct{}

func (stubSession) Open(context.Context) error   { return nil }
func (stubSession) Close() error                 { return nil }
func (stubSession) RunStatement(context.Context, string) (*QueryResult, error) {
	return nil, nil
}
func (stubSession) Cancel() bool   { return false }
func (stubSession) Caps() Capabilities { return 0 }

func TestRegisterOpen(t *testing.T) {
	if _, err := Open(Profile{Kind: "definitely-unknown"}); err == nil {
		t.Fatal("unknown kind must return an error")
	}
	Register("stubkind", func(p Profile) (Session, error) { return stubSession{}, nil })
	s, err := Open(Profile{Kind: "stubkind", Name: "n1", DSN: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(stubSession); !ok {
		t.Fatalf("got %T, want stubSession", s)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Register must panic")
		}
	}()
	Register("stubkind", func(p Profile) (Session, error) { return stubSession{}, nil })
}

func TestCapabilitiesHas(t *testing.T) {
	var c Capabilities = CapEditableResult | CapFetchMore
	if !c.Has(CapEditableResult) || !c.Has(CapFetchMore) {
		t.Fatal("set bits must report true")
	}
	if c.Has(CapObjectDDL) {
		t.Fatal("unset bit must report false")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `$GO test ./engine/ -count=1`
Expected: FAIL（`no Go files` 或 `undefined: Open`）

- [ ] **Step 3: 实现 engine 包**

创建 `engine/model.go`：

```go
package engine

// QueryResult 是一次语句执行的结果：查询结果集，或无结果集语句的受影响行数。
type QueryResult struct {
	Columns   []string
	Rows      [][]any
	Truncated bool
	IsQuery   bool
	Affected  int64
}
```

创建 `engine/caps.go`：

```go
package engine

// Capabilities 是数据源能力位；UI 按位降级功能。
type Capabilities uint32

const (
	CapEditableResult Capabilities = 1 << iota
	CapFetchMore
	CapObjectDDL
	CapSQLCompletion
)

func (c Capabilities) Has(x Capabilities) bool { return c&x != 0 }
```

创建 `engine/session.go`：

```go
package engine

import (
	"context"
	"fmt"
	"sync"
)

// Profile 描述一个数据源连接的静态配置。
type Profile struct {
	Kind string // "sqlite" | "mysql" | "postgres"
	Name string // 展示名
	DSN  string // sqlite：文件路径或 ":memory:"
}

// Session 是协议无关的数据源会话契约；实现由 driver 包提供。
type Session interface {
	Open(ctx context.Context) error
	Close() error
	RunStatement(ctx context.Context, sql string) (*QueryResult, error)
	Cancel() bool
	Caps() Capabilities
}

// Factory 创建未打开的会话。
type Factory func(p Profile) (Session, error)

var (
	regMu     sync.RWMutex
	factories = map[string]Factory{}
)

// Register 注册数据源工厂，由 driver 包 init() 调用；重复注册 panic。
func Register(kind string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := factories[kind]; dup {
		panic("engine: duplicate Register " + kind)
	}
	factories[kind] = f
}

// Open 按 Profile.Kind 创建未打开的会话。
func Open(p Profile) (Session, error) {
	regMu.RLock()
	f, ok := factories[p.Kind]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("engine: unknown data source kind %q", p.Kind)
	}
	return f(p)
}
```

- [ ] **Step 4: 运行确认通过**

Run: `$GO test ./engine/ -count=1 -v`
Expected: `--- PASS: TestRegisterOpen`、`--- PASS: TestCapabilitiesHas`

- [ ] **Step 5: Commit**

```bash
git add engine/
git commit -m "task1: engine 契约包（Session/能力位/注册表）"
```

### Task 2: driver/sqlite 实现与集成测试

**Files:**
- Modify: `go.mod` / `go.sum`（新增 modernc.org/sqlite v1.33.1）
- Create: `driver/sqlite/sqlite.go`
- Test: `driver/sqlite/sqlite_test.go`

**Interfaces:**
- Consumes: Task 1 的 `engine.Session / Profile / Register / QueryResult / Capabilities`。
- Produces:
  - `init()` 注册 `Kind="sqlite"`（`DSN` = 文件路径，或 `":memory:"`）
  - 行为约定：`Open` 对不存在文件报错（`:memory:` 除外）；`SetMaxOpenConns(1)` + `sync.Mutex` 串行执行；查询类语句走 `query()` 预算逻辑；`Cancel()` 空闲返回 false、执行中经 `ctx → sqlite3_interrupt` 取消
  - `isQueryLike(string) bool`（首词启发式：SELECT/WITH/PRAGMA/EXPLAIN/VALUES；完整词法归 M3）

- [ ] **Step 1: 添加依赖**

```bash
export GO=/home/zk/Code/go/bin/go1.27.1
$GO get modernc.org/sqlite@v1.33.1
```

Expected: 退出码 0；`go.mod` 出现 `modernc.org/sqlite v1.33.1` 及传递依赖。

- [ ] **Step 2: 写失败测试**

创建 `driver/sqlite/sqlite_test.go`：

```go
package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"db-g/engine"
)

func newMemSession(t *testing.T) engine.Session {
	t.Helper()
	s, err := engine.Open(engine.Profile{Kind: "sqlite", Name: "test", DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)

	res, err := s.RunStatement(ctx, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatal(err)
	}
	if res.IsQuery || res.Affected != 0 {
		t.Fatalf("ddl result: %+v", res)
	}

	insert, err := s.RunStatement(ctx, "INSERT INTO users (name) VALUES ('alice'), ('bob'), ('carol')")
	if err != nil {
		t.Fatal(err)
	}
	if insert.Affected != 3 {
		t.Fatalf("affected = %d, want 3", insert.Affected)
	}

	q, err := s.RunStatement(ctx, "SELECT id, name FROM users ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if !q.IsQuery || q.Truncated {
		t.Fatalf("flags: %+v", q)
	}
	if len(q.Columns) != 2 || q.Columns[0] != "id" || q.Columns[1] != "name" {
		t.Fatalf("columns: %v", q.Columns)
	}
	if len(q.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(q.Rows))
	}
	if q.Rows[1][0].(int64) != 2 || q.Rows[1][1].(string) != "bob" {
		t.Fatalf("row 1: %#v", q.Rows[1])
	}
	if q.Rows[2][1] != "carol" {
		t.Fatalf("row 2: %#v", q.Rows[2])
	}
}

func TestRowLimitAndTruncation(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, "CREATE TABLE big (i INTEGER)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 601; i++ {
		if _, err := s.RunStatement(ctx, "INSERT INTO big (i) VALUES (1)"); err != nil {
			t.Fatal(err)
		}
	}
	q, err := s.RunStatement(ctx, "SELECT i FROM big")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Rows) != 500 {
		t.Fatalf("rows = %d, want 500", len(q.Rows))
	}
	if !q.Truncated {
		t.Fatal("601 rows must mark Truncated")
	}
}

func TestErrorStatement(t *testing.T) {
	s := newMemSession(t)
	if _, err := s.RunStatement(context.Background(), "SELECT * FROM missing"); err == nil {
		t.Fatal("missing table must error")
	}
}

func TestNotOpen(t *testing.T) {
	s, err := engine.Open(engine.Profile{Kind: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(context.Background(), "SELECT 1"); err == nil ||
		!strings.Contains(err.Error(), "not open") {
		t.Fatalf("err = %v, want not open", err)
	}
}

func TestOpenMissingFile(t *testing.T) {
	s, err := engine.Open(engine.Profile{Kind: "sqlite", DSN: filepath.Join(t.TempDir(), "no", "x.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(context.Background()); err == nil {
		t.Fatal("missing file must fail Open")
	}
}

func TestCancelIdle(t *testing.T) {
	if newMemSession(t).Cancel() {
		t.Fatal("idle Cancel must return false")
	}
}

func TestCancelRunning(t *testing.T) {
	s := newMemSession(t)
	if _, err := s.RunStatement(context.Background(), "CREATE TABLE n (i INTEGER)"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.RunStatement(context.Background(),
			"WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c WHERE i<2000000000) SELECT count(*) FROM c")
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !s.Cancel() {
		if time.Now().After(deadline) {
			t.Fatal("Cancel never observed a running statement")
		}
		time.Sleep(2 * time.Millisecond)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled statement must return an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("statement did not stop after Cancel")
	}
}

func TestCellAndResultBudget(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, "CREATE TABLE c (v TEXT)"); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("x", (1<<20)+100)
	if _, err := s.RunStatement(ctx, "INSERT INTO c (v) VALUES ('"+huge+"')"); err != nil {
		t.Fatal(err)
	}
	q, err := s.RunStatement(ctx, "SELECT v FROM c")
	if err != nil {
		t.Fatal(err)
	}
	cell := q.Rows[0][0].(string)
	if len(cell) != 1<<20 {
		t.Fatalf("cell len = %d, want %d", len(cell), 1<<20)
	}
	if !q.Truncated {
		t.Fatal("oversize cell must mark Truncated")
	}

	mb := strings.Repeat("y", 1<<20)
	for i := 0; i < 17; i++ {
		if _, err := s.RunStatement(ctx, "INSERT INTO c (v) VALUES ('"+mb+"')"); err != nil {
			t.Fatal(err)
		}
	}
	q, err = s.RunStatement(ctx, "SELECT v FROM c")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Rows) != 17 {
		t.Fatalf("rows = %d, want 17 (one row overshoots the 16MB budget)", len(q.Rows))
	}
	if !q.Truncated {
		t.Fatal("16MB budget must mark Truncated")
	}
}
```

- [ ] **Step 3: 运行确认失败**

Run: `$GO test ./driver/sqlite/ -count=1`
Expected: FAIL（`no Go files` / `unknown data source kind "sqlite"`）

- [ ] **Step 4: 实现 driver/sqlite/sqlite.go**

```go
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"db-g/engine"

	_ "modernc.org/sqlite"
)

const (
	rowLimit      = 500
	maxCellBytes  = 1 << 20
	maxResultByte = 16 << 20
)

func init() {
	engine.Register("sqlite", openSession)
}

type session struct {
	path      string
	db        *sql.DB
	mu        sync.Mutex
	curCancel atomic.Pointer[context.CancelFunc]
}

func openSession(p engine.Profile) (engine.Session, error) {
	return &session{path: p.DSN}, nil
}

func (s *session) Open(ctx context.Context) error {
	if s.path != ":memory:" {
		if _, err := os.Stat(s.path); err != nil {
			return fmt.Errorf("sqlite: %w", err)
		}
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return err
	}
	s.db = db
	return nil
}

func (s *session) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *session) Caps() engine.Capabilities { return 0 }

func (s *session) Cancel() bool {
	c := s.curCancel.Load()
	if c == nil {
		return false
	}
	(*c)()
	return true
}

func (s *session) RunStatement(parent context.Context, query string) (*engine.QueryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("sqlite: session not open")
	}
	ctx, cancel := context.WithCancel(parent)
	s.curCancel.Store(&cancel)
	defer func() {
		cancel()
		s.curCancel.Store(nil)
	}()
	if isQueryLike(query) {
		return s.query(ctx, query)
	}
	res, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		n = 0
	}
	return &engine.QueryResult{Affected: n}, nil
}

func isQueryLike(q string) bool {
	q = strings.TrimSpace(q)
	if i := strings.IndexFunc(q, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n'
	}); i >= 0 {
		q = q[:i]
	}
	switch strings.ToUpper(q) {
	case "SELECT", "WITH", "PRAGMA", "EXPLAIN", "VALUES":
		return true
	}
	return false
}

func (s *session) query(ctx context.Context, query string) (*engine.QueryResult, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := &engine.QueryResult{Columns: cols, IsQuery: true}
	total := 0
	for rows.Next() {
		if len(out.Rows) == rowLimit {
			out.Truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			switch t := v.(type) {
			case string:
				total += len(t)
				if len(t) > maxCellBytes {
					vals[i] = trimStr(t, maxCellBytes)
					out.Truncated = true
				}
			case []byte:
				total += len(t)
				if len(t) > maxCellBytes {
					vals[i] = append([]byte(nil), trimBytes(t, maxCellBytes)...)
					out.Truncated = true
				}
			}
		}
		out.Rows = append(out.Rows, vals)
		if total > maxResultByte {
			out.Truncated = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func trimStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && s[max]&0xC0 == 0x80 {
		max--
	}
	return s[:max]
}

func trimBytes(b []byte, max int) []byte {
	if len(b) <= max {
		return b
	}
	for max > 0 && b[max]&0xC0 == 0x80 {
		max--
	}
	return b[:max]
}
```

- [ ] **Step 5: 运行确认通过**

Run: `$GO test ./driver/sqlite/ -count=1 -race -v`
Expected: 全部 `--- PASS`。若 `TestCancelRunning` 偶发时序失败，重跑一次确认（轮询窗口 2s，长查询不可中断窗口极小）。

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum driver/
git commit -m "task2: driver/sqlite 实现（串行执行+行数/字符预算+ctx取消）"
```

### Task 3: app.Console 执行状态机

**Files:**
- Create: `app/console.go`
- Test: `app/console_test.go`

**Interfaces:**
- Consumes: Task 1 的 `engine.Session / QueryResult`。
- Produces:
  - `func NewConsole() *Console`（publish 默认直调）
  - `SetPublisher(f func(func()))`
  - `UseSession(engine.Session)` / `SwapSession(engine.Session)`（取消在途执行、generation++、清空状态、关闭旧会话）
  - `Session() engine.Session` / `Running() bool` / `Result() *engine.QueryResult` / `Err() string`
  - `Run(query string) bool`（nil 会话 / 执行中 / 空串 → false；30s 超时；完成经锁更新 + publish 重绘；generation 不匹配丢弃）
  - `Cancel()`（取消当前执行；完成回调把 `context canceled` 写入 Err）
  - `SetOutcome(res *engine.QueryResult, errMsg string)`（测试/基准注入）

- [ ] **Step 1: 写失败测试**

创建 `app/console_test.go`：

```go
package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"db-g/engine"
)

type fakeSession struct {
	mu    sync.Mutex
	got   string
	block chan struct{}
}

func (f *fakeSession) Open(context.Context) error { return nil }
func (f *fakeSession) Close() error                { return nil }
func (f *fakeSession) Caps() engine.Capabilities   { return 0 }
func (f *fakeSession) Cancel() bool                { return false }

func (f *fakeSession) RunStatement(ctx context.Context, q string) (*engine.QueryResult, error) {
	f.mu.Lock()
	f.got = q
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
			return &engine.QueryResult{IsQuery: true, Columns: []string{"ok"}, Rows: [][]any{{"y"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &engine.QueryResult{Affected: 3}, nil
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", d)
}

func TestRunCompletes(t *testing.T) {
	f := &fakeSession{}
	c := NewConsole()
	c.UseSession(f)
	if !c.Run("UPDATE t SET x=1") {
		t.Fatal("Run must start")
	}
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if c.Err() != "" {
		t.Fatalf("err: %s", c.Err())
	}
	if got := c.Result(); got == nil || got.Affected != 3 {
		t.Fatalf("result: %+v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.got != "UPDATE t SET x=1" {
		t.Fatalf("sql = %q", f.got)
	}
}

func TestRunGuards(t *testing.T) {
	c := NewConsole()
	if c.Run("SELECT 1") {
		t.Fatal("nil session must refuse")
	}
	f := &fakeSession{block: make(chan struct{})}
	c = NewConsole()
	c.UseSession(f)
	if !c.Run("SELECT 1") {
		t.Fatal("first run must start")
	}
	if c.Run("SELECT 2") {
		t.Fatal("second run while running must refuse")
	}
	close(f.block)
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if c.Run("") {
		t.Fatal("empty query must refuse")
	}
}

func TestCancelStopsRun(t *testing.T) {
	f := &fakeSession{block: make(chan struct{})}
	c := NewConsole()
	c.UseSession(f)
	if !c.Run("SELECT slow") {
		t.Fatal("run must start")
	}
	c.Cancel()
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if !strings.Contains(c.Err(), "context canceled") {
		t.Fatalf("err = %q", c.Err())
	}
}

func TestSwapSessionDropsInflight(t *testing.T) {
	old := &fakeSession{block: make(chan struct{})}
	c := NewConsole()
	c.UseSession(old)
	if !c.Run("SELECT 1") {
		t.Fatal("run must start")
	}
	next := &fakeSession{}
	c.SwapSession(next)
	if c.Running() {
		t.Fatal("swap must clear running")
	}
	if c.Session() != engine.Session(next) {
		t.Fatal("session must be swapped")
	}
	if c.Result() != nil || c.Err() != "" {
		t.Fatalf("outcome must be cleared: %v %q", c.Result(), c.Err())
	}
	close(old.block)
	time.Sleep(20 * time.Millisecond)
	if c.Running() || c.Result() != nil {
		t.Fatal("stale completion must be dropped by generation")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `$GO test ./app/ -count=1`
Expected: FAIL（`no Go files`）

- [ ] **Step 3: 实现 app/console.go**

```go
package app

import (
	"context"
	"sync"
	"time"

	"db-g/engine"
)

const queryTimeout = 30 * time.Second

// Console 承载一条控制台的执行状态；所有字段由 mu 保护。
type Console struct {
	mu      sync.Mutex
	publish func(func())
	sess    engine.Session
	gen     uint64
	running bool
	result  *engine.QueryResult
	err     string
	cancel  context.CancelFunc
}

func NewConsole() *Console {
	return &Console{publish: func(func()) {}}
}

func (c *Console) SetPublisher(f func(func())) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publish = f
}

func (c *Console) UseSession(s engine.Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sess = s
}

// SwapSession 取消在途执行、丢弃其结果并替换会话，同时关闭旧会话。
func (c *Console) SwapSession(s engine.Session) {
	c.mu.Lock()
	old := c.sess
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.gen++
	c.sess = s
	c.running, c.result, c.err = false, nil, ""
	pub := c.publish
	c.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	pub(func() {})
}

func (c *Console) Session() engine.Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sess
}

func (c *Console) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *Console) Result() *engine.QueryResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result
}

func (c *Console) Err() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// SetOutcome 注入结果（测试与性能基准用）。
func (c *Console) SetOutcome(res *engine.QueryResult, errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running, c.result, c.err = false, res, errMsg
}

// Run 在后台协程执行 query；无会话、已在执行或空串时返回 false。
func (c *Console) Run(query string) bool {
	c.mu.Lock()
	if c.sess == nil || c.running || query == "" {
		c.mu.Unlock()
		return false
	}
	c.gen++
	gen := c.gen
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	c.cancel = cancel
	c.running, c.result, c.err = true, nil, ""
	sess := c.sess
	c.mu.Unlock()

	go func() {
		res, err := sess.RunStatement(ctx, query)
		cancel()
		c.mu.Lock()
		if gen == c.gen {
			c.running, c.cancel = false, nil
			if err != nil {
				c.result, c.err = nil, err.Error()
			} else {
				c.result, c.err = res, ""
			}
		}
		pubFn := c.publish
		c.mu.Unlock()
		pubFn(func() {})
	}()
	return true
}

// Cancel 取消当前执行；完成回调把取消错误写入 Err。
func (c *Console) Cancel() {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
```

- [ ] **Step 4: 运行确认通过**

Run: `$GO test ./app/ -count=1 -race -v`
Expected: 全部 `--- PASS`

- [ ] **Step 5: Commit**

```bash
git add app/
git commit -m "task3: app.Console 执行状态机（超时/取消/generation/回投）"
```

---

### Task 4: 包结构迁移（main → ui + 薄入口）

**Files:**
- Move: `editor.go` `findmatch.go` `fixtures.go` `metrics.go` `bench_test.go` `editor_test.go` `findmatch_test.go` `fixtures_test.go` `metrics_test.go` → `ui/`
- Create: `ui/root.go`（由 `main.go` 改造）
- Delete: `main.go` 内容（入口重建）
- Create: `main.go`（新薄入口）
- Modify: `ui/bench_test.go`、`ui/editor_test.go`（`newApp` → `newRoot`）
- Modify: `go.mod`（`go get -tool` mygo）

**Interfaces:**
- Consumes: Task 3 的 `app.NewConsole / SetPublisher`。
- Produces:
  - `ui.NewRoot() *root`（`root` 未导出；导出方法 `View(*ui.Context)`、`Attach(*mygo.Window)` 供 main 调用）
  - `newRoot(text string, s strats) *root`（同包测试入口，替代原 `newApp`）
  - `rootView(a *root) func(c *ui.Context)`（同包测试继续使用）
  - `(a *root) Attach(win *mygo.Window)`：存 win + `con.SetPublisher(win.Update 包装)`

- [ ] **Step 1: 迁移文件并改包名**

```bash
mkdir -p ui
git mv editor.go findmatch.go fixtures.go metrics.go bench_test.go editor_test.go findmatch_test.go fixtures_test.go metrics_test.go ui/
```

对这 9 个文件逐一编辑：首行 `package main` → `package ui`。

- [ ] **Step 2: 创建 `ui/root.go`（原 main.go 改造）**

从原 `main.go` 组装 `ui/root.go`：
- `fmtDur`（原 main.go:25-27）与 `rootView`（原 main.go:29-110）**原样拷贝**，仅 `rootView` 签名 `func rootView(a *app)` → `func rootView(a *root)`，函数体其余一字不改（含 HUD、spike 工具栏、查找栏、编辑器块）。
- `type app` / `newApp` / `main()` 按下文替换（`main()` 删除，入口在 Step 4 重建）：

```go
package ui

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
)

type root struct {
	ed         *editor
	findQuery  string
	findActive bool
	sampler    *Sampler
	con        *app.Console
	win        *mygo.Window
}

func newRoot(text string, s strats) *root {
	return &root{ed: NewEditor(text, s), sampler: NewSampler(128), con: app.NewConsole()}
}

// NewRoot 创建交互窗口状态（初始 3k 行 SQL，SCOPE 环境变量选策略）。
func NewRoot() *root {
	return newRoot(buildSQL(3_000), stratsFromEnv())
}

func stratsFromEnv() strats {
	switch os.Getenv("SCOPE") {
	case "FF":
		return strats{scopeFull, scopeFull}
	case "FV":
		return strats{scopeFull, scopeViewport}
	}
	return strats{scopeViewport, scopeViewport}
}

// Attach 在窗口创建后接通主线程回投与原生窗口句柄。
func (a *root) Attach(win *mygo.Window) {
	a.win = win
	a.con.SetPublisher(func(f func()) { win.Update(f) })
}

// View 是窗口内容入口。
func (a *root) View(c *ui.Context) {
	rootView(a)(c)
}
```

注意：`fmtDur` 的格式串保持原样 `fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))`。

- [ ] **Step 3: 更新同包测试的改名引用**

`ui/bench_test.go`（1 处：49 行）与 `ui/editor_test.go`（2 处：17、38 行）中 `newApp(` → `newRoot(` 全量替换；`rootView(` 调用不变。

- [ ] **Step 4: 新建薄入口 `main.go`**

```go
package main

import (
	"log"

	"github.com/egoist/mygo"
	mygoUI "github.com/egoist/mygo/ui"

	_ "db-g/driver/sqlite"
	"db-g/ui"
)

func main() {
	r := ui.NewRoot()
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "db-g",
			Width:   1280,
			Height:  800,
			Content: mygoUI.View(r.View),
		})
		r.Attach(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

（包内 `ui` 同时指本模块 `db-g/ui`，mygo 的 `ui` 必须别名 `mygoUI`；`ui/root.go` 等文件内 `import "github.com/egoist/mygo/ui"` 保持默认名 `ui`——文件作用域与包名不冲突，合法。）

- [ ] **Step 5: 注册 mygo vet 工具并运行检查**

```bash
$GO get -tool github.com/egoist/mygo/cmd/mygo
$GO tool mygo vet .
```

Expected: 退出码 0、`0 native UI lifetime issue(s)` 或等价空输出。若 `go get -tool` 不可用，降级 `$GO vet ./...` 并在 commit message 注明。

- [ ] **Step 6: 全量测试（跳过慢基准）**

```bash
$GO test ./... -skip TestSpikeMatrix -count=1
```

Expected: `? db-g [no test files]`、`ok db-g/ui`、`ok db-g/engine`、`ok db-g/app`、`ok db-g/driver/sqlite`、`ok db-g/highlight`（另有 `/bin/ld .sframe` 无害告警）。

- [ ] **Step 7: 交互冒烟（手动）**

Run: `$GO run .`
Expected: 窗口打开，HUD/spike 工具栏/查找栏/编辑器与迁移前一致；3k/12k/100k 按钮、Find、输入均正常。随后关闭窗口。

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "task4: 包结构迁移 main→ui + 薄入口 + mygo vet 工具"
```

### Task 5: 结果面板（表格/错误/截断/执行中）

**Files:**
- Modify: `ui/root.go`（root 增字段 + newRoot 初始化 + rootView 布局改 SplitVertical）
- Create: `ui/result.go`
- Test: `ui/result_test.go`

**Interfaces:**
- Consumes: Task 3 的 `Console.Running()/Result()/Err()/Cancel()/SetOutcome()/UseSession()/Run()`；Task 1 的 `engine.QueryResult`。
- Produces:
  - `root` 新增字段 `resultState ui.ListState`、`resultsHeight float32`（newRoot 初始化 `resultsHeight: 260`）
  - `(a *root) resultPane(c *ui.Context)`：四态渲染——执行中（Spinner+停止）/ 错误（danger 横幅）/ 结果（截断徽章+表格 或 OK 行数）/ 空态
  - `(a *root) resultTable(c *ui.Context, r *engine.QueryResult)`：`#` 行号列（Fixed, 宽56, End对齐）+ 数据列（M1 不可排序）
  - `cellText(v any) string`、`cutRunes(s string, n int) string`

- [ ] **Step 1: 写失败测试**

创建 `ui/result_test.go`：

```go
package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

type blockingFake struct{}

func (blockingFake) Open(context.Context) error { return nil }
func (blockingFake) Close() error               { return nil }
func (blockingFake) Caps() engine.Capabilities  { return 0 }
func (blockingFake) Cancel() bool               { return false }
func (blockingFake) RunStatement(ctx context.Context, _ string) (*engine.QueryResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func newTestRoot() *root {
	return newRoot("", strats{scopeViewport, scopeViewport})
}

func TestResultPaneEmpty(t *testing.T) {
	a := newTestRoot()
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("Ctrl+Enter 执行 SQL") {
		t.Fatalf("empty hint missing: %q", tt.Texts())
	}
}

func TestResultPaneTable(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true,
		Columns: []string{"id", "name"},
		Rows:    [][]any{{int64(1), "alice"}, {int64(2), nil}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	for range 3 {
		tt.Frame()
	}
	for _, want := range []string{"id", "name", "alice", "NULL"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestResultPaneTruncatedAndAffected(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true, Truncated: true,
		Columns: []string{"v"},
		Rows:    [][]any{{"x"}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("结果已截断") {
		t.Fatalf("truncation badge missing: %q", tt.Texts())
	}

	a.con.SetOutcome(&engine.QueryResult{Affected: 7}, "")
	tt.Frame()
	if !tt.HasText("7 行受影响") {
		t.Fatalf("affected text missing: %q", tt.Texts())
	}
}

func TestResultPaneErrorAndStop(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(nil, "no such table: missing")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("no such table: missing") {
		t.Fatalf("error banner missing: %q", tt.Texts())
	}

	a.con.SetOutcome(nil, "")
	a.con.UseSession(blockingFake{})
	if !a.con.Run("SELECT 1") {
		t.Fatal("run must start")
	}
	tt.Frame()
	if !tt.HasText("执行中") {
		t.Fatalf("running state missing: %q", tt.Texts())
	}
	if err := tt.Click("停止"); err != nil {
		t.Fatal(err)
	}
}

func TestCellTextAndCutRunes(t *testing.T) {
	if got := cellText(nil); got != "NULL" {
		t.Fatalf("nil → %q", got)
	}
	if got := cellText([]byte{1, 2, 3}); got != "[3 bytes]" {
		t.Fatalf("[]byte → %q", got)
	}
	if got := cellText(int64(42)); got != "42" {
		t.Fatalf("int64 → %q", got)
	}
	if got := cutRunes("short", 512); got != "short" {
		t.Fatalf("short cut = %q", got)
	}
	long := strings.Repeat("好", 600)
	got := cutRunes(long, 512)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 513 {
		t.Fatalf("cutRunes runes = %d, suffix ok=%v", len([]rune(got)), strings.HasSuffix(got, "…"))
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `$GO test ./ui/ -run 'TestResultPane|TestCellText' -count=1`
Expected: FAIL（`a.con undefined` / `undefined: newTestRoot` 编译错误，或 `resultPane` 未定义）

- [ ] **Step 3: root 增字段与布局**

`ui/root.go` 修改 1——`root` 结构体与 `newRoot` 整体替换为：

```go
type root struct {
	ed            *editor
	findQuery     string
	findActive    bool
	sampler       *Sampler
	con           *app.Console
	win           *mygo.Window
	resultState   ui.ListState
	resultsHeight float32
}

func newRoot(text string, s strats) *root {
	return &root{
		ed:            NewEditor(text, s),
		sampler:       NewSampler(128),
		con:           app.NewConsole(),
		resultsHeight: 260,
	}
}
```

`ui/root.go` 修改 2——`rootView` 中原「editor + gutter」的 `ui.Row(c).Fill().Children(func() { ... })` 整块（原 main.go:81-107，Task 4 原样拷入）包进垂直分屏，成为如下结构（编辑器 Row 内容一字不改，仅外层包装变化）：

```go
		// 编辑器 / 结果 分屏
		ui.SplitVertical(c, &a.resultsHeight, func() {
			ui.Row(c).Fill().Children(func() {
				ui.Box(c).Width(44).FillHeight().Draw(func(p *ui.Painter, r ui.Rect) {
					ed.viewH = r.H
					p.Clip(r, 0, func() {
						top := r.Y - ed.scroll.Y
						cur := ed.lines.At(ed.scroll.Y)
						for i := cur; i < ed.lines.Count(); i++ {
							y := top + ed.lines.Top(i)
							if y > r.Y+r.H {
								break
							}
							if i == cur {
								p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 16}, ui.RGBA(80, 100, 160, 0.25), 0)
							}
							p.Text(r.X+8, y, strconv.Itoa(i+1), 12, ui.RGB(130, 130, 130))
						}
					})
				})

				in := ui.TextAreaBuffer(c, ed.Buf).Label("编辑器").Fill().Font("monospace").
					TrackLines(&ed.lines).TrackScroll(&ed.scroll)
				if a.findActive && a.findQuery != "" {
					in.TextRanges(FindAll(ed.Buf.String(), a.findQuery)...)
				} else {
					in.TextRanges(ed.allRanges...)
				}
			})
		}, func() {
			a.resultPane(c)
		}).Grow(1)
```

（原 Row 的 `.Fill()` 移到分屏元素的 `.Grow(1)`；HUD、两行 spike 工具栏、查找栏保持在分屏之前，位置不动。）

- [ ] **Step 4: 实现 `ui/result.go`**

```go
package ui

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

func (a *root) resultPane(c *ui.Context) {
	ui.Column(c).Fill().Gap(6).Children(func() {
		switch {
		case a.con.Running():
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "执行中…")
				if ui.Button(c, "停止").Clicked() {
					a.con.Cancel()
				}
			})
		case a.con.Err() != "":
			ui.Text(c, "执行失败").Bold()
			ui.Text(c, a.con.Err()).TextColor(c.Theme().Danger).Font("monospace")
		case a.con.Result() != nil:
			r := a.con.Result()
			if !r.IsQuery {
				ui.Textf(c, "OK — %d 行受影响", r.Affected)
				return
			}
			if r.Truncated {
				ui.Badge(c, "结果已截断：超过 500 行或 16MB 字符预算")
			}
			a.resultTable(c, r)
		default:
			ui.Text(c, "Ctrl+Enter 执行 SQL").TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *root) resultTable(c *ui.Context, r *engine.QueryResult) {
	cols := make([]ui.TableColumn, 0, len(r.Columns)+1)
	cols = append(cols, ui.TableColumn{Title: "#", ID: "#", Width: 56, Fixed: true, Align: ui.End})
	for _, name := range r.Columns {
		cols = append(cols, ui.TableColumn{Title: name, ID: name})
	}
	ui.Table(c, &a.resultState, cols, len(r.Rows), func(row, col int) {
		if col == 0 {
			ui.Text(c, strconv.Itoa(row+1)).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Text(c, cellText(r.Rows[row][col-1])).SingleLine()
	}).Grow(1)
}

func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return fmt.Sprintf("[%d bytes]", len(t))
	case string:
		return cutRunes(t, 512)
	default:
		return fmt.Sprint(t)
	}
}

func cutRunes(s string, n int) string {
	i := 0
	for k := 0; k < n; k++ {
		if i >= len(s) {
			return s
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	if i >= len(s) {
		return s
	}
	return s[:i] + "…"
}
```

- [ ] **Step 5: 运行确认通过**

Run: `$GO test ./ui/ -run 'TestResultPane|TestCellText' -count=1 -race -v`
Expected: `TestResultPaneEmpty/Table/TruncatedAndAffected/ErrorAndStop`、`TestCellTextAndCutRunes` 全 PASS

- [ ] **Step 6: 全套回归 + vet**

```bash
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
```

Expected: 全部 ok、vet 无告警（布局变更后 spike 测量视口变小，正式门槛在 Task 8 全量验证）。

- [ ] **Step 7: Commit**

```bash
git add ui/
git commit -m "task5: 结果面板（ui.Table 表格/错误横幅/截断徽章/执行中态）"
```

---

### Task 6: 打开文件与执行接线（端到端）

**Files:**
- Create: `ui/run.go`
- Modify: `ui/root.go`（rootView 插入连接/执行工具行 + 快捷键）
- Test: `ui/run_test.go`

**Interfaces:**
- Consumes: Task 3 的 `Console.Run/Session/SwapSession`；Task 2 的 sqlite 注册（测试空白导入）；mygo `Dialog.Open`、`win.Update`。
- Produces:
  - `(a *root) openSQLite(path string) error`（主线程调用：存在性检查由驱动完成 → `engine.Open` → `sess.Open` → `con.SwapSession`；错误写 `a.openErr`）
  - `(a *root) openDatabase()`（协程弹原生文件对话框 → `win.Update(openSQLite)`）
  - `(a *root) runSQL()`（`con.Run(ed.Buf.String())`——执行整个缓冲区，见 Global Constraints）
  - rootView 新增工具行：「打开…」+ 库名 + 打开错误 + 「执行」（Ctrl+Enter/F5，运行中禁用）+ 「停止」占位由结果面板承担
  - `root` 增字段 `dbName string`、`openErr string`

- [ ] **Step 1: 写失败测试**

创建 `ui/run_test.go`：

```go
package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/engine"

	_ "db-g/driver/sqlite"
)

func seedDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	s, err := engine.Open(engine.Profile{Kind: "sqlite", Name: "seed", DSN: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stmts := []string{
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
		"INSERT INTO users (name) VALUES ('alice')",
		"INSERT INTO users (name) VALUES ('bob')",
	}
	for _, q := range stmts {
		if _, err := s.RunStatement(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestOpenSQLiteErrors(t *testing.T) {
	a := newTestRoot()
	if err := a.openSQLite(filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Fatal("missing file must fail")
	}
	if a.openErr == "" {
		t.Fatal("openErr must be set")
	}
}

func TestRunSQLiteEndToEnd(t *testing.T) {
	path := seedDB(t)
	a := newTestRoot()
	if err := a.openSQLite(path); err != nil {
		t.Fatal(err)
	}
	if a.dbName != "app.db" {
		t.Fatalf("dbName = %q", a.dbName)
	}
	a.ed.Buf.Set("SELECT id, name FROM users ORDER BY id")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if err := tt.Click("执行"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.con.Result() != nil || a.con.Err() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	tt.Frame()
	if a.con.Err() != "" {
		t.Fatalf("exec err: %s", a.con.Err())
	}
	res := a.con.Result()
	if res == nil || len(res.Rows) != 2 {
		t.Fatalf("result: %+v", res)
	}
	if !tt.HasText("alice") || !tt.HasText("bob") {
		t.Fatalf("rows not rendered: %q", tt.Texts())
	}
}

func TestRunErrorEndToEnd(t *testing.T) {
	path := seedDB(t)
	a := newTestRoot()
	if err := a.openSQLite(path); err != nil {
		t.Fatal(err)
	}
	a.ed.Buf.Set("SELECT * FROM nope")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if err := tt.Click("执行"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.con.Err() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	tt.Frame()
	if a.con.Err() == "" {
		t.Fatal("expected error")
	}
	if !tt.HasText("nope") {
		t.Fatalf("error not rendered: %q", tt.Texts())
	}
}
```

注意：`T.Context()` 要求 Go 1.24+（本项目 1.27.1 满足），测试内的打开/执行统一用 `t.Context()`。

- [ ] **Step 2: 运行确认失败**

Run: `$GO test ./ui/ -run 'TestOpenSQLite|TestRunSQLite|TestRunError' -count=1`
Expected: FAIL（`a.openSQLite undefined` / `a.dbName undefined` 编译错误）

- [ ] **Step 3: root 增字段**

`ui/root.go` 的 `root` 结构体追加：

```go
	dbName  string
	openErr string
```

- [ ] **Step 4: 实现 `ui/run.go`**

```go
package ui

import (
	"context"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"db-g/engine"
)

func (a *root) openSQLite(path string) error {
	sess, err := engine.Open(engine.Profile{Kind: "sqlite", Name: filepath.Base(path), DSN: path})
	if err != nil {
		a.openErr = err.Error()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Open(ctx); err != nil {
		a.openErr = err.Error()
		return err
	}
	a.con.SwapSession(sess)
	a.dbName = filepath.Base(path)
	a.openErr = ""
	return nil
}

func (a *root) openDatabase() {
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win,
			Title:  "打开 SQLite 数据库",
			Filters: []mygo.FileFilter{
				{Name: "SQLite", Extensions: []string{"db", "sqlite", "sqlite3"}},
			},
		})
		if err != nil {
			win.Update(func() { a.openErr = err.Error() })
			return
		}
		if len(paths) == 0 {
			return
		}
		path := paths[0]
		win.Update(func() { a.openSQLite(path) })
	}()
}

func (a *root) runSQL() {
	a.con.Run(a.ed.Buf.String())
}
```

（`openSQLite` 在主线程或测试中同步调用，`SwapSession` 负责关闭旧会话；文件存在性检查由驱动 `Open` 完成。）

- [ ] **Step 5: rootView 插入工具行与快捷键**

`ui/root.go` 的 `rootView`：
1. 函数体第一行（`ed.Update()` 之后）插入快捷键：

```go
		if c.Shortcut(ui.Ctrl, ui.KeyEnter) || c.Shortcut(0, ui.KeyF5) {
			a.runSQL()
		}
```

2. 查找栏 `ui.Row(...find...)` 之后、分屏之前插入连接/执行工具行：

```go
			// 连接与执行
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, "打开…").Clicked() {
					a.openDatabase()
				}
				if a.dbName != "" {
					ui.Text(c, a.dbName).Bold()
				}
				if a.openErr != "" {
					ui.Text(c, a.openErr).TextColor(c.Theme().Danger)
				}
				ui.Spacer(c)
				if ui.Button(c, "执行").Disabled(a.con.Running() || a.con.Session() == nil).Clicked() {
					a.runSQL()
				}
			})
```

- [ ] **Step 6: 运行确认通过**

```bash
$GO test ./ui/ -count=1 -race -v -run 'TestOpenSQLite|TestRunSQLite|TestRunError|TestResultPane'
$GO tool mygo vet .
```

Expected: 新测试与 Task 5 结果面板测试全 PASS，vet 无告警。

- [ ] **Step 7: 全套回归**

```bash
$GO test ./... -skip TestSpikeMatrix -count=1
```

Expected: 全部 ok。

- [ ] **Step 8: 交互演示（手动验收）**

Run: `$GO run .`
Expected 清单：
1. 点「打开…」→ 系统文件对话框选一个 `.db` 文件 → 标题行显示文件名；
2. 编辑器输入 `SELECT 1 AS a;` 或对真实库写 SELECT → 点「执行」或按 **Ctrl+Enter** → 结果面板出表格（列头、行号、单元格）；
3. 执行非法 SQL → 「执行失败」+ 错误信息；
4. 对大表 SELECT（>500 行）→ 截断徽章出现；
5. 慢查询（若有）→ 「执行中…」+ Spinner + 「停止」可中断（中断后显示 context canceled）。

- [ ] **Step 9: Commit**

```bash
git add ui/
git commit -m "task6: 打开 SQLite 文件 + 执行接线（工具行/快捷键/端到端）"
```

### Task 7: 结果网格性能基准

**Files:**
- Create: `ui/result_bench_test.go`

**Interfaces:**
- Consumes: Task 3 的 `SetOutcome`、Task 4 的 `newRoot/rootView`、既有 `bench_test.go` 的 `median/pct` 助手（同包复用）。
- Produces: `TestResultGridFrameBudget`——100k 行 × 50 列渲染帧 p50 ≤ 16ms（spec 门槛），数据经 `t.Logf` 留档。

- [ ] **Step 1: 写基准测试**

创建 `ui/result_bench_test.go`：

```go
package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

func TestResultGridFrameBudget(t *testing.T) {
	const (
		rows = 100_000
		cols = 50
	)
	labels := make([]any, cols)
	for j := range labels {
		labels[j] = fmt.Sprintf("col_%02d_value", j)
	}
	r := &engine.QueryResult{
		IsQuery: true,
		Columns: make([]string, cols),
		Rows:    make([][]any, rows),
	}
	for j := range r.Columns {
		r.Columns[j] = fmt.Sprintf("col_%02d", j)
	}
	for i := range r.Rows {
		row := make([]any, cols)
		copy(row, labels)
		r.Rows[i] = row
	}

	a := newRoot("", strats{scopeViewport, scopeViewport})
	a.con.SetOutcome(r, "")
	tt := ui.NewTester(rootView(a), 1600, 900)
	for range 5 {
		tt.Frame()
	}
	var samples []time.Duration
	for range 30 {
		t0 := time.Now()
		tt.Frame()
		samples = append(samples, time.Since(t0))
	}
	p50, p99 := median(samples), pct(samples, 0.99)
	t.Logf("result grid 100k x %d: frame p50=%v p99=%v", cols, p50, p99)
	if p50 > 16*time.Millisecond {
		t.Errorf("frame p50 %v exceeds 16ms budget", p50)
	}
}
```

- [ ] **Step 2: 运行并确认通过（或定位降级路径）**

Run: `$GO test ./ui/ -run TestResultGridFrameBudget -count=1 -v`
Expected: PASS 且日志 `frame p50 <= 16ms`。

若 FAIL：这是 spec 预案的风险点——按 spec 降级路径改造 `resultTable`（结果只读视图走 `ui.List` 自绘行，放弃列拖拽），在本任务追加改造步骤后重测，直至通过；并在 commit message 注明走的是降级路径。

- [ ] **Step 3: Commit**

```bash
git add ui/result_bench_test.go
git commit -m "task7: 结果网格基准（100k×50 帧 p50 预算守门）"
```

---

### Task 8: 性能守门断言 + 文档收尾（M1 里程碑验收）

**Files:**
- Modify: `ui/bench_test.go`（VV 配置加 p99 断言）
- Modify: `Readme.md`（功能清单 + 里程碑状态）
- Test: 全量 `$GO test ./...`

**Interfaces:**
- Consumes: Task 4 迁入后的 `ui/bench_test.go`（`measureOne/median/pct`）。
- Produces: spike 门槛断言 `spikeP99Limit` map（3k=8ms、12k=15ms、100k=50ms，仅对 VV 配置生效）；Readme 里程碑勾选。

- [ ] **Step 1: 写失败的门槛断言**

`ui/bench_test.go` 在 `var matrixCfg = ...` 之后新增：

```go
var spikeP99Limit = map[int]time.Duration{
	3_000:  8 * time.Millisecond,
	12_000: 15 * time.Millisecond,
	100_000: 50 * time.Millisecond,
}
```

`TestSpikeMatrix` 的双层循环内（`t.Logf` 之后、CSV 落盘之前）插入：

```go
			if ck.name == "VV" {
				if limit, ok := spikeP99Limit[size]; ok {
					if p := pct(keys, 0.99); p > limit {
						t.Errorf("size=%d VV key p99 %v exceeds limit %v", size, p, limit)
					}
				}
			}
```

- [ ] **Step 2: 运行确认断言生效**

Run: `$GO test ./ui/ -run TestSpikeMatrix -count=1 -v`（耗时数分钟）
Expected: 9 行矩阵日志（100k VV 单键 p99 基线 40.1ms，迁移后视口变小只会更快），无 `exceeds limit` 错误，整体 `PASS`。若某档超限：先确认非本机负载噪声（重跑一次），仍超则回查 Task 5/6 的视图改动是否引入每键额外开销（如每帧 `Buf.String()` 调用、resultPane 中的全量扫描），修复后重跑。

- [ ] **Step 3: 更新 Readme**

将 `Readme.md` 全文替换为：

```markdown
# DB-G

对标 db-k 的数据库客户端（Go + mygo），规格与里程碑见
`docs/superpowers/specs/2026-10-14-db-g-roadmap-design.md`。

## 功能清单

- [x] 长文本编辑器：虚拟化渲染 + 增量 SQL 高亮 + 查找（性能基准见 docs/spike-results.md）
- [x] M1：打开 SQLite 文件、异步执行 SQL（Ctrl+Enter）、虚拟化结果表格
      （行号/截断徽章/错误横幅/停止取消/500 行与 1MB/16MB 预算）
- [ ] M2：连接管理 + 对象树
- [ ] M3：多语句控制台 + 执行历史
- [ ] M4：MySQL/PostgreSQL（方言与能力位抽象验证）
- [ ] M5：结果排序/筛选/分页/导出
- [ ] M6：结果编辑（事务写回）
- [ ] M7：编辑器补全与格式化

## 构建与测试

```bash
export GO=/home/zk/Code/go/bin/go1.27.1
$GO run .            # 交互窗口
$GO test ./...       # 全量（含 spike 与结果网格基准，数分钟）
$GO tool mygo vet .  # UI 生命周期检查
```
```

- [ ] **Step 4: M1 验收清单核对**

对照 spec M1 验收（逐项确认，任何一项缺失先补齐再收尾）：
1. 集成测试：`$GO test ./driver/sqlite/ -v`（临时/:memory: 全链路）全绿；
2. headless UI 测试：`$GO test ./ui/ -run 'TestResultPane|TestRunSQLite|TestRunError' -v` 全绿；
3. 结果网格基准：`$GO test ./ui/ -run TestResultGridFrameBudget -v` p50 ≤ 16ms；
4. spike 基准无回退：`$GO test ./ui/ -run TestSpikeMatrix -v` 断言通过；
5. 交互演示：Task 6 Step 8 的五项手动清单现场可复现（含 Ctrl+Enter 快捷键）；
6. `$GO tool mygo vet .` 无告警。

- [ ] **Step 5: Commit**

```bash
git add ui/bench_test.go Readme.md
git commit -m "task8: spike p99 门槛断言 + Readme 功能清单（M1 收尾）"
```

---

## 计划级注意事项（给执行者）

- **不要**在 M1 引入对象树、连接 profile 持久化、多语句拆分、排序筛选——分别是 M2/M3/M5 的内容；`engine.Session` 也不加元数据方法（spec：M2 才扩展接口）。
- `ui` 包（本模块）与 `github.com/egoist/mygo/ui`（别名默认 `ui`）同名共存是刻意安排：本模块包名永不作为限定符使用，`ui.` 一律指 mygo；根 `main.go` 中 mygo 必须别名 `mygoUI`。
- 测试一律用 `t.TempDir()` / `:memory:`，不写仓库目录；CSV 落盘仅 spike 既有 `/tmp/opencode`。
- 遇到 `ui.Tester` 点击中文按钮失败：先 `tt.Frame()` 让按钮入帧再 `Click`；仍失败则用 `tt.Texts()` 排查文案是否被截断渲染。
- modernc 编译首跑约 1–2 分钟属正常，勿误判挂死。

## Self-Review（写完计划后由计划作者执行）

1. **Spec 覆盖**：spec M1 条目——文件对话框打开（Task 6 `openDatabase` + 手动演示）、异步+Spinner+取消（Task 3/5）、Ctrl+Enter（Task 6 Step 5 + 手动演示）、`ui.Table` 列头/行号/错误横幅/500 行截断（Task 5/2）、临时 SQLite 集成测试（Task 2）、headless 测试（Task 5/6）、结果网格基准（Task 7）、spike 无回退（Task 8）、抽象骨架 engine/driver 分层（Task 1/2/3）。无缺口。
2. **占位符**：全文无 TBD/TODO/「同上」「类似 Task N」；每个代码步骤均给完整代码。
3. **类型一致性**：`QueryResult` 字段（Task 1 定义）在 Task 2/3/5/7 用法一致；`Console` 方法集与 Task 5/6 调用一致；`newRoot/rootView` 在 Task 4 定义、Task 5/6/7 复用；`SetOutcome` 在 Task 3 定义、Task 5/7 调用。




