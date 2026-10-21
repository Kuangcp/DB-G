# 选区执行 + MySQL 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 仅执行编辑器选中的 SQL（禁止整个控制台执行），并新增 MySQL 数据源（连接/执行/对象树）。

**Architecture:** 选区经 mygo 既有 `Element.TextSelection()` + `TextBuffer.Slice` 实现（帧内缓存 + 延迟执行标志）；MySQL 经 `engine.Register` 注册新 driver，`ConnProfile`/`engine.Profile` 扩字段，DSN 仅由 driver 用 `mysql.Config.FormatDSN` 拼装；对象树零改动（`ObjectLister` 自动生效）。

**Tech Stack:** Go 1.27.1、mygo v0.4.8（钉死）、`github.com/go-sql-driver/mysql`（新依赖，纯 Go）、modernc.org/sqlite（既有）。

## Global Constraints

- `$GO` = `/home/zk/Code/go/bin/go1.27.1`（系统 go 是 1.26.5，禁用）。
- mygo 必须保持 v0.4.8；`go get` 若升级立即钉回。
- 选区语义：有选区→执行选区原文（应用层不切分多语句）；无选区→不执行；禁止 `ed.Buf.String()` 全量执行。
- `Element` 跨 pass 使用会 panic——选区只在构建 `in` 的同一帧内读取。
- MySQL：密码明文 JSON（文件 0600）；DSN 由 driver 拼装，ui 不拼；`multiStatements=true`、`parseTime=true`、`charset=utf8mb4`、`loc=Local`；未选数据库时 Objects 报「未选择数据库」。
- 校验：sqlite = Path 存在且非目录；mysql = Name/Host 非空 + Port ∈ [1,65535]；未知 Kind 拒绝。
- 测试红线：store 测试一律 `t.TempDir()`；不读写真实 `~/.config/db-g/`；MySQL 集成测试无 `DB_G_MYSQL_HOST` 环境变量时 `t.Skip`。
- 用户可见文案全中文；不写行内注释；导出 API doc comment；commit 格式 `taskN: <内容>`；每任务结束 `$GO tool mygo vet .`。
- 不做：Postgres、语句切分（M3）、密码加密、连接搜索、编辑器多标签。

## 环境与已核实 API（执行者必读）

- `ui.Element.TextSelection() (int, int)`：rune 偏移，`start==end` 为光标（mygo v0.4.8 `ui/api_gen.go:4709`）。
- `ui.TextBuffer.Slice(start, end string)`：rune 偏移切片（`ui/text_buffer.go:48`）。
- `ui.Element.Password()`：密码掩码（`ui/api_gen.go:4079`）。
- `ui.Radio[T comparable](c, selected *T, value T, label string)` + `ui.RadioGroup(c, fn)`（`ui/api_gen.go:1316/1347`）。
- `ui.Tester`：`Key(mods, key)`、`Command(name)`（含 "selectAll"）、`Click`、`Find`（`ui/headless.go`）。
- `app.Console` 默认 publisher 为 no-op，headless 测试可直接 `Run` 后轮询 `Result()`。
- `engine.Register` 全局注册表；ui 测试二进制**不** import `driver/mysql`，故可在 ui 测试中注册捕获用的 "mysql" 假 factory（无重复 panic）。

---

### Task 1: 选区执行（ui 层）

**Files:**
- Modify: `ui/root.go`（root 增字段；rootView 触发逻辑与选区缓存）
- Modify: `ui/run.go`（runSQL）
- Modify: `ui/result.go`（默认提示按态）
- Test: `ui/selection_test.go`（新建）、`ui/sidebar_test.go`（改 e2e）

**Interfaces:**
- Consumes: 既有 `app.Console.Run/Result/Err`、`ed.Buf`（`*ui.TextBuffer`）、`openConn`（测试用）。
- Produces: `root.selStart/selEnd int`（rune 偏移缓存）、`root.execPending bool`（同帧延迟执行标志）；`runSQL()` 只执行选区。Task 2–4 不依赖这些字段，但后续计划中「执行」语义以此为准。

- [ ] **Step 1: 写失败测试**

`ui/selection_test.go` 新建：

