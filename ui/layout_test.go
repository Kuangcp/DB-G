package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

// 编辑器/结果分隔线按窗口高度取比例：窗口变矮时两栏等比缩小。
func TestEditorResultSplitIsProportional(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true,
		Columns: []string{"v"},
		Rows:    [][]any{{"alice"}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 900)
	tt.Frame()
	big, ok := tt.Find("编辑器")
	if !ok {
		t.Fatal("editor missing")
	}
	tt.SetSize(1200, 600)
	tt.Frame()
	small, ok := tt.Find("编辑器")
	if !ok {
		t.Fatal("editor missing after resize")
	}
	if small.H >= big.H {
		t.Fatalf("editor should shrink with the window: %v -> %v", big.H, small.H)
	}
	// 比例保持（工具栏等固定高度会带来一点偏差）。
	got := small.H / big.H
	want := float32(600) / 900
	if got < want-0.08 || got > want+0.08 {
		t.Fatalf("editor keeps its share: ratio %.3f want ~%.3f", got, want)
	}
}

// Alt+D 显示/隐藏结果区；再按一次恢复。
func TestAltDTogglesResults(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true,
		Columns: []string{"v"},
		Rows:    [][]any{{"alice"}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("alice") {
		t.Fatalf("results should show first: %q", tt.Texts())
	}
	tt.Key(ui.Alt, ui.KeyD)
	tt.Frame()
	if !a.resultsHidden || tt.HasText("alice") {
		t.Fatalf("Alt+D should hide the results: hidden=%v", a.resultsHidden)
	}
	if !tt.HasText("Ctrl+F / Ctrl+Space") {
		t.Fatal("editor tools should stay")
	}
	tt.Key(ui.Alt, ui.KeyD)
	tt.Frame()
	if a.resultsHidden || !tt.HasText("alice") {
		t.Fatalf("Alt+D again should show the results: hidden=%v", a.resultsHidden)
	}
}

// Ctrl+Q 打开所选对象的 DDL 弹窗。
func TestCtrlQShowsDDL(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	a.selConn = prof
	a.selObj = engine.DBObject{Kind: engine.ObjectTable, Name: "users"}
	a.hasSelObj = true
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyQ)
	tt.Frame()
	if !a.ddlOpen {
		t.Fatal("Ctrl+Q should open the DDL panel")
	}
	if !strings.Contains(a.ddlTitle, "users") {
		t.Fatalf("ddl title: %q", a.ddlTitle)
	}
}
