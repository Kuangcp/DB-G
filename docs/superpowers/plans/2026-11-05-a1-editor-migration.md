# A1：迁移到 ui/editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 db-g 的主编辑器从自制的 `ui.TextAreaBuffer` + `highlight/` 换成 mygo 的 `ui/editor`，SQL 高亮/行号/查找替换/折叠/多光标由库提供，而「仅执行选中 SQL」、控制台正文与光标持久化、工作区标签切换等既有行为一条不变。

**Architecture:** 新增 `ui/code.go`（`codeEditor`）包住 `ui/editor`：每控制台一个 `*editor.Buffer`（切换零拷贝、撤销独立），光标在 rune（持久化语义）与字节（编辑器语义）之间只在激活/切换时换算。`root` 不再持有自制 `editor`；选区、文本、版本号都经 `codeEditor` 取。

**Tech Stack:** Go 1.27.3、mygo（`ui/editor`、`ui.Tester` headless）、`db-g/app`（ConsoleManager 不变）。

**Spec:** `docs/superpowers/specs/2026-11-05-editor-completions-design.md`（阶段 A1）

## Global Constraints

- `go.mod` 版本下限 `1.27.3`；模块 `db-g`；**不新增依赖**（`ui/editor` 已在 mygo 内）。
- 所有命令加 `CGO_ENABLED=0`（本机 cgo 链接有 `.sframe` 环境问题）；全量测试用 `-p 1` 避免并行包争 CPU 让基准抖动。
- 控制台光标持久化仍存 **rune** 偏移（兼容已上线数据）；编辑器内部用 **字节** 偏移。
- 「仅执行选中」语义不变：无选区时按钮禁用且快捷键不执行。
- 文案中文，风格与现有 UI 一致；提交信息沿用 `taskN: …`。
- 每个任务收尾必须绿：`CGO_ENABLED=0 go test -p 1 ./...`、`CGO_ENABLED=0 go vet ./...`；涉及基准的任务单独跑基准命令。

## Review Focus

规范没写、但用户一定会撞上的五类情况，按可能性排序。每条都在括注任务的测试里钉住：

1. **中文/emoji 光标**：非 ASCII 文本下切换控制台，光标必须落回同一个字符边界，不能跳位或落到半个字符里。（Task 1、Task 2）
2. **Ctrl+Enter 冲突**：`ui/editor` 默认把 Ctrl+Enter 绑成「下一行插入换行」；按 Ctrl+Enter 必须执行选中且正文不变。（Task 1、Task 2）
3. **100k 行键入性能**：换编辑器后键入不能慢于可接受包线（首帧一次性 tokenize 视口是已知风险）。（Task 4）
4. **控制台切换后的正文与光标**：切换仍必须换正文、回位光标，且未落盘编辑不丢（上次修的 bug 不能回潮）。（Task 2）
5. **无控制台/空文本**：没有激活控制台或正文为空时，编辑器仍可显示、执行按钮禁用、不 panic；`Reset("")` 后撤销栈应为空。（Task 1、Task 2）

---

### Task 1: `ui/code.go` —— 编辑器封装、每控制台 Buffer、rune↔字节换算

**Files:**
- Create: `ui/code.go`
- Test: `ui/code_test.go`