```go
package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func openForSelection(t *testing.T) *root {
	t.Helper()
	a := newSidebarRoot(t)
	if err := a.openConn(app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}); err != nil {
		t.Fatal(err)
	}
	return a
}

func pollResult(t *testing.T, a *root, tt *ui.Tester) {
	t.Helper()
	for range 200 {
		if a.con.Result() != nil || a.con.Err() != "" {
			return
		}
		tt.Frame()
	}
	t.Fatal("timeout waiting for result")
}

func TestRunSQLExecutesSelectionOnly(t *testing.T) {
	a := openForSelection(t)
	text := "SELECT 9 AS x;\nSELECT name FROM users;"
	a.ed.Buf.Set(text)
	cut := strings.Index(text, "SELECT name")
	a.selStart, a.selEnd = 0, cut-1
	a.runSQL()
	tt := ui.NewTester(rootView(a), 1400, 800)
	pollResult(t, a, tt)
	if a.con.Err() != "" {
		t.Fatalf("exec err: %s", a.con.Err())
	}
	r := a.con.Result()
	if r == nil || !r.IsQuery || len(r.Columns) != 1 || r.Columns[0] != "x" {
		t.Fatalf("selection must run only first statement: %+v", r)
	}
	if got := r.Rows[0][0]; got != int64(9) && got != "9" && got != 9 {
		t.Fatalf("x = %v (%T)", got, got)
	}
}

func TestRunSQLEmptySelectionDoesNothing(t *testing.T) {
	a := openForSelection(t)
	a.ed.Buf.Set("SELECT name FROM users")
	a.selStart, a.selEnd = 0, 0
	a.runSQL()
	if a.con.Running() || a.con.Result() != nil || a.con.Err() != "" {
		t.Fatal("empty selection must not start execution")
	}
}

func TestExecuteButtonDisabledWithoutSelection(t *testing.T) {
	a := openForSelection(t)
	a.ed.Buf.Set("SELECT name FROM users")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	err := tt.Click("执行")
	if err == nil && a.con.Running() {
		t.Fatal("disabled 执行 must not start a run")
	}
	if a.con.Result() != nil {
		t.Fatal("no result expected")
	}
}

func TestResultPaneHintByState(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("请先连接数据源") {
		t.Fatalf("no-session hint missing: %q", tt.Texts())
	}
	if err := a.openConn(app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}); err != nil {
		t.Fatal(err)
	}
	a.selStart, a.selEnd = 0, 0
	tt.Frame()
	if !tt.HasText("请先选中要执行的 SQL") {
		t.Fatalf("no-selection hint missing: %q", tt.Texts())
	}
}
```

注：`pollResult` 里 `tt.Frame()` 会用 UI 真实选区覆写 `selStart/selEnd`，但 `runSQL` 已同步取值进入 `con.Run`，不影响断言。

- [ ] **Step 2: 跑测试确认失败**

Run: `$GO test ./ui/ -run 'TestRunSQL|TestExecuteButton|TestResultPaneHint' -count=1`
Expected: 编译错误（`a.selStart`/`a.execPending` undefined）。

- [ ] **Step 3: 实现**

`ui/root.go` root 结构体 `treeSelected` 之后追加：

```go
	selStart    int
	selEnd      int
	execPending bool
```

`rootView` 顶部快捷键块替换为：

```go
		if c.Shortcut(ui.Ctrl, ui.KeyEnter) || c.Shortcut(0, ui.KeyF5) {
			a.execPending = true
		}
```

「执行」按钮替换为：

```go
					if ui.Button(c, "执行").Disabled(a.con.Running() || a.con.Session() == nil || a.selStart == a.selEnd).Clicked() {
						a.execPending = true
					}
```

编辑器 `in` 构建块（`in.TextRanges(...)` 之后、Row 闭合前）追加：

```go
						a.selStart, a.selEnd = in.TextSelection()
						if a.execPending {
							a.execPending = false
							a.runSQL()
						}
```

`ui/run.go` 的 `runSQL` 替换为：

```go
func (a *root) runSQL() {
	if a.selStart == a.selEnd {
		return
	}
	a.con.Run(a.ed.Buf.Slice(a.selStart, a.selEnd))
}
```

`ui/result.go` `default:` 分支替换为：

```go
		default:
			switch {
			case a.con.Session() == nil:
				ui.Text(c, "请先连接数据源").TextColor(c.Theme().TextMuted)
			case a.selStart == a.selEnd:
				ui.Text(c, "请先选中要执行的 SQL").TextColor(c.Theme().TextMuted)
			default:
				ui.Text(c, "Ctrl+Enter 执行选中 SQL").TextColor(c.Theme().TextMuted)
			}
```

- [ ] **Step 4: 更新受影响的既有 e2e 测试**

`ui/sidebar_test.go` 的 `TestOpenConnAndRunEndToEnd`：按钮在无 UI 选区时禁用是新契约，点击路径改为直调 `runSQL`（生产点击经 execPending 同帧读活选区，headless 无法注入真实选区）。把 `a.ed.Buf.Set(...)` 之后到结果断言之前的执行段替换为：

