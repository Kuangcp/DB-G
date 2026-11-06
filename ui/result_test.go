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
	return newRoot("")
}

func TestResultPaneEmpty(t *testing.T) {
	a := newTestRoot()
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("请先连接数据源") {
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
	a.testSetSession("test", blockingFake{})
	if !a.con.Run(a.sessionFor("test"), "SELECT 1") {
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
		t.Fatalf("binary []byte → %q", got)
	}
	// MySQL 的 TEXT/VARCHAR 以 []byte 返回：必须渲染真实文本，而不是字节数摘要。
	if got := cellText([]byte("hello 世界")); got != "hello 世界" {
		t.Fatalf("text []byte → %q", got)
	}
	if got := cellText([]byte("a\x00b")); got != "[3 bytes]" {
		t.Fatalf("NUL bytes must stay binary → %q", got)
	}
	if got := cellText([]byte{0xff, 0xfe}); got != "[2 bytes]" {
		t.Fatalf("invalid UTF-8 must stay binary → %q", got)
	}
	longBytes := []byte(strings.Repeat("好", 600))
	cut := cellText(longBytes)
	if !strings.HasSuffix(cut, "…") || len([]rune(cut)) != 513 {
		t.Fatalf("long text []byte must be cut like string, runes=%d", len([]rune(cut)))
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

func TestDisplayWidthCountsWideRunes(t *testing.T) {
	cases := map[string]int{
		"":           0,
		"abc":        3,
		"中文":         4,
		"a中b":        4,
		"2026-11-04": 10,
		"ＡＢ":         4, // 全角
	}
	for in, want := range cases {
		if got := displayWidth(in); got != want {
			t.Fatalf("displayWidth(%q) = %d, want %d", in, got, want)
		}
	}
}

func resultWith(cols []string, rows ...[]any) *engine.QueryResult {
	return &engine.QueryResult{IsQuery: true, Columns: cols, Rows: rows}
}

func TestResultWidthsFollowValues(t *testing.T) {
	r := resultWith([]string{"id", "name", "memo"},
		[]any{int64(1), "短", strings.Repeat("长", 200)},
		[]any{int64(2), "一个比较长的名字", ""},
	)
	w := resultWidths(r)
	if len(w) != 3 {
		t.Fatalf("widths len = %d", len(w))
	}
	if w[0] != minColumnWidth {
		t.Fatalf("id column must fall back to the minimum: %v", w[0])
	}
	if w[1] <= minColumnWidth || w[1] >= maxColumnWidth {
		t.Fatalf("name column must fit its widest value: %v", w[1])
	}
	if w[2] != maxColumnWidth {
		t.Fatalf("memo column must be capped at the maximum: %v", w[2])
	}
	if w[1] >= w[2] {
		t.Fatalf("wide value must be wider than a short one: %v vs %v", w[1], w[2])
	}
}

func TestResultWidthsCountHeader(t *testing.T) {
	r := resultWith([]string{"一个很长的列标题名字"}, []any{"1"})
	w := resultWidths(r)
	if w[0] <= minColumnWidth {
		t.Fatalf("header must widen the column: %v", w[0])
	}
}

func TestResultWidthsSeeRenderedTextNotByteCounts(t *testing.T) {
	// MySQL 的 TEXT 以 []byte 返回：宽度必须按渲染后的字符算，不是 "[N bytes]"。
	r := resultWith([]string{"t"}, []any{[]byte("abcdef")}, []any{[]byte("x")})
	w := resultWidths(r)
	if want := float32(6*charWidth + cellPad); w[0] < want || w[0] > want+1 {
		t.Fatalf("text []byte width = %v, want ~%v", w[0], want)
	}
}

func TestResultWidthsCachedPerResult(t *testing.T) {
	a := newTestRoot()
	r := resultWith([]string{"c"}, []any{"v"})
	first := a.columnWidths(r)
	second := a.columnWidths(r)
	if &first[0] != &second[0] {
		t.Fatal("widths must be cached for the same result")
	}
	other := resultWith([]string{"c"}, []any{"a much longer value here"})
	if w := a.columnWidths(other); w[0] <= first[0] {
		t.Fatalf("new result must be measured again: %v vs %v", w[0], first[0])
	}
}

func TestResultPaneRendersTextBytes(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(resultWith([]string{"v"}, []any{[]byte("真实文本")}), "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("真实文本") {
		t.Fatalf("text cell missing: %q", tt.Texts())
	}
	if tt.HasText("bytes]") {
		t.Fatalf("byte summary must not show for text: %q", tt.Texts())
	}
}

func TestResultCopyRowInsertViaMenu(t *testing.T) {
	a := openForSelection(t)
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (name) VALUES ('alice')"); err != nil {
		t.Fatal(err)
	}
	a.setActiveText("SELECT id, name FROM users ORDER BY id")
	a.setSelection(0, len([]rune("SELECT id, name FROM users ORDER BY id")))
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.runSQL()
	pollResult(t, a, tt)
	tt.Frame()
	if err := tt.RightClick("alice"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("复制本行 → INSERT"); err != nil {
		t.Fatal(err)
	}
	got := tt.Clipboard()
	if !strings.HasPrefix(got, `INSERT INTO "users" (`) || !strings.Contains(got, "'alice'") {
		t.Fatalf("clipboard: %q", got)
	}
}

func TestResultCellViewerShowsFullValue(t *testing.T) {
	a := newTestRoot()
	long := strings.Repeat("长", 40)
	a.con.SetOutcome(resultWith([]string{"note"}, []any{long}), "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if err := tt.RightClick(long); err != nil {
		t.Fatalf("right click failed: %v", err)
	}
	if err := tt.ChooseMenuItem("查看完整内容"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(long) {
		t.Fatalf("viewer must show the full value: %q", tt.Texts())
	}
}

// 结果表可以选中单个单元格：点击同时移动行/列光标，Ctrl+C 只复制该格。
func TestResultCellSelectionAndCopy(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true,
		Columns: []string{"id", "name"},
		Rows:    [][]any{{int64(1), "alice"}, {int64(2), "bob"}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if err := tt.Click("bob"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.resultRow != 1 {
		t.Fatalf("row cursor: %d", a.resultRow)
	}
	if got := a.resultState.Cells.Cursor; got != "name" {
		t.Fatalf("column cursor: %q", got)
	}
	if !a.resultSel.Has(1) {
		t.Fatal("the clicked row is not chosen")
	}
	if a.resultState.CellsStyle.Cell == (ui.Color{}) || a.resultState.CellsStyle.Row == (ui.Color{}) {
		t.Fatal("result table must paint chosen cells and rows with CellsStyle")
	}
	tt.Key(ui.Ctrl, ui.KeyC)
	tt.Frame()
	if got := tt.Clipboard(); got != "bob" {
		t.Fatalf("Ctrl+C copied %q, want the chosen cell only", got)
	}
}

// 查看完整内容：合法 JSON 渲染成可折叠树，折叠后隐藏子节点。
func TestCellViewerJSONTree(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true,
		Columns: []string{"payload"},
		Rows:    [][]any{{`{"user":{"name":"alice","age":30},"ok":true}`}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	a.openCellViewer(a.con.Result(), 0, 0)
	tt.Frame()
	if !a.cellViewJSONOK {
		t.Fatal("JSON should render as a tree")
	}
	if !tt.HasText("user") || !tt.HasText(`"alice"`) {
		t.Fatalf("json tree: %q", tt.Texts())
	}
	if err := tt.Click("收起：user"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("name:") || tt.HasText("age:") {
		t.Fatalf("collapsed node still shows its children: %q", tt.Texts())
	}
	if err := tt.Click("复制原文"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); !strings.Contains(got, `"alice"`) {
		t.Fatalf("copy original: %q", got)
	}
}
