# M2 布局骨架 + 连接管理与对象树 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 主页面布局对齐 db-k 骨架（左树边栏 + 顶栏 + 单行工具栏 + 编辑器/结果分屏），交付连接 profile 持久化与 SQLite 懒加载对象树。

**Architecture:** 沿用 M1 四层：`engine` 增可选 `ObjectLister` 接口与 `CapObjects` 能力位（不改 `Session` 方法集）；`driver/sqlite` 经 `sqlite_master` 枚举；`app` 新增 `ConnStore`（JSON 原子落盘）；`ui` 重构 rootView 为 `ui.Split` 骨架并新增侧栏/树/连接表单。

**Tech Stack:** Go 1.27.1（`/home/zk/Code/go/bin/go1.27.1`）、mygo v0.4.8（`ui.Split/Tree/TreeItem/Modal/Scroll`）、modernc.org/sqlite v1.33.1。

## Global Constraints

- Go 工具链固定 `export GO=/home/zk/Code/go/bin/go1.27.1`；模块名 `db-g`；mygo 钉 v0.4.8（若 `go get` 意外升级必须钉回）。
- 本模块包 `ui` 与 `github.com/egoist/mygo/ui` 同名共存是既有约定：`ui/` 目录文件内 `ui.` 一律指 mygo 的 ui；根 `main.go` 中 mygo ui 别名 `mygoUI`。
- 用户可见文案全中文；不写行内注释；导出 API 写 doc comment；代码过 `$GO fmt`。
- commit 格式 `taskN: <内容>`（N 为本计划任务号）。
- 每任务收尾跑 `$GO tool mygo vet .`（Task 1 起工具已注册）。
- spec 位值笔误修正：`engine/caps.go` 中 `CapSQLCompletion` 已占 `1<<3`，本计划新增 `CapObjects` 经 iota 续位得 `1<<4`（=16），不与任何既有位冲突。
- `ui.T.Context()` 需 Go 1.24+（本项目 1.27.1 满足）；测试统一 `t.Context()`。
- 性能守门（布局大改后必跑）：VV 单键 p99 3k≤8ms / 12k≤15ms / 100k≤50ms（既有 `spikeP99Limit` 断言）；结果网格 100k×50 帧 p50≤16ms。
- 测试不读写用户真实配置：凡涉及 `ConnStore` 的 UI 测试一律注入 `app.LoadStore(filepath.Join(t.TempDir(), "connections.json"))`。

## File Structure

| 文件 | 动作 | 职责 |
|---|---|---|
| `engine/caps.go` | 修改 | 追加 `CapObjects`（iota 续位 → 1<<4） |
| `engine/object.go` | 新建 | `ObjectKind` / `DBObject` / `ObjectLister` 类型契约 |
| `driver/sqlite/sqlite.go` | 修改 | `Objects()` 实现 + `Caps()` 返回 `CapObjects` |
| `driver/sqlite/sqlite_test.go` | 修改 | 追加 `TestObjects` |
| `app/conn.go` | 新建 | `ConnProfile` / `ConnStore`（JSON 原子落盘、损坏恢复） |
| `app/conn_test.go` | 新建 | store 单测 |
| `ui/root.go` | 修改 | 布局骨架重构（`ui.Split`、顶栏、单行工具栏、字段更名） |
| `ui/sidebar.go` | 新建 | 侧栏：工具条、连接列表树、状态点、连接 Modal、双击打开 |
| `ui/tree.go` | 新建 | 对象树分组节点、懒加载缓存、失败红字 |
| `ui/run.go` | 修改 | 删 `openDatabase`；`openSQLite` 改为 `openConn` 包装；字段更名适配 |
| `ui/run_test.go` | 修改 | `dbName`→`connName` 引用 |
| `ui/sidebar_test.go` | 新建 | 侧栏/连接/树 headless 测试 |
| `Readme.md` | 修改 | 勾选 M2 |

依赖顺序：Task 1（engine+driver）→ Task 2（store）→ Task 3（布局壳）→ Task 4（连接 UI）→ Task 5（对象树）→ Task 6（收尾）。

---

### Task 1: engine 对象契约 + SQLite Objects 实现

**Files:**
- Modify: `engine/caps.go`
- Create: `engine/object.go`
- Modify: `driver/sqlite/sqlite.go`（`Caps()` + `Objects()`）
- Modify: `driver/sqlite/sqlite_test.go`（追加 `TestObjects`）

**Interfaces:**
- Consumes: Task 1（M1）的 `engine.Session / Capabilities`；driver 既有 `session` 结构与 `mu`。
- Produces:
  - `engine.ObjectKind`（`ObjectTable/ObjectView/ObjectIndex`）、`engine.DBObject{Kind,Name}`、`engine.ObjectLister` 接口 `Objects(ctx, ObjectKind) ([]DBObject, error)`
  - `engine.CapObjects`（`1<<4`）
  - `*sqlite.session` 实现 `ObjectLister`；`Caps()` 返回 `engine.CapObjects`
  - UI/测试经 `sess.(engine.ObjectLister)` 断言使用

- [ ] **Step 1: 写失败测试（caps + objects）**

`engine/caps.go` 的 const 块末尾追加一行（iota 续位）：

```go
const (
	CapEditableResult Capabilities = 1 << iota
	CapFetchMore
	CapObjectDDL
	CapSQLCompletion
	CapObjects
)
```

`engine/session_test.go` 追加：

```go
func TestCapObjects(t *testing.T) {
	var c Capabilities = CapObjects
	if !c.Has(CapObjects) {
		t.Fatal("CapObjects must be settable and readable")
	}
	if CapObjects == CapSQLCompletion || CapObjects == CapEditableResult ||
		CapObjects == CapFetchMore || CapObjects == CapObjectDDL {
		t.Fatal("CapObjects must not collide with existing bits")
	}
	if CapObjects != 1<<4 {
		t.Fatalf("CapObjects = %d, want %d", CapObjects, 1<<4)
	}
}
```

`driver/sqlite/sqlite_test.go` 末尾追加：

