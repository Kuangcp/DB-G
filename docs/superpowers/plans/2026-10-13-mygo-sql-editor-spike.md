# mygo SQL 编辑器可行性基准 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 mygo 原生 UI 构建一个 SQL 编辑器基准应用，实测 3k/12k/100k 行大 SQL 在「全量 vs 视口」两种 ranges 注入与「全量 vs 视口」两种 tokenize 范围组合下的单键延迟与滚动/帧时间，判定 mygo 能否根治 db-k 的多行 SQL 编辑卡顿。

**Architecture:** 窗口持有 `*ui.TextBuffer`；`highlight` 包做行级 SQL 词法（rune 偏移 token）；`editor.go` 每帧对比 buffer 版本与滚动窗口，按配置作用域重扫并注入 `ui.TextRanges`；`bench_test.go` 用 `ui.NewTester` 无窗口测量配置矩阵并输出数据。

**Tech Stack:** Go 1.27.1（`/home/zk/Code/go/bin/go1.27.1`）、`github.com/egoist/mygo v0.4.8`、标准库 `testing`（不引入第三方）。

## Global Constraints

- 一律用 `/home/zk/Code/go/bin/go1.27.1` 执行 go 命令（本机默认 `go` 是 1.26.5）。
- SQL 词法只支持**行内**结构：`'...'` 字符串（含 `''` 转义）、`--` 行注释、数字、关键字；**不支持**跨行块注释/多行字符串。fixtures 用行导向 SQL，行级 tokenizer 才正确。该简化写入报告。
- `ui.TextRange` 偏移是 **rune 偏移**；一切 token/range 计算以 rune 计。
- 高亮色用 `ui.RGB(...)` 常量，不依赖主题。
- 已核实的 v0.4.8 API：`ui.NewTextBuffer`、`ui.TextAreaBuffer(c,b)`（Element，支持 `.Changed/.Label/.Fill/.Font/.TrackLines/.TrackScroll/.TextRanges/.SetTextSelection`）、`ui.ScrollState{X,Y,MaxY}`、`ui.TextLines.At(int64?)/Top/Count/Height`（`.At` 接收 `float64`，已核签）、`ui.Painter.Text/Clip/Fill`、`ui.NewTester(view,w,h)`、`Tester.Frame/Type/Click/Scroll/SetSize`。
- **框架每帧清空 `ed.ranges`，`TextRanges` 每次调用都对传入数组排序** → 想连续显示颜色就必须每帧再传入；视口策略 R 小，此框架侧成本才可控。这是「视口注入」论证的关键。
- 目标目录 `/home/zk/Code/go/db-k` 现有 `go.mod`(module db-k, go 1.27.1)、`docs/superpowers/specs/2026-10-13-mygo-sql-editor-spike-design.md`，已 git 提交。后续全部在其上迭代。

---

### Task 1: fixtures.go 生成器 + 冒烟测试

**Files:**
- Create: `fixtures.go`
- Test: `fixtures_test.go`

**Interfaces:**
- Produces: `func buildSQL(lines int) string` —— 行导向 SQL 文本，每行含关键字/标识符/数字/单行注释/带 `''` 与中文的字符串，行数精确 = `lines`。

- [ ] **Step 1: 写生成器 fixtures.go**

```go
package main

import "strings"

// buildSQL returns "lines" lines of line-oriented SQL. Each line is a
// self-contained statement fragment with keywords, identifiers, numbers,
// quoted strings (incl. non-ASCII) and a trailing -- comment, so line-level
// tokenization is exact. Block comments / multi-line strings are out of
// scope by design (see plan notes).
func buildSQL(lines int) string {
	var b strings.Builder
	cols := []string{"id", "customer_name", "region", "amount", "created_at", "status"}
	for i := 0; i < lines; i++ {
		if i%8 == 0 || i%8 == 4 {
			b.WriteString("INSERT INTO orders (")
			for j, c := range cols {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(c)
			}
			b.WriteString(") VALUES (1001, 2002, '华东大区', 99.99, '2026-01-01', 'open')")
			b.WriteString("; -- 用户")
		} else {
			b.WriteString("SELECT id, customer_name, amount FROM orders")
			b.WriteString(" WHERE status = 'open' AND amount > ")
			b.WriteByte(byte('0' + i%10))
			b.WriteString(".00; -- row 用户")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
```

- [ ] **Step 2: 写测试并运行（验证行数精确）**

`fixtures_test.go`：

```go
package main

import (
	"strings"
	"testing"
)

func TestFixtures(t *testing.T) {
	for _, n := range []int{3_000, 12_000, 100_000} {
		s := buildSQL(n)
		if got := strings.Count(s, "\n"); got != n {
			t.Fatalf("lines=%d want %d", got, n)
		}
		for _, tok := range []string{"INSERT", "SELECT", "'华东大区'", "-- "} {
			if !strings.Contains(s, tok) {
				t.Fatalf("fixture missing %q", tok)
			}
		}
	}
}
```

Run:

```bash
/home/zk/Code/go/bin/go1.27.1 test ./... -run TestFixtures -v
```

Expected: PASS。

- [ ] **Step 3: Commit**

```bash
git add fixtures.go fixtures_test.go && git commit -q -m "task1: line-oriented SQL fixture generator"
```

---

### Task 2: highlight 包 —— 行级 SQL tokenizer

**Files:**
- Create: `highlight/highlight.go`
- Test: `highlight/highlight_test.go`

**Interfaces:**
- Consumes: 纯逻辑，仅 `github.com/egoist/mygo/ui`。
- Produces:
  ```go
  type Kind uint8
  const (KDefault Kind = iota; KKeyword; KString; KComment; KNumber)
  func (k Kind) Color() ui.Color
  type Run struct { Start, End int; Kind Kind }   // 行内 rune 偏移
  func TokenizeLine(s string) []Run               // 只返回非 KDefault 的 token
  ```

- [ ] **Step 1: 写失败测试 `highlight/highlight_test.go`**