**Interfaces:**
- Consumes: `github.com/egoist/mygo/ui/editor`（`NewBuffer`、`Buffer{Reset,Set,String,Len,LineCount,Version,Slice,Pos,Offset,CanUndo,SetLanguage}`、`State{Buffer,Bindings,Placeholder}`、`View`、`LanguageNamed`、`NoCommand`、`Selection{Anchor,Head}`、`Selection.Empty`、`State{Selections,Select,Cursor,SelectedText,Insert,Find}`、`Binding{Mods,Key,Command}`）。
- Produces（Task 2 依赖）:
  ```go
  type codeEditor struct{ /* state, buffers, active */ }
  func newCodeEditor(placeholder string) *codeEditor
  func (c *codeEditor) SetActive(consoleID, text string) // 取/建 Buffer，Reset 到 text，语言 SQL，state.Buffer 指向它
  func (c *codeEditor) Clear()                          // 空态：state.Buffer = 空 Buffer，active = ""
  func (c *codeEditor) Text() string                     // 当前 Buffer 文本
  func (c *codeEditor) SetText(text string)              // Reset（不产生撤销步）
  func (c *codeEditor) Version() uint64
  func (c *codeEditor) Selection() (start, end int, hasSel bool) // 字节偏移
  func (c *codeEditor) SetSelection(startRune, endRune int)      // rune → 字节后 Select
  func (c *codeEditor) CaretRunes() (start, end int)             // 字节 → rune
  func (c *codeEditor) SelectedText() (string, bool)             // 无选区返回 false
  func (c *codeEditor) Insert(text string)
  func (c *codeEditor) Find()
  func (c *codeEditor) LineCount() int
  func (c *codeEditor) ByteLen() int
  func (c *codeEditor) View(c *ui.Context) ui.Element  // editor.View(c, c.state).Grow(1).FillWidth()
  func (c *codeEditor) Drop(consoleID string)          // 控制台删除时丢弃 Buffer
  ```
  以及两个纯函数（同文件）：`runeToByte(s string, runeIdx int) int`、`byteToRune(s string, byteOff int) int`。

- [ ] **Step 1: Write the failing tests**

```go
func newTestCode() *codeEditor { return newCodeEditor("在左侧数据源下新建或打开控制台") }

func TestCodeEditorRuneByteRoundTrip(t *testing.T) {
	c := newTestCode()
	text := "SELECT '你好😀' AS s" // 含 CJK 与 4 字节 emoji
	c.SetActive("c1", text)
	for _, runePos := range []int{0, 7, 8, 10, 14, len([]rune(text))} {
		c.SetSelection(runePos, runePos)
		if s, e := c.CaretRunes(); s != runePos || e != runePos {
			t.Fatalf("caret %d round trip → %d,%d", runePos, s, e)
		}
	}
	c.SetSelection(8, 10) // '你好'
	if got, ok := c.SelectedText(); !ok || got != "你好" {
		t.Fatalf("selected text = %q ok=%v", got, ok)
	}
	if s, e, has := c.Selection(); !has || s != runeToByte(text, 8) || e != runeToByte(text, 10) {
		t.Fatalf("byte selection = %d,%d", s, e)
	}
}

func TestCodeEditorPerConsoleBuffers(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 'A'")
	c.SetActive("b", "SELECT 'B'")
	c.SetActive("a", "")
	if got := c.Text(); got != "SELECT 'A'" {
		t.Fatalf("buffer per console: %q", got)
	}
}

func TestCodeEditorResetHasNoUndoButTypingDoes(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 1")
	c.SetText("SELECT 2")
	if c.state.Buffer.CanUndo() {
		t.Fatal("SetText/Reset must not create an undo step")
	}
	c.Insert("3")
	if !c.state.Buffer.CanUndo() {
		t.Fatal("typing must create an undo step")
	}
}

func TestCodeEditorEmptyState(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 1")
	c.Clear()
	if c.Text() != "" || c.LineCount() == 0 {
		t.Fatalf("clear: text=%q lines=%d", c.Text(), c.LineCount())
	}
	if _, _, has := c.Selection(); has {
		t.Fatal("empty state must have no selection")
	}
	if _, ok := c.SelectedText(); ok {
		t.Fatal("empty state has no selected text")
	}
}

func TestCtrlEnterIsTakenFromTheEditor(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 1")
	tt := ui.NewTester(func(ctx *ui.Context) {
		c.View(ctx).AutoFocus()
	}, 800, 400)
	tt.Frame()
	before := c.Text()
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Frame()
	if got := c.Text(); got != before {
		t.Fatalf("Ctrl+Enter must not edit the text: %q", got)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := c.Text(); got == before {
		t.Fatal("plain Enter must still insert a newline")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'CodeEditor|CtrlEnter' -v`
