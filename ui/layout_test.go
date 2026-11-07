package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
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
	if !tt.HasText("编辑器工具") {
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

// Ctrl+Q：光标停在编辑器里的表名上，即使左侧树没选中任何对象也要能看 DDL。
func TestCtrlQFromEditorCaret(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	// 激活控制台绑定到该连接（只连库、不选中任何树节点）。
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir()})
	rec, err := a.mgr.Create(prof.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	a.hasSelObj = false
	a.setActiveText("select * from users;")
	a.setSelection(len([]rune("select * from use")), len([]rune("select * from use")))

	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if word := editorWordAtCaret(a.code); word != "users" {
		t.Fatalf("word at caret: %q", word)
	}
	tt.Key(ui.Ctrl, ui.KeyQ)
	tt.Frame()
	if !a.ddlOpen {
		t.Fatal("Ctrl+Q 应打开光标处对象的 DDL 弹窗")
	}
	if !strings.Contains(a.ddlTitle, "users") {
		t.Fatalf("ddl title: %q", a.ddlTitle)
	}
	if !strings.Contains(a.ddlText, "CREATE TABLE") {
		t.Fatalf("ddl text: %q", a.ddlText)
	}
}

// 光标停在词的不同位置都要取到整词；不在词上返回空。
func TestEditorWordAtCaret(t *testing.T) {
	a := newConsoleRoot(t)
	text := "select * from category where id = 1;"
	a.setActiveText(text)
	cases := []struct {
		caret int
		want  string
	}{
		{0, "select"},                // 词首
		{3, "select"},                // 词中
		{6, "select"},                // 词末（空格前）
		{7, ""},                      // 空白处
		{14, "category"},             // 词首
		{17, "category"},             // 词中
		{21, "category"},             // 词末
		{len([]rune(text)) - 1, "1"}, // 数字也算标识符
	}
	for _, c := range cases {
		a.setSelection(c.caret, c.caret)
		if got := editorWordAtCaret(a.code); got != c.want {
			t.Fatalf("caret %d: got %q want %q", c.caret, got, c.want)
		}
	}
}

// 对象树已加载、光标处的词不是表/视图时，Ctrl+Q 不弹「未找到」的弹窗。
func TestCtrlQIgnoresNonTableWord(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	a.treeData = map[string]map[engine.ObjectKind][]engine.DBObject{
		objectTreeKey(prof.ID, ""): {
			engine.ObjectTable: {{Kind: engine.ObjectTable, Name: "users"}},
		},
	}
	a.setActiveText("select * from users;")
	a.setSelection(2, 2) // 光标在 select 上

	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyQ)
	tt.Frame()
	if a.ddlOpen {
		t.Fatalf("光标在关键字上不该弹 DDL：%q", a.ddlTitle)
	}
}
