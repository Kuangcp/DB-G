# SQL 控制台 + 工作区 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 db-g 的 SQL 编辑器正文变成「数据源 → 多个命名控制台 → 各自一份本地 .sql 文件」，控制台可按工作区分组、可关闭并从数据源重新打开，元数据与光标持久化、启动恢复。

**Architecture:** 持久化仍走 `app.ConnStore`（单个原子写的 JSON）；正文文件由 `app.ConsoleFiles` 管；`app.ConsoleManager` 编排工作区成员、激活、脏标记、防抖落盘与光标；`ui` 只做渲染与事件。执行会话从 `app.Console` 上提到 `root`，每个控制台各持一份运行态（结果）。

**Tech Stack:** Go 1.27.3、mygo（自绘 GUI：`ui.MenuButton`/`ui.Modal`/`TextBuffer`/`ScrollHorizontal`/`ui.Tester` headless 测试）、纯标准库 JSON 持久化。

**Spec:** `docs/superpowers/specs/2026-11-04-consoles-design.md`

## Global Constraints

- Go 版本下限 `1.27.3`（`go.mod` 已就位），模块名 `db-g`；**不引入任何新依赖**，不引入 app.db/SQLite 存储。
- 本机 cgo 链接有环境问题（`/bin/ld: error in .../crt1.o(.sframe)`，与本改动无关）。所有构建/测试命令统一加 `CGO_ENABLED=0`：
  `CGO_ENABLED=0 go test ./...`、`CGO_ENABLED=0 go build ./...`、`CGO_ENABLED=0 go vet ./...`。
- 元数据只写 `connections.json`（临时文件 + rename 原子替换；目录 `0700`、文件 `0600`）；正文只写 `<storeDir>/consoles/<consoleID>.sql`。
- 控制台/工作区 id = 现有 `genID()`（4 字节 hex）；正文文件名 `<id>.sql`，重命名不换文件。
- 光标/选区一律 **rune 偏移**（与 `ui.TextSelection()`/`SetTextSelection()` 同单位）。
- UI 文案用中文，风格与现有代码一致（`请先连接数据源`、`执行中…`）。
- 提交信息前缀沿用仓库风格：`taskN: <中文描述>`。
- 每个任务收尾必须绿：`CGO_ENABLED=0 go test ./app ./ui` 与 `CGO_ENABLED=0 go vet ./...`；涉及编辑器/结果网格的任务额外跑 `CGO_ENABLED=0 go test ./ui -run 'Spike|Bench' -bench . -benchtime 1x`。

## Review Focus

规范文档描述的是「应能做什么」，下面是它没写、但用户一定会撞上的五类情况，按可能性排序。每一条都必须在括号所指任务的测试里被钉住：

1. **老配置/空配置启动**：`connections.json` 没有 `consoles`/`workspaces` 字段，或文件不存在 → 必须正常启动进空态，且首次操作后新字段写回、老连接不丢。（Task 1、Task 9）
2. **磁盘 `.sql` 缺失/被外部改短**：正文读成空串照常打开，首次编辑重建文件；持久化光标超出新正文长度 → 选区被夹紧，不 panic。（Task 3、Task 4、Task 7）
3. **写盘失败**（只读目录、磁盘满）：编辑内容不丢（脏标记保留）、错误可见、修复后重试成功。（Task 4）
4. **删除连接/工作区时它正被激活**：不得残留悬空 id，激活态必须迁移到合法控制台或空态。（Task 2、Task 5、Task 8）
5. **成员/末次激活指针指向不存在的记录**（手工改 JSON、旧备份覆盖）：读取时跳过，激活回落到合法成员或空态。（Task 2、Task 5、Task 9）

---

### Task 1: 控制台元数据持久化（store）

**Files:**
- Modify: `app/conn.go`（`ConnStore` 结构 28–35；`LoadStore` 46–71；`save` 100–118）
- Test: `app/conn_test.go`（追加；复用 `tempStore(t)`）

**Interfaces:**
- Consumes: 现有 `genID()`、`save()` 的临时文件 + rename 模式、`tempStore(t)` 测试助手。
- Produces:
  ```go
  type ConsoleRecord struct {
      ID           string `json:"id"`
      ConnectionID string `json:"connectionID"`
      Name         string `json:"name"`
      FilePath     string `json:"filePath"`
      SortOrder    int    `json:"sortOrder"`
      CreatedAt    int64  `json:"createdAt"`
      UpdatedAt    int64  `json:"updatedAt"`
      CaretStart   int    `json:"caretStart"`
      CaretEnd     int    `json:"caretEnd"`
  }
  func (s *ConnStore) ConsolesDir() string                       // filepath.Join(filepath.Dir(s.path), "consoles")
  func (s *ConnStore) ListConsoles(connID string) []ConsoleRecord // SortOrder, CreatedAt 升序
  func (s *ConnStore) GetConsole(id string) (ConsoleRecord, bool)
  func (s *ConnStore) CreateConsole(connID, name string) (ConsoleRecord, error)
  func (s *ConnStore) RenameConsole(id, name string) error
  func (s *ConnStore) DeleteConsole(id string) error
  func (s *ConnStore) SetConsoleCaret(id string, start, end int)
  func (s *ConnStore) TouchConsole(id string, at int64)
  ```

- [ ] **Step 1: Write the failing tests**

在 `app/conn_test.go` 追加（`tempStore` 已存在）：

```go
func TestConsolesRoundTrip(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	rec, err := s.CreateConsole(p.ID, "  查询  ")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "查询" || rec.ConnectionID != p.ID || rec.FilePath != filepath.Join(s.ConsolesDir(), rec.ID+".sql") {
		t.Fatalf("bad record: %+v", rec)
	}
	again := LoadStore(s.path)
	got, ok := again.GetConsole(rec.ID)
	if !ok || got.Name != "查询" || got.SortOrder != rec.SortOrder {
		t.Fatalf("round trip: %+v %v", got, ok)
	}
}

func TestCreateConsoleDefaults(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	first, err := s.CreateConsole(p.ID, "")
	if err != nil || first.Name != "控制台 1" {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := s.CreateConsole(p.ID, "  ")
	if err != nil || second.Name != "控制台 2" || second.SortOrder != first.SortOrder+1 {
		t.Fatalf("second: %+v %v", second, err)
	}
	if _, err := s.CreateConsole("nope", "x"); err == nil {
		t.Fatal("unknown connection must fail")
	}
}

func TestConsolesLegacyJSONWithoutField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	legacy := `{"conns":[{"id":"abc","name":"旧","kind":"sqlite","path":"/tmp/x.db"}],"activeID":"abc"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(path)
	if len(s.List()) != 1 || len(s.ListConsoles("abc")) != 0 {
		t.Fatalf("legacy load: %+v", s.List())
	}
	if _, err := s.CreateConsole("abc", "新"); err != nil {
		t.Fatal(err)
	}
	re := LoadStore(path)
	if len(re.List()) != 1 || len(re.ListConsoles("abc")) != 1 {
		t.Fatalf("legacy rewrite lost data: conns=%d consoles=%d", len(re.List()), len(re.ListConsoles("abc")))
	}
}