Expected: 编译失败 `undefined: codeEditor` / `newCodeEditor`。

- [ ] **Step 3: Implement `ui/code.go`**

要点（其余按现有代码风格补齐）：
- `newCodeEditor`：`state := &editor.State{Bindings: []editor.Binding{{Mods: ui.Ctrl, Key: ui.KeyEnter, Command: editor.NoCommand}}, Placeholder: placeholder}`。
- `SetActive`：`buf, ok := c.buffers[id]`；没有则 `editor.NewBuffer(text)`、`SetLanguage(editor.LanguageNamed("SQL"))` 存入；有则 `buf.Reset(text)`；`c.state.Buffer = buf`；`c.active = id`。
- `Clear`：`c.state.Buffer = editor.NewBuffer("")`（不动 `buffers`，控制台切换回来仍可用）；`c.active = ""`。
- `runeToByte`：遍历前缀累加 `utf8.RuneLen`，`runeIdx` 夹在 `[0, runeCount]`。`byteToRune`：`utf8.RuneCountInString(s[:clamped])`。
- `SetSelection`：用**当前文本**换算（`c.Text()`），`c.state.Select(runeToByte(text,start), runeToByte(text,end))`。
- `CaretRunes`：取 `c.state.Selection()`（`Anchor/Head` 字节）→ `byteToRune`，返回 `Start(), End()`（无序选区也返回有序）。
- `SelectedText`：`sel := c.state.Selection(); if sel.Empty() { return "", false }`；返回 `c.state.SelectedText()`。
- `Find()`：`c.state.Find("")`（打开查找栏）。
- `Drop(id)`：`delete(c.buffers, id)`；若 `c.active == id` 则 `Clear()`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -run 'CodeEditor|CtrlEnter' -v`
Expected: PASS（5 个用例）。

- [ ] **Step 5: Commit**

```bash
git add ui/code.go ui/code_test.go
git commit -m "task1: ui/code.go 编辑器封装（每控制台 Buffer + rune/字节换算 + Ctrl+Enter 让位）"
```

---

### Task 2: root 换成新编辑器（行为不变）+ 迁移受影响的测试

**Files:**
- Modify: `ui/root.go`（`root` 字段 16–62、`newRoot` 74–92、`NewRoot`/`newRootWithStore`、`activateConsole` 195–215、`syncActiveBuffer` 240–252、`rootView` 297–392）、`ui/run.go`（`runSQL` 93–110）、`ui/tabs.go`（`clearEditorConsole` 135–145）、`ui/settings.go`（HUD/策略 18–50、预设 66–72）、`ui/preset.go`（新建：`buildSQL` 从 `ui/fixtures.go` 搬来）
- Test（机械改造，断言不变）: `ui/selection_test.go`、`ui/run_test.go`、`ui/sidebar_test.go`、`ui/result_test.go`、`ui/result_bench_test.go`、`ui/tabs_test.go`、`ui/switch_repro_test.go`、`ui/restore_test.go`、`ui/settings_test.go`
- 删除（本任务只删对被替换符号的引用，不删整个包）: `ui/root.go` 的 `ed` 字段与手绘 gutter、`strats*`/`scopeName` 引用（`ui/editor.go` 留到 Task 3 删）
- Delete（本任务必须做，否则包编译不过）: `ui/bench_test.go`、`ui/editor_test.go`、`ui/findmatch_test.go`、`ui/fixtures_test.go` —— 它们测的是被替换的自制编辑器

**Interfaces:**
- Consumes: Task 1 的 `codeEditor` 全部；`app.ConsoleManager`（不变）。
- Produces（Task 3/4 与后续 A2 依赖）:
  ```go
  // ui/root.go
  code *codeEditor            // 替换旧 `ed *editor`
  // 字段 selStart/selEnd 语义改为「字节偏移」，每帧从 code.Selection() 刷新
  func (a *root) setActiveText(text string)          // 测试/预设用：换当前正文（Reset 语义）
  func (a *root) activeText() string
  func (a *root) setSelection(startRune, endRune int) // 测试用：设置选区并同步 selStart/selEnd
  // ui/preset.go
  func buildSQL(lines int) string                    // 从 ui/fixtures.go 原样搬来
  ```

- [ ] **Step 1: 先把测试改成新 API（RED）**

映射表（逐文件机械替换，断言不动）：

| 旧 | 新 |
|---|---|
| `a.ed.Buf.Set(x)` | `a.setActiveText(x)` |
| `a.ed.Buf.String()` | `a.activeText()` |
| `a.selStart, a.selEnd = s, e`（rune） | `a.setSelection(s, e)` |
| `a.ed.Buf.Slice(s, e)` | 由 `a.setSelection` + `a.code.SelectedText()` 间接覆盖，测试不再直接调用 |
| `a.ed.strat` / `ed.SetStrategy` / `scopeViewport` / `strats{...}` / `newRoot(text, strats)` | 删除相关断言与参数：`newRoot(text)`、`newTestRoot()` 无参 |
| `a.con.Run(...)`（结果区测试用） | 不变（运行态未动） |

新增两个行为测试（防回潮）：

```go
func TestSwitchKeepsCaretWithWideRunes(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	A, _ := m.Create(prof.ID, "甲")
	m.SetText(A.ID, "SELECT '你好😀'")
	_ = m.Flush(A.ID)
	B, _ := m.Create(prof.ID, "乙")
	m.SetText(B.ID, "SELECT 'B'")
	_ = m.Flush(B.ID)
	if !a.activateConsole(A.ID) {
		t.Fatal("activate A")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Frame()
	a.setSelection(9, 9) // '你好😀' 之后
	tt.Frame()
	if err := tt.Click("乙"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if err := tt.Click("甲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if got := a.activeText(); got != "SELECT '你好😀'" {
		t.Fatalf("body: %q", got)
	}
	if s, e := a.code.CaretRunes(); s != 9 || e != 9 {
		t.Fatalf("caret must come back at the same rune: %d,%d", s, e)
	}
}

func TestCtrlEnterRunsSelection(t *testing.T) {
	a := openForSelection(t) // selection_test.go 里已有：打开 sqlite 并选好
	a.setActiveText("SELECT 1 AS x")
	a.setSelection(0, len([]rune("SELECT 1 AS x")))
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	before := a.activeText()
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Frame()
	pollResult(t, a, tt)
	if got := a.activeText(); got != before {
		t.Fatalf("Ctrl+Enter must not edit the text: %q", got)
	}
	if r := a.con.Result(); r == nil || len(r.Columns) != 1 || r.Columns[0] != "x" {
		t.Fatalf("Ctrl+Enter must run the selection: %+v", r)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui -run 'SwitchKeepsCaret|CtrlEnterRuns' -v`
Expected: 编译失败（`a.setActiveText`、`a.setSelection`、`a.code` 未定义）。

- [ ] **Step 3: 实现 root 接线**

- `root`：删 `ed *editor`、`findQuery`、`findActive`、`sampler`；加 `code *codeEditor`；`newRoot(text string)` 里 `code: newCodeEditor("在左侧数据源下新建或打开控制台")` 并 `code.SetActive("", text)`。
- `newRootWithStore(storePath string)`、`NewRoot()`、`stratsFromEnv()`（删除）与测试里的 `strats{...}` 参数全部去掉。
- `activateConsole`：`a.code.SetActive(rec.ID, text)`；`start,end := m.Caret(rec.ID)`；`a.code.SetSelection(start,end)`；删掉 `a.pendingSel` 机制；随后 `a.code.state.ScrollTo(a.code.state.CursorPos().Line)` 把光标行滑进视野（**不用 `GoTo`**：它会移动光标）。
- `syncActiveBuffer`：`ver := a.code.Version()`；`m.SetTextSource(rec.ID, func() string { return a.code.Text() })`；`m.TouchVersion(...)` 不变。
- `clearEditorConsole`：`a.code.Clear()`（替代 `a.ed.Buf.Set("")`）。
- `rootView` 编辑器区：整块手绘 gutter + `TextAreaBuffer` + `TextRanges` 换成 `a.code.View(c)`；每帧 `a.selStart, a.selEnd, _ = a.code.Selection()`（无选区时两值相等，正是「执行」按钮禁用所需）。
- 工具行：`查找` 按钮 → `a.code.Find()`；删掉 `清除` 按钮与 `findQuery/findActive` 输入框；`Ctrl+Enter`/`F5` 的 `c.Shortcut(...)` 保留（编辑器已经把 Ctrl+Enter 让出来）。
- `runSQL`：`text, ok := a.code.SelectedText(); if !ok { return }` 后 `Run(a.sess, text)`；其余（flush、ensureConsoleConn）不动。
- `settings.go`：删 HUD 与 FF/FV/VV；信息行改 `行数=%d 字节=%d 版本=%d`（`a.code.LineCount()`、`a.code.ByteLen()`、`a.code.Version()`）；预设 `a.setActiveText(buildSQL(n))` 后 `a.syncActiveBuffer(); _ = a.flushActive()`。
- `ui/preset.go`：新建，把 `buildSQL` 从 `ui/fixtures.go` 原样搬过来（`fixtures.go` 本任务暂留）。
- 控制台/连接被删时丢弃 Buffer：`ui/tabs.go` 的删除控制台弹窗与 `ui/run.go` 的 `removeConn` 里，凡 `delete(a.consoles, id)` / `delete(a.lastVer, id)` 处同步 `a.code.Drop(id)`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ui -count=1`（全套 ui，含改造后的 13 个文件）
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add -A ui
git commit -m "task2: 主编辑器换成 ui/editor（选中执行/控制台切换/光标持久化行为不变）"
```

---

### Task 3: 删除旧编辑器与 highlight 包

**Files:**
- Delete: `ui/editor.go`、`ui/findmatch.go`、`ui/fixtures.go`、`highlight/`（整包含测试）；旧编辑器测试文件已在 Task 2 删除
- **保留 `ui/metrics.go` 与 `ui/metrics_test.go`**：`Sampler` 留给 Task 4 的性能基准。
- Modify: 任何仍引用 `NewEditor`/`scopeName`/`appLastBuild`/`FindAll`/`buildSQL` 的位置（`buildSQL` 已在 Task 2 搬到 `ui/preset.go`）

**Interfaces:**
- Consumes: Task 2 完成后的 `root`（不再有 `ed` 字段）。
- Produces: 无新增；删除 `db-g/highlight` 模块与 `appLastBuild` 全局。

- [ ] **Step 1: 确认无引用（RED 的一种：先证明删除会漏）**

Run: `grep -rn "NewEditor\|appLastBuild\|FindAll\|highlight\.\|NewSampler" --include="*.go" ui/ | grep -v "_test.go"`
Expected: 只剩 `ui/editor.go`、`ui/findmatch.go`、`ui/fixtures.go` 自身（`NewSampler` 只应在 `ui/metrics.go`；若有遗漏，先在本步修掉）。

- [ ] **Step 2: 删除文件并修正编译**

```bash
git rm -r highlight ui/editor.go ui/findmatch.go ui/fixtures.go
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./...
```
Expected: 构建通过（`buildSQL` 由 `ui/preset.go` 提供；`Sampler` 留在 `ui/metrics.go`）。

- [ ] **Step 3: 跑全量**

Run: `CGO_ENABLED=0 go test -p 1 -count=1 ./...`
Expected: 全绿（此时基准只剩 `ui/result_bench_test.go`）。

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "task3: 删除自制编辑器与 highlight 包（能力由 ui/editor 接管）"
```

---

### Task 4: 新性能基准 + spike 记录 + Readme

**Files:**
- Create: `ui/perf_test.go`
- Modify: `docs/spike-results.md`（新章节 + 旧章节标注退役）、`Readme.md`（编辑器说明、`SCOPE` 环境变量移除、构建命令）

**Interfaces:**
- Consumes: Task 1 的 `codeEditor`；`buildSQL`（`ui/preset.go`）。
- Produces:
  ```go
  // ui/perf_test.go
  var perfBudget = map[int]time.Duration{ /* 3k/12k/100k → 实测×1.5，先测后填 */ }
  func TestEditorTypingPerf(t *testing.T) // 键入单字符
  func TestEditorScrollPerf(t *testing.T) // 滚一屏
  func TestEditorFirstFramePerf(t *testing.T) // 首帧构建
  func TestEditorTypingWithCompletionsOpen(t *testing.T) // 补全弹层打开时键入
  ```

- [ ] **Step 1: 写基准（先只记录，不设限值）**

`ui/perf_test.go` 骨架：对每个 size（3000/12000/100000）用 `buildSQL(size)` 建 `codeEditor`，headless `ui.Tester`：
- 键入：`c.SetSelection(runeCount/2, runeCount/2)` 后循环 20 次 `tt.Key(0, 'a')` + `tt.Frame()`，取每帧耗时 p50/p99（复用 `ui/metrics.go` 的 `Sampler`：`sampler.Add(d)` 后 `Median()/P90()`）。
- 滚动：`tt.Scroll(x, y, 0, 400)` + `tt.Frame()` 计时。
- 首帧：新建 `codeEditor` 后第一次 `tt.Frame()` 计时。
- 补全弹层：`c.state.OnCompletion = func(r editor.CompletionRequest) { c.state.ShowCompletions(r.Offset-len(r.Prefix), []editor.Completion{{Label: "select"}}) }`，键入若干字符使弹层打开后计时。

Run: `CGO_ENABLED=0 go test ./ui -run 'TestEditor.*Perf' -v -count=1`
Expected: 打印各组 p50/p99 数字（此时**不**断言）。

- [ ] **Step 2: 定限值**

取各 size/场景的 p99，写 `perfBudget` = `p99 × 1.5` 向上取整到毫秒；断言 p99 ≤ 限值。若 100k 首帧 p99 > 50ms，先不要放宽限值 —— 记录数字并在计划里暂停（见「升级条件」）。
Run: `CGO_ENABLED=0 go test ./ui -run 'TestEditor.*Perf' -v -count=1`
Expected: PASS，并打印各场景数字。

- [ ] **Step 3: 记录到文档**

- `docs/spike-results.md`：顶部标注「以下 FF/FV/VV 数据来自自制编辑器，已随 A1 退役」；新增「A1：ui/editor 基准（日期、机器、命令）」小节，逐条抄入实测 p50/p99/限值。
- `Readme.md`：功能清单改「编辑器：mygo `ui/editor`（SQL 高亮/行号/查找替换/折叠/多光标/撤销）」；删除 `SCOPE` 环境变量说明；构建命令保持 `CGO_ENABLED=0 …`；补一句「性能基准：`CGO_ENABLED=0 go test ./ui -run 'TestEditor.*Perf' -v`」。

- [ ] **Step 4: 全量验证**

Run: `CGO_ENABLED=0 go test -p 1 -count=1 ./...`、`CGO_ENABLED=0 go vet ./...`、`CGO_ENABLED=0 go build ./...`
Expected: 全绿 / 无输出 / 无输出。

- [ ] **Step 5: Commit**

```bash
git add ui/perf_test.go docs/spike-results.md Readme.md
git commit -m "task4: ui/editor 性能基准与 spike/Readme 更新"
```

---

## 升级条件（何时停下来问）

- 100k 行首帧 p99 > 50ms，且无法通过「不逐帧 tokenize 全文」的配置解决 → 停下来汇报数字，而不是把限值放宽。
- `ui/editor` 的默认键位与本仓库其他快捷键再冲突（除 Ctrl+Enter 外发现第二处）→ 停下来确认取舍。
- 迁移后 `TestSwitchPreservesUnsavedEdits` / `TestSwitchingTabsSwapsEditorContent` 无法在不改变产品行为的前提下通过 → 停下来重新设计 `codeEditor` 的 Buffer 归属。