```go
package highlight

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// runeOf returns the rune offsets of the first occurrence of sub in s.
func runeOf(s, sub string) (int, int) {
	bi := strings.Index(s, sub)
	if bi < 0 {
		return -1, -1
	}
	return utf8.RuneCountInString(s[:bi]), utf8.RuneCountInString(s[:bi]) + utf8.RuneCountInString(sub)
}

func TestTokenizeLine(t *testing.T) {
	line := "SELECT id, '华东大区', 99.99 FROM t -- note"
	runs := TokenizeLine(line)
	selS, selE := runeOf(line, "SELECT")
	strS, strE := runeOf(line, "'华东大区'")
	numS, numE := runeOf(line, "99.99")
	fromS, fromE := runeOf(line, "FROM")
	comS, comE := runeOf(line, "-- note")
	want := []Run{
		{selS, selE, KKeyword},
		{strS, strE, KString},
		{numS, numE, KNumber},
		{fromS, fromE, KKeyword},
		{comS, comE, KComment},
	}
	if len(runs) != len(want) {
		t.Fatalf("got %d runs %+v, want %d", len(runs), runs, len(want))
	}
	for i, w := range want {
		if runs[i] != w {
			t.Errorf("run[%d]=%+v want %+v", i, runs[i], w)
		}
	}
}

func TestEscapedQuote(t *testing.T) {
	line := "SELECT 'it''s' AS a"
	runs := TokenizeLine(line)
	sS, sE := runeOf(line, "'it''s'")
	want := []Run{{sS, sE, KString}}
	if len(runs) != len(want) || runs[0] != want[0] {
		t.Fatalf("got %+v want %+v", runs, want)
	}
}

func TestKeywordNotStringInside(t *testing.T) {
	// keyword list words inside a string must stay string, and a keyword
	// right after a number boundary must still be found.
	line := "WHERE 'SELECT' AND 42 FROM x"
	runs := TokenizeLine(line)
	if len(runs) != 4 {
		t.Fatalf("got %+v", runs)
	}
	if runs[1].Kind != KString {
		t.Errorf("runs[1]=%+v want string", runs[1])
	}
	if runs[2].Kind != KNumber {
		t.Errorf("runs[2]=%+v want number", runs[2])
	}
	if runs[3].Kind != KKeyword { // FROM
		t.Errorf("runs[3]=%+v want keyword", runs[3])
	}
}
```

- [ ] **Step 2: 确认失败**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./highlight/ -v
```

Expected: FAIL（`undefined: TokenizeLine`）。

- [ ] **Step 3: 实现 `highlight/highlight.go`**

```go
// Package highlight provides a line-scoped SQL tokenizer producing
// rune-offset runs, and range builders on top of it for mygo's TextRanges.
package highlight

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

type Kind uint8

const (
	KDefault Kind = iota
	KKeyword
	KString
	KComment
	KNumber
)

func (k Kind) Color() ui.Color {
	switch k {
	case KKeyword:
		return ui.RGB(132, 140, 255)
	case KString:
		return ui.RGB(178, 145, 90)
	case KComment:
		return ui.RGB(112, 135, 115)
	case KNumber:
		return ui.RGB(130, 178, 210)
	default:
		return ui.RGB(210, 210, 210)
	}
}

// Run is a token span within a single line, in rune offsets.
type Run struct {
	Start, End int
	Kind       Kind
}