```go
	a.ed.Buf.Set("SELECT name FROM users")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.selStart, a.selEnd = 0, len([]rune("SELECT name FROM users"))
	a.runSQL()
	for range 200 {
		if a.con.Result() != nil || a.con.Err() != "" {
			break
		}
		tt.Frame()
	}
```

其后 `a.con.Err()`/`a.con.Result()` 断言保持不变。

- [ ] **Step 5: 跑测试确认通过 + 回归 + vet**

```bash
$GO test ./ui/ -run 'TestRunSQL|TestExecuteButton|TestResultPaneHint|TestOpenConnAndRunEndToEnd|TestSidebar' -count=1 -race -v
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
```

Expected: 全 PASS；全套 ok；vet 0。

- [ ] **Step 6: Commit**

```bash
git add ui/
git commit -m "task1: 仅执行选中 SQL（无选区禁用 + 按态提示）"
```

---

### Task 2: 数据模型扩展与按 Kind 校验

**Files:**
- Modify: `engine/session.go`（Profile 增字段）
- Modify: `app/conn.go`（ConnProfile 增字段；validateProfile 抽取与分支）
- Test: `app/conn_test.go`（新用例 + 既有用例适配）

**Interfaces:**
- Consumes: 既有 `ConnStore.Add/Update/LoadStore`。
- Produces: `app.ConnProfile{ID,Name,Kind,Path,Host,Port int,User,Password,Database}`；`validateProfile(p ConnProfile) error`（私有）；`engine.Profile{Kind,Name,DSN,Host,Port int,User,Password,Database}`。Task 3/4 依赖这些字段名与类型。

- [ ] **Step 1: 写失败测试**

`app/conn_test.go` 末尾追加：

```go
func TestStoreValidateProfileByKind(t *testing.T) {
	s := LoadStore(filepath.Join(t.TempDir(), "c.json"))
	dir := t.TempDir()
	if err := s.Add(ConnProfile{Name: "d", Kind: "sqlite", Path: dir}); err == nil {
		t.Fatal("sqlite directory path must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "m", Kind: "mysql"}); err == nil {
		t.Fatal("mysql without host must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "m", Kind: "mysql", Host: "h", Port: 0}); err == nil {
		t.Fatal("mysql port 0 must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "m", Kind: "mysql", Host: "h", Port: 70000}); err == nil {
		t.Fatal("mysql port out of range must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "pg", Kind: "postgres", Host: "h", Port: 5432}); err == nil {
		t.Fatal("unknown kind must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "m", Kind: "mysql", Host: "h", Port: 3306, User: "root", Password: "s", Database: "app"}); err != nil {
		t.Fatalf("valid mysql profile: %v", err)
	}
	if len(s.List()) != 1 || s.List()[0].Host != "h" || s.List()[0].Port != 3306 || s.List()[0].Password != "s" {
		t.Fatalf("round-trip: %+v", s.List())
	}
}

func TestStoreLoadsLegacyJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(path, []byte(`{"conns":[{"id":"a","name":"x","kind":"sqlite","path":"/tmp/x.db"}],"activeID":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(path)
	if len(s.List()) != 1 || s.List()[0].Name != "x" || s.List()[0].Host != "" || s.List()[0].Port != 0 {
		t.Fatalf("legacy load: %+v", s.List())
	}
}
```

既有用例适配：`TestStoreValidation` 中 `s.Add(ConnProfile{Name: "n", Kind: "mysql", Path: dir})`（约 :67，原断言 mysql 被拒）现在因缺 Host 仍被拒——保留断言即可；确认其 err 分支不检查具体文案（若是则无需改）。

- [ ] **Step 2: 跑测试确认失败**

Run: `$GO test ./app/ -run 'TestStoreValidate|TestStoreLoadsLegacy|TestStoreValidation' -count=1`
Expected: FAIL（`ConnProfile.Host undefined` / `Profile.Host undefined` / `validateProfile` 相关编译错误或断言失败）。

- [ ] **Step 3: 实现**

`engine/session.go` Profile 替换为：

```go
// Profile 描述一个数据源连接的静态配置。
type Profile struct {
	Kind     string // "sqlite" | "mysql" | "postgres"
	Name     string // 展示名
	DSN      string // sqlite：文件路径或 ":memory:"
	Host     string // mysql：主机
	Port     int    // mysql：端口
	User     string // mysql：用户
	Password string // mysql：密码
	Database string // mysql：数据库（可空）
}
```

`app/conn.go` ConnProfile 替换为：

```go
// ConnProfile 是一个持久化的数据源连接配置。
type ConnProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Database string `json:"database,omitempty"`
}
```

`app/conn.go` 新增私有函数（放在 Add 之前）：

```go
func validateProfile(p ConnProfile) error {
	if p.Name == "" {
		return errors.New("连接名称不能为空")
	}
	switch p.Kind {
	case "sqlite":
		fi, err := os.Stat(p.Path)
		if err != nil {
			return fmt.Errorf("路径不可用: %w", err)
		}
		if fi.IsDir() {
			return errors.New("路径不可用: 是目录而非数据库文件")
		}
	case "mysql":
		if p.Host == "" {
			return errors.New("主机不能为空")
		}
		if p.Port < 1 || p.Port > 65535 {
			return errors.New("端口必须在 1–65535")
		}
	default:
		return fmt.Errorf("不支持的数据源类型: %q", p.Kind)
	}
	return nil
}
```

`Add` 开头的三段校验（`p.Name==""`/`p.Kind!="sqlite"`/`os.Stat`）替换为 `if err := validateProfile(p); err != nil { return err }`；`Update` 同样替换。

- [ ] **Step 4: 跑测试确认通过 + 回归 + vet**

```bash
$GO test ./app/ -count=1 -race -v
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
```

Expected: app 全 PASS（含既有 store 套件）；全套 ok；vet 0。

- [ ] **Step 5: Commit**

```bash
git add engine/session.go app/conn.go app/conn_test.go
git commit -m "task2: ConnProfile/Profile 扩展 mysql 字段 + 按 Kind 校验"
```

---

### Task 3: driver/mysql

**Files:**
- Modify: `go.mod` / `go.sum`（新依赖）
- Create: `driver/mysql/mysql.go`
- Test: `driver/mysql/mysql_test.go`
- Modify: `main.go`（blank import）

**Interfaces:**
- Consumes: Task 2 `engine.Profile{Host,Port,User,Password,Database}`；既有 `engine.Register/Open`、`engine.QueryResult`、`engine.ObjectLister` 约定。
- Produces: `engine.Register("mysql", ...)`；`Session` 全契约（Open/Close/RunStatement/Cancel/Caps/Objects）。Caps = `engine.CapObjects`。ui 对象树自动生效，无需 Task 4 额外接线。

- [ ] **Step 1: 写失败测试**

`driver/mysql/mysql_test.go` 新建：

```go
package mysql