```go
func TestObjects(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	lister, ok := s.(engine.ObjectLister)
	if !ok {
		t.Fatal("sqlite session must implement engine.ObjectLister")
	}
	if !s.Caps().Has(engine.CapObjects) {
		t.Fatal("sqlite Caps must include CapObjects")
	}
	stmts := []string{
		"CREATE TABLE a (id INTEGER)",
		"CREATE TABLE b (id INTEGER)",
		"CREATE VIEW v AS SELECT id FROM a",
		"CREATE INDEX ia ON a (id)",
		"CREATE TABLE seq (id INTEGER PRIMARY KEY AUTOINCREMENT)",
	}
	for _, q := range stmts {
		if _, err := s.RunStatement(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	tables, err := lister.Objects(ctx, engine.ObjectTable)
	if err != nil {
		t.Fatal(err)
	}
	if names := objectNames(tables); !equalStrings(names, []string{"a", "b", "seq"}) {
		t.Fatalf("tables = %v, want [a b seq]（sqlite_sequence 已被过滤）", names)
	}
	views, err := lister.Objects(ctx, engine.ObjectView)
	if err != nil {
		t.Fatal(err)
	}
	if names := objectNames(views); !equalStrings(names, []string{"v"}) {
		t.Fatalf("views = %v, want [v]", names)
	}
	indexes, err := lister.Objects(ctx, engine.ObjectIndex)
	if err != nil {
		t.Fatal(err)
	}
	if names := objectNames(indexes); !equalStrings(names, []string{"ia"}) {
		t.Fatalf("indexes = %v, want [ia]", names)
	}
}

func objectNames(objs []engine.DBObject) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.Name
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: 运行确认失败**

Run: `$GO test ./driver/sqlite/ ./engine/ -run 'TestObjects|TestCapObjects' -count=1`
Expected: FAIL（`undefined: engine.ObjectLister` / `undefined: engine.ObjectTable` / Caps 块缺 `CapObjects` 编译错误）

- [ ] **Step 3: 实现**

`engine/object.go`：

```go
package engine

import "context"

// ObjectKind 是对象树中的条目类型。
type ObjectKind int

const (
	ObjectTable ObjectKind = iota
	ObjectView
	ObjectIndex
)

// DBObject 是对象树枚举出的一个条目。
type DBObject struct {
	Kind ObjectKind
	Name string
}

// ObjectLister 是可选能力接口：数据源枚举自身对象（表/视图/索引）。
// UI 经类型断言与 CapObjects 能力位判定后使用。
type ObjectLister interface {
	Objects(ctx context.Context, kind ObjectKind) ([]DBObject, error)
}
```

`driver/sqlite/sqlite.go`：`Caps` 改为

```go
func (s *session) Caps() engine.Capabilities { return engine.CapObjects }
```

并在文件末尾追加：

```go
func (s *session) Objects(ctx context.Context, kind engine.ObjectKind) ([]engine.DBObject, error) {
	var t string
	switch kind {
	case engine.ObjectTable:
		t = "table"
	case engine.ObjectView:
		t = "view"
	case engine.ObjectIndex:
		t = "index"
	default:
		return nil, fmt.Errorf("sqlite: unknown object kind %d", kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("sqlite: session not open")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = ? AND name NOT LIKE 'sqlite_%' ORDER BY name`, t)
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

- [ ] **Step 4: 运行确认通过**

Run: `$GO test ./driver/sqlite/ ./engine/ -count=1 -race -v -run 'TestObjects|TestCapObjects'`
Expected: `TestObjects`、`TestCapObjects` 均 `--- PASS`

- [ ] **Step 5: 全包回归 + vet + Commit**

```bash
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
git add engine/ driver/
git commit -m "task1: engine 对象契约 + sqlite_master 枚举（CapObjects 1<<4）"
```

Expected: 全部 ok；vet 退出码 0。

---

### Task 2: app.ConnStore（持久化连接库）

**Files:**
- Create: `app/conn.go`
- Create: `app/conn_test.go`

**Interfaces:**
- Consumes: 无（纯 Go，标准库 only）。
- Produces（Task 4/5 与测试依赖）:
  - `type ConnProfile struct { ID, Name, Kind, Path string }`
  - `func DefaultStorePath() string`（`os.UserConfigDir()/db-g/connections.json`）
  - `func LoadStore(path string) *ConnStore`
  - `(*ConnStore) List() []ConnProfile` / `Get(id) (ConnProfile, bool)` / `Warning() string`
  - `(*ConnStore) Add(ConnProfile) error` / `Update(ConnProfile) error` / `Remove(id string) error`
  - `(*ConnStore) Active() (ConnProfile, bool)` / `SetActive(id string)`（空串清空）
  - 行为：Add/Update/Remove/SetActive 每次同步原子落盘（tmp+rename，0600，MkdirAll 0700）；Add 校验 Name 非空、Path 存在、Kind=="sqlite"、ID 空则生成、重复报错；损坏 JSON → 改名 `.bak` + `Warning()` 非空；`Remove` 活动连接时清空 activeID。

- [ ] **Step 1: 写失败测试**

创建 `app/conn_test.go`：

```go
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempStore(t *testing.T) *ConnStore {
	t.Helper()
	return LoadStore(filepath.Join(t.TempDir(), "connections.json"))
}

func mustAdd(t *testing.T, s *ConnStore, name, path string) ConnProfile {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	p := ConnProfile{Name: name, Kind: "sqlite", Path: path}
	if err := s.Add(p); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(p.ID)
	if !ok {
		t.Fatalf("added profile %q not found", name)
	}
	return got
}

func TestStoreAddRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s := LoadStore(path)
	db := filepath.Join(dir, "a.db")
	p := mustAdd(t, s, "本地库", db)
	if p.ID == "" {
		t.Fatal("Add must assign an ID")
	}
	if len(s.List()) != 1 {
		t.Fatalf("list = %v", s.List())
	}

	s2 := LoadStore(path)
	if len(s2.List()) != 1 || s2.List()[0].Name != "本地库" || s2.List()[0].ID != p.ID {
		t.Fatalf("reload: %+v", s2.List())
	}
}

func TestStoreAddValidation(t *testing.T) {
	dir := t.TempDir()
	s := LoadStore(filepath.Join(dir, "c.json"))
	if err := s.Add(ConnProfile{Name: "", Kind: "sqlite", Path: filepath.Join(dir, "x.db")}); err == nil {
		t.Fatal("empty name must fail")
	}
	if err := s.Add(ConnProfile{Name: "n", Kind: "sqlite", Path: filepath.Join(dir, "missing.db")}); err == nil {
		t.Fatal("missing path must fail")
	}
	if err := s.Add(ConnProfile{Name: "n", Kind: "mysql", Path: dir}); err == nil {
		t.Fatal("non-sqlite kind must fail in M2")
	}
	ok := mustAdd(t, s, "dup", filepath.Join(dir, "b.db"))
	if err := s.Add(ConnProfile{ID: ok.ID, Name: "clash", Kind: "sqlite", Path: ok.Path}); err == nil {
		t.Fatal("explicit duplicate ID must fail")
	}
}

func TestStoreUpdateRemoveActive(t *testing.T) {
	dir := t.TempDir()
	s := LoadStore(filepath.Join(dir, "c.json"))
	p := mustAdd(t, s, "one", filepath.Join(dir, "a.db"))
	p.Name = "renamed"
	if err := s.Update(p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(p.ID)
	if got.Name != "renamed" {
		t.Fatalf("update: %+v", got)
	}
	s.SetActive(p.ID)
	if act, ok := s.Active(); !ok || act.ID != p.ID {
		t.Fatalf("active: %+v %v", act, ok)
	}
	if err := s.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Active(); ok {
		t.Fatal("removing active connection must clear activeID")
	}
	if len(s.List()) != 0 {
		t.Fatal("list must be empty")
	}
	s2 := LoadStore(filepath.Join(dir, "c.json"))
	if len(s2.List()) != 0 {
		t.Fatal("removal must persist")
	}
}

func TestStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(path)
	if len(s.List()) != 0 {
		t.Fatal("corrupt file must start empty")
	}
	if !strings.Contains(s.Warning(), "损坏") {
		t.Fatalf("warning = %q", s.Warning())
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal("corrupt file must be kept as .bak")
	}
}

func TestStoreUpdateUnknownID(t *testing.T) {
	s := tempStore(t)
	if err := s.Update(ConnProfile{ID: "nope", Name: "n", Kind: "sqlite", Path: os.TempDir()}); err == nil {
		t.Fatal("update unknown ID must fail")
	}
	if err := s.Remove("nope"); err == nil {
		t.Fatal("remove unknown ID must fail")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `$GO test ./app/ -run TestStore -count=1`
Expected: FAIL（`undefined: LoadStore` / `undefined: ConnProfile`）

- [ ] **Step 3: 实现 `app/conn.go`**

```go
package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ConnProfile 是一个持久化的数据源连接配置（M2 仅 sqlite 文件）。
type ConnProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// ConnStore 管理连接列表与活动连接；变更同步原子落盘。
type ConnStore struct {
	mu       sync.Mutex
	path     string
	conns    []ConnProfile
	activeID string
	warning  string
}

// DefaultStorePath 返回用户配置目录下的连接库路径。
func DefaultStorePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "connections.json"
	}
	return filepath.Join(dir, "db-g", "connections.json")
}

// LoadStore 读取持久化文件；缺失为空表，损坏改名 .bak 后空表并记警告。
func LoadStore(path string) *ConnStore {
	s := &ConnStore{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.warning = "读取连接配置失败: " + err.Error()
		}
		return s
	}
	var file struct {
		Conns    []ConnProfile `json:"conns"`
		ActiveID string        `json:"activeID"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		_ = os.WriteFile(path+".bak", data, 0o600)
		s.warning = "连接配置损坏，已备份为 .bak"
		return s
	}
	s.conns = file.Conns
	s.activeID = file.ActiveID
	return s
}

// Warning 返回加载期警告（损坏/读失败），空串表示正常。
func (s *ConnStore) Warning() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.warning
}

// List 返回全部连接（副本）。
func (s *ConnStore) List() []ConnProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ConnProfile, len(s.conns))
	copy(out, s.conns)
	return out
}