func isIdentStart(r rune) bool { return unicode.IsLetter(r) || r == '_' }
func isIdentPart(r rune) bool  { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' }
func isNumPart(r rune) bool    { return (r >= '0' && r <= '9') || r == '.' }

func kwWord(ident string) bool {
	switch ident {
	case "SELECT", "FROM", "WHERE", "INSERT", "INTO", "VALUES", "ORDER",
		"BY", "UPDATE", "DELETE", "CREATE", "TABLE", "DROP", "ALTER",
		"ADD", "JOIN", "LEFT", "RIGHT", "FULL", "INNER", "OUTER", "ON",
		"AS", "AND", "OR", "NOT", "NULL", "IS", "GROUP", "HAVING",
		"LIMIT", "OFFSET", "SET", "IN", "DISTINCT", "COUNT", "SUM",
		"AVG", "MIN", "MAX", "CASE", "WHEN", "THEN", "ELSE", "END",
		"UNION", "ALL", "ASC", "DESC", "PRIMARY", "KEY", "FOREIGN",
		"REFERENCES", "DEFAULT", "INDEX", "EXISTS", "LIKE", "BETWEEN",
		"CAST", "COALESCE":
		return true
	}
	return false
}

// TokenizeLine scans one SQL line, returning runs of KKeyword / KString /
// KComment / KNumber in rune offsets. Plain identifiers and operators are
// not emitted. It handles '...' strings with '' escapes, and -- comments.
func TokenizeLine(s string) []Run {
	var runs []Run
	i, ri := 0, 0 // byte and rune cursors at the current rune
	peek := func() rune {
		if i >= len(s) {
			return 0
		}
		r, _ := utf8.DecodeRuneInString(s[i:])
		return r
	}
	advance := func() {
		_, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
		ri++
	}
	tok := -1 // start rune of the current token; -1 = none
	var tokK Kind
	flush := func() {
		if tok >= 0 {
			runs = append(runs, Run{Start: tok, End: ri, Kind: tokK})
			tok = -1
		}
	}
	begin := func(k Kind) {
		tok, tokK = ri, k
	}

	for i < len(s) {
		r := peek()
		switch {
		case r == '-' && i+1 < len(s) && s[i+1] == '-':
			flush()
			begin(KComment)
			for i < len(s) {
				advance()
			}
			flush()
		case r == '\'':
			flush()
			begin(KString)
			advance()
			for i < len(s) {
				r2 := peek()
				advance()
				if r2 == '\'' {
					if i < len(s) && peek() == '\'' {
						advance() // '' escape
						continue
					}
					break
				}
			}
			flush()
		case isIdentStart(r):
			flush()
			begin(KDefault)
			byteStart := i
			for i < len(s) && isIdentPart(peek()) {
				advance()
			}
			if kwWord(strings.ToUpper(s[byteStart:i])) {
				tokK = KKeyword
			} else {
				tok = -1 // plain identifier: not emitted
			}
			flush()
		case isNumPart(r):
			flush()
			begin(KNumber)
			for i < len(s) && isNumPart(peek()) {
				advance()
			}
			flush()
		default:
			flush()
			advance()
		}
	}
	return runs
}
```

- [ ] **Step 4: 跑测试通过**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./highlight/ -v
```

Expected: PASS（3 个用例）。

- [ ] **Step 5: Commit**

```bash
git add highlight/ && git commit -q -m "task2: line-scoped SQL tokenizer"
```

---

### Task 3: highlight 包 —— LineStarts / Tokenize 作用域 / Ranges

**Files:**
- Modify: `highlight/highlight.go`
- Create: `highlight/ranges.go`
- Test: `highlight/ranges_test.go`

**Interfaces:**
- Consumes: `TokenizeLine`, `Run`, `Kind`（Task 2）
- Produces:
  ```go
  // LineStarts 返回每行起始的字节偏移与 rune 偏移（含末尾无换行的最后一行）。
  func LineStarts(s string) (byteStarts, runeStarts []int)
  // Tokenize 对区间 [lo,hi) 的行做词法，返回按绝对行号索引的 [][]Run。
  func Tokenize(s string, byteStarts []int, lo, hi int) [][]Run
  // Ranges 将 runesPerLine（按绝对行号索引，只填了 [lo,hi) 行）合并为绝对
  // rune 偏移的 ui.TextRange，行内相邻同色合并；空区间返回 nil。
  func Ranges(runsPerLine [][]Run, runeStarts []int, lo, hi int) []ui.TextRange
  ```

- [ ] **Step 1: 写失败测试 `highlight/ranges_test.go`**

```go
package highlight

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestLineStarts(t *testing.T) {
	s := "ab\nc\ndef\n"
	bs, rs := LineStarts(s)
	wantB := []int{0, 3, 5, 9}
	wantR := []int{0, 3, 5, 9}
	for i := range wantB {
		if bs[i] != wantB[i] || rs[i] != wantR[i] {
			t.Fatalf("i=%d bs=%v rs=%v want %v/%v", i, bs, rs, wantB, wantR)
		}
	}
	// 中文行验证 rune 偏移
	s2 := "选择\nx"
	bs2, rs2 := LineStarts(s2)
	if bs2[0] != 0 || rs2[0] != 0 {
		t.Fatalf("line0 %v/%v", bs2, rs2)
	}
	if bs2[1] != 6 || rs2[1] != 2 { // '\n' 在字节 6，rune 2
		t.Fatalf("line1 byte=%d rune=%d", bs2[1], rs2[1])
	}
}

func TestTokenizeRange(t *testing.T) {
	s := "SELECT a FROM t\nWHERE x = 42\n-- c"
	bs, _ := LineStarts(s)
	// 只词法第 1、2 行（0 基）。
	runs := Tokenize(s, bs, 1, 3)
	if len(runs) != 2 {
		t.Fatalf("lines=%d want 2", len(runs))
	}
	// 第 2 行（runs[1]）是 -- 注释，行内 rune 偏移 0..4。
	if len(runs[1]) != 1 || runs[1][0].Kind != KComment {
		t.Fatalf("runs[1]=%+v", runs[1])
	}
}

func TestRangesMergesAdjacent(t *testing.T) {
	// 三行：关键字相邻合并；KDefault 被剔除；跨行不合并。
	s := "SELECT FROM\nx='它'\n"
	bs, rs := LineStarts(s)
	runs := Tokenize(s, bs, 0, 2)
	toks := Ranges(runs, rs, 0, 2)
	// SELECT(0..6)与FROM(7..11)同色应合并为 0..11；'它'为字符串 3..6（rune）。
	if len(toks) != 2 {
		t.Fatalf("got %+v want 2", toks)
	}
	if toks[0].Start != 0 || toks[0].End != 11 {
		t.Fatalf("toks[0]=%+v want 0..11", toks[0])
	}
	if toks[1].Kind != KString && toks[1].Start != 4 { // '它' 在 rune 4..5
		t.Fatalf("toks[1]=%+v", toks[1])
	}
	_ = ui.RGB
}
```

- [ ] **Step 2: 确认失败**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./highlight/ -v
```

Expected: FAIL（`undefined: LineStarts/Tokenize/Ranges`）。

- [ ] **Step 3: 追加实现（`highlight/ranges.go`）**

```go
package highlight

import (
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// LineStarts returns the byte and rune offset of each line's first
// character. A trailing '\n' does not add an empty final line.
func LineStarts(s string) (byteStarts, runeStarts []int) {
	byteStarts = []int{0}
	runeStarts = []int{0}
	b, ri := 0, 0
	for b < len(s) {
		r, sz := utf8.DecodeRuneInString(s[b:])
		if r == '\n' {
			byteStarts = append(byteStarts, b+sz)
			runeStarts = append(runeStarts, ri+1)
		}
		b += sz
		ri++
	}
	return byteStarts, runeStarts
}

// Tokenize lexes lines [lo,hi) (always in range) and returns per-line runs
// indexed by absolute line number; lines outside the window are nil.
func Tokenize(s string, byteStarts []int, lo, hi int) [][]Run {
	n := len(byteStarts)
	if lo < 0 {
		lo = 0
	}
	if hi > n {
		hi = n
	}
	if lo >= hi {
		return nil
	}
	out := make([][]Run, n)
	for p := lo; p < hi; p++ {
		start := byteStarts[p]
		end := len(s)
		if p+1 < n {
			end = byteStarts[p+1]
		}
		if end > start {
			end-- // strip the trailing '\n' if present
			if end > start && s[end-1] == '\n' {
				end--
			}
		}
		out[p] = TokenizeLine(s[start:end])
	}
	return out
}

// Ranges merges per-line runs (indexed by absolute line number, window
// [lo,hi)) into absolute rune-offset ui.TextRange, merging adjacent runs
// of the same color within a line and dropping KDefault. Empty window or
// no colored runs returns nil.
func Ranges(runsPerLine [][]Run, runeStarts []int, lo, hi int) []ui.TextRange {
	if lo < 0 {
		lo = 0
	}
	if hi > len(runeStarts) {
		hi = len(runeStarts)
	}
	var out []ui.TextRange
	for p := lo; p < hi; p++ {
		base := runeStarts[p]
		for _, r := range runsPerLine[p] {
			if r.Kind == KDefault {
				continue
			}
			c := r.Kind.Color()
			if k := len(out); k > 0 && out[k-1].End == base+r.Start && out[k-1].Color == c {
				out[k-1].End = base + r.End
				continue
			}
			out = append(out, ui.TextRange{Start: base + r.Start, End: base + r.End, Color: c})
		}
	}
	return out
}
```

- [ ] **Step 4: 跑测试**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./highlight/ -v
```

Expected: PASS。若 `TestTokenizeRange`/`TestRangesMergesAdjacent` 的偏移断言与实现不符，先核对预期再修（断言以 rune 偏移为准）。

- [ ] **Step 5: Commit**

```bash
git add highlight/ && git commit -q -m "task3: line starts, scoped tokenize, range builder"
```

---

### Task 4: editor.go —— 集成 TextAreaBuffer + 行号 + 作用域注入

**Files:**
- Create: `editor.go`
- Test: `editor_test.go`（用 `ui.NewTester` 冒烟：注入不崩溃、range 数随作用域变化）

**Interfaces:**
- Consumes: `buildSQL`（Task 1）、`highlight.Tokenize/Ranges/LineStarts/TokenizeLine`（Task 2-3）
- Produces:
  ```go
  type editorScope int
  const (scopeViewport editorScope = iota; scopeFull)
  type strats struct { tokenize, inject editorScope }
  type editor struct {
      Buf *ui.TextBuffer
      strat strats
      margin int                     // 视口高亮余量行数
      lines  ui.TextLines            // TrackLines 输出
      scroll ui.ScrollState          // TrackScroll 输出
      viewH float32                  // 由行号槽 Draw 记录视口高度(DIP)
      allRanges []ui.TextRange       // 最近注入的 ranges
      Injected int                   // 最近注入数量（供 HUD/bench）
      dirty bool                     // 配置切换强制重建
  }
  func NewEditor(text string, s strats) *editor
  func (ed *editor) window() (lo, hi int)  // 视口行窗 [+margin]
  func (ed *editor) Update()               // 版本或视口变化时重建 allRanges
  func (ed *editor) SetStrategy(s strats)  // 切配置，置 dirty
  ```

- [ ] **Step 1: 写冒烟测试 `editor_test.go`**

```go
package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestEditorInjection(t *testing.T) {
	text := buildSQL(3_000)
	for _, sc := range []strats{
		{scopeFull, scopeFull},      // FF
		{scopeFull, scopeViewport},  // FV
		{scopeViewport, scopeViewport}, // VV
	} {
		ed := NewEditor(text, sc)
		a := &app{ed: *ed} // editor by value; see app shape in Task 6
		tt := ui.NewTester(rootView(a), 900, 640)
		for range 3 {
			tt.Frame()
		}
		if ed.Injected <= 0 {
			t.Fatalf("%+v: injected=%d want >0", sc, ed.Injected)
		}
		if sc.inject == scopeFull && sc.tokenize == scopeFull && ed.Injected < 10_000 {
			t.Fatalf("FF expected many ranges, got %d", ed.Injected)
		}
		if sc.inject == scopeViewport && ed.Injected > 20_000 {
			t.Fatalf("viewport injection blew up: %d", ed.Injected)
		}
		if len(ed.allRanges) == 0 {
			t.Fatalf("%+v: empty allRanges", sc)
		}
	}
}

func TestEditorTypingKeepsColors(t *testing.T) {
	// typing must not panic and must keep a non-empty injection.
	ed := NewEditor(buildSQL(200), strats{scopeViewport, scopeViewport})
	a := &app{ed: *ed}
	tt := ui.NewTester(rootView(a), 900, 640)
	for range 2 {
		tt.Frame()
	}
	tt.Click("编辑器") // focus editor by its label (see rootView)
	tt.Type("x")
	tt.Frame()
	if len(ed.allRanges) == 0 && ed.Injected == 0 {
		t.Fatal("no ranges after typing")
	}
	if !strings.Contains(ed.Buf.String(), "x") {
		t.Fatal("typed char missing")
	}
}
```

> 说明：`editor`/`app` 的具体形状在 Task 6 落定；本任务先定 `editor` 并让 `app` 暂时内联（Task 6 会给出最终 `main.go` 的 `app`）。若 `app` 形状与 Task 6 冲突，以 Task 6 为准同步修改测试。

- [ ] **Step 2: 确认失败**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./... -run 'TestEditor' -v
```

Expected: FAIL（`undefined: NewEditor/rootView` 等）。

- [ ] **Step 3: 实现 `editor.go`**

```go
package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"db-k/highlight"
)

type editorScope int

const (
	scopeViewport editorScope = iota
	scopeFull
)

type strats struct {
	tokenize editorScope
	inject   editorScope
}

type editor struct {
	Buf   *ui.TextBuffer
	strat strats
	margin int

	lines  ui.TextLines
	scroll ui.ScrollState
	viewH  float32

	allRanges []ui.TextRange
	Injected  int
	dirty     bool

	lastVersion uint64
	byteStarts  []int
	runeStarts  []int
	lastLo, lastHi int
}

func NewEditor(text string, s strats) *editor {
	return &editor{Buf: ui.NewTextBuffer(text), strat: s, margin: 4}
}

func (ed *editor) SetStrategy(s strats) {
	if ed.strat != s {
		ed.strat = s
		ed.dirty = true
	}
}

func (ed *editor) lineCount() int {
	if len(ed.byteStarts) > 0 {
		return len(ed.byteStarts)
	}
	if ed.lines.Count() > 0 {
		return ed.lines.Count()
	}
	return 0
}

// window returns the line window [lo,hi) to tokenize/inject for the
// viewport strategy, derived from tracked scroll + gutter height + margin.
func (ed *editor) window() (int, int) {
	n := ed.lineCount()
	if n == 0 {
		return 0, 0
	}
	lo := ed.lines.At(float64(ed.scroll.Y))
	hi := ed.scroll.Y + ed.viewH
	if hi < ed.scroll.Y {
		hi = ed.scroll.Y
	}
	hiL := ed.lines.At(float64(hi)) + 1
	if hiL < lo {
		hiL = lo + 1
	}
	lo -= ed.margin
	if lo < 0 {
		lo = 0
	}
	hiL += ed.margin
	if hiL > n {
		hiL = n
	}
	if lo >= hiL {
		return lo, min(lo+1, n)
	}
	return lo, hiL
}

// Update rebuilds ed.allRanges when the text version changed, the viewport
// window moved (viewport inject), or a strategy switch was requested.
func (ed *editor) Update() {
	ver := ed.Buf.Version()
	lo, hi := 0, ed.lineCount()
	if ed.strat.inject == scopeViewport || ed.strat.tokenize == scopeViewport {
		lo, hi = ed.window()
	}
	if ver == ed.lastVersion && !ed.dirty && ed.lastLo == lo && ed.lastHi == hi {
		return
	}
	ed.rebuild(lo, hi)
	ed.lastVersion, ed.lastLo, ed.lastHi = ver, lo, hi
	ed.dirty = false
}

func (ed *editor) rebuild(lo, hi int) {
	t0 := time.Now()
	txt := ed.Buf.String()
	ed.byteStarts, ed.runeStarts = highlight.LineStarts(txt)
	if hi > len(ed.byteStarts) {
		hi = len(ed.byteStarts)
	}
	tokenLo, tokenHi := 0, len(ed.byteStarts)
	if ed.strat.tokenize == scopeViewport {
		tokenLo, tokenHi = lo, hi
	}
	// Build ranges for the inject window; tokenize window must cover it.
	injLo, injHi := 0, len(ed.byteStarts)
	if ed.strat.inject == scopeViewport {
		injLo, injHi = lo, hi
	}
	if injLo < tokenLo || injHi > tokenHi {
		tokenLo, tokenHi = injLo, injHi // ensure tokenize covers inject
	}
	runs := highlight.Tokenize(txt, ed.byteStarts, tokenLo, tokenHi)
	ed.allRanges = highlight.Ranges(runs, ed.runeStarts, injLo, injHi)
	ed.Injected = len(ed.allRanges)
	appLastBuild = time.Since(t0) // set in Task 6; guarded by metrics
	_ = txt
}
```

> `appLastBuild` 在 Task 6 引入（metrics 包级变量）。本任务先以包级 `var appLastBuild time.Duration` 提供占位，Task 6 归入 metrics 结构后此引用保留即可（同一包，无编译冲突）。

- [ ] **Step 4: 补 `rootView` 与 `app` 最小形状（本任务可编译的最小版，Task 6 再完善）**

在 `editor.go` 顶部文件内加：

```go
type app struct {
	ed editor
}

func rootView(a *app) func(c *ui.Context) {
	return func(c *ui.Context) {
		a.ed.Update()
		ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
			ui.Text(c, "mygo SQL editor benchmark").FontSize(13).Bold()
			ui.Row(c).Fill().Children(func() {
				// 行号槽
				ui.Box(c).Width(44).FillHeight().Draw(func(p *ui.Painter, r ui.Rect) {
					a.ed.viewH = r.H
					p.Clip(r, 0, func() {
						top := r.Y - a.ed.scroll.Y
						for i := a.ed.lines.At(float64(a.ed.scroll.Y)); i < a.ed.lines.Count(); i++ {
							y := top + a.ed.lines.Top(i)
							if y > r.Y+r.H {
								break
							}
							p.Text(r.X+8, y, strconv.Itoa(i+1), 12, ui.RGB(130, 130, 130))
						}
					})
				})
				in := ui.TextAreaBuffer(c, a.ed.Buf).Label("编辑器").Fill().Font("monospace").
					TrackLines(&a.ed.lines).TrackScroll(&a.ed.scroll)
				in.TextRanges(a.ed.allRanges...)
			})
		})
	}
}
```

`editor.go` 需补 import `strconv` 与 `db-k/highlight`（module 为 `db-k`）。

- [ ] **Step 5: 编译 + 测试**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./... -run 'TestEditor' -v
```

