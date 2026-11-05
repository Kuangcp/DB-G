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
	a.setActiveText(text)
	cut := strings.Index(text, "SELECT name")
	a.setSelection(0, len([]rune(text[:cut-1])))
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
	a.setActiveText("SELECT name FROM users")
	a.setSelection(0, 0)
	a.runSQL()
	if a.con.Running() || a.con.Result() != nil || a.con.Err() != "" {
		t.Fatal("empty selection must not start execution")
	}
}

func TestExecuteButtonDisabledWithoutSelection(t *testing.T) {
	a := openForSelection(t)
	a.setActiveText("SELECT name FROM users")
	a.setSelection(0, 0)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if a.hasSelection() {
		t.Fatal("precondition: no selection")
	}
	// 无选区：点「执行」不得开始执行（按钮禁用）
	_ = tt.Click("执行")
	if a.con.Running() || a.con.Result() != nil || a.con.Err() != "" {
		t.Fatal("执行 must do nothing without a selection")
	}
	// 有选区（真实 select-all）：按钮可点并开始执行
	tt.Click("编辑器")
	tt.Command("selectAll")
	tt.Frame()
	if !a.hasSelection() {
		t.Fatal("selectAll must produce a non-empty selection")
	}
	if err := tt.Click("执行"); err != nil {
		t.Fatalf("enabled 执行 must be clickable: %v", err)
	}
	pollResult(t, a, tt)
	if a.con.Err() != "" {
		t.Fatalf("exec err: %s", a.con.Err())
	}
	if a.con.Result() == nil {
		t.Fatal("enabled click must start a run")
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
	a.setSelection(0, 0)
	tt.Frame()
	if !tt.HasText("请先选中要执行的 SQL") {
		t.Fatalf("no-selection hint missing: %q", tt.Texts())
	}
}

func TestCtrlEnterRunsSelection(t *testing.T) {
	a := openForSelection(t)
	a.setActiveText("SELECT 1 AS x")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	// 先聚焦编辑器（点击会移动光标），再选区，否则选区会被点击清掉。
	tt.Click("编辑器")
	tt.Frame()
	a.setSelection(0, len([]rune("SELECT 1 AS x")))
	tt.Frame()
	before := a.activeText()
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Frame()
	pollResult(t, a, tt)
	if got := a.activeText(); got != before {
		t.Fatalf("Ctrl+Enter must not edit the text: %q", got)
	}
	r := a.con.Result()
	if a.con.Err() != "" {
		t.Fatalf("exec err: %s", a.con.Err())
	}
	if r == nil || len(r.Columns) != 1 || r.Columns[0] != "x" {
		t.Fatalf("Ctrl+Enter must run the selection: %+v", r)
	}
}
