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

	// Disabled elements are excluded from the focus order; walk Tab until
	// focusing the editor auto-selects its text (button then legitimately
	// enables) and assert 执行 never took focus along the way.
	sawFocus := false
	for range 32 {
		if tt.Focused("执行") {
			sawFocus = true
			break
		}
		if a.selStart != a.selEnd {
			break
		}
		tt.Key(0, ui.KeyTab)
	}
	if sawFocus {
		t.Fatal("执行 must be disabled (unfocusable) without selection")
	}
	if a.selStart == a.selEnd {
		t.Fatal("tab walk must reach the editor (auto selectAll)")
	}

	// Positive control: a real UI selection enables the button.
	tt.Click("编辑器")
	tt.Command("selectAll")
	tt.Frame()
	if a.selStart == a.selEnd {
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
	a.selStart, a.selEnd = 0, 0
	tt.Frame()
	if !tt.HasText("请先选中要执行的 SQL") {
		t.Fatalf("no-selection hint missing: %q", tt.Texts())
	}
}