Expected: PASS（三个注入策略都注入非零 ranges；FF 的 Injected 很大、视口注入很小）。如有断言不符，以实际行为校准断言（例如 FF 在 3000 行 fixture 的实际 range 数）。

- [ ] **Step 6: Commit**

```bash
git add editor.go editor_test.go && git commit -q -m "task4: buffer editor integration with scoped ranges injection"
```

---

### Task 5: findbar —— 非重叠查找高亮

**Files:**
- Create: `findmatch.go`
- Test: `findmatch_test.go`

**Interfaces:**
- Produces:
  ```go
  var findColor = ui.RGB(255,160,200)
  // FindAll 返回 q 在 s 中出现位置的 rune 偏移 ui.TextRange（非重叠、不含空串）。
  func FindAll(s, q string) []ui.TextRange
  ```

- [ ] **Step 1: 写失败测试 `findmatch_test.go`**

```go
package main

import (
	"testing"
)

func TestFindAll(t *testing.T) {
	s := "foo xx foo"
	ranges := FindAll(s, "foo")
	if len(ranges) != 2 {
		t.Fatalf("got %d want 2", len(ranges))
	}
	if ranges[0].Start != 0 || ranges[0].End != 3 {
		t.Errorf("ranges[0]=%+v", ranges[0])
	}
	if ranges[1].Start != 7 || ranges[1].End != 10 {
		t.Errorf("ranges[1]=%+v", ranges[1])
	}
	if FindAll(s, "") != nil {
		t.Fatal("empty query must return nil")
	}
	if FindAll(s, "missing") != nil {
		t.Fatal("no match must return nil")
	}
}
```