// Get 按 ID 查找连接。
func (s *ConnStore) Get(id string) (ConnProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.conns {
		if p.ID == id {
			return p, true
		}
	}
	return ConnProfile{}, false
}

func (s *ConnStore) save() error {
	data, err := json.MarshalIndent(struct {
		Conns    []ConnProfile `json:"conns"`
		ActiveID string        `json:"activeID"`
	}{s.conns, s.activeID}, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func genID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Add 校验并新增连接；ID 为空时自动生成。
func (s *ConnStore) Add(p ConnProfile) error {
	if p.Name == "" {
		return errors.New("连接名称不能为空")
	}
	if p.Kind != "sqlite" {
		return fmt.Errorf("M2 仅支持 sqlite，收到 %q", p.Kind)
	}
	if _, err := os.Stat(p.Path); err != nil {
		return fmt.Errorf("路径不可用: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		id, err := genID()
		if err != nil {
			return err
		}
		p.ID = id
	}
	for _, e := range s.conns {
		if e.ID == p.ID {
			return fmt.Errorf("连接 ID 已存在: %s", p.ID)
		}
	}
	s.conns = append(s.conns, p)
	if err := s.save(); err != nil {
		s.conns = s.conns[:len(s.conns)-1]
		return err
	}
	return nil
}

// Update 按 ID 更新连接（校验同 Add）。
func (s *ConnStore) Update(p ConnProfile) error {
	if p.Name == "" {
		return errors.New("连接名称不能为空")
	}
	if p.Kind != "sqlite" {
		return fmt.Errorf("M2 仅支持 sqlite，收到 %q", p.Kind)
	}
	if _, err := os.Stat(p.Path); err != nil {
		return fmt.Errorf("路径不可用: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.conns {
		if e.ID == p.ID {
			s.conns[i] = p
			if err := s.save(); err != nil {
				s.conns[i] = e
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("连接不存在: %s", p.ID)
}

// Remove 按 ID 删除；删除活动连接时清空活动指针。
func (s *ConnStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.conns {
		if e.ID == id {
			oldActive := s.activeID
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			if s.activeID == id {
				s.activeID = ""
			}
			if err := s.save(); err != nil {
				s.conns = append(s.conns[:i], append([]ConnProfile{e}, s.conns[i:]...)...)
				s.activeID = oldActive
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("连接不存在: %s", id)
}

// Active 返回当前活动连接。
func (s *ConnStore) Active() (ConnProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.conns {
		if p.ID == s.activeID {
			return p, true
		}
	}
	return ConnProfile{}, false
}

// SetActive 设置活动连接；空串清空。
func (s *ConnStore) SetActive(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeID = id
	_ = s.save()
}
```

- [ ] **Step 4: 运行确认通过**

Run: `$GO test ./app/ -count=1 -race -v -run TestStore`
Expected: `TestStoreAddRoundTrip/AddValidation/UpdateRemoveActive/CorruptFile/UpdateUnknownID` 全 `--- PASS`

- [ ] **Step 5: 全包回归 + vet + Commit**

```bash
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
git add app/
git commit -m "task2: app.ConnStore 连接库（JSON 原子落盘/损坏恢复/活动连接）"
```

---

### Task 3: 布局骨架（rootView 重构为 db-k 结构）

**Files:**
- Modify: `ui/root.go`（root 结构/newRoot/NewRoot/rootView 重排）
- Modify: `ui/run.go`（删 `openDatabase`，`openSQLite` 字段适配）
- Modify: `ui/run_test.go`（`dbName/openErr` → `connName/connErr`）
- Create: `ui/sidebar.go`（本任务仅空态壳；Task 4 填充）

**Interfaces:**
- Consumes: Task 2 的 `app.LoadStore / DefaultStorePath / ConnStore`（本任务仅接线加载，不交互）。
- Produces:
  - `root` 新字段：`sidebarWidth float32`（默认 280）、`store *app.ConnStore`；`dbName/openErr` 更名 `connName/connErr`
  - `NewRoot()` 用 `app.LoadStore(app.DefaultStorePath())` 填充 store；`newRoot()`（测试入口）store 为 nil（UI 对 nil 容错为空列表）
  - `(*root) sidebar(c *ui.Context)`：侧栏壳（`ui/sidebar.go`）
  - `(*root) connDot(c *ui.Context)`：状态点（绿=有会话/红=connErr/灰=无连接）
  - rootView 结构：`ui.Split(sidebarWidth, sidebar, 右侧Column)`；右侧 Column = 顶栏行 + 工具行（HUD+尺寸+策略+查找合并单行）+ 编辑器/结果分屏（分屏内容不动）
  - 移除：`openDatabase()`、「打开…」按钮

- [ ] **Step 1: 重构 `ui/root.go`**

`root` 结构体与 `newRoot`/`NewRoot` 整体替换为：

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
	sidebarWidth  float32
	store         *app.ConnStore
	connName      string
	connErr       string
}

func newRoot(text string, s strats) *root {
	return &root{
		ed:            NewEditor(text, s),
		sampler:       NewSampler(128),
		con:           app.NewConsole(),
		resultsHeight: 260,
		sidebarWidth:  280,
	}
}

// NewRoot 创建交互窗口状态（初始 3k 行 SQL，SCOPE 环境变量选策略，加载用户连接库）。
func NewRoot() *root {
	r := newRoot(buildSQL(3_000), stratsFromEnv())
	r.store = app.LoadStore(app.DefaultStorePath())
	return r
}
```

`rootView` 整体替换为（编辑器/分屏函数体一字不改，仅外层包装与顶栏/工具行变化）：

```go
func rootView(a *root) func(c *ui.Context) {
	return func(c *ui.Context) {
		ed := a.ed
		ed.Update()

		if c.Shortcut(ui.Ctrl, ui.KeyEnter) || c.Shortcut(0, ui.KeyF5) {
			a.runSQL()
		}

		ui.Split(c, &a.sidebarWidth, func() {
			a.sidebar(c)
		}, func() {
			ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
				// 顶栏：连接状态 + 执行
				ui.Row(c).Gap(8).Children(func() {
					a.connDot(c)
					if a.connName != "" {
						ui.Text(c, a.connName).Bold()
					}
					ui.Spacer(c)
					if ui.Button(c, "执行").Disabled(a.con.Running() || a.con.Session() == nil).Clicked() {
						a.runSQL()
					}
				})

				// 工具行：HUD + 尺寸 + 策略 + 查找
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, fmt.Sprintf(
						"lines=%d injected=%d tokenize=%s inject=%s build(last)=%s",
						ed.lineCount(), ed.Injected,
						scopeName(ed.strat.tokenize), scopeName(ed.strat.inject),
						fmtDur(appLastBuild),
					)).FontSize(12).SingleLine()
					if ui.Button(c, "3k").Clicked() {
						ed.Buf.Set(buildSQL(3_000))
						ed.dirty = true
					}
					if ui.Button(c, "12k").Clicked() {
						ed.Buf.Set(buildSQL(12_000))
						ed.dirty = true
					}
					if ui.Button(c, "100k").Clicked() {
						ed.Buf.Set(buildSQL(100_000))
						ed.dirty = true
					}
					if ui.Button(c, "FF").Clicked() {
						ed.SetStrategy(strats{scopeFull, scopeFull})
					}
					if ui.Button(c, "FV").Clicked() {
						ed.SetStrategy(strats{scopeFull, scopeViewport})
					}
					if ui.Button(c, "VV").Clicked() {
						ed.SetStrategy(strats{scopeViewport, scopeViewport})
					}
					ui.Spacer(c)
					ui.TextInput(c, &a.findQuery).Placeholder("查找")
					if ui.Button(c, "查找").Clicked() {
						a.findActive = true
					}
					if ui.Button(c, "清除").Clicked() {
						a.findQuery, a.findActive = "", false
					}
				})

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
			})
		}).Fill()
	}
}
```

（查找按钮文案从 Find/Clear 改为中文「查找/清除」，placeholder 改「查找」——用户可见文案中文约束；`bench_test.go` 的 measureOne 只点「编辑器」标签，不受影响。）

- [ ] **Step 2: 侧栏壳与状态点**

创建 `ui/sidebar.go`：

```go
package ui

import (
	"github.com/egoist/mygo/ui"
)

func (a *root) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
		if a.store != nil {
			if w := a.store.Warning(); w != "" {
				ui.Text(c, w).TextColor(c.Theme().Danger)
			}
			if len(a.store.List()) == 0 {
				ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
			}
		} else {
			ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *root) connDot(c *ui.Context) {
	switch {
	case a.con.Session() != nil:
		ui.Text(c, "●").TextColor(c.Theme().Success)
	case a.connErr != "":
		ui.Text(c, "●").TextColor(c.Theme().Danger)
	default:
		ui.Text(c, "●").TextColor(c.Theme().TextMuted)
	}
}
```

（Task 4 起在此文件追加工具条、连接行与 Modal；`a.store == nil` 分支保证未注入 store 的既有测试（`newRoot` 直造）不受影响。）

- [ ] **Step 3: 适配 `ui/run.go` 与测试**

`ui/run.go`：删除 `openDatabase` 函数；`openSQLite` 内 `a.openErr`→`a.connErr`、`a.dbName`→`a.connName`（3 处）；`mygo` import 若仅 `openDatabase` 使用则移除。

`ui/run_test.go`：`a.dbName` → `a.connName`（1 处）、`a.openErr` → `a.connErr`（1 处）。

- [ ] **Step 4: 编译与测试确认**

```bash
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
```

Expected: 全部 ok（既有 ui 测试如 `TestResultPane*`、`TestRun*` 全绿）；vet 0。

- [ ] **Step 5: 布局变更性能守门（快验）**

```bash
$GO test ./ui/ -run 'TestResultGridFrameBudget|TestSpikeMatrix' -count=1 -v
```

Expected: 两项 PASS（spike VV p99 仍 ≤8/15/50ms，网格 p50≤16ms）。若超限：回查工具行合并是否引入每帧重分配（如 HUD 每帧 `fmt.Sprintf` 为既有行为，不应劣化），修复后重测。

- [ ] **Step 6: Commit**

```bash
git add ui/
git commit -m "task3: 主页面骨架对齐 db-k（侧栏分栏/顶栏/单行工具栏）"
```

---

### Task 4: 连接管理 UI（侧栏工具条 / Modal 表单 / 双击打开 / 删除确认）

**Files:**
- Modify: `ui/root.go`（root 增表单状态字段；顶栏显示 connErr）
- Modify: `ui/sidebar.go`（工具条 + 连接行 + Modal + 删除确认）
- Modify: `ui/run.go`（`openSQLite` 重构为 `openConn` + 包装）
- Create: `ui/sidebar_test.go`

**Interfaces:**
- Consumes: Task 2 `app.ConnStore` 全方法；Task 3 的 `sidebar/connDot/root` 字段；`engine.Open`；既有 `mygo.Dialog.Open`。
- Produces:
  - `root` 新字段：`connFormOpen/connFormID/connFormName/connFormPath/connFormErr/connDeleteOpen/connDeleting`（均零值即初始）
  - `(*root) openConn(p app.ConnProfile) error`：engine.Open → sess.Open(5s) → SwapSession + connName + SetActive(p.ID)；失败写 connErr 返回
  - `(*root) openSQLite(path string) error`：保留包装（`openConn(ConnProfile{Name: Base, Kind:"sqlite", Path})`），既有 run_test 不改语义
  - `(*root) browsePath()`：a.win==nil 直接返回（测试容错）；否则协程弹文件选择器，win.Update 回写 connFormPath
  - `(*root) saveConnForm()` / `(*root) connModal(c)` / `(*root) connRow(c, p)` / `(*root) deleteModal(c)`
  - UI 测试约定：`ui.Tester` 无双击模拟——连接打开路径测 `openConn` 直调 + 渲染断言；双击接线进手动验收

- [ ] **Step 1: 写失败测试**

创建 `ui/sidebar_test.go`：

```go
package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"

	_ "db-g/driver/sqlite"
)

func newSidebarRoot(t *testing.T) *root {
	t.Helper()
	a := newTestRoot()
	a.store = app.LoadStore(filepath.Join(t.TempDir(), "connections.json"))
	return a
}

func seedConnFile(t *testing.T) string {
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
	if _, err := s.RunStatement(t.Context(), "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	return path
}

func TestSidebarEmptyHint(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("新建连接") {
		t.Fatalf("empty hint missing: %q", tt.Texts())
	}
}

func TestSidebarCreateConnFlow(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.connFormOpen {
		t.Fatal("modal must open")
	}
	a.connFormName = "本地库"
	a.connFormPath = path
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if a.connFormOpen {
		t.Fatal("modal must close after save")
	}
	if len(a.store.List()) != 1 || a.store.List()[0].Name != "本地库" {
		t.Fatalf("store: %+v", a.store.List())
	}
	tt.Frame()
	if !tt.HasText("本地库") {
		t.Fatalf("conn row not rendered: %q", tt.Texts())
	}
}

func TestSidebarCreateConnValidation(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.connFormName = "无路径"
	a.connFormPath = filepath.Join(t.TempDir(), "missing.db")
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if !a.connFormOpen || a.connFormErr == "" {
		t.Fatalf("invalid form must stay open with error: open=%v err=%q", a.connFormOpen, a.connFormErr)
	}
	if len(a.store.List()) != 0 {
		t.Fatal("invalid form must not persist")
	}
}

func TestOpenConnAndRunEndToEnd(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "本地库", Kind: "sqlite", Path: path}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	if err := a.openConn(stored); err != nil {
		t.Fatal(err)
	}
	if a.con.Session() == nil {
		t.Fatal("openConn must install a session")
	}
	if a.connName != "本地库" || a.connErr != "" {
		t.Fatalf("connName=%q connErr=%q", a.connName, a.connErr)
	}
	if act, ok := a.store.Active(); !ok || act.ID != stored.ID {
		t.Fatalf("active: %+v %v", act, ok)
	}
	a.ed.Buf.Set("SELECT name FROM users")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("执行"); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		if a.con.Result() != nil || a.con.Err() != "" {
			break
		}
		tt.Frame()
	}
	if a.con.Err() != "" {
		t.Fatalf("exec err: %s", a.con.Err())
	}
	if a.con.Result() == nil {
		t.Fatal("expected result")
	}
}

func TestOpenConnFailureKeepsState(t *testing.T) {
	a := newSidebarRoot(t)
	good := app.ConnProfile{Name: "good", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(good); err != nil {
		t.Fatal(err)
	}
	if err := a.openConn(a.store.List()[0]); err != nil {
		t.Fatal(err)
	}
	bad := app.ConnProfile{Name: "bad", Kind: "sqlite", Path: filepath.Join(t.TempDir(), "gone.db")}
	if err := a.openConn(bad); err == nil {
		t.Fatal("missing file must fail")
	}
	if a.connErr == "" {
		t.Fatal("connErr must be set")
	}
	if a.con.Session() == nil || a.connName != "good" {
		t.Fatal("failed open must keep previous session")
	}
}

func TestRemoveActiveConnClearsSession(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "临时", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	if err := a.openConn(stored); err != nil {
		t.Fatal(err)
	}
	a.removeConn(stored.ID)
	if a.con.Session() != nil || a.connName != "" {
		t.Fatalf("session=%v connName=%q", a.con.Session(), a.connName)
	}
	if _, ok := a.store.Get(stored.ID); ok {
		t.Fatal("profile must be removed")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `$GO test ./ui/ -run 'TestSidebar|TestOpenConn|TestRemoveActive' -count=1`
Expected: FAIL（`a.connFormOpen undefined` / `a.openConn undefined` / `a.removeConn undefined` 等编译错误）

- [ ] **Step 3: root 增字段 + 顶栏显示错误**

`ui/root.go` 的 `root` 结构体在 `connErr` 之后追加：

```go
	connFormOpen   bool
	connFormID     string
	connFormName   string
	connFormPath   string
	connFormErr    string
	connDeleteOpen bool
	connDeleting   string
```

顶栏（`connDot` 与 `connName` 之后）插入：

```go
					if a.connErr != "" {
						ui.Text(c, a.connErr).TextColor(c.Theme().Danger).SingleLine()
					}
```

- [ ] **Step 4: 实现 openConn（`ui/run.go`）**

`openSQLite` 整体替换为：

```go
func (a *root) openSQLite(path string) error {
	return a.openConn(app.ConnProfile{Name: filepath.Base(path), Kind: "sqlite", Path: path})
}

func (a *root) openConn(p app.ConnProfile) error {
	sess, err := engine.Open(engine.Profile{Kind: p.Kind, Name: p.Name, DSN: p.Path})
	if err != nil {
		a.connErr = err.Error()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Open(ctx); err != nil {
		a.connErr = err.Error()
		return err
	}
	a.con.SwapSession(sess)
	a.connName = p.Name
	a.connErr = ""
	if a.store != nil && p.ID != "" {
		a.store.SetActive(p.ID)
	}
	return nil
}

func (a *root) removeConn(id string) {
	if a.store == nil {
		return
	}
	if act, ok := a.store.Active(); ok && act.ID == id {
		a.con.SwapSession(nil)
		a.connName = ""
		a.store.SetActive("")
	}
	_ = a.store.Remove(id)
}
```

`ui/run.go` import 需包含 `db-g/app`（`openSQLite` 构造 `app.ConnProfile`）。

- [ ] **Step 5: 实现侧栏 UI（`ui/sidebar.go` 整体替换）**

```go
package ui

import (
	"github.com/egoist/mygo"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func (a *root) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "新建连接").Clicked() {
				a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, "", "", "", ""
			}
			if ui.Button(c, "刷新").Clicked() {
				a.reloadTree()
			}
		})
		if a.store == nil {
			ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
			a.connModal(c)
			return
		}
		if w := a.store.Warning(); w != "" {
			ui.Text(c, w).TextColor(c.Theme().Danger)
		}
		list := a.store.List()
		if len(list) == 0 {
			ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
		}
		ui.Scroll(c).Grow(1).Children(func() {
			for _, p := range list {
				a.connRow(c, p)
			}
		})
	})
	a.connModal(c)
}

func (a *root) connRow(c *ui.Context, p app.ConnProfile) {
	act, _ := a.store.Active()
	isActive := act.ID == p.ID && a.con.Session() != nil
	ui.Row(c).Gap(6).Children(func() {
		dot := ui.Text(c, "●")
		switch {
		case isActive:
			dot.TextColor(c.Theme().Success)
		case a.connErr != "" && a.connName == p.Name:
			dot.TextColor(c.Theme().Danger)
		default:
			dot.TextColor(c.Theme().TextMuted)
		}
		lbl := ui.Button(c, p.Name)
		if isActive {
			lbl.Bold()
		}
		if lbl.DoubleClicked() {
			_ = a.openConn(p)
		}
		ui.Spacer(c)
		if ui.Button(c, "编辑").Clicked() {
			a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, p.ID, p.Name, p.Path, ""
		}
		if ui.Button(c, "删除").Clicked() {
			a.connDeleting, a.connDeleteOpen = p.ID, true
		}
	})
}

func (a *root) connModal(c *ui.Context) {
	if a.connDeleteOpen {
		a.deleteModal(c)
		return
	}
	ui.Modal(c, &a.connFormOpen, func() {
		title := "新建连接"
		if a.connFormID != "" {
			title = "编辑连接"
		}
		ui.Text(c, title).Bold()
		ui.TextInput(c, &a.connFormName).Placeholder("名称").AutoFocus()
		ui.Row(c).Gap(6).Children(func() {
			ui.TextInput(c, &a.connFormPath).Placeholder("SQLite 文件路径").Fill()
			if ui.Button(c, "浏览…").Clicked() {
				a.browsePath()
			}
		})
		if a.connFormErr != "" {
			ui.Text(c, a.connFormErr).TextColor(c.Theme().Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.connFormOpen = false
			}
			if ui.PrimaryButton(c, "保存").Clicked() {
				a.saveConnForm()
			}
		})
	})
}

func (a *root) deleteModal(c *ui.Context) {
	name := ""
	for _, p := range a.store.List() {
		if p.ID == a.connDeleting {
			name = p.Name
		}
	}
	ui.Modal(c, &a.connDeleteOpen, func() {
		ui.Text(c, "删除连接「"+name+"」？").Bold()
		ui.Text(c, "仅移除配置，不删除数据库文件").TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.connDeleteOpen = false
			}
			if ui.Button(c, "删除").Clicked() {
				a.removeConn(a.connDeleting)
				a.connDeleting, a.connDeleteOpen = "", false
			}
		})
	})
}

func (a *root) saveConnForm() {
	if a.store == nil {
		a.connFormErr = "连接库不可用"
		return
	}
	p := app.ConnProfile{ID: a.connFormID, Name: a.connFormName, Kind: "sqlite", Path: a.connFormPath}
	var err error
	if a.connFormID == "" {
		err = a.store.Add(p)
	} else {
		err = a.store.Update(p)
	}
	if err != nil {
		a.connFormErr = err.Error()
		return
	}
	a.connFormOpen = false
}

func (a *root) browsePath() {
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win,
			Title:  "选择 SQLite 数据库",
			Filters: []mygo.FileFilter{
				{Name: "SQLite", Extensions: []string{"db", "sqlite", "sqlite3"}},
			},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		win.Update(func() { a.connFormPath = paths[0] })
	}()
}

func (a *root) reloadTree() {}
```

（`reloadTree` 本任务为空桩，Task 5 实现缓存清空；`ui.PrimaryButton(c, label)` 已对照 v0.4.8 `api_gen.go:1295` 确认存在。）

- [ ] **Step 6: 运行确认通过 + 回归 + vet**

```bash
$GO test ./ui/ -run 'TestSidebar|TestOpenConn|TestRemoveActive' -count=1 -race -v
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
```

Expected: 新测试全 PASS；全套 ok；vet 0。

- [ ] **Step 7: Commit**

```bash
git add ui/
git commit -m "task4: 连接管理 UI（Modal 表单/双击打开/删除确认/状态点）"
```

---

### Task 5: 对象树（分组懒加载 / 缓存 / 失败红字 / 刷新）

**Files:**
- Modify: `ui/root.go`（root 增树状态字段）
- Modify: `ui/sidebar.go`（连接行下挂对象分组；`reloadTree` 实现）
- Create: `ui/objecttree.go`（分组渲染与加载）
- Modify: `ui/sidebar_test.go`（追加树测试）

**Interfaces:**
- Consumes: Task 1 `engine.ObjectLister / ObjectKind / DBObject / CapObjects`；Task 4 `openConn/connRow/reloadTree` 桩。
- Produces:
  - `root` 新字段：`treeExpand map[string]bool`（连接展开）、`treeOpen map[string]*bool`（分组展开指针，键 `connID|kind`）、`treeData map[string]map[engine.ObjectKind][]engine.DBObject`、`treeErr map[string]string`（键同 treeOpen）、`treeSelected string`
  - `(*root) reloadTree()`：清空 treeData/treeErr（保留 treeExpand）
  - `(*root) loadGroups(p app.ConnProfile)`：经 `a.con.Session().(engine.ObjectLister)` 同步加载三分组（5s ctx），失败写 treeErr；会话缺 `CapObjects` → treeErr 写「该数据源不支持对象树」
  - `(*root) objectGroups(c, p)`：三个 `ui.TreeItem` 分组 + 叶子
  - **行为约定（spec 单会话模型落地）**：非活动连接展开时先 `openConn(p)`（自动切换连接，db-k 的双击语义在展开路径复用）；加载为帧内同步（sqlite_master 毫秒级，超时 5s ctx 兜底）；缓存命中不重复查询，「刷新」清空重来。

- [ ] **Step 1: 写失败测试**

`ui/sidebar_test.go` 末尾追加：

```go
type treeFakeSession struct {
	err  error
	objs []engine.DBObject
}

func (f treeFakeSession) Open(context.Context) error { return nil }
func (f treeFakeSession) Close() error               { return nil }
func (f treeFakeSession) Cancel() bool               { return false }
func (f treeFakeSession) Caps() engine.Capabilities  { return engine.CapObjects }
func (f treeFakeSession) RunStatement(context.Context, string) (*engine.QueryResult, error) {
	return &engine.QueryResult{}, nil
}
func (f treeFakeSession) Objects(context.Context, engine.ObjectKind) ([]engine.DBObject, error) {
	return f.objs, f.err
}

func TestObjectTreeRendersLeaves(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.con.UseSession(treeFakeSession{objs: []engine.DBObject{
		{Kind: engine.ObjectTable, Name: "users"},
	}})
	a.connName = stored.Name
	a.treeExpand[stored.ID] = true
	a.groupOpenPtr(stored.ID+"|"+kindKey(engine.ObjectTable), true)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("users") {
		t.Fatalf("leaf missing: %q", tt.Texts())
	}
	if !tt.HasText("表 (1)") {
		t.Fatalf("group count missing: %q", tt.Texts())
	}
}

func TestObjectTreeErrorLine(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.con.UseSession(treeFakeSession{err: errors.New("枚举失败")})
	a.connName = stored.Name
	a.treeExpand[stored.ID] = true
	a.groupOpenPtr(stored.ID+"|"+kindKey(engine.ObjectTable), true)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("枚举失败") {
		t.Fatalf("error line missing: %q", tt.Texts())
	}
}

func TestReloadTreeClearsCache(t *testing.T) {
	a := newSidebarRoot(t)
	a.treeData = map[string]map[engine.ObjectKind][]engine.DBObject{"x": {}}
	a.treeErr = map[string]string{"x|table": "boom"}
	a.reloadTree()
	if a.treeData != nil || len(a.treeErr) != 0 {
		t.Fatalf("reloadTree must clear cache: %v %v", a.treeData, a.treeErr)
	}
}

func TestExpandInactiveAutoConnects(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "auto", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.treeExpand[stored.ID] = true
	if a.con.Session() == nil {
		a.expandConn(stored)
	}
	if a.con.Session() == nil || a.connName != "auto" {
		t.Fatalf("expand of inactive conn must connect: session=%v name=%q", a.con.Session(), a.connName)
	}
}
```

import 需追加 `"context"`、`"errors"`（`sidebar_test.go` 顶部）。

- [ ] **Step 2: 运行确认失败**

Run: `$GO test ./ui/ -run 'TestObjectTree|TestReloadTree|TestExpandInactive' -count=1`
Expected: FAIL（`a.treeExpand undefined` / `a.groupOpenPtr undefined` / `a.expandConn undefined`）

- [ ] **Step 3: root 增树状态字段**

`ui/root.go` 的 `root` 结构体在 `connDeleting` 之后追加：

```go
	treeExpand  map[string]bool
	treeOpen    map[string]*bool
	treeData    map[string]map[engine.ObjectKind][]engine.DBObject
	treeErr     map[string]string
	treeSelected string
```

`newRoot` 返回值中追加初始化：

```go
		treeExpand: map[string]bool{},
		treeOpen:   map[string]*bool{},
		treeData:   map[string]map[engine.ObjectKind][]engine.DBObject{},
		treeErr:    map[string]string{},
```

`ui/root.go` import 追加 `"db-g/engine"`。

- [ ] **Step 4: 实现 `ui/objecttree.go`**

```go
package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

var objectKinds = []struct {
	kind    engine.ObjectKind
	label   string
}{
	{engine.ObjectTable, "表"},
	{engine.ObjectView, "视图"},
	{engine.ObjectIndex, "索引"},
}

func kindKey(k engine.ObjectKind) string {
	switch k {
	case engine.ObjectTable:
		return "table"
	case engine.ObjectView:
		return "view"
	default:
		return "index"
	}
}

func (a *root) groupOpenPtr(key string, v bool) *bool {
	if a.treeOpen == nil {
		a.treeOpen = map[string]*bool{}
	}
	if p, ok := a.treeOpen[key]; ok {
		return p
	}
	p := new(bool)
	*p = v
	a.treeOpen[key] = p
	return p
}

func (a *root) reloadTree() {
	a.treeData = nil
	a.treeErr = nil
}

func (a *root) expandConn(p app.ConnProfile) {
	if a.con.Session() == nil || a.connName != p.Name {
		_ = a.openConn(p)
	}
}

func (a *root) loadGroups(p app.ConnProfile) {
	key := p.ID
	if _, ok := a.treeData[key]; ok {
		return
	}
	sess := a.con.Session()
	if sess == nil {
		a.treeErr[key] = "未连接"
		return
	}
	if !sess.Caps().Has(engine.CapObjects) {
		a.treeErr[key] = "该数据源不支持对象树"
		return
	}
	lister, ok := sess.(engine.ObjectLister)
	if !ok {
		a.treeErr[key] = "该数据源不支持对象树"
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data := map[engine.ObjectKind][]engine.DBObject{}
	for _, g := range objectKinds {
		objs, err := lister.Objects(ctx, g.kind)
		if err != nil {
			a.treeErr[key] = err.Error()
			return
		}
		data[g.kind] = objs
	}
	if a.treeData == nil {
		a.treeData = map[string]map[engine.ObjectKind][]engine.DBObject{}
	}
	a.treeData[key] = data
}

func (a *root) objectGroups(c *ui.Context, p app.ConnProfile) {
	if err := a.treeErr[p.ID]; err != "" {
		ui.Text(c, err).TextColor(c.Theme().Danger)
		return
	}
	if _, ok := a.treeData[p.ID]; !ok {
		ui.Text(c, "加载中…").TextColor(c.Theme().TextMuted)
		a.loadGroups(p)
		if err := a.treeErr[p.ID]; err != "" {
			ui.Text(c, err).TextColor(c.Theme().Danger)
			return
		}
	}
	data := a.treeData[p.ID]
	ui.Tree(c, func() {
		for _, g := range objectKinds {
			objs := data[g.kind]
			label := g.label
			if _, loaded := a.treeData[p.ID]; loaded {
				label = fmt.Sprintf("%s (%d)", g.label, len(objs))
			}
			open := a.groupOpenPtr(p.ID+"|"+kindKey(g.kind), false)
			ui.TreeItem(c, label, open, func() {
				for _, o := range objs {
					if ui.TreeItem(c, o.Name, nil, nil).Selected(a.treeSelected == p.ID+"|"+o.Name).Clicked() {
						a.treeSelected = p.ID + "|" + o.Name
					}
				}
			})
		}
	})
}
```

（`loadGroups` 在「加载中…」文本已入帧后同步执行；若首帧即完成，同一帧后续 Tree 构建读到新数据，重绘帧显示计数。）

- [ ] **Step 5: 侧栏接线（`ui/sidebar.go` 修改）**

`connRow` 在名称按钮（`lbl := ui.Button(c, p.Name)` 及其点击处理）之后、`ui.Spacer(c)` 之前插入展开切换按钮：

```go
		expandLabel := "▸"
		if a.treeExpand[p.ID] {
			expandLabel = "▾"
		}
		if ui.Button(c, expandLabel).Clicked() {
			if a.treeExpand == nil {
				a.treeExpand = map[string]bool{}
			}
			a.treeExpand[p.ID] = !a.treeExpand[p.ID]
			if a.treeExpand[p.ID] {
				a.expandConn(p)
			}
		}
```

`connRow` 函数末尾（`})` 之后、函数结束前）插入：

```go
	if a.treeExpand[p.ID] {
		a.objectGroups(c, p)
	}
