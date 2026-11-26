package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
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
	// 编辑器按设置比例分配“窗口变矮减少的高度”；标题栏/工具栏/常驻状态栏是固定 chrome。
	got := (big.H - small.H) / float32(900-600)
	want := a.editorPercent / 100
	if got < want-0.06 || got > want+0.06 {
		t.Fatalf("editor keeps its share: shrunk %.3f want ~%.3f", got, want)
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
	if _, ok := tt.Find(locales.TopFind()); !ok {
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
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
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

// Ctrl+Q：光标处的词是视图时，按视图取 DDL，而不是当成表。
func TestCtrlQFromEditorCaretOnView(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, err := a.testSession(t).RunStatement(t.Context(),
		`CREATE VIEW user_view AS SELECT id FROM users`); err != nil {
		t.Fatal(err)
	}
	a.ensureCatalog(prof.ID) // headless：同步把表/视图装进目录
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	rec, err := a.mgr.Create(prof.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	a.hasSelObj = false
	a.setActiveText("select * from user_view;")
	a.setSelection(len([]rune("select * from user_vi")), len([]rune("select * from user_vi")))

	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyQ)
	tt.Frame()
	if !a.ddlOpen {
		t.Fatal("Ctrl+Q should open the view DDL panel")
	}
	if !strings.Contains(strings.ToUpper(a.ddlText), "CREATE VIEW") {
		t.Fatalf("view DDL expected, got %q", a.ddlText)
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

// 左侧数据源列表通顶，顶栏只覆盖编辑区；执行/分析跟着控制台标签行。
func TestLayoutSidebarFullHeightExecOnTabRow(t *testing.T) {
	a, _ := topBarTestRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	conn, ok := tt.Find("数据源 本地")
	if !ok {
		t.Fatalf("顶栏没有数据源名: %v", tt.Texts())
	}
	newConn, ok := tt.Find(locales.SidebarNewConn())
	if !ok {
		t.Fatalf("侧栏缺少新建数据源按钮: %v", tt.Texts())
	}
	if newConn.Y >= TitleBarHeight {
		t.Fatalf("侧栏应通顶，新建按钮 y=%v 低于顶栏高度 %v", newConn.Y, TitleBarHeight)
	}
	if conn.Y >= TitleBarHeight {
		t.Fatalf("数据源名应在顶栏里，y=%v", conn.Y)
	}
	exec, ok := tt.Find(locales.TopExec())
	if !ok {
		t.Fatalf("缺少执行按钮: %v", tt.Texts())
	}
	ws, ok := tt.Find("默认")
	if !ok {
		t.Fatalf("缺少工作区切换器: %v", tt.Texts())
	}
	if exec.Y <= conn.Y {
		t.Fatalf("执行按钮应在标签行（顶栏下方），exec y=%v conn y=%v", exec.Y, conn.Y)
	}
	if d := exec.Y - ws.Y; d < -6 || d > 6 {
		t.Fatalf("执行按钮应与工作区切换器同一行: exec=%v ws=%v", exec, ws)
	}
	if exec.X <= ws.X {
		t.Fatalf("执行按钮应在工作区切换器右侧: exec=%v ws=%v", exec, ws)
	}
}

// 侧栏顶部的图标能隐藏数据源面板，隐藏后顶栏左上角出现「显示」入口。
func TestSidebarToggleHidesAndShowsPanel(t *testing.T) {
	a, _ := topBarTestRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if _, ok := tt.Find(locales.SidebarSearchObjects()); !ok {
		t.Fatalf("侧栏搜索框应可见: %v", tt.Texts())
	}
	if err := tt.Click(locales.SidebarHide()); err != nil {
		t.Fatalf("点击隐藏面板: %v", err)
	}
	tt.Frame()
	if !a.sidebarHidden {
		t.Fatal("点击后应隐藏侧栏")
	}
	if _, ok := tt.Find(locales.SidebarSearchObjects()); ok {
		t.Fatalf("隐藏后不该再有侧栏搜索框: %v", tt.Texts())
	}
	if err := tt.Click(locales.SidebarShow()); err != nil {
		t.Fatalf("顶栏应有显示面板按钮: %v", err)
	}
	tt.Frame()
	if a.sidebarHidden {
		t.Fatal("点击显示后应恢复侧栏")
	}
	if _, ok := tt.Find(locales.SidebarSearchObjects()); !ok {
		t.Fatalf("恢复后侧栏搜索框应可见: %v", tt.Texts())
	}
}

// 查找/格式化/AI 补全悬浮在编辑区内部，不再占一整行。
func TestFloatingEditorToolsOverEditor(t *testing.T) {
	a, _ := topBarTestRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	editor, ok := tt.Find("编辑器")
	if !ok {
		t.Fatalf("找不到编辑区: %v", tt.Texts())
	}
	for _, label := range []string{locales.TopFind(), locales.KeyFormat(), locales.TopAIComplete()} {
		tool, ok := tt.Find(label)
		if !ok {
			t.Fatalf("找不到悬浮按钮 %q: %v", label, tt.Texts())
		}
		if tool.Y < editor.Y || tool.Y+tool.H > editor.Y+editor.H {
			t.Fatalf("%q 应悬浮在编辑区内: tool=%v editor=%v", label, tool, editor)
		}
		if tool.X < editor.X || tool.X+tool.W > editor.X+editor.W {
			t.Fatalf("%q 应悬浮在编辑区内: tool=%v editor=%v", label, tool, editor)
		}
	}
}