- [ ] **Step 2: 确认失败**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./... -run TestFindAll -v
```

Expected: FAIL。

- [ ] **Step 3: 实现 `findmatch.go`**

```go
package main

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

var findColor = ui.RGB(255, 160, 200)

// FindAll returns non-overlapping rune-offset ui.TextRange of every
// occurrence of q in s (case-sensitive). Empty query or no match: nil.
func FindAll(s, q string) []ui.TextRange {
	if q == "" {
		return nil
	}
	var out []ui.TextRange
	b, ri := 0, 0
	for {
		bi := strings.Index(s[b:], q)
		if bi < 0 {
			return out
		}
		absB := b + bi
		start := ri + utf8.RuneCountInString(s[b:absB])
		end := start + utf8.RuneCountInString(q)
		out = append(out, ui.TextRange{Start: start, End: end, Color: findColor})
		b = absB + len(q)
		ri = end
	}
}
```

- [ ] **Step 4: 测试通过**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./... -run TestFindAll -v
```

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add findmatch.go findmatch_test.go && git commit -q -m "task5: non-overlapping find ranges"
```

---

### Task 6: metrics + HUD + 配置切换（完整 main.go）

**Files:**
- Create: `metrics.go`, `metrics_test.go`
- Modify: `main.go`（完整窗口：顶栏 HUD、配置按钮、行号槽、编辑器、查找栏）

**Interfaces:**
- Consumes: `editor`, `rootView`（Task 4）、`FindAll`（Task 5）
- Produces:
  ```go
  type Sampler struct { ... }
  func NewSampler(cap int) *Sampler
  func (s *Sampler) Add(d time.Duration)
  func (s *Sampler) Median() time.Duration
  func (s *Sampler) P90() time.Duration
  func (s *Sampler) P99() time.Duration
  var appLastBuild time.Duration  // 最近一次 editor.rebuild 耗时，供 HUD
  type app struct { ed editor; findQuery string; lastVersion uint64 }
  ```

- [ ] **Step 1: 写失败测试 `metrics_test.go`**

```go
package main