```

- [ ] **Step 6: 运行确认通过 + 回归 + vet**

```bash
$GO test ./ui/ -run 'TestObjectTree|TestReloadTree|TestExpandInactive|TestSidebar|TestOpenConn|TestRemoveActive' -count=1 -race -v
$GO test ./... -skip TestSpikeMatrix -count=1
$GO tool mygo vet .
```

Expected: 全部 PASS；全套 ok；vet 0。

- [ ] **Step 7: Commit**

```bash
git add ui/
git commit -m "task5: 对象树（分组懒加载/缓存/失败红字/展开自动连接）"
```

---

### Task 6: 收尾（全量守门 / Readme / 手动验收）

**Files:**
- Modify: `Readme.md`
- Test: 全量 `$GO test ./...`（含 spike）

**Interfaces:**
- Consumes: Task 1–5 全部产物。
- Produces: M2 验收记录（自动化全绿 + 手动清单）；Readme 勾选 M2。

- [ ] **Step 1: 全量性能守门**

```bash
$GO test ./ui/ -run 'TestSpikeMatrix|TestResultGridFrameBudget' -count=1 -v
```

Expected: 两项 PASS；spike 9 行日志无 `exceeds limit`；网格 p50≤16ms。若超限：对照 Task 3 后基线排查侧栏/树渲染是否引入每帧开销（如 `store.List()` 每帧拷贝可接受；`loadGroups` 缓存命中必须零查询），修复后重测。

- [ ] **Step 2: 全量测试 + vet**

```bash
$GO test ./... -count=1
$GO tool mygo vet .
```

Expected: `ok` 全部包（engine/app/driver/sqlite/highlight/ui）；vet 0。

- [ ] **Step 3: 更新 Readme**

`Readme.md` 功能清单中 `- [ ] M2：连接管理 + 对象树` 替换为：

```markdown
- [x] M2：主页面布局对齐 db-k 骨架（侧栏/顶栏/单行工具栏）；连接管理
      （JSON 持久化/新建/编辑/删除/状态点）；SQLite 对象树（懒加载）