import (
	"context"
	"os"
	"testing"

	"db-g/engine"
)

func TestOpenSessionBuildsConfig(t *testing.T) {
	s, err := openSession(engine.Profile{
		Kind: "mysql", Name: "n",
		Host: "db.example", Port: 3307, User: "root", Password: "p@ss/w", Database: "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess := s.(*session)
	if sess.cfg.User != "root" || sess.cfg.Passwd != "p@ss/w" {
		t.Fatalf("creds: %+v", sess.cfg)
	}
	if sess.cfg.Addr != "db.example:3307" || sess.cfg.Net != "tcp" {
		t.Fatalf("addr: %q net: %q", sess.cfg.Addr, sess.cfg.Net)
	}
	if sess.cfg.DBName != "app" || !sess.cfg.MultiStatements || !sess.cfg.ParseTime {
		t.Fatalf("dbname=%q multi=%v parseTime=%v", sess.cfg.DBName, sess.cfg.MultiStatements, sess.cfg.ParseTime)
	}
	if sess.cfg.Params["charset"] != "utf8mb4" {
		t.Fatalf("params: %+v", sess.cfg.Params)
	}
	dsn := sess.cfg.FormatDSN()
	if dsn == "" {
		t.Fatal("FormatDSN empty")
	}
}

func TestIsQueryLikeMySQL(t *testing.T) {
	yes := []string{"SELECT 1", " with x", "\nEXPLAIN t", "show tables", "DESCRIBE t", "desc t", "VALUES (1)"}
	no := []string{"INSERT INTO t VALUES (1)", "update t set a=1", "delete from t", "CREATE TABLE a(b int)"}
	for _, q := range yes {
		if !isQueryLike(q) {
			t.Errorf("expected query-like: %q", q)
		}
	}
	for _, q := range no {
		if isQueryLike(q) {
			t.Errorf("expected exec-like: %q", q)
		}
	}
}

func TestObjectsErrors(t *testing.T) {
	s := &session{cfg: newTestConfig()}
	if _, err := s.Objects(context.Background(), engine.ObjectTable); err == nil || err.Error() != "mysql: session not open" {
		t.Fatalf("want session not open, got %v", err)
	}
	db, err := sql.Open("mysql", "root@tcp(127.0.0.1:1)/")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s2 := &session{cfg: newTestConfig(), db: db}
	if _, err := s2.Objects(context.Background(), engine.ObjectTable); err == nil || err.Error() != "mysql: 未选择数据库" {
		t.Fatalf("want 未选择数据库, got %v", err)
	}
	s2.cfg.DBName = "app"
	if _, err := s2.Objects(context.Background(), engine.ObjectKind(99)); err == nil {
		t.Fatal("unknown kind must error")
	}
}

func TestMySQLIntegration(t *testing.T) {
	host := os.Getenv("DB_G_MYSQL_HOST")
	if host == "" {
		t.Skip("DB_G_MYSQL_HOST 未设置")
	}
	port := 3306
	if v := os.Getenv("DB_G_MYSQL_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatal(err)
		}
		port = p
	}
	s, err := openSession(engine.Profile{
		Kind: "mysql", Name: "it",
		Host: host, Port: port,
		User: os.Getenv("DB_G_MYSQL_USER"), Password: os.Getenv("DB_G_MYSQL_PASSWORD"),
		Database: os.Getenv("DB_G_MYSQL_DATABASE"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, err := s.RunStatement(ctx, "SELECT 1 AS a")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsQuery || len(res.Rows) != 1 {
		t.Fatalf("SELECT 1: %+v", res)
	}
	if _, err := s.Objects(ctx, engine.ObjectTable); err != nil {
		t.Fatalf("Objects: %v", err)
	}
}
```

测试辅助加在同文件（import 需含 `database/sql`、`strconv`、`github.com/go-sql-driver/mysql`）：

```go
func newTestConfig() *mysql.Config {
	return mysql.NewConfig()
}
```

- [ ] **Step 2: 引入依赖并确认测试失败（编译期）**

```bash
$GO get github.com/go-sql-driver/mysql@latest
$GO test ./driver/mysql/ -count=1
```

Expected: FAIL/编译错误（`openSession`/`isQueryLike`/`session` undefined——包还不存在时由 Step 3 创建；若 Step 2 先建了空包文件则看 undefined 符号）。`go.mod` 出现 `github.com/go-sql-driver/mysql`；**记录其版本号**（后续提交说明用）。若此命令意外升级 mygo，立即 `$GO get github.com/egoist/mygo@v0.4.8` 钉回。

- [ ] **Step 3: 实现 `driver/mysql/mysql.go`**

```go
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"db-g/engine"

	"github.com/go-sql-driver/mysql"
)

const (
	rowLimit      = 500
	maxCellBytes  = 1 << 20
	maxResultByte = 16 << 20
)

func init() {
	engine.Register("mysql", openSession)
}

type session struct {
	cfg       *mysql.Config
	db        *sql.DB
	mu        sync.Mutex
	curCancel atomic.Pointer[context.CancelFunc]
}

func openSession(p engine.Profile) (engine.Session, error) {
	cfg := mysql.NewConfig()
	cfg.User = p.User
	cfg.Passwd = p.Password
	cfg.Net = "tcp"
	addr := p.Host
	if addr == "" {
		return nil, errors.New("mysql: 主机不能为空")
	}
	if p.Port > 0 {
		addr += ":" + strconv.Itoa(p.Port)
	}
	cfg.Addr = addr
	cfg.DBName = p.Database
	cfg.ParseTime = true
	cfg.MultiStatements = true
	cfg.Loc = time.Local
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["charset"] = "utf8mb4"
	return &session{cfg: cfg}, nil
}

func (s *session) Open(ctx context.Context) error {
	db, err := sql.Open("mysql", s.cfg.FormatDSN())
	if err != nil {
		return err
	}
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

func (s *session) Caps() engine.Capabilities { return engine.CapObjects }

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
		return nil, errors.New("mysql: session not open")
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
	case "SELECT", "WITH", "EXPLAIN", "SHOW", "DESCRIBE", "DESC", "VALUES":
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
				if len(t) > maxCellBytes {
					t = trimStr(t, maxCellBytes)
					vals[i] = t
					out.Truncated = true
				}
				total += len(t)
			case []byte:
				if len(t) > maxCellBytes {
					t = trimBytes(t, maxCellBytes)
					vals[i] = append([]byte(nil), t...)
					out.Truncated = true
				}
				total += len(t)
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

func (s *session) Objects(ctx context.Context, kind engine.ObjectKind) ([]engine.DBObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("mysql: session not open")
	}
	if s.cfg.DBName == "" {
		return nil, errors.New("mysql: 未选择数据库")
	}
	var q string
	switch kind {
	case engine.ObjectTable:
		q = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`
	case engine.ObjectView:
		q = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'VIEW' ORDER BY TABLE_NAME`
	case engine.ObjectIndex:
		q = `SELECT INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? GROUP BY INDEX_NAME ORDER BY INDEX_NAME`
	default:
		return nil, fmt.Errorf("mysql: unknown object kind %d", kind)
	}
	rows, err := s.db.QueryContext(ctx, q, s.cfg.DBName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.DBObject
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, engine.DBObject{Kind: kind, Name: name})
	}
	return out, rows.Err()
}
```

`main.go` import 块加一行（sqlite 之后）：

```go
	_ "db-g/driver/mysql"
```

- [ ] **Step 4: 跑测试确认通过 + 回归 + vet**

```bash
$GO test ./driver/mysql/ -count=1 -v
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
```

Expected: 除 `TestMySQLIntegration` 显示 SKIP 外全部 PASS；全套 ok；vet 0。

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum driver/mysql/ main.go
git commit -m "task3: driver/mysql（FormatDSN/多语句/information_schema 对象树）"
```

---

### Task 4: 连接表单 UI（类型切换/MySQL 字段）+ openConn 字段搬运

**Files:**
- Modify: `ui/root.go`（root 表单字段；newRoot 初始化 connFormKind）
- Modify: `ui/sidebar.go`（connModal 类型切换与字段组、新建/编辑回填、saveConnForm）
- Modify: `ui/run.go`（openConn 搬运 engine.Profile 字段）
- Test: `ui/sidebar_test.go`（新增 2 测试）

**Interfaces:**
- Consumes: Task 2 `app.ConnProfile` 新字段与校验；Task 1 既有侧栏结构；`ui.Radio/RadioGroup/Password`（已核实 API）。
- Produces: `root.connFormKind/Host/Port/User/Pass/DB string`（Port 为字符串输入，保存时 Atoi）；`saveConnForm` 按 kind 组装；`openConn` 完整搬运。对象树对 mysql 自动生效（Task 3 已实现 `ObjectLister`）。

- [ ] **Step 1: 写失败测试**

`ui/sidebar_test.go` 末尾追加（import 追加 `strconv` 若未用）：

```go
func TestSidebarMySQLConnFlow(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("MySQL"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connFormKind != "mysql" {
		t.Fatalf("kind toggle: %q", a.connFormKind)
	}
	a.connFormName = "远程库"
	a.connFormDB = "app"
	a.connFormHost = "10.0.0.1"
	a.connFormPort = "3306"
	a.connFormUser = "root"
	a.connFormPass = "secret"
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if a.connFormOpen {
		t.Fatalf("save must close modal: %s", a.connFormErr)
	}
	stored := a.store.List()
	if len(stored) != 1 {
		t.Fatalf("store: %+v", stored)
	}
	p := stored[0]
	if p.Kind != "mysql" || p.Host != "10.0.0.1" || p.Port != 3306 || p.User != "root" || p.Password != "secret" || p.Database != "app" {
		t.Fatalf("stored: %+v", p)
	}
	tt.Frame()
	if err := tt.Click("编辑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.connFormOpen || a.connFormKind != "mysql" || a.connFormHost != "10.0.0.1" || a.connFormPort != "3306" || a.connFormPass != "secret" {
		t.Fatalf("edit refill: kind=%q host=%q port=%q pass=%q", a.connFormKind, a.connFormHost, a.connFormPort, a.connFormPass)
	}
}

func TestOpenConnMySQLProfileFields(t *testing.T) {
	var captured engine.Profile
	engine.Register("mysql", func(p engine.Profile) (engine.Session, error) {
		captured = p
		return treeFakeSession{}, nil
	})
	a := newSidebarRoot(t)
	p := app.ConnProfile{
		ID: "m1", Name: "远程", Kind: "mysql",
		Host: "db.local", Port: 3307, User: "u", Password: "pw", Database: "app",
	}
	if err := a.openConn(p); err != nil {
		t.Fatal(err)
	}
	if captured.Kind != "mysql" || captured.Name != "远程" || captured.Host != "db.local" || captured.Port != 3307 ||
		captured.User != "u" || captured.Password != "pw" || captured.Database != "app" {
		t.Fatalf("profile mapping: %+v", captured)
	}
	if a.con.Session() == nil || a.connName != "远程" {
		t.Fatal("openConn must install session")
	}
}
```

注：`engine.Register("mysql", ...)` 在 ui 测试二进制内合法——ui 包测试不 import `driver/mysql`，注册表里无 "mysql" 键。若同包其他测试未来引入 driver/mysql 会 panic，届时改用独立 kind 名并同步 openConn 映射测试（当前不存在）。

- [ ] **Step 2: 跑测试确认失败**

Run: `$GO test ./ui/ -run 'TestSidebarMySQLConnFlow|TestOpenConnMySQLProfileFields' -count=1`
Expected: FAIL（`a.connFormKind undefined` / radio "MySQL" 找不到 / captured 零值）。

- [ ] **Step 3: 实现**

`ui/root.go` root 结构体 `connFormErr` 之后追加：

```go
	connFormKind string
	connFormHost string
	connFormPort string
	connFormUser string
	connFormPass string
	connFormDB   string
```

`newRoot` 返回字面量追加一行（connFormKind 默认 sqlite，既有测试直接写 Name/Path 时依赖此默认）：

```go
		connFormKind: "sqlite",
```

`ui/sidebar.go` 「新建连接」点击块替换为：

```go
			if ui.Button(c, "新建连接").Clicked() {
				a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, "", "", "", ""
				a.connFormKind, a.connFormHost, a.connFormPort = "sqlite", "", ""
				a.connFormUser, a.connFormPass, a.connFormDB = "", "", ""
			}
```

`connRow` 「编辑」点击块替换为：

```go
		if ui.Button(c, "编辑").Clicked() {
			a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, p.ID, p.Name, p.Path, ""
			a.connFormKind = p.Kind
			a.connFormHost, a.connFormUser, a.connFormPass, a.connFormDB = p.Host, p.User, p.Password, p.Database
			a.connFormPort = ""
			if p.Port > 0 {
				a.connFormPort = strconv.Itoa(p.Port)
			}
		}
```

`connModal` 内 `ui.Text(c, title).Bold()` 与路径行之间的内容替换为：

```go
		ui.RadioGroup(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Radio(c, &a.connFormKind, "sqlite", "SQLite")
				ui.Radio(c, &a.connFormKind, "mysql", "MySQL")
			})
		})
		ui.TextInput(c, &a.connFormName).Placeholder("名称").AutoFocus()
		if a.connFormKind == "mysql" {
			ui.TextInput(c, &a.connFormDB).Placeholder("数据库（可选）")
			ui.Row(c).Gap(6).Children(func() {
				ui.TextInput(c, &a.connFormHost).Placeholder("主机").Fill()
				ui.TextInput(c, &a.connFormPort).Placeholder("端口")
			})
			ui.Row(c).Gap(6).Children(func() {
				ui.TextInput(c, &a.connFormUser).Placeholder("用户").Fill()
				ui.TextInput(c, &a.connFormPass).Placeholder("密码").Password()
			})
		} else {
			ui.Row(c).Gap(6).Children(func() {
				ui.TextInput(c, &a.connFormPath).Placeholder("SQLite 文件路径").Fill()
				if ui.Button(c, "浏览…").Clicked() {
					a.browsePath()
				}
			})
		}
```

（原「名称 TextInput」与「路径 Row+浏览」两块由此整体替换；Modal 其余部分不动。）

`saveConnForm` 中 profile 组装行替换为：

```go
	p := app.ConnProfile{ID: a.connFormID, Name: a.connFormName, Kind: a.connFormKind, Path: a.connFormPath}
	if a.connFormKind == "mysql" {
		port, err := strconv.Atoi(a.connFormPort)
		if err != nil {
			a.connFormErr = "端口必须是数字"
			return
		}
		p.Host = a.connFormHost
		p.Port = port
		p.User = a.connFormUser
		p.Password = a.connFormPass
		p.Database = a.connFormDB
	}
```

`ui/sidebar.go` import 追加 `"strconv"`。

`ui/run.go` 的 `openConn` 中 `engine.Open(...)` 调用替换为：

```go
	sess, err := engine.Open(engine.Profile{
		Kind: p.Kind, Name: p.Name, DSN: p.Path,
		Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, Database: p.Database,
	})
```

- [ ] **Step 4: 跑测试确认通过 + 回归 + vet**

```bash
$GO test ./ui/ -run 'TestSidebar|TestOpenConn|TestRemoveActive|TestRunSQL|TestExecuteButton|TestResultPaneHint|TestObjectTree|TestReloadTree|TestExpandInactive' -count=1 -race -v
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
```

Expected: 全 PASS；全套 ok；vet 0。

- [ ] **Step 5: Commit**

```bash
git add ui/
git commit -m "task4: 连接表单类型切换与 MySQL 字段 + openConn 搬运"
```

---

### Task 5: 收尾（全量守门 / Readme / 验收清单）

**Files:**
- Modify: `Readme.md`
- Test: 全量门禁

**Interfaces:**
- Consumes: Task 1–4 全部产物。
- Produces: 里程碑记录与手动验收清单。

- [ ] **Step 1: 全量性能守门与测试**

```bash
$GO test ./ui/ -run 'TestSpikeMatrix|TestResultGridFrameBudget' -count=1 -v
$GO test ./... -count=1
$GO tool mygo vet .
```

Expected: spike 9 行无 `exceeds limit`（VV p99 3k≤8ms/12k≤15ms/100k≤50ms）、网格 p50≤16ms；全套 ok（`TestMySQLIntegration` SKIP 属预期）；vet 0。

- [ ] **Step 2: 更新 Readme**

功能清单在 M2 条目之后插入两行，并改写 M4 行：

```markdown
- [x] 选区执行：仅执行编辑器选中的 SQL，无选区时禁用（禁止跑整个控制台）
- [x] M4（部分）：MySQL 连接 + 执行 + 对象树（information_schema；PostgreSQL 未做）
```

（原 `- [ ] M4：MySQL/PostgreSQL（方言与能力位抽象验证）` 行删除，由上面第二行替代。）

- [ ] **Step 3: M3+ 前的手动验收清单（记录到报告，不代跑 GUI）**

1. SQLite 连接后选中一条 SELECT → 执行按钮亮起 → 只跑选区；无选区时按钮灰 + 提示「请先选中要执行的 SQL」；
2. F5/Ctrl+Enter 仅在有选区时生效；
3. 新建 MySQL 连接（Radio 切换、密码掩码、端口校验非数字报「端口必须是数字」）→ 重启后仍在；
4. 双击 MySQL 连接成功（真实 MySQL）；错密码 → 红点 + 节点错误行；修复后再次展开可重试；
5. MySQL 连接选中库后对象树显示 表/视图/索引；未选库展开 → 红字「未选择数据库」；
6. 选中多条语句执行（MySQL `multiStatements=true`）可跑通；
7. sqlite 浏览按钮仅在 SQLite 类型显示。

- [ ] **Step 4: Commit**

```bash
git add Readme.md
git commit -m "task5: 收尾（Readme 勾选选区执行与 MySQL）"
```

---

## 计划级注意事项（给执行者）

- **选区时序**：`selStart/selEnd` 每帧由 `in.TextSelection()` 覆写（帧内、`in` 构建后）；`execPending` 在同一帧的覆写之后处理——这是生产路径。测试不要试图用 `tt.Frame()` 前设置缓存来驱动按钮（会被覆写）；正路径断言一律直调 `runSQL`。`Element` 禁止跨 pass 使用。
- **既有测试连锁**：Task 1 后任何「点执行按钮」的测试都必须有真实 UI 选区或改为直调 `runSQL`——全局 grep `Click("执行")` 检查。
- **engine.Register 全局性**：ui 测试注册 "mysql" 假 factory 后，同二进制内任何测试不得 import `driver/mysql`；driver/mysql 的测试在独立包内互不影响。
- **go.mod**：`go get github.com/go-sql-driver/mysql@latest` 只应新增这一条直接依赖；若 mygo 被动升级立即钉回 v0.4.8；commit message 说明新依赖及版本。
- **MySQL 集成测试**：默认 SKIP；有真实库时 `DB_G_MYSQL_HOST=... DB_G_MYSQL_PORT=3306 DB_G_MYSQL_USER=... DB_G_MYSQL_PASSWORD=... DB_G_MYSQL_DATABASE=... $GO test ./driver/mysql/ -run Integration -v` 验证。
- **每帧开销红线**：选区缓存是两次整数赋值；不得在渲染路径做 Profile→DSN 拼装或文件 IO。
- **文案红线**：「请先选中要执行的 SQL」「请先连接数据源」「端口必须是数字」「主机不能为空」「端口必须在 1–65535」「mysql: 未选择数据库」逐字使用。

## Self-Review（计划作者执行）

1. **Spec 覆盖**：§1 选区执行（Task 1：缓存/延迟执行/按钮禁用/按态提示/原样投递）；§2 数据模型（Task 2：字段/校验分支/validateProfile 消重/IsDir/旧 JSON）；§3 driver（Task 3：Register/FormatDSN 五参数/RunStatement 预算/Caps/Objects 三查询/未选库错误）；§4 表单（Task 4：Radio 切换/字段组/掩码/浏览仅 sqlite/编辑回填/openConn 搬运）；§5 对象树零改动（Task 3 Objects 即达成，Task 4 测试隐含验证注册链）；§6 测试（各任务 + Task 5 集成 SKIP 契约）；§7 不做清单未触碰。无缺口。
2. **占位符**：无 TBD/TODO/「类似 Task N」；每步含完整代码或精确 diff 指令。
3. **类型一致性**：`connFormPort string` ↔ `ConnProfile.Port int`（Atoi/Itoa 转换点唯一在 saveConnForm/编辑回填）；`engine.Profile` 字段名 Task 2 定义、Task 3/4 使用一致；`openSession/isQueryLike/session` 在测试与实现间签名一致；`treeFakeSession` 复用 Task 5(M2) 已有定义。