import (
	"sort"
	"testing"
	"time"
)

func TestSampler(t *testing.T) {
	s := NewSampler(1000)
	for i := 0; i < 100; i++ {
		s.Add(time.Duration(i+1) * time.Millisecond)
	}
	sorted := s.Sorted()
	got := sorted[49] * time.Millisecond
	if s.Median() != 50*time.Millisecond {
		t.Fatalf("median=%v want 50ms", s.Median())
	}
	_ = got
	if s.P90() < 89*time.Millisecond || s.P90() > 91*time.Millisecond {
		t.Fatalf("p90=%v", s.P90())
	}
	if s.P99() != 99*time.Millisecond {
		t.Fatalf("p99=%v", s.P99())
	}
}

func TestSamplerBounded(t *testing.T) {
	s := NewSampler(10)
	for i := 0; i < 25; i++ {
		s.Add(time.Millisecond)
	}
	if len(s.Sorted()) != 10 {
		t.Fatalf("cap not honored: %d", len(s.Sorted()))
	}
}

func TestPercentileHelper(t *testing.T) {
	xs := []time.Duration{1, 2, 3, 4, 5}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	if percentile(xs, 0.5) != 3*time.Millisecond {
		t.Fatalf("p50=%v", percentile(xs, 0.5))
	}
}
```

- [ ] **Step 2: 确认失败**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./... -run TestSampler -v
```

Expected: FAIL。

- [ ] **Step 3: 实现 `metrics.go`**

```go
package main

import "sort"
import "time"

// appLastBuild reports how long the last editor rebuild (copy+tokenize+
// ranges) took; written by editor.rebuild, read by the HUD.
var appLastBuild time.Duration

// percentile returns the value at position p of a sorted slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// Sampler is a bounded ring buffer of durations with percentile queries.
type Sampler struct {
	vals []time.Duration
	i, n int
}

func NewSampler(cap int) *Sampler {
	return &Sampler{vals: make([]time.Duration, cap)}
}

func (s *Sampler) Add(d time.Duration) {
	if len(s.vals) == 0 {
		return
	}
	s.vals[s.i] = d
	s.i = (s.i + 1) % len(s.vals)
	if s.n < len(s.vals) {
		s.n++
	}
}

func (s *Sampler) Sorted() []time.Duration {
	out := make([]time.Duration, s.n)
	j := 0
	for k := s.i; k < len(s.vals); k++ {
		if j >= s.n {
			break
		}
		out[j], j = s.vals[k], j+1
	}
	for k := 0; k < s.i && j < s.n; k++ {
		out[j], j = s.vals[k], j+1
	}
	sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
	return out
}

func (s *Sampler) Median() time.Duration { return percentile(s.Sorted(), 0.5) }
func (s *Sampler) P90() time.Duration    { return percentile(s.Sorted(), 0.9) }
func (s *Sampler) P99() time.Duration    { return percentile(s.Sorted(), 0.99) }
```

- [ ] **Step 4: 运行 metrics 测试通过**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./... -run 'TestSampler|TestPercentile' -v
```

Expected: PASS。

- [ ] **Step 5: 完成 `main.go`（窗口 + HUD + 切换）**

```go
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type app struct {
	ed         editor
	findQuery  string
	hasFindNow bool
	sampler    *Sampler
}

func newApp(text string, s strats) *app {
	return &app{ed: *NewEditor(text, s), sampler: NewSampler(128)}
}

func fmtDur(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
}

