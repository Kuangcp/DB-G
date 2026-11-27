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
	if !a.modal.DDLOpen {
		t.Fatal("Ctrl+Q should open the DDL panel")
	}
	if !strings.Contains(a.modal.DDLTitle, "users") {
		t.Fatalf("ddl title: %q", a.modal.DDLTitle)
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
	a.session.Mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	rec, err := a.session.Mgr.Create(prof.ID, "t")
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
	if !a.modal.DDLOpen {
		t.Fatal("Ctrl+Q 应打开光标处对象的 DDL 弹窗")
	}
	if !strings.Contains(a.modal.DDLTitle, "users") {
		t.Fatalf("ddl title: %q", a.modal.DDLTitle)
	}
	if !strings.Contains(a.modal.DDLText, "CREATE TABLE") {
		t.Fatalf("ddl text: %q", a.modal.DDLText)
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
	a.session.Mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	rec, err := a.session.Mgr.Create(prof.ID, "t")
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
	if !a.modal.DDLOpen {
		t.Fatal("Ctrl+Q should open the view DDL panel")
	}
	if !strings.Contains(strings.ToUpper(a.modal.DDLText), "CREATE VIEW") {
		t.Fatalf("view DDL expected, got %q", a.modal.DDLText)
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
	a.tree.Data = map[string]map[engine.ObjectKind][]engine.DBObject{
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
	if a.modal.DDLOpen {
		t.Fatalf("光标在关键字上不该弹 DDL：%q", a.modal.DDLTitle)
	}
}

// 左侧数据源列表通顶；标题栏下面是控制台标签行，再下面是数据源/数据库/执行。
func TestLayoutSidebarFullHeightConnRow(t *testing.T) {
	a, _ := topBarTestRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	entry, ok := tt.Find(locales.PaletteOpen())
	if !ok {
		t.Fatalf("标题栏没有命令面板入口: %v", tt.Texts())
	}
	tabs, ok := tt.Find(locales.TabToolbarLabel())
	if !ok {
		t.Fatalf("没有控制台标签行: %v", tt.Texts())
	}
	conn, ok := tt.Find("数据源 本地")
	if !ok {
		t.Fatalf("第二行没有数据源名: %v", tt.Texts())
	}
	newConn, ok := tt.Find(locales.SidebarNewConn())
	if !ok {
		t.Fatalf("侧栏缺少新建数据源按钮: %v", tt.Texts())
	}
	if newConn.Y >= TitleBarHeight {
		t.Fatalf("侧栏应通顶，新建按钮 y=%v 低于顶栏高度 %v", newConn.Y, TitleBarHeight)
	}
	if entry.Y+entry.H > TitleBarHeight+1 {
		t.Fatalf("命令面板入口应在标题栏里: entry=%v 顶栏高 %v", entry, TitleBarHeight)
	}
	if tabs.Y < TitleBarHeight {
		t.Fatalf("控制台标签应在标题栏下面的第二行: tabs=%v", tabs)
	}
	if conn.Y <= tabs.Y {
		t.Fatalf("数据源名应在标签行下面: conn=%v tabs=%v", conn, tabs)
	}
	exec, ok := tt.Find(locales.TopExec())
	if !ok {
		t.Fatalf("缺少执行按钮: %v", tt.Texts())
	}
	ws, ok := tt.Find("默认")
	if !ok {
		t.Fatalf("缺少工作区切换器: %v", tt.Texts())
	}
	if d := exec.Y - conn.Y; d < -8 || d > 8 {
		t.Fatalf("执行按钮应与数据源同一行: exec=%v conn=%v", exec, conn)
	}
	if exec.X <= conn.X {
		t.Fatalf("执行按钮应在数据源控件右侧: exec=%v conn=%v", exec, conn)
	}
	if d := ws.Y - conn.Y; d < -8 || d > 8 {
		t.Fatalf("工作区切换器应与数据源同一行: ws=%v conn=%v", ws, conn)
	}
	if ws.X <= exec.X {
		t.Fatalf("工作区切换器应在执行按钮右侧: ws=%v exec=%v", ws, exec)
	}
	for _, label := range []string{locales.TopFind(), locales.KeyFormat(), locales.TopAIComplete()} {
		tool, ok := tt.Find(label)
		if !ok {
			t.Fatalf("找不到编辑工具 %q: %v", label, tt.Texts())
		}
		if ws.X <= tool.X {
			t.Fatalf("工作区切换器应在编辑工具 %q 右侧: ws=%v tool=%v", label, ws, tool)
		}
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

// 查找/格式化/AI 补全在第二行右侧，不悬浮在编辑区上；工作区切换器在这行最右边。
func TestEditorToolsOnConnRow(t *testing.T) {
	a, _ := topBarTestRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	editor, ok := tt.Find("编辑器")
	if !ok {
		t.Fatalf("找不到编辑区: %v", tt.Texts())
	}
	conn, ok := tt.Find("数据源 本地")
	if !ok {
		t.Fatalf("找不到数据源名: %v", tt.Texts())
	}
	exec, ok := tt.Find(locales.TopExec())
	if !ok {
		t.Fatalf("缺少执行按钮: %v", tt.Texts())
	}
	ws, ok := tt.Find("默认")
	if !ok {
		t.Fatalf("缺少工作区切换器: %v", tt.Texts())
	}
	for _, label := range []string{locales.TopFind(), locales.KeyFormat(), locales.TopAIComplete()} {
		tool, ok := tt.Find(label)
		if !ok {
			t.Fatalf("找不到编辑工具 %q: %v", label, tt.Texts())
		}
		if d := tool.Y - conn.Y; d < -8 || d > 8 {
			t.Fatalf("%q 应与数据源同一行: tool=%v conn=%v", label, tool, conn)
		}
		if tool.X <= exec.X {
			t.Fatalf("%q 应在执行按钮右侧: tool=%v exec=%v", label, tool, exec)
		}
		if tool.Y+tool.H > editor.Y {
			t.Fatalf("%q 不该压在编辑区上: tool=%v editor=%v", label, tool, editor)
		}
		if tool.X+tool.W > ws.X {
			t.Fatalf("%q 应在工作区切换器左侧: tool=%v ws=%v", label, tool, ws)
		}
	}
}