```

- [ ] **Step 4: M2 验收清单核对**

自动化（已在前两步覆盖）：
1. `app` store 单测全绿（round-trip/损坏恢复/校验）；
2. `driver/sqlite` TestObjects 全绿（表/视图/索引枚举 + `sqlite_%` 过滤）；
3. `ui` 侧栏/连接/树 headless 全绿（Modal 新建、校验、openConn 端到端、失败保持、删除清会话、树叶子、失败红字、刷新清缓存）；
4. spike/网格/vet 无回退。

手动清单（`$GO run .`，需用户验收）：
1. 布局：左侧 280dp 侧栏（可拖宽）、顶栏「● 状态 + 执行」、单行工具栏（HUD+尺寸+策略+查找）、编辑器/结果分屏；
2. 「新建连接」→ Modal 填名称+路径（或浏览…）→ 保存 → 侧栏出现连接行，重启后仍在；
3. 双击连接 → 状态点变绿 + 名称显示；展开（▸）→ 显示「表/视图/索引 (n)」，再展开「表」→ 表名叶子；
4. 连接含表库后「执行」SELECT 正常；非法 SQL 错误横幅；「停止」可中断慢查询；
5. 「编辑」「删除」（删除活动连接 → 状态点回灰）；「刷新」清空并可重新展开；
6. 沙箱验证持久化：`~/.config/db-g/connections.json` 生成且内容正确。

- [ ] **Step 5: Commit**

```bash
git add Readme.md
git commit -m "task6: M2 收尾（Readme 勾选 + 验收清单）"
```

---

## 计划级注意事项（给执行者）

- **不要**在 M2 引入：多控制台标签、连接搜索/右键/文件夹/拖拽/导入导出、双击表预览、密码字段、非 sqlite 方言、侧栏显隐快捷键、分栏宽度持久化——分别是 M3+ 的内容。
- spec §4.3「未连接节点展开即查询」在单会话模型下落地为：**展开未连接 = 先自动 openConn 切换会话再查**（计划 `expandConn`）；这是有意的行为映射，不是遗漏。
- `ui.Tester` 无双击模拟：连接打开路径用 `openConn` 直调测试 + 手动验收覆盖双击；不要用脆弱的 Press/Release 时序模拟。
- 每帧开销红线：`loadGroups` 缓存命中零查询；`store.List()` 每帧一次浅拷贝可接受；禁止在渲染路径做 JSON IO 或文件 Stat。
- `~/.config` 污染红线：所有 UI 测试必须 `a.store = app.LoadStore(filepath.Join(t.TempDir(), ...))`；`newRoot`（测试入口）不加载真实配置，仅 `NewRoot()`（交互入口）加载。
- 环境意外处理：若 `go get`/`go mod` 动作把 mygo 升离 v0.4.8，立即 `go get github.com/egoist/mygo@v0.4.8` 钉回；`go.mod` 出现新依赖需在 commit message 说明。

## Self-Review（计划作者执行）

1. **Spec 覆盖**：§2 布局（Task 3）；§3 store 全方法与持久化（Task 2）+ Modal/双击/删除（Task 4）；§4 ObjectLister+sqlite 实现（Task 1）+ 树 UI 懒加载/缓存/降级（Task 5）；§5 错误表（Task 2 损坏、Task 4 校验/失败保持/删除、Task 5 失败红字）；§6 测试四层（各任务+Task 6）与验收清单（Task 6）。无缺口。
2. **占位符**：无 TBD/TODO/「类似 Task N」；每个代码步骤给完整代码。
3. **类型一致性**：`ConnProfile/ConnStore` 方法集在 Task 2 定义、Task 4/5 调用一致；`ObjectLister.Objects` 签名 Task 1 定义、Task 5 `loadGroups` 调用一致；`openConn/removeConn/reloadTree/groupOpenPtr/kindKey` 定义与测试引用一致；`connName/connErr` 全局更名一致（run.go/run_test/rootView/sidebar）。