func rootView(a *app) func(c *ui.Context) {
	return func(c *ui.Context) {
		ed := &a.ed
		ed.Update()

		ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
			// HUD
			ui.Text(c, fmt.Sprintf(
				"lines=%d ranges=%d tokenize=%s inject=%s build=%s(last) lines/per=k-%d",
				ed.lineCount(), ed.Injected,
				scopeName(ed.strat.tokenize), scopeName(ed.strat.inject),
				fmtDur(appLastBuild), a.sampler.n,
			)).FontSize(12)
			// 工具栏
			ui.Row(c).Gap(4).Children(func() {
				ui.Button(c, "3k").OnClick(func() { ed.Buf.Set(buildSQL(3_000)); ed.dirty = true })
				ui.Button(c, "12k").OnClick(func() { ed.Buf.Set(buildSQL(12_000)); ed.dirty = true })
				ui.Button(c, "100k").OnClick(func() { ed.Buf.Set(buildSQL(100_000)); ed.dirty = true })
				ui.Spacer(c)
				ui.Button(c, "FF").OnClick(func() { ed.SetStrategy(strats{scopeFull, scopeFull}) })
				ui.Button(c, "FV").OnClick(func() { ed.SetStrategy(strats{scopeFull, scopeViewport}) })
				ui.Button(c, "VV").OnClick(func() { ed.SetStrategy(strats{scopeViewport, scopeViewport}) })
			})
			// 编辑器 + 行号槽
			ui.Row(c).Fill().Children(func() {
				ui.Box(c).Width(44).FillHeight().Draw(func(p *ui.Painter, r ui.Rect) {
					ed.viewH = r.H
					p.Clip(r, 0, func() {
						top := r.Y - ed.scroll.Y
						for i := ed.lines.At(float64(ed.scroll.Y)); i < ed.lines.Count(); i++ {
							y := top + ed.lines.Top(i)
							if y > r.Y+r.H {
								break
							}
							if i == ed.currentLine() {
								p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 16}, ui.RGBA(80, 100, 160, 0.25), 0)
							}
							p.Text(r.X+8, y, strconv.Itoa(i+1), 12, ui.RGB(130, 130, 130))
						}
					})
				})
				in := ui.TextAreaBuffer(c, ed.Buf).Label("编辑器").Fill().Font("monospace").
					TrackLines(&ed.lines).TrackScroll(&ed.scroll)
				if a.hasFindNow && a.findQuery != "" {
					in.TextRanges(FindAll(ed.Buf.String(), a.findQuery)...)
				} else {
					in.TextRanges(ed.allRanges...)
				}
			})
			// 查找栏
			ui.Row(c).Gap(4).Children(func() {
				ui.TextInput(c, &a.findQuery).Placeholder("查找（非空时覆盖高亮）")
				ui.Button(c, "查找").OnClick(func() { a.hasFindNow = true })
				ui.Button(c, "清除").OnClick(func() { a.findQuery = ""; a.hasFindNow = false })
			})
		})
	}
}

func scopeName(s editorScope) string {
	if s == scopeFull {
		return "full"
	}
	return "viewport"
}

func (ed *editor) currentLine() int { return ed.lines.At(float64(ed.scroll.Y)) }