func TestConsoleCaretAndDelete(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	rec, _ := s.CreateConsole(p.ID, "c")
	s.SetConsoleCaret(rec.ID, 3, 7)
	s.TouchConsole(rec.ID, 1234)
	got, _ := LoadStore(s.path).GetConsole(rec.ID)
	if got.CaretStart != 3 || got.CaretEnd != 7 || got.UpdatedAt != 1234 {
		t.Fatalf("caret/updated: %+v", got)
	}
	if err := s.DeleteConsole(rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadStore(s.path).GetConsole(rec.ID); ok {
		t.Fatal("console must be deleted")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./app -run 'Console' -v`
Expected: 编译失败 `undefined: ConsoleRecord` / `s.CreateConsole undefined`.

- [ ] **Step 3: Implement store support**

`app/conn.go` 要点（其余按现有风格补齐）：

```go
type ConnStore struct {
	mu       sync.Mutex
	path     string
	conns    []ConnProfile
	activeID string
	consoles []ConsoleRecord // 新增
	warning  string
}
```

- `LoadStore`/`save` 的文件结构统一为私有的 `storeFile`（含 `conns`/`activeID`/`consoles`，Task 2 再加 `workspaces`/`activeWorkspaceID`），两处共用，避免以后两处漂移。
- `ConsolesDir()` = `filepath.Join(filepath.Dir(s.path), "consoles")`。
- `CreateConsole`：校验连接存在；`strings.TrimSpace(name)`，空则 `fmt.Sprintf("控制台 %d", 该连接现有数+1)`；`genID`；`FilePath = filepath.Join(s.ConsolesDir(), id+".sql")`；`SortOrder` = 该连接现有最大值+1；`CreatedAt/UpdatedAt = time.Now().UnixMilli()`；追加后 `save()`，失败回滚切片。
- `DeleteConsole`：删除行；`save()` 失败回滚。
- `SetConsoleCaret`/`TouchConsole`：改内存后 `save()`；失败回滚并 `s.warning = "保存控制台失败: " + err.Error()`（不 return error，编辑不中断）。
- 加 `"strings"`、`"time"` import。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./app -run 'Console|Legacy' -v` 然后 `CGO_ENABLED=0 go test ./app`
Expected: PASS（含既有 store 测试）。

- [ ] **Step 5: Commit**

```bash
git add app/conn.go app/conn_test.go
git commit -m "task1: 控制台元数据持久化（store CRUD/光标/兼容老配置）"
```

---

### Task 2: 工作区 + 成员关系 + 删除级联

**Files:**
- Modify: `app/conn.go`（`ConnStore` 28–35、`LoadStore`、`save`、`DeleteConsole`、`Remove` 202–222）
- Test: `app/conn_test.go`（追加）

**Interfaces:**
- Consumes: Task 1 的 `ConsoleRecord`、`ConsolesDir()`、`save()`/`storeFile`。
- Produces:
  ```go
  type WorkspaceRecord struct {
      ID                  string   `json:"id"`
      Name                string   `json:"name"`
      AutoNamed           bool     `json:"autoNamed"`
      SortOrder           int      `json:"sortOrder"`
      CreatedAt           int64    `json:"createdAt"`
      LastActiveConsoleID string   `json:"lastActiveConsoleID"`
      Members             []string `json:"members"` // 有序列 consoleID = 标签顺序
  }
  func (s *ConnStore) ListWorkspaces() []WorkspaceRecord
  func (s *ConnStore) GetWorkspace(id string) (WorkspaceRecord, bool)
  func (s *ConnStore) CreateWorkspace(name string) (WorkspaceRecord, error) // 空名 → "默认" 且 AutoNamed=true
  func (s *ConnStore) RenameWorkspace(id, name string) error                // 空名 → "默认"
  func (s *ConnStore) DeleteWorkspace(id string) error
  func (s *ConnStore) AddWorkspaceMember(wsID, consoleID string) error      // 幂等
  func (s *ConnStore) RemoveWorkspaceMember(wsID, consoleID string) error
  func (s *ConnStore) SetWorkspaceLastActive(wsID, consoleID string) error
  func (s *ConnStore) ActiveWorkspaceID() string
  func (s *ConnStore) SetActiveWorkspace(id string) // 空串清空；落盘失败回滚 + warning
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestWorkspaceCRUDRoundTrip(t *testing.T) {
	s := tempStore(t)
	ws, err := s.CreateWorkspace("")
	if err != nil || ws.Name != "默认" || !ws.AutoNamed {
		t.Fatalf("default ws: %+v %v", ws, err)
	}
	named, err := s.CreateWorkspace("分析")
	if err != nil || named.AutoNamed || named.SortOrder != ws.SortOrder+1 {
		t.Fatalf("named ws: %+v %v", named, err)
	}
	if err := s.RenameWorkspace(ws.ID, "日常"); err != nil {
		t.Fatal(err)
	}
	s.SetActiveWorkspace(named.ID)
	again := LoadStore(s.path)
	if got, _ := again.GetWorkspace(ws.ID); got.Name != "日常" {
		t.Fatalf("rename lost: %+v", got)
	}
	if again.ActiveWorkspaceID() != named.ID {
		t.Fatalf("active ws: %q", again.ActiveWorkspaceID())
	}
	if err := s.DeleteWorkspace(named.ID); err != nil {
		t.Fatal(err)
	}
	if LoadStore(s.path).ActiveWorkspaceID() != "" {
		t.Fatal("deleting active workspace must clear pointer")
	}
}

func TestWorkspaceMembersAndLastActive(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	ws, _ := s.CreateWorkspace("默认")
	a, _ := s.CreateConsole(p.ID, "a")
	b, _ := s.CreateConsole(p.ID, "b")
	if err := s.AddWorkspaceMember(ws.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddWorkspaceMember(ws.ID, a.ID); err != nil {
		t.Fatal("add must be idempotent:", err)
	}
	_ = s.AddWorkspaceMember(ws.ID, b.ID)
	if err := s.SetWorkspaceLastActive(ws.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadStore(s.path).GetWorkspace(ws.ID)
	if len(got.Members) != 2 || got.Members[0] != a.ID || got.LastActiveConsoleID != b.ID {
		t.Fatalf("members: %+v", got)
	}
	_ = s.RemoveWorkspaceMember(ws.ID, a.ID)
	got, _ = LoadStore(s.path).GetWorkspace(ws.ID)
	if len(got.Members) != 1 || got.Members[0] != b.ID {
		t.Fatalf("remove: %+v", got.Members)
	}
}

func TestDeleteConsoleStripsMembership(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	ws, _ := s.CreateWorkspace("默认")
	rec, _ := s.CreateConsole(p.ID, "c")
	_ = s.AddWorkspaceMember(ws.ID, rec.ID)
	_ = s.SetWorkspaceLastActive(ws.ID, rec.ID)
	if err := s.DeleteConsole(rec.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadStore(s.path).GetWorkspace(ws.ID)
	if len(got.Members) != 0 || got.LastActiveConsoleID != "" {
		t.Fatalf("membership not stripped: %+v", got)
	}
}

func TestDeleteWorkspaceKeepsConsoles(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	ws, _ := s.CreateWorkspace("默认")
	rec, _ := s.CreateConsole(p.ID, "c")
	_ = s.AddWorkspaceMember(ws.ID, rec.ID)
	if err := s.DeleteWorkspace(ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadStore(s.path).GetConsole(rec.ID); !ok {
		t.Fatal("deleting a workspace must keep consoles")
	}
}

func TestRemoveConnCascadesConsoles(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	ws, _ := s.CreateWorkspace("默认")
	rec, _ := s.CreateConsole(p.ID, "c")
	_ = s.AddWorkspaceMember(ws.ID, rec.ID)
	if err := s.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.ListConsoles(p.ID)) != 0 {
		t.Fatal("consoles must be removed with the connection")
	}
	got, _ := LoadStore(s.path).GetWorkspace(ws.ID)
	if len(got.Members) != 0 {
		t.Fatalf("members must be stripped: %+v", got.Members)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./app -run 'Workspace|Cascade|StripsMembership' -v`
Expected: 编译失败 `undefined: WorkspaceRecord`。

- [ ] **Step 3: Implement**

- `WorkspaceRecord` 加入 `conn.go`；`storeFile` 增 `Workspaces []WorkspaceRecord `json:"workspaces"``、`ActiveWorkspaceID string `json:"activeWorkspaceID"``。
- 排序：`SortOrder`，再 `CreatedAt`；`Members` 追加到末尾（幂等：已存在直接 return nil）。
- `DeleteWorkspace`：删行；若 `activeWorkspaceID == id` 清空；save 失败整体回滚。
- `DeleteConsole`（Task 1 的实现）追加：移除所有工作区 `Members` 中的该 id、清 `LastActiveConsoleID == id`。
- `Remove(connID)` 追加级联：删该连接的全部控制台行、从所有工作区移除这些成员、清对应 `LastActiveConsoleID`；save 失败回滚全部三个切片。
- 为多点回滚加私有 `type storeState struct{ conns []ConnProfile; consoles []ConsoleRecord; workspaces []WorkspaceRecord; activeID, activeWorkspaceID string }` + `snapshot()`/`restore(st)`（切片用 `slices.Clone`），现有 `Add/Update/Remove` 也改用它，避免手写回滚出错。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./app -v`
Expected: PASS（含所有既有 store 测试）。

- [ ] **Step 5: Commit**

```bash
git add app/conn.go app/conn_test.go
git commit -m "task2: 工作区与成员关系持久化 + 删除级联"
```

---

### Task 3: 正文文件层（ConsoleFiles + TextSink）

**Files:**
- Create: `app/consolefiles.go`
- Test: `app/consolefiles_test.go`

**Interfaces:**
- Consumes: 无（只用标准库）。
- Produces:
  ```go
  type ConsoleFiles struct{ Dir string }
  func (f ConsoleFiles) Path(id string) string
  func (f ConsoleFiles) Read(id string) (string, error) // 不存在 → "", nil
  func (f ConsoleFiles) Write(id, text string) error    // MkdirAll 0700 + 临时文件 + rename，失败回落普通写
  func (f ConsoleFiles) Delete(id string) error         // 不存在不算错
  func (f ConsoleFiles) Exists(id string) bool

  type TextSink interface {
      Read(id string) (string, error)
      Write(id, text string) error
      Delete(id string) error
      Exists(id string) bool
  }
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestConsoleFilesReadMissing(t *testing.T) {
	f := ConsoleFiles{Dir: t.TempDir()}
	text, err := f.Read("deadbeef")
	if err != nil || text != "" {
		t.Fatalf("missing read: %q %v", text, err)
	}
	if f.Exists("deadbeef") {
		t.Fatal("must not exist")
	}
}

func TestConsoleFilesWriteReplacesAtomically(t *testing.T) {
	f := ConsoleFiles{Dir: filepath.Join(t.TempDir(), "consoles")}
	if err := f.Write("c1", "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if err := f.Write("c1", "SELECT 2"); err != nil {
		t.Fatal(err)
	}
	text, err := f.Read("c1")
	if err != nil || text != "SELECT 2" {
		t.Fatalf("read: %q %v", text, err)
	}
	if !f.Exists("c1") {
		t.Fatal("must exist")
	}
	if err := f.Delete("c1"); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete("c1"); err != nil {
		t.Fatal("delete must be idempotent:", err)
	}
	if f.Exists("c1") {
		t.Fatal("must be gone")
	}
}

func TestConsoleFilesPath(t *testing.T) {
	f := ConsoleFiles{Dir: "/tmp/x"}
	if got := f.Path("abc"); got != "/tmp/x/abc.sql" {
		t.Fatalf("path: %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./app -run 'ConsoleFiles' -v`
Expected: 编译失败 `undefined: ConsoleFiles`。

- [ ] **Step 3: Implement `app/consolefiles.go`**

- `Read`：`os.ReadFile`；`errors.Is(err, fs.ErrNotExist)` → `""`, nil。
- `Write`：`os.MkdirAll(dir, 0o700)`；`os.CreateTemp(dir, "."+id+".*.tmp")`（失败直接普通写）；写内容 → `os.Rename`；rename 失败删临时文件再普通 `os.WriteFile(path, data, 0o600)`（跨设备场景）。
- `Delete`：`os.Remove`，`fs.ErrNotExist` 忽略。
- `Exists`：`os.Stat` + `!IsDir`。
- 文件权限 `0o600`（`os.WriteFile` 对已存在文件不改权限，可接受）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./app -run 'ConsoleFiles' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add app/consolefiles.go app/consolefiles_test.go
git commit -m "task3: 控制台正文文件层（原子写/缺文件读空/删除幂等）"
```

---

### Task 4: ConsoleManager —— 正文、脏标记、防抖、光标

**Files:**
- Create: `app/consolemgr.go`
- Test: `app/consolemgr_test.go`

**Interfaces:**
- Consumes: Task 1/2 的 `ConnStore` 控制台 API；Task 3 的 `TextSink`。
- Produces:
  ```go
  type ConsoleManager struct {
      SaveDelay  time.Duration // 默认 700ms；测试置 10ms
      CaretDelay time.Duration // 默认 1500ms；测试置 10ms
      // 私有：store, sink, dirty, lastSaved, cache, saveTimers, caretTimers
  }
  func NewConsoleManager(store *ConnStore, sink TextSink) *ConsoleManager
  func (m *ConsoleManager) Text(id string) (string, bool)  // 首次读盘并缓存；未知 id → "", false
  func (m *ConsoleManager) SetText(id, text string)        // 内容不变 → no-op；置脏 + 重置防抖
  func (m *ConsoleManager) Dirty(id string) bool
  func (m *ConsoleManager) Flush(id string) error          // 立即写盘；成功清脏 + TouchConsole
  func (m *ConsoleManager) FlushAll()
  func (m *ConsoleManager) Caret(id string) (int, int)
  func (m *ConsoleManager) SetCaret(id string, start, end int) // 内存即时 + 防抖落库
  ```

- [ ] **Step 1: Write the failing tests**

```go
type fakeSink struct {
	mu     sync.Mutex
	files  map[string]string
	fail   error
	writes int
}

func newFakeSink() *fakeSink { return &fakeSink{files: map[string]string{}} }

func (f *fakeSink) Read(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[id], nil
}
func (f *fakeSink) Write(id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.files[id] = text
	f.writes++
	return nil
}
func (f *fakeSink) Delete(id string) error { f.mu.Lock(); defer f.mu.Unlock(); delete(f.files, id); return nil }
func (f *fakeSink) Exists(id string) bool  { f.mu.Lock(); defer f.mu.Unlock(); _, ok := f.files[id]; return ok }

// text 是并发安全的读值（防抖定时器在别的 goroutine 写）。
func (f *fakeSink) text(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[id]
}

func newMgr(t *testing.T, sink TextSink) (*ConnStore, *ConnProfile, *ConsoleRecord, *ConsoleManager) {
	t.Helper()
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	rec, err := s.CreateConsole(p.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	m := NewConsoleManager(s, sink)
	m.SaveDelay, m.CaretDelay = 10*time.Millisecond, 10*time.Millisecond
	return s, &p, &rec, m
}

func TestSetTextDebouncesWrite(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	if text, ok := m.Text(rec.ID); !ok || text != "" {
		t.Fatalf("text: %q %v", text, ok)
	}
	m.SetText(rec.ID, "SELECT 1")
	m.SetText(rec.ID, "SELECT 1;")
	if !m.Dirty(rec.ID) {
		t.Fatal("must be dirty right after edit")
	}
	waitFor(t, 2*time.Second, func() bool { return !m.Dirty(rec.ID) })
	if got := sink.text(rec.ID); got != "SELECT 1;" {
		t.Fatalf("written: %q", got)
	}
}

func TestSetTextUnchangedIsNoop(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	m.SetText(rec.ID, "")
	if m.Dirty(rec.ID) {
		t.Fatal("same text must not mark dirty")
	}
}

func TestFlushWritesImmediately(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	m.SetText(rec.ID, "SELECT 42")
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	if got := sink.text(rec.ID); got != "SELECT 42" || m.Dirty(rec.ID) {
		t.Fatalf("flush: %q dirty=%v", got, m.Dirty(rec.ID))
	}
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal("second flush must be a no-op:", err)
	}
}

func TestFlushFailureKeepsDirty(t *testing.T) {
	sink := newFakeSink()
	sink.fail = errors.New("disk full")
	_, _, rec, m := newMgr(t, sink)
	m.SetText(rec.ID, "SELECT 7")
	if err := m.Flush(rec.ID); err == nil {
		t.Fatal("must report write failure")
	}
	if !m.Dirty(rec.ID) {
		t.Fatal("failed write must keep content dirty")
	}
	sink.fail = nil
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	if got := sink.text(rec.ID); got != "SELECT 7" || m.Dirty(rec.ID) {
		t.Fatalf("retry: %q dirty=%v", got, m.Dirty(rec.ID))
	}
}

func TestCaretPersistsAcrossManagers(t *testing.T) {
	s, _, rec, m := newMgr(t, newFakeSink())
	m.SetCaret(rec.ID, 4, 9)
	if start, end := m.Caret(rec.ID); start != 4 || end != 9 {
		t.Fatalf("immediate caret: %d %d", start, end)
	}
	waitFor(t, 2*time.Second, func() bool {
		got, _ := s.GetConsole(rec.ID)
		return got.CaretStart == 4 && got.CaretEnd == 9
	})
	reloaded := NewConsoleManager(LoadStore(s.path), newFakeSink())
	if start, end := reloaded.Caret(rec.ID); start != 4 || end != 9 {
		t.Fatalf("persisted caret: %d %d", start, end)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./app -run 'SetText|Flush|CaretPersists|Unchanged' -v`
Expected: 编译失败 `undefined: ConsoleManager`。

- [ ] **Step 3: Implement `app/consolemgr.go`**

- `Text`：`cache` 命中即返回；否则 `sink.Read`（错误 → `""` 并记 `m.err`？本任务先直接返回 `""`，错误由 `Flush` 路径暴露）；写回缓存。
- `SetText`：`cache[id] == text` → return；`cache[id] = text`；`dirty[id] = text != lastSaved[id]`（`lastSaved` 缺失 = 空串）；重置该 id 的 `time.AfterFunc(SaveDelay, func(){ _ = m.Flush(id) })`；每次重置前 `Stop()` 旧 timer。
- `Flush`：`!dirty[id]` → `nil`；`sink.Write`；成功 → `delete(dirty, id)`、`lastSaved[id]=text`、`store.TouchConsole(id, time.Now().UnixMilli())`、`Stop` 挂起 timer；失败 → 返回错误并保留脏。
- `FlushAll`：对 `dirty` 里所有 id 调 `Flush`（测试/退出用）。
- `Caret`/`SetCaret`：内存 `caretDraft map[string][2]int` 优先，回落 `store.GetConsole(...).CaretStart/End`；`SetCaret` 只在与内存/落库值不同的时候重置防抖 timer → 到点 `store.SetConsoleCaret`。
- 并发：`ConnStore` 自己有锁；`ConsoleManager` 内部状态用 `sync.Mutex` 保护（UI 主线程 + timer 协程），timer 回调里取锁后调 sink/store（不要持锁做 IO：先把文本取出，再释放锁写盘）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./app -run 'SetText|Flush|Caret|Unchanged' -v` 然后 `CGO_ENABLED=0 go test ./app`
Expected: PASS。（防抖测试用 10ms + `waitFor`，不要 sleep 死等。）

- [ ] **Step 5: Commit**

```bash
git add app/consolemgr.go app/consolemgr_test.go
git commit -m "task4: ConsoleManager 正文/脏标记/防抖落盘/光标"
```

---

### Task 5: ConsoleManager —— 工作区编排与开关控制台

**Files:**
- Modify: `app/consolemgr.go`
- Test: `app/consolemgr_test.go`（追加）

**Interfaces:**
- Consumes: Task 2 工作区 store API；Task 4 的 manager 内部（`cache`/`dirty`/`Flush`/`SetCaret`）。
- Produces:
  ```go
  func (m *ConsoleManager) Workspaces() []WorkspaceRecord
  func (m *ConsoleManager) ActiveWorkspace() (WorkspaceRecord, bool)
  func (m *ConsoleManager) EnsureWorkspace() (WorkspaceRecord, error)
  func (m *ConsoleManager) CreateWorkspace(name string) (WorkspaceRecord, error) // 创建并激活
  func (m *ConsoleManager) SetActiveWorkspace(id string) (WorkspaceRecord, bool) // 恢复其 LastActiveConsoleID
  func (m *ConsoleManager) RenameWorkspace(id, name string) error
  func (m *ConsoleManager) DeleteWorkspace(id string) error                      // 激活态迁移
  func (m *ConsoleManager) ConsolesOf(connID string) []ConsoleRecord
  func (m *ConsoleManager) Open() []ConsoleRecord   // 当前工作区成员（成员顺序）
  func (m *ConsoleManager) IsOpen(id string) bool
  func (m *ConsoleManager) Active() (ConsoleRecord, bool)
  func (m *ConsoleManager) Create(connID, name string) (ConsoleRecord, error)    // 建行+空文件+入组+激活
  func (m *ConsoleManager) OpenConsole(id string) (ConsoleRecord, bool)          // 入组（末尾）+激活+清缓存
  func (m *ConsoleManager) CloseConsole(id string) error                         // 仅移出当前工作区
  func (m *ConsoleManager) DeleteConsole(id string) error                        // 行+文件+所有工作区成员
  func (m *ConsoleManager) Rename(id, name string) error
  func (m *ConsoleManager) OnConnectionDeleted(connID string)
  ```
  激活语义（所有方法共用）：`Active()` 返回管理器内存里的 `activeConsoleID` 对应记录；每次激活都 `store.SetWorkspaceLastActive(activeWS, id)`。

- [ ] **Step 1: Write the failing tests**

```go
func TestCreateAddsMemberAndActivates(t *testing.T) {
	sink := newFakeSink()
	s, p, _, m := newMgr(t, sink)
	rec, err := m.Create(p.ID, "第一条")
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsOpen(rec.ID) {
		t.Fatal("created console must be a member of the active workspace")
	}
	if got, ok := m.Active(); !ok || got.ID != rec.ID {
		t.Fatalf("active: %+v %v", got, ok)
	}
	if !sink.Exists(rec.ID) {
		t.Fatal("empty .sql file must be created")
	}
	ws, _ := m.ActiveWorkspace()
	if ws.LastActiveConsoleID != rec.ID {
		t.Fatalf("last active: %+v", ws)
	}
	_ = s
}

func TestCloseKeepsFileAndSwitchesActive(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	a, _ := m.Create(p.ID, "a")
	b, _ := m.Create(p.ID, "b")
	m.SetText(b.ID, "SELECT 2")
	if err := m.CloseConsole(b.ID); err != nil {
		t.Fatal(err)
	}
	if m.IsOpen(b.ID) {
		t.Fatal("closed console must leave the workspace")
	}
	if !sink.Exists(b.ID) {
		t.Fatal("closing must keep the .sql file")
	}
	if got, ok := m.Active(); !ok || got.ID != a.ID {
		t.Fatalf("active after close: %+v %v", got, ok)
	}
	sink.files[b.ID] = "SELECT 99"
	if _, ok := m.OpenConsole(b.ID); !ok {
		t.Fatal("reopen must succeed")
	}
	if text, _ := m.Text(b.ID); text != "SELECT 99" {
		t.Fatalf("reopen must re-read disk: %q", text)
	}
}

func TestOpenConsoleAppendsMember(t *testing.T) {
	_, p, _, m := newMgr(t, newFakeSink())
	a, _ := m.Create(p.ID, "a")
	b, err := m.Store().CreateConsole(p.ID, "b")
	_ = err
	_ = m.CloseConsole(a.ID)
	if _, ok := m.OpenConsole(b.ID); !ok {
		t.Fatal("open unknown-to-workspace console")
	}
	open := m.Open()
	if len(open) != 1 || open[0].ID != b.ID {
		t.Fatalf("open order: %+v", open)
	}
}

func TestOpenSkipsDanglingMemberIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	raw := `{"consoles":[{"id":"c1","connectionID":"p1","name":"a"}],
		"workspaces":[{"id":"w1","name":"默认","members":["c1","deadbeef"]}],
		"activeWorkspaceID":"w1"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewConsoleManager(LoadStore(path), newFakeSink())
	open := m.Open()
	if len(open) != 1 || open[0].ID != "c1" {
		t.Fatalf("dangling member must be skipped: %+v", open)
	}
}

func TestDeleteWorkspaceKeepsConsolesReopenable(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	rec, _ := m.Create(p.ID, "c")
	ws, _ := m.ActiveWorkspace()
	if err := m.DeleteWorkspace(ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.ActiveWorkspace(); ok {
		t.Fatal("no workspace must remain")
	}
	if len(m.Open()) != 0 {
		t.Fatal("open list must be empty without a workspace")
	}
	if !sink.Exists(rec.ID) {
		t.Fatal("file must survive workspace deletion")
	}
	if _, ok := m.Active(); ok {
		t.Fatal("no active console without a workspace")
	}
}

func TestSetActiveWorkspaceRestoresLastActive(t *testing.T) {
	_, p, _, m := newMgr(t, newFakeSink())
	a, _ := m.Create(p.ID, "a")
	first, _ := m.ActiveWorkspace()
	b, _ := m.Create(p.ID, "b")
	second, _ := m.ActiveWorkspace()
	_ = second
	m.SetActiveWorkspace(first.ID)
	if got, _ := m.Active(); got.ID != a.ID {
		t.Fatalf("restore first ws console: %+v", got)
	}
	// 新建控制台会自动进入当前工作区；两个工作区互不影响
	_ = b
}

func TestEnsureWorkspaceCreatesDefaultOnce(t *testing.T) {
	_, _, _, m := newMgr(t, newFakeSink())
	ws1, err := m.EnsureWorkspace()
	if err != nil || ws1.Name != "默认" {
		t.Fatalf("ensure: %+v %v", ws1, err)
	}
	ws2, err := m.EnsureWorkspace()
	if err != nil || ws2.ID != ws1.ID {
		t.Fatalf("ensure must be idempotent: %+v %v", ws2, err)
	}
	if n := len(m.Workspaces()); n != 1 {
		t.Fatalf("workspaces: %d", n)
	}
}

func TestDeleteConsoleRemovesEverywhere(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	rec, _ := m.Create(p.ID, "c")
	m.SetText(rec.ID, "SELECT 1")
	if err := m.DeleteConsole(rec.ID); err != nil {
		t.Fatal(err)
	}
	if sink.Exists(rec.ID) {
		t.Fatal(".sql must be deleted with the console")
	}
	if _, ok := m.Active(); ok {
		t.Fatal("deleted active console must clear activation")
	}
	if len(m.Open()) != 0 {
		t.Fatal("member must be removed")
	}
}

func TestOnConnectionDeletedClearsActive(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	rec, _ := m.Create(p.ID, "c")
	_ = m.Store().Remove(p.ID)
	m.OnConnectionDeleted(p.ID)
	if _, ok := m.Active(); ok {
		t.Fatal("active must clear")
	}
	if sink.Exists(rec.ID) {
		t.Fatal("files of the deleted connection must be removed")
	}
}
```

（`m.Store()` 是本任务需要补的只读访问器：`func (m *ConsoleManager) Store() *ConnStore`。）

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./app -run 'Create|Close|OpenConsole|DeleteWorkspace|SetActiveWorkspace|EnsureWorkspace|DeleteConsole|OnConnectionDeleted' -v`
Expected: 编译失败 `m.Create undefined` 等。

- [ ] **Step 3: Implement**

- 管理器字段加 `activeWorkspaceID`、`activeConsoleID`。
- `Workspaces()` → `store.ListWorkspaces()`；`ActiveWorkspace()`：`activeWorkspaceID` 有效则取，否则第一个并记下（不写盘）。
- `SetActiveWorkspace`：先 `flushCaret(当前激活)`，再切换 + 按 `LastActiveConsoleID`（无效 → 最后一个成员）激活；无成员则清激活。
- `Create`：`ws, err := m.EnsureWorkspace()`；`rec, err := store.CreateConsole(connID, name)`；`sink.Write(rec.ID, "")`（失败 → 返回错误并删行）；`store.AddWorkspaceMember(ws.ID, rec.ID)`；激活；返回。
- `OpenConsole`：记录不存在 → `false`；`EnsureWorkspace`；非成员 → 追加；`delete(cache, id)`（重新读盘）；激活。
- `CloseConsole`：`Flush(id)`（错误返回）+ `flushCaret(id)`；`store.RemoveWorkspaceMember(activeWS, id)`；若关的是激活 → 按工作区 `LastActiveConsoleID`（无效 → 最后成员）激活，否则清激活。
- `DeleteConsole`：`Flush` 忽略错误（要删了）；停 timer；`delete(cache/dirty/lastSaved)`；`store.DeleteConsole`；`sink.Delete`；激活单元同 `CloseConsole` 的回落逻辑。
- `OnConnectionDeleted`：对该连接每个控制台删缓存/timer 与文件（`sink.Delete`）；若激活属于该连接 → 清空。
- `Active()` 解析顺序：`activeConsoleID` → `GetConsole` → 若已不是当前工作区成员则清空并返回 false。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./app -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add app/consolemgr.go app/consolemgr_test.go
git commit -m "task5: 工作区成员编排 + 控制台开关/删除/连接级联"
```

---

### Task 6: 会话上提（`app.Console` 只留运行态）

**Files:**
- Modify: `app/console.go`（14–133）、`app/console_test.go`
- Modify: `ui/root.go`（`root` 字段 16–52、`Attach` 87–91）、`ui/run.go`（16–71）、`ui/result.go`（13–48）、`ui/sidebar.go`（48、219）、`ui/objecttree.go`（60、83）
- Test: `app/console_test.go`、`ui/run_test.go`、`ui/sidebar_test.go`（改造成新 API；不新增行为）

**Interfaces:**
- Consumes: `engine.Session`。
- Produces:
  ```go
  // app/console.go —— 会话所有权移出
  func (c *Console) Run(sess engine.Session, query string) bool // sess == nil 或 running 或空串 → false
  // 删除：UseSession / SwapSession / Session / 字段 sess
  // 其余（NewConsole/SetPublisher/Running/Result/Err/SetOutcome/Cancel/queryTimeout）不变

  // ui/root.go —— 新增字段
  sess       engine.Session
  sessConnID string
  ```
  会话切换统一用 `root.setSession(connID string, s engine.Session)`：关旧会话、换新、清 `connErr`/`failedConnID` 由调用方处理。

- [ ] **Step 1: Write the failing tests**

`app/console_test.go` 改造（保留原有用例语义，去掉会话所有权）：

```go
func TestRunRequiresSession(t *testing.T) {
	c := NewConsole()
	if c.Run(nil, "SELECT 1") {
		t.Fatal("Run without a session must return false")
	}
}

func TestRunCompletes(t *testing.T) {
	f := &fakeSession{}
	c := NewConsole()
	if !c.Run(f, "UPDATE t SET x=1") { ... }  // 断言同现有
}
```

并新增 `TestCancelLeavesNoStaleResult` 证明取消/重跑后不会残留旧结果：

```go
func TestCancelLeavesNoStaleResult(t *testing.T) {
	f := &fakeSession{block: make(chan struct{})}
	c := NewConsole()
	if !c.Run(f, "SELECT 1") {
		t.Fatal("first run")
	}
	if c.Run(f, "SELECT 2") {
		t.Fatal("a running console must reject a second run")
	}
	c.Cancel()
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if c.Result() != nil || c.Err() == "" {
		t.Fatalf("cancel must surface an error and no result: %+v %q", c.Result(), c.Err())
	}
	f2 := &fakeSession{}
	if !c.Run(f2, "SELECT 3") {
		t.Fatal("must be runnable again after cancel")
	}
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if c.Err() != "" {
		t.Fatalf("err after rerun: %q", c.Err())
	}
}
```

原 `TestSwapSessionDropsInflight` 删除（`SwapSession` 不复存在，其「丢弃迟到结果」语义由 generation guard 自动维持，由上两个用例覆盖）。

`ui` 侧：把 `a.con.Session()` 断言改成 `a.sess != nil`（`ui/sidebar_test.go` 4 处、`ui/run_test.go` 1 处），`a.con.Run(...)` 调用改成 `a.runSQL()` 已有路径（不改测试意图）。

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./app ./ui -run 'Run|Session|RemoveActiveConn|OpenConn|Expand' -v`
Expected: 编译失败 `c.Run` 参数不匹配 / `a.con.Session undefined`。

- [ ] **Step 3: Implement**

- `app/console.go`：删 `sess` 字段、`UseSession`、`SwapSession`、`Session()`；`Run(sess engine.Session, query string) bool` 把 `sess` 作为参数捕获到 goroutine 里；`SetOutcome`/`Cancel` 不动。
- `ui/root.go`：加 `sess`/`sessConnID`；`a.con` 字段保留到 Task 7 再删（本任务先用单控制台实例，避免一次改太多）。
- `ui/run.go` 的 `openConn`：成功 `sess.Open` 后 `a.setSession(p.ID, sess)`；失败不动旧会话；`removeConn` 里 `a.setSession("", nil)`。
- `ui/result.go` / `sidebar.go` / `objecttree.go`：`a.con.Session() != nil` → `a.sess != nil`；objecttree 的 `sess := a.con.Session()` → `sess := a.sess`（并保留 `a.sessConnID != p.ID` 的归属校验）。
- `setSession(connID string, s engine.Session)`：`if a.sess != nil && a.sess != s { _ = a.sess.Close() }`；赋值 `a.sess, a.sessConnID = s, connID`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./app ./ui -v`（全套）
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add app/console.go app/console_test.go ui/root.go ui/run.go ui/result.go ui/sidebar.go ui/objecttree.go ui/run_test.go ui/sidebar_test.go
git commit -m "task6: 会话从 Console 上提到 root（每控制台一个运行态的前置重构）"
```

---

### Task 7: 标签条 + 工作区切换器 + 激活切换

**Files:**
- Create: `ui/tabs.go`、`ui/tabs_test.go`
- Modify: `ui/root.go`（`root` 字段、`newRoot`、`NewRoot`、`rootView` 顶栏/编辑器区、`Attach`）、`ui/run.go`（`runSQL` 执行前 Flush）、`ui/result.go`（读激活控制台的运行态）
- Test: `ui/tabs_test.go`、`ui/result_test.go`（空态文案变化）

**Interfaces:**
- Consumes: Task 4/5 的 `ConsoleManager` API、Task 6 的 `root.sess`。
- Produces:
  ```go
  // ui/root.go
  mgr            *app.ConsoleManager
  consoles       map[string]*app.Console // consoleID → 运行态
  activeConsole  string                  // 镜像 mgr.Active() 的 id，避免每帧查 store
  pendingActivate string                 // 下一帧要激活的控制台 id（菜单/侧栏点击设置）
  lastVer        map[string]uint64
  lastCaret      [2]int
  pendingSel     *[2]int
  consoleErr     string

  func (a *root) consoleMgr() *app.ConsoleManager // 懒初始化：store 为 nil → nil
  func (a *root) runtime(id string) *app.Console  // 懒创建 + SetPublisher(win.Update)
  func (a *root) activateConsole(id string) bool  // Flush 旧 → mgr.OpenConsole → 换 buffer → pendingSel
  func (a *root) flushActive() error              // Flush(activeConsole) + SetCaret
  func (a *root) syncActiveBuffer()               // 帧首：Version 变化才 SetText；选区变化才 SetCaret

  // ui/tabs.go
  func (a *root) tabBar(c *ui.Context)     // 成员标签 + × + ＋ + 工作区 MenuButton
  func (a *root) workspaceMenu(m *ui.Menu)
  func (a *root) tabModal(c *ui.Context)   // 新建/重命名/删除确认（ui.Modal）
  ```

- [ ] **Step 1: Write the failing tests**

`ui/tabs_test.go` 需要新助手（生产代码里也用它，见 Task 9）：

```go
func newConsoleRoot(t *testing.T) *root {
	t.Helper()
	a := newRoot("", strats{scopeViewport, scopeViewport})
	a.store = app.LoadStore(filepath.Join(t.TempDir(), "connections.json"))
	a.consoles, a.lastVer = map[string]*app.Console{}, map[string]uint64{}
	return a
}
```

```go
func TestTabBarShowsWorkspaceMembers(t *testing.T) {
	a := newConsoleRoot(t)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	prof := a.store.List()[0]
	rec, err := a.consoleMgr().Create(prof.ID, "查询一")
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("查询一") {
		t.Fatalf("tab missing: %q", tt.Texts())
	}
	if !tt.HasText("默认") {
		t.Fatalf("workspace switcher missing: %q", tt.Texts())
	}
	_ = rec
}

func TestTabClickSwitchesBufferAndCaret(t *testing.T) {
	a := newConsoleRoot(t)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	_ = a.store.Add(p)
	prof := a.store.List()[0]
	m := a.consoleMgr()
	first, _ := m.Create(prof.ID, "A")
	m.SetText(first.ID, "SELECT 1")
	second, _ := m.Create(prof.ID, "B")
	m.SetText(second.ID, "SELECT 2")
	m.SetCaret(second.ID, 3, 3)
	// 激活 A（当前激活是 B）
	m.SetActiveWorkspace(mustWorkspace(t, m).ID) // 保证工作区存在
	a.activateConsole(first.ID)
	if got := a.ed.Buf.String(); got != "SELECT 1" {
		t.Fatalf("buffer: %q", got)
	}
	a.activateConsole(second.ID)
	if got := a.ed.Buf.String(); got != "SELECT 2" {
		t.Fatalf("buffer: %q", got)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if s, e := tt.Find("SELECT 2"); e {
		_ = s
	}
	// 选区回位（rune 偏移 3）
	if a.ed.Buf.String() != "SELECT 2" {
		t.Fatal("buffer must not change on frame")
	}
	if a.pendingSel != nil {
		t.Fatal("pending selection must be consumed")
	}
}

func TestTabDirtyMarker(t *testing.T) {
	a := newConsoleRoot(t)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	_ = a.store.Add(p)
	rec, _ := a.consoleMgr().Create(a.store.List()[0].ID, "脏")
	a.mgr.SetText(rec.ID, "SELECT 1")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("●") {
		t.Fatalf("dirty dot missing: %q", tt.Texts())
	}
}

func TestTabCreateButtonNeedsConnection(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("＋"); err == nil {
		t.Fatal("＋ must be disabled without a connection")
	}
}

func TestCaretClampedAfterExternalShorten(t *testing.T) {
	a := newConsoleRoot(t)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	_ = a.store.Add(p)
	m := a.consoleMgr()
	rec, _ := m.Create(p.ID, "c")
	m.SetText(rec.ID, "SELECT 1")
	m.SetCaret(rec.ID, 500, 500) // 超出正文
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if a.pendingSel != nil {
		t.Fatal("pending selection must be applied")
	}
	// SetTextSelection 内部夹紧；再走一帧后选区必须落在正文范围内
	tt.Frame()
	if a.selStart > len([]rune("SELECT 1")) || a.selEnd > len([]rune("SELECT 1")) {
		t.Fatalf("caret not clamped: %d %d", a.selStart, a.selEnd)
	}
}
```

（`mustWorkspace(t, m)` 助手：`ws, err := m.ActiveWorkspace(); if err != nil { t.Fatal(err) }; return ws`。）

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'TabBar|TabClick|TabDirty|TabCreate|CaretClamped' -v`
Expected: 编译失败 `a.consoleMgr undefined` / `a.activateConsole undefined`。

- [ ] **Step 3: Implement**

- `root`：按 Interfaces 加字段；`newRoot(text, s)` 初始化 `consoles`/`lastVer`；`NewRoot()` 保持现有签名（Task 9 接 store）。
- `consoleMgr()`：`a.mgr == nil && a.store != nil` → `app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir()})`；`store == nil` → nil（既有测试 `newTestRoot` 依赖）。
- `activateConsole(id)`：`_ = a.flushActive()`；`rec, ok := mgr.OpenConsole(id)`（false → false）；`text, _ := mgr.Text(rec.ID)`；`a.ed.Buf.Set(text)`、`a.ed.dirty = true`、`a.ed.scroll = ui.ScrollState{}`；`start, end := mgr.Caret(rec.ID)`；`a.pendingSel = &[2]int{start, end}`；`a.activeConsole = rec.ID`；`a.lastVer[rec.ID] = a.ed.Buf.Version()`；`a.consoles[rec.ID]` 经 `runtime(rec.ID)` 建立。
- `rootView` 帧首调 `a.syncActiveBuffer()`：`ver := a.ed.Buf.Version()` 与 `a.lastVer[a.activeConsole]` 不同 → `mgr.SetText(id, a.ed.Buf.String())` 并记 ver；选区 `(a.selStart, a.selEnd)` 与 `a.lastCaret` 不同且 `a.activeConsole != ""` → `mgr.SetCaret(...)`。
- 编辑器构建处：`in := ui.TextAreaBuffer(...)` 后 `if a.pendingSel != nil { in.SetTextSelection(a.pendingSel[0], a.pendingSel[1]); a.pendingSel = nil }`。
- 布局：在编辑器上方插入 `a.tabBar(c)`；顶栏「执行」按钮 `Disabled(...)` 条件改为 `runtime(a.activeConsole).Running() || a.sess == nil || a.selStart == a.selEnd || a.activeConsole == ""`。
- 帧首处理 `pendingActivate`：`if a.pendingActivate != "" { id := a.pendingActivate; a.pendingActivate = ""; a.activateConsole(id) }`（菜单/侧栏点击发生在构建期间，必须延迟到下一帧交换 buffer，不能在构建中途 `Buf.Set`）。
- 快速键：`c.Shortcut(ui.Ctrl, ui.KeyS)` → `_ = a.flushActive()`；失败写 `a.consoleErr`。
- 空态：`a.activeConsole == ""` 时，标签条位置渲染一行提示文字 `在左侧数据源下新建或打开控制台`（无工作区时先提示 `新建工作区 ▾` 仍显示在右侧）；顶栏渲染 `a.consoleErr`（Danger 色，点击清除）。
- 演示按钮（3k/12k/100k）代码不改：它们 `ed.Buf.Set(...)` 后由帧首 `syncActiveBuffer` 自动置脏并防抖落盘；无激活控制台时按钮仍可点但不写文件（`activeConsole == ""` → `SetText` 不调用），这与 Spec 「仅在有激活控制台时可用」一致，若需严格禁用则加 `Disabled(a.activeConsole == "")`。
- `tabBar`：`ui.Row` 里 `ui.ScrollHorizontal`（成员标签）+ `＋` + `ui.Spacer` + `ui.MenuButton(c, 工作区名, a.workspaceMenu)`。标签 = `ui.Button(c, label)` + 独立的 `ui.Button(c, "×")`；`label` = 脏时 `"● "+name`，跨数据源时附加 `" · "+连接名`；`.ContextMenu(func(m *ui.Menu){ m.Item("关闭").Chosen(); m.Item("重命名…").Chosen(); m.Item("删除…").Chosen() })`。菜单选择通过 `m.Item(...).Chosen()` 读到后设置 `a.tabMenuAction`/`a.tabMenuTarget`，由 `tabModal` 执行（Modal 与 Menu 都与 `ui.Modal` 互斥，先做动作再开 Modal 也可）。
- `workspaceMenu(m *ui.Menu)`：每个工作区一项（当前 `.Checked(true)`，标签 `名字`），点击 `mgr.SetActiveWorkspace(id)` + 本帧后 `a.activateConsole` 同步编辑器（用 `a.pendingActivate` 延迟到下一帧执行）；分隔线；`新建工作区…`/`重命名当前工作区…`/`删除当前工作区…` 打开 `a.tabModal`。
- `ui/run.go` 的 `runSQL`：开头 `if a.activeConsole != "" { if err := a.flushActive(); err != nil { a.consoleErr = err.Error(); return } }`；用 `a.runtime(a.activeConsole).Run(a.sess, text)`；目标控制台的数据源 ≠ `a.sessConnID` 时先 `openConn`（取 `a.store.Get(rec.ConnectionID)`），失败则写 `a.consoleErr` 并 return。
- `ui/result.go`：`a.con` → `a.runtime(a.activeConsole)`（无激活控制台时全部走「请先连接数据源/新建控制台」文案）；`TestResultPaneEmpty` 文案断言改为 `在左侧数据源下新建或打开控制台`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'Tab|Caret|ResultPane' -v` 然后 `CGO_ENABLED=0 go test ./ui`
Expected: PASS；既有的 `newTestRoot` 用例不崩（无 store → 无标签、空态提示）。

- [ ] **Step 5: Commit**

```bash
git add ui/tabs.go ui/tabs_test.go ui/root.go ui/run.go ui/result.go ui/result_test.go
git commit -m "task7: 标签条/工作区切换器/激活切换与光标回位"
```

---

### Task 8: 侧栏控制台小节（打开/关闭/新建/重命名/删除）+ 删连接级联

**Files:**
- Modify: `ui/sidebar.go`（`connRow` 47–82、`deleteModal` 137–160）、`ui/run.go`（`removeConn` 46–64）
- Test: `ui/sidebar_test.go`（追加）

**Interfaces:**
- Consumes: Task 5 `ConsoleManager`（`ConsolesOf`/`IsOpen`/`OpenConsole`/`CloseConsole`/`Create`/`Rename`/`DeleteConsole`/`OnConnectionDeleted`）、Task 7 `activateConsole`/`flushActive`。
- Produces:
  ```go
  func (a *root) consoleSection(c *ui.Context, p app.ConnProfile) // 连接行展开区：控制台列表 + ＋新建
  func (a *root) consoleRow(c *ui.Context, rec app.ConsoleRecord, isOpen bool)
  ```
  `root` 新增字段：`consoleFormOpen bool`、`consoleFormID string`、`consoleFormName string`、`consoleDeleteID string`。

- [ ] **Step 1: Write the failing tests**

```go
func TestSidebarConsolesListOpenState(t *testing.T) {
	a := newConsoleRoot(t)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	_ = a.store.Add(p)
	prof := a.store.List()[0]
	m := a.consoleMgr()
	open, _ := m.Create(prof.ID, "开着的")
	closed, _ := m.Create(prof.ID, "关掉的")
	_ = m.CloseConsole(closed.ID)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("开着的") || !tt.HasText("关掉的") {
		t.Fatalf("consoles missing: %q", tt.Texts())
	}
	_ = open
}

func TestSidebarReopenClosedConsole(t *testing.T) {
	a := newConsoleRoot(t)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	_ = a.store.Add(p)
	prof := a.store.List()[0]
	m := a.consoleMgr()
	rec, _ := m.Create(prof.ID, "草稿")
	m.SetText(rec.ID, "SELECT 5")
	_ = m.Flush(rec.ID)
	_ = m.CloseConsole(rec.ID)
	if err := ui.NewTester(rootView(a), 1400, 800).Click("草稿"); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !a.consoleMgr().IsOpen(rec.ID) {
		t.Fatal("clicking a closed console must reopen it")
	}
	if got := a.ed.Buf.String(); got != "SELECT 5" {
		t.Fatalf("buffer after reopen: %q", got)
	}
}

func TestSidebarCreateConsoleWithoutConnection(t *testing.T) {
	a := newConsoleRoot(t)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	_ = a.store.Add(p)
	if a.sess != nil {
		t.Fatal("precondition: not connected")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("＋ 新建控制台"); err != nil {
		t.Fatal("must be clickable without a connection:", err)
	}
	if len(a.consoleMgr().ConsolesOf(a.store.List()[0].ID)) != 1 {
		t.Fatal("console must be created without connecting")
	}
}

func TestRemoveConnDeletesConsoleFiles(t *testing.T) {
	a := newConsoleRoot(t)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	_ = a.store.Add(p)
	prof := a.store.List()[0]
	rec, _ := a.consoleMgr().Create(prof.ID, "c")
	files := app.ConsoleFiles{Dir: a.store.ConsolesDir()}
	_ = files.Write(rec.ID, "SELECT 1")
	a.removeConn(prof.ID)
	if files.Exists(rec.ID) {
		t.Fatal(".sql must be removed with the connection")
	}
	if len(a.consoleMgr().Open()) != 0 {
		t.Fatal("tabs must be gone")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'SidebarConsole|SidebarReopen|SidebarCreate|RemoveConnDeletes' -v`
Expected: FAIL（找不到「＋ 新建控制台」/点击无效）。

- [ ] **Step 3: Implement**

- `connRow`：展开时在 `objectGroups` **之前**调 `a.consoleSection(c, p)`。
- `consoleSection`：小标题「控制台」；`mgr.ConsolesOf(p.ID)` 逐行 `consoleRow`；`＋ 新建控制台` 按钮 → `mgr.Create(p.ID, "")` + `a.pendingActivate = rec.ID`（下一帧激活，避免同帧换 buffer）。无 mgr（`newTestRoot` 场景）→ 直接 return，保持旧测试绿。
- `consoleRow`：`isOpen := mgr.IsOpen(rec.ID)`；打开 → `.Bold()`；未打开 → `.TextColor(c.Theme().TextMuted)`；点击 → `a.pendingActivate = rec.ID`；右键菜单：`关闭`（`Disabled(!isOpen)`）/`重命名…`/`删除…`。
- `tabModal` 复用：重命名 Modal（`TextInput` + 保存 → `mgr.Rename`）、删除确认 Modal（「删除控制台「X」及其 .sql 文件？」→ `mgr.DeleteConsole`）。新建不走 Modal（默认名，直接建后可在标签右键改名）。
- `sidebar` 的 `deleteModal`：在既有确认文案下追加 `fmt.Sprintf("将同时删除该数据源的 %d 个控制台及其 .sql 文件", len(mgr.ConsolesOf(id)))`（仅当 >0）。
- `removeConn`（`ui/run.go`）：在 `store.Remove(id)` 之前 `recs := a.consoleMgr().ConsolesOf(id)`；之后 `a.consoleMgr().OnConnectionDeleted(id)` 删文件/清状态；逐个从 `a.consoles`（运行态）、`a.lastVer` 删除对应 id；若 `a.activeConsole` 属于该连接 → `a.activeConsole = ""`、`a.ed.Buf.Set("")`。
- `mgr.DeleteConsole` 成功后同样从 `a.consoles`/`a.lastVer` 删除该 id（控制台已不存在，运行态无意义）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -v`
Expected: PASS（含既有 sidebar 15 个用例）。

- [ ] **Step 5: Commit**

```bash
git add ui/sidebar.go ui/run.go ui/sidebar_test.go ui/tabs.go ui/root.go
git commit -m "task8: 侧栏控制台小节（开关/新建/重命名/删除）+ 删连接级联"
```

---

### Task 9: 启动恢复 + 退出落盘 + 文档

**Files:**
- Modify: `ui/root.go`（`NewRoot` 70–74、新增 `newRootWithStore`/`restoreSession`/`FlushAll`）、`main.go`、`Readme.md`、`Makefile`
- Test: `ui/restore_test.go`

**Interfaces:**
- Consumes: Task 5 `ConsoleManager` 全部；Task 7 `activateConsole`。
- Produces:
  ```go
  func NewRoot() *root                                   // 签名不变：store=DefaultStorePath() + 恢复
  func newRootWithStore(storePath string, s strats) *root // NewRoot 与测试共用
  func (a *root) restoreSession() bool                    // 恢复工作区+控制台+正文+光标
  func (a *root) FlushAll()                               // 退出落盘（main 调用）
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestRestoreWorkspaceAndConsole(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")
	s := app.LoadStore(storePath)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	if err := s.Add(p); err != nil {
		t.Fatal(err)
	}
	prof := s.List()[0]
	m := app.NewConsoleManager(s, app.ConsoleFiles{Dir: s.ConsolesDir()})
	rec, err := m.Create(prof.ID, "上次的")
	if err != nil {
		t.Fatal(err)
	}
	m.SetText(rec.ID, "SELECT 100")
	m.SetCaret(rec.ID, 7, 7)
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	m.FlushCarets() // 立即落库光标草稿（见 Step 3）
	a := newRootWithStore(storePath, strats{scopeViewport, scopeViewport})
	if a.activeConsole != rec.ID {
		t.Fatalf("active console: %q", a.activeConsole)
	}
	if got := a.ed.Buf.String(); got != "SELECT 100" {
		t.Fatalf("restored text: %q", got)
	}
	if a.pendingSel == nil || a.pendingSel[0] != 7 {
		t.Fatalf("restored caret: %+v", a.pendingSel)
	}
}

func TestRestoreFallsBackOnInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")
	raw := `{"conns":[],"consoles":[],"workspaces":[{"id":"w1","name":"默认","members":[],"lastActiveConsoleID":"gone"}],"activeWorkspaceID":"nowhere"}`
	if err := os.WriteFile(storePath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newRootWithStore(storePath, strats{scopeViewport, scopeViewport})
	if ws, ok := a.consoleMgr().ActiveWorkspace(); !ok || ws.ID != "w1" {
		t.Fatalf("fallback workspace: %+v %v", ws, ok)
	}
	if _, ok := a.consoleMgr().Active(); ok {
		t.Fatal("no console must be active")
	}
	if a.ed.Buf.String() != "" {
		t.Fatalf("empty buffer expected: %q", a.ed.Buf.String())
	}
}

func TestEmptyStoreDoesNotWriteOnStartup(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")
	a := newRootWithStore(storePath, strats{scopeViewport, scopeViewport})
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if _, err := os.Stat(storePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup must not write config: %v", err)
	}
	if !tt.HasText("在左侧数据源下新建或打开控制台") {
		t.Fatalf("empty hint missing: %q", tt.Texts())
	}
}

func TestFlushAllWritesDirtyConsoles(t *testing.T) {
	a := newConsoleRoot(t)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	_ = a.store.Add(p)
	rec, _ := a.consoleMgr().Create(a.store.List()[0].ID, "c")
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.ed.Buf.Set("SELECT 77")
	a.syncActiveBuffer()
	a.FlushAll()
	text, err := app.ConsoleFiles{Dir: a.store.ConsolesDir()}.Read(rec.ID)
	if err != nil || text != "SELECT 77" {
		t.Fatalf("flush all: %q %v", text, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'Restore|EmptyStore|FlushAll' -v`
Expected: 编译失败 `newRootWithStore undefined` / `FlushAll undefined`。

- [ ] **Step 3: Implement**

- `newRootWithStore(storePath string, s strats) *root`：`newRoot("", s)` → `a.store = app.LoadStore(storePath)` → `a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir()})` → `a.restoreSession()`。
- `NewRoot()`：`return newRootWithStore(app.DefaultStorePath(), stratsFromEnv())`。
- `restoreSession()`：取 `mgr.Active()`；无则清空 buffer 并 return false；否则压入 `text`/`pendingSel`（复用 `activateConsole` 的赋值逻辑，但不重复加入成员——`Active()` 已经是成员）。
- `FlushAll()`：`_ = a.flushActive()`；`a.mgr.FlushAll()`。
- `main.go`：`if err := mygo.App.Run(); err != nil { log.Fatal(err) }; r.FlushAll()`（`r` 在 Run 前已创建）。
- 测试辅助：`ConsoleManager` 补一个 `func (m *ConsoleManager) FlushCarets()`（立即落库所有光标草稿），`FlushAll` 里也调用它。
- `Readme.md`：功能清单勾掉控制台/工作区条目并写明「关闭控制台不删文件，删除才删；演示按钮 3k/12k/100k 会写入当前控制台」；构建命令里的 `go1.27.1` 改成 `go1.27.3`，并注明本机需 `CGO_ENABLED=0`。
- `Makefile`：`go1.27.1` → `go1.27.3`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'Restore|EmptyStore|FlushAll' -v` 然后全量：
`CGO_ENABLED=0 go test ./...`、`CGO_ENABLED=0 go vet ./...`、`CGO_ENABLED=0 go build ./...`
Expected: 全部 PASS/无输出。（全量测试含 spike 基准，约数分钟。）

- [ ] **Step 5: Commit**

```bash
git add ui/root.go ui/restore_test.go main.go Readme.md Makefile
git commit -m "task9: 启动恢复/退出落盘 + 文档与构建命令更新"
```

---

## 执行完成后

- 手工验收（真实窗口）：`CGO_ENABLED=0 go run .`
  1. 新建连接 → 侧栏「＋ 新建控制台」→ 输入并 Ctrl+S → 关标签（×，文件仍在）→ 侧栏点回 → 正文与光标回位。
  2. 新建工作区 → 在 B 工作区打开另一个控制台 → 切回 A：标签条只显示 A 成员。
  3. 退出重开：工作区、标签、正文、光标全部恢复。
  4. 删除连接：控制台标签消失、`~/.config/db-g/consoles/` 下对应 `.sql` 消失。
- 性能：`CGO_ENABLED=0 go test ./ui -run Spike -v` 与结果网格基准不回退（编辑器主体未变，只多了每帧一次 `Version()` 比较）。