func main() {
	sc := strats{scopeViewport, scopeViewport}
	if os.Getenv("SCOPE") == "FF" {
		sc = strats{scopeFull, scopeFull}
	} else if os.Getenv("SCOPE") == "FV" {
		sc = strats{scopeFull, scopeViewport}
	}
	a := newApp(buildSQL(3_000), sc)

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "db-k mygo spike",
			Width:   900,
			Height:  640,
			Content: rootView(a),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

`main.go` 追加 import `"time"`。删除 Task 4 里 `main.go` 的旧骨架与 `app` 定义（统一到此处）。

> 注意：`editor`/`app` 现在定义两处冲突？不——Task 4 只在 `editor.go` 定义了 `app` 与 `rootView`。本任务把 `app` 扩展（加 `findQuery/hasFindNow/sampler`）与 `rootView` 完善（加 HUD/按钮/查找）移到 `main.go`，需**从 `editor.go` 删除旧的 `app`/`rootView`**，只保留 `editor` 相关。同步更新 `editor_test.go` 里的 `app` 构造为 `newApp(text, sc)` 形式。

- [ ] **Step 6: 编译 + 全量测试**

```bash
/home/zk/Code/go/bin/go1.27.1 build ./... && /home/zk/Code/go/bin/go1.27.1 vet ./... && /home/zk/Code/go/bin/go1.27.1 test ./... -v
```

Expected: 全部 PASS（fixtures / highlight / ranges / editor / find / sampler）。

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -q -m "task6: full window, HUD, config buttons, find bar"
```

---

### Task 7: headless 基准矩阵 bench_test.go

**Files:**
- Create: `bench_test.go`
- Test/run: `go test ./... -run TestSpikeMatrix -v`

**Interfaces:**
- Consumes: `newApp`, `rootView`, `editor`, `strats`, `highlight`（全部前序任务）
- Produces: 无新 API；测试内输出三规模 × 三配置的：单键中值/90/99、滚动中值/90/99、稳态帧中值、注入 ranges 数。

- [ ] **Step 1: 写 `bench_test.go`（完整）**

```go
package main

import (
	"sort"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type cfgKey struct {
	name  string
	tok   editorScope
	inject editorScope
}

var matrixCfg = []cfgKey{
	{"FF", scopeFull, scopeFull},
	{"FV", scopeFull, scopeViewport},
	{"VV", scopeViewport, scopeViewport},
}

func sample(acc []time.Duration, d time.Duration) []time.Duration {
	// 跳过首样本（含 warmup/布局首帧噪声）
	if len(acc) < 2 {
		if len(acc) == 0 {
			return []time.Duration{d}
		}
		return append(acc, d)
	}
	return append(acc, d)
}

func median(acc []time.Duration) time.Duration {
	if len(acc) == 0 {
		return 0
	}
	s := make([]time.Duration, len(acc))
	copy(s, acc)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func pct(acc []time.Duration, p float64) time.Duration {
	if len(acc) == 0 {
		return 0
	}
	s := make([]time.Duration, len(acc))
	copy(s, acc)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(p * float64(len(s)))
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}

func measureOne(size int, ck cfgKey) (injected int, keys, scrolls, frames []time.Duration) {
	a := newApp(buildSQL(size), strats{tokenize: ck.tok, inject: ck.inject})
	tt := ui.NewTester(rootView(a), 900, 640)
	for range 4 {
		tt.Frame()
	}
	injected = a.ed.Injected

	_ = tt.Click("编辑器")
	// 单键：在文档末尾追加一个字符，各自计时
	for range 24 {
		t0 := time.Now()
		tt.Type("a")
		keys = append(keys, time.Since(t0))
	}
	// 滚动
	for range 12 {
		t0 := time.Now()
		tt.Scroll(0, 0, 0, 40)
		scrolls = append(scrolls, time.Since(t0))
	}
	// 稳态帧（含每帧 TextRanges 排序）
	for range 12 {
		t0 := time.Now()
		tt.Frame()
		frames = append(frames, time.Since(t0))
	}
	return injected, keys, scrolls, frames
}

func TestSpikeMatrix(t *testing.T) {
	sizes := []int{3_000, 12_000, 100_000}
	for _, size := range sizes {
		for _, ck := range matrixCfg {
			ck := ck
			injected, keys, scrolls, frames := measureOne(size, ck)
			t.Logf("size=%-6d cfg=%-3s injected=%-7d "+
				"key med=%-8v p90=%-8v p99=%-8v | "+
				"scroll med=%-8v p90=%-8v | "+
				"frame med=%-8v",
				size, ck.name, injected,
				median(keys), pct(keys, 0.9), pct(keys, 0.99),
				median(scrolls), pct(scrolls, 0.9),
				median(frames))
		}
	}
}
```

- [ ] **Step 2: 跑矩阵**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./... -run TestSpikeMatrix -v 2>&1 | tee /tmp/opencode/matrix.txt
```

Expected: 输出 9 行（3 规模 × 3 配置）的延迟矩阵。**不设通过/失败断言**——这是数据采集，失败条件由人工按门槛判定（见 Task 8）。若 `tt.Click("编辑器")` 找不到（label 匹配问题），改用 `tt.ClickAt` 或检查 `tt.Texts()` 后调整。

- [ ] **Step 3: 把原始样本也留档（每次全样倾倒到 /tmp/opencode）**

在上述测试末尾追加（记录全部样本的 CSV，供复盘）：

```go
	// 追加：
	f, _ := os.Create(fmt.Sprintf("/tmp/opencode/matrix-%d-%s.csv", size, ck.name))
	defer f.Close()
	for _, d := range keys {
		fmt.Fprintf(f, "key,%d\n", d.Nanoseconds())
	}
	for _, d := range scrolls {
		fmt.Fprintf(f, "scroll,%d\n", d.Nanoseconds())
	}
	for _, d := range frames {
		fmt.Fprintf(f, "frame,%d\n", d.Nanoseconds())
	}
```

`bench_test.go` 需追加 import `fmt`, `os`。

- [ ] **Step 4: Commit**

```bash
git add bench_test.go && git commit -q -m "task7: headless spike matrix benchmark"
```

---

### Task 8: 结果整理 + 报告

**Files:**
- Create: `docs/spike-results.md`
- Run: 复测矩阵一次并落盘

**Interfaces:**
- Consumes: Task 7 的矩阵输出与 CSV。
- Produces: 结论报告（三种判定之一 + 决策建议 + 对我 go 关键结论）。

- [ ] **Step 1: 复跑矩阵，收集全部输出**

```bash
/home/zk/Code/go/bin/go1.27.1 test ./... -run TestSpikeMatrix -v -count=2 2>&1 | tee /tmp/opencode/matrix-final.txt
```

- [ ] **Step 2: 按门槛判定**

| 规模 | FF 单键中值 | FV 单键中值 | VV 单键中值 | 判定 |
|---|---|---|---|---|
| 3k | <16ms | <16ms | <16ms | 可行基线 |
| 12k | <16ms 或 | FV 救回 <16ms 或 | <16ms | 可行 |
| 100k | 记录 | 记录 | 记录 | 记录 |

- 若 12k×FF > 16ms 且 12k×VV < 16ms → **结论 B：需视口策略才可行**（正结果，框架虚拟化成立）。
- 若 12k×VV 仍 > 16ms → **结论 C：不可行**（数据为否决证据）。
- 若 3k 全过且 12k×FF 也 < 16ms → **结论 A：可行（连全量都够）**。

- [ ] **Step 3: 写 `docs/spike-results.md`**

结构：环境（机器/Go/version）、矩阵表、每项解读（copy/tokenize/range 的三段式成本分解——由 `appLastBuild` 与注入数推断）、fixtures 简化说明（行内 SQL，无跨行块注释/多行字符串）、对 db-k 的启示（三条：a. mygo 虚拟化文本域根治整篇重排版；b. 视口注入规避 O(P×R)；c. 数据层/编辑器智能仍需自研）。

- [ ] **Step 4: Commit**

```bash
git add docs/spike-results.md && git commit -q -m "task8: spike results report"
```

---

## Self-Review

- **Spec 覆盖**：spec 的四项目标——①虚拟化让整篇重排版消失（Task 7 单键/帧测量）；②全量 vs 视口 ranges 对比（矩阵三配置）；③增量 tokenize 的必要性（editor 作用域 + appLastBuild 分解）；④三档规模门槛（Task 8 判定表）——均有对应任务。
- **占位符扫描**：无 TODO/TBD 占位；`editor.go` 中的 `appLastBuild` 占位在 Task 6 落地为实际引用，已注明。
- **类型一致性**：`editorScope`/`scopeViewport`/`scopeFull`、`strats{tokenize,inject}`、`NewEditor`、`rootView(a *app)`、`FindAll`、`Sampler.Median/P90/P99` 跨任务签名一致。查到的真实 API：`ui.TextLines.At(float64)`、`ui.Tester.Type/Click/Scroll/Frame`、`ui.RGB/RGBA` 均与计划内调用匹配。