package ui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
)

// viewerFakeSession 是表视图测试用的假会话：记录执行过的 SQL，
// 提供对象树不需要的列/DDL 与固定结果。
type viewerFakeSession struct {
	mu      sync.Mutex
	queries []string
	result  *engine.QueryResult
	err     error
	cols    []engine.Column
	indexes []engine.Index
	ddl     string
	ddlErr  error
}

func (s *viewerFakeSession) Open(context.Context) error { return nil }
func (s *viewerFakeSession) Close() error               { return nil }
func (s *viewerFakeSession) Cancel() bool               { return false }
func (s *viewerFakeSession) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapColumns | engine.CapIndexes | engine.CapObjectDDL
}
func (s *viewerFakeSession) RunStatement(_ context.Context, sql string) (*engine.QueryResult, error) {
	s.mu.Lock()
	s.queries = append(s.queries, sql)
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if s.result == nil {
		return &engine.QueryResult{IsQuery: true}, nil
	}
	return s.result, nil
}
func (s *viewerFakeSession) Columns(context.Context, string) ([]engine.Column, error) {
	return s.cols, nil
}
func (s *viewerFakeSession) Indexes(context.Context, string) ([]engine.Index, error) {
	return s.indexes, nil
}
func (s *viewerFakeSession) ObjectDDL(context.Context, engine.ObjectKind, string) (string, error) {
	return s.ddl, s.ddlErr
}
func (s *viewerFakeSession) lastQuery() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) == 0 {
		return ""
	}
	return s.queries[len(s.queries)-1]
}

// viewerRoot 往连接库里加一个数据源并挂上假会话；返回 root、数据源与标签 id。
func viewerRoot(t *testing.T, sess engine.Session, obj engine.DBObject) (*root, app.ConnProfile, string) {
	t.Helper()
	a := newSidebarRoot(t)
	if err := a.store.Add(app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}); err != nil {
		t.Fatal(err)
	}
	p := a.store.List()[0]
	a.testSetSession(p.ID, sess)
	return a, p, viewerTabID(p.ID, "", obj)
}

// waitViewer 等数据页读完（假会话/真会话都在后台协程执行）。
func waitViewer(t *testing.T, a *root, id string) *viewerTab {
	t.Helper()
	tb := a.tabs.ViewerTabs[id]
	if tb == nil {
		t.Fatalf("viewer tab %q missing", id)
	}
	for range 400 {
		if !tb.query.loading && tb.rt != nil && (tb.rt.Result() != nil || tb.rt.Err() != "") {
			return tb
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting viewer data: loading=%v err=%q", tb.query.loading, tb.query.err)
	return nil
}

// 双击树里的表：打开一个表视图标签；再双击同一张表只激活已有标签。
func TestViewerDoubleClickOpensOneTab(t *testing.T) {
	a := newSidebarRoot(t)
	if err := a.store.Add(app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}); err != nil {
		t.Fatal(err)
	}
	p := a.store.List()[0]
	a.testSetSession(p.ID, &viewerFakeSession{result: &engine.QueryResult{IsQuery: true, Columns: []string{"id"}}})
	a.tree.Expand[p.ID] = true
	a.groupOpenPtr(p.ID+"|"+kindKey(engine.ObjectTable), true)
	a.tree.Data = map[string]map[engine.ObjectKind][]engine.DBObject{
		objectTreeKey(p.ID, ""): {engine.ObjectTable: {{Kind: engine.ObjectTable, Name: "users"}}},
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	rect, ok := tt.Find("users")
	if !ok {
		t.Fatalf("树里没有 users: %q", tt.Texts())
	}
	tt.ClickAt(rect.X+2, rect.Y+rect.H/2)
	tt.Frame()
	if len(a.tabs.ViewerTabs) != 0 {
		t.Fatal("单击不该打开表视图")
	}
	tt.ClickAt(rect.X+2, rect.Y+rect.H/2)
	tt.ClickAt(rect.X+2, rect.Y+rect.H/2)
	tt.Frame()
	id := viewerTabID(p.ID, "", engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	if len(a.tabs.ViewerTabs) != 1 || a.tabs.ActiveViewer != id {
		t.Fatalf("双击应开一个表视图: tabs=%d active=%q", len(a.tabs.ViewerTabs), a.tabs.ActiveViewer)
	}
	// 再双击一次：仍是同一个标签。
	tt.ClickAt(rect.X+2, rect.Y+rect.H/2)
	tt.ClickAt(rect.X+2, rect.Y+rect.H/2)
	tt.Frame()
	if len(a.tabs.ViewerTabs) != 1 || len(a.tabs.ViewerOrder) != 1 {
		t.Fatalf("重复双击应复用标签: tabs=%d order=%d", len(a.tabs.ViewerTabs), len(a.tabs.ViewerOrder))
	}
}

// 数据页 SQL：WHERE / 服务端排序 / 分页偏移；复制与带去控制台用不带分页的 SQL。
func TestViewerSQLComposition(t *testing.T) {
	fake := &viewerFakeSession{result: &engine.QueryResult{IsQuery: true, Columns: []string{"id", "name"}}}
	a, p, id := viewerRoot(t, fake, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	a.openViewerTab(p, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	tb := waitViewer(t, a, id)
	if !strings.Contains(fake.lastQuery(), `SELECT * FROM "users"`) {
		t.Fatalf("首查 SQL: %q", fake.lastQuery())
	}

	tb.query.whereIn, tb.query.where = "name = 'a'", "name = 'a'"
	tb.query.sortBy = []ui.SortOrder{{Column: "name", Descending: true}}
	tb.query.offset = 500
	a.viewerReload(tb, nil)
	waitViewerQuiet(t, tb)
	q := fake.lastQuery()
	for _, want := range []string{`WHERE name = 'a'`, `ORDER BY "name" DESC`, "LIMIT 500 OFFSET 500"} {
		if !strings.Contains(q, want) {
			t.Fatalf("查询缺少 %q: %s", want, q)
		}
	}
	full, err := a.viewerSQL(tb, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(full, "LIMIT") || !strings.Contains(full, `ORDER BY "name" DESC`) {
		t.Fatalf("复制用的 SQL 不该带分页: %q", full)
	}
}

// WHERE 里追加第二条语句必须被拒绝，且不发给数据库。
func TestViewerRejectsExtraStatement(t *testing.T) {
	fake := &viewerFakeSession{result: &engine.QueryResult{IsQuery: true, Columns: []string{"id"}}}
	a, p, id := viewerRoot(t, fake, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	obj := engine.DBObject{Kind: engine.ObjectTable, Name: "users"}
	a.openViewerTab(p, obj)
	tb := waitViewer(t, a, id)
	before := len(fake.queries)
	tb.query.where = "1=1; DROP TABLE users"
	a.viewerReload(tb, nil)
	if tb.query.err == "" {
		t.Fatal("追加语句应被拒绝")
	}
	if len(fake.queries) != before {
		t.Fatalf("被拒绝的条件不该发给数据库: %v", fake.queries)
	}
}

// 复制 SQL / 在控制台打开：走同一份生成 SQL，控制台被创建并排队追加。
func TestViewerOpenInConsole(t *testing.T) {
	fake := &viewerFakeSession{result: &engine.QueryResult{IsQuery: true, Columns: []string{"id"}}}
	a, p, id := viewerRoot(t, fake, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	obj := engine.DBObject{Kind: engine.ObjectTable, Name: "users"}
	a.openViewerTab(p, obj)
	tb := waitViewer(t, a, id)
	tb.query.where = "id > 1"
	sql, err := a.viewerSQL(tb, false)
	if err != nil {
		t.Fatal(err)
	}
	a.openSQLInConsole(p.ID, sql+"\n")
	if len(a.deferred) != 1 {
		t.Fatalf("应排队在控制台追加，deferred=%d", len(a.deferred))
	}
	a.drainDeferred()
	if a.tabs.ActiveConsole == "" || !strings.Contains(a.activeText(), sql) {
		t.Fatalf("应在控制台追加: active=%q text=%q", a.tabs.ActiveConsole, a.activeText())
	}
}

// 表视图的数据页可编辑：改动提交后写进数据库。
func TestViewerDataIsEditable(t *testing.T) {
	a := newSidebarRoot(t)
	prof := seedConsoleConn(t, a, "lib")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1, 'alice')"); err != nil {
		t.Fatal(err)
	}
	obj := engine.DBObject{Kind: engine.ObjectTable, Name: "users"}
	a.openViewerTab(prof, obj)
	id := viewerTabID(prof.ID, "", obj)
	tb := waitViewer(t, a, id)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if tb.rt.Result() == nil || len(tb.rt.Result().Rows) == 0 {
		t.Fatal("数据页没读到行")
	}
	if a.grid.plan == nil {
		t.Fatalf("表视图数据页应可编辑: %q", a.grid.readonly)
	}
	a.setCellEdit(0, 1, "alice2")
	if err := a.applyEditsSync(); err != nil {
		t.Fatal(err)
	}
	r, err := a.testSession(t).RunStatement(t.Context(), "SELECT name FROM users WHERE id = 1")
	if err != nil || r.Rows[0][0] != "alice2" {
		t.Fatalf("数据库值: %+v err=%v", r, err)
	}
}

// 结构页读字段；DDL 页与 Ctrl+Q 同源。
func TestViewerStructureAndDDL(t *testing.T) {
	fake := &viewerFakeSession{
		result: &engine.QueryResult{IsQuery: true, Columns: []string{"id", "name"}},
		cols: []engine.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
			{Name: "name", Type: "TEXT", Nullable: true, Comment: "用户姓名"},
		},
		indexes: []engine.Index{
			{Name: "idx_name", Columns: []string{"name"}, Unique: true, Type: "BTREE"},
			{Name: "idx_age", Columns: []string{"age DESC"}, Type: "BTREE", Predicate: "age > 18"},
		},
		ddl: "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);",
	}
	a, p, id := viewerRoot(t, fake, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	obj := engine.DBObject{Kind: engine.ObjectTable, Name: "users"}
	a.openViewerTab(p, obj)
	tb := waitViewer(t, a, id)
	tt := ui.NewTester(rootView(a), 1400, 800)

	tb.page = viewerStructure
	a.ensureViewerPage(tb)
	tt.Frame()
	for _, want := range []string{
		"id", "INTEGER", "name", "TEXT", "PK", locales.ViewerColComment(), "用户姓名",
		locales.ViewerFieldsTitle(), locales.ViewerIndexesTitle(),
		locales.ViewerIndexName(), locales.ViewerIndexColumns(), locales.ViewerIndexUnique(),
		locales.ViewerIndexPrimary(), locales.ViewerIndexType(), locales.ViewerIndexPredicate(),
		"idx_name", "idx_age", "age > 18",
	} {
		if !tt.HasText(want) {
			t.Fatalf("结构页缺少 %q: %q", want, tt.Texts())
		}
	}

	tb.page = viewerDDL
	a.ensureViewerPage(tb)
	tt.Frame()
	if tb.ddl == "" || !strings.Contains(tb.ddl, "CREATE TABLE") {
		t.Fatalf("DDL 没读到: %q err=%q", tb.ddl, tb.ddlErr)
	}
}

// 关闭当前表视图回到控制台；标签行重新显示控制台。
func TestViewerCloseFallsBackToConsole(t *testing.T) {
	a := newSidebarRoot(t)
	prof := seedConsoleConn(t, a, "lib")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	rec, err := a.mgr.Create(prof.ID, "q")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	fake := &viewerFakeSession{result: &engine.QueryResult{IsQuery: true, Columns: []string{"id"}}}
	a.testSetSession(prof.ID, fake) // 覆盖会话：数据页不碰真实库
	obj := engine.DBObject{Kind: engine.ObjectTable, Name: "users"}
	a.openViewerTab(prof, obj)
	id := viewerTabID(prof.ID, "", obj)
	waitViewer(t, a, id)
	if a.tabs.ActiveViewer != id {
		t.Fatal("表视图应处于激活状态")
	}
	a.closeViewerTab(id)
	if a.tabs.ActiveViewer != "" || len(a.tabs.ViewerTabs) != 0 {
		t.Fatalf("关闭后应回到控制台: active=%q tabs=%d", a.tabs.ActiveViewer, len(a.tabs.ViewerTabs))
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("lib") {
		t.Fatalf("控制台标签应回来: %q", tt.Texts())
	}
}

// 表视图 WHERE 输入框：补全当前结果集的列名，只替换光标所在的词。
func TestViewerWhereColumnCompletion(t *testing.T) {
	fake := &viewerFakeSession{result: &engine.QueryResult{IsQuery: true, Columns: []string{"id", "name", "status"}}}
	a, p, id := viewerRoot(t, fake, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	a.openViewerTab(p, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	tb := waitViewer(t, a, id)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	rect, ok := tt.Find("WHERE")
	if !ok {
		t.Fatalf("WHERE 标签缺失: %q", tt.Texts())
	}
	tt.ClickAt(rect.X+rect.W+24, rect.Y+rect.H/2)
	tt.Frame()
	tt.Type("id > 1 AND na")
	tt.Frame()
	if !tt.HasText("name") {
		t.Fatalf("条件输入应有列名补全: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got, want := tb.query.whereIn, "id > 1 AND name"; got != want {
		t.Fatalf("补全应只替换当前词: got %q want %q", got, want)
	}
	if tb.query.where != "" {
		t.Fatalf("选择补全不应直接执行: %q", tb.query.where)
	}
	// 列名已完整时不再提示自己，下一次 Enter 直接应用条件。
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got, want := tb.query.where, "id > 1 AND name"; got != want {
		t.Fatalf("完整列名后 Enter 应应用条件: got %q want %q", got, want)
	}
	waitViewerQuiet(t, tb)
}

// 数据页工具条：WHERE、排序、复制 SQL、在控制台打开、翻页都在。
func TestViewerDataToolbarVisible(t *testing.T) {
	fake := &viewerFakeSession{result: &engine.QueryResult{IsQuery: true, Columns: []string{"id", "name"}}}
	a, p, id := viewerRoot(t, fake, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	a.openViewerTab(p, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	waitViewer(t, a, id)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText(locales.ViewerData()) || !tt.HasText(locales.ViewerStructure()) || !tt.HasText(locales.ViewerDDL()) {
		t.Fatalf("子页签缺失: %q", tt.Texts())
	}
	for _, label := range []string{locales.ViewerSort(), locales.ViewerCopySQL(), locales.ViewerOpenInConsole()} {
		if !tt.HasText(label) {
			t.Fatalf("工具条缺少 %q: %q", label, tt.Texts())
		}
	}
}

// 表视图里的快捷键：Ctrl+Q 切到 DDL 页，Ctrl+Enter 重新读数据。
func TestViewerKeys(t *testing.T) {
	fake := &viewerFakeSession{result: &engine.QueryResult{IsQuery: true, Columns: []string{"id"}}}
	a, p, id := viewerRoot(t, fake, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	a.openViewerTab(p, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	tb := waitViewer(t, a, id)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	before := len(fake.queries)
	tt.Key(ui.Ctrl, ui.KeyEnter)
	waitViewerQuiet(t, tb)
	if len(fake.queries) <= before {
		t.Fatal("Ctrl+Enter 应重新读数据")
	}
	tt.Key(ui.Ctrl, ui.KeyQ)
	tt.Frame()
	if tb.page != viewerDDL {
		t.Fatalf("Ctrl+Q 应切到 DDL 页: page=%d", tb.page)
	}
}

func waitViewerQuiet(t *testing.T, tb *viewerTab) {
	t.Helper()
	for range 400 {
		if !tb.query.loading {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timeout waiting viewer query")
}

// 控制台与表视图的网格状态互不串：切过去再切回来，各自的筛选/条件还在。
func TestGridStateSurvivesTabSwitch(t *testing.T) {
	a := newSidebarRoot(t)
	prof := seedConsoleConn(t, a, "lib")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	rec, err := a.mgr.Create(prof.ID, "q")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	// 控制台结果：塞一份结果并把列筛选设上。
	a.con.SetOutcome(&engine.QueryResult{IsQuery: true, Columns: []string{"id", "name"},
		Rows: [][]any{{int64(1), "alice"}}}, "")
	a.grid.view.setFilter(1, "ali")

	obj := engine.DBObject{Kind: engine.ObjectTable, Name: "users"}
	a.testSetSession(prof.ID, &viewerFakeSession{result: &engine.QueryResult{IsQuery: true,
		Columns: []string{"id"}, Rows: [][]any{{int64(1)}}}})
	a.openViewerTab(prof, obj)
	id := viewerTabID(prof.ID, "", obj)
	tb := waitViewer(t, a, id)
	tb.query.where = "id = 1"
	if _, err := a.viewerSQL(tb, false); err != nil {
		t.Fatal(err)
	}

	// 回控制台：控制台的列筛选还在，表视图的 WHERE 不影响它。
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	if got := a.grid.view.filters[1]; got != "ali" {
		t.Fatalf("控制台筛选丢了: %q", got)
	}
	if a.editSet().Count() != 0 {
		t.Fatal("表视图的编辑不该带进控制台")
	}

	// 再回表视图：WHERE 还在。
	a.activateViewer(id)
	if tb.query.where != "id = 1" {
		t.Fatalf("表视图条件丢了: %q", tb.query.where)
	}
}

// 表视图数据页的编辑走与结果区相同的网格交互：Ctrl+双击小格就地编辑，提交写库。
func TestViewerInlineEditThroughGrid(t *testing.T) {
	a := newSidebarRoot(t)
	prof := seedConsoleConn(t, a, "lib")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1, 'alice')"); err != nil {
		t.Fatal(err)
	}
	obj := engine.DBObject{Kind: engine.ObjectTable, Name: "users"}
	a.openViewerTab(prof, obj)
	id := viewerTabID(prof.ID, "", obj)
	waitViewer(t, a, id)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if a.grid.plan == nil {
		t.Fatalf("表视图应可编辑: %q", a.grid.readonly)
	}
	rect, ok := tt.Find("alice")
	if !ok {
		t.Fatalf("单元格没渲染: %q", tt.Texts())
	}
	tt.HoldModifiers(ui.Ctrl)
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+2)
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+2)
	tt.Frame()
	tt.Frame()
	tt.HoldModifiers(0)
	if !a.inlineEditOn || a.inlineEditRow != 0 || a.inlineEditCol != 1 {
		t.Fatalf("应就地编辑 name 格: inline=%v row=%d col=%d plan=%v err=%q",
			a.inlineEditOn, a.inlineEditRow, a.inlineEditCol, a.grid.plan, a.grid.readonly)
	}
	tt.Type("2")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := a.editSet().Cells[app.CellKey{Row: 0, Col: 1}]; got != "alice2" {
		t.Fatalf("待提交值 = %#v", got)
	}
	if err := a.applyEditsSync(); err != nil {
		t.Fatal(err)
	}
	r, err := a.testSession(t).RunStatement(t.Context(), "SELECT name FROM users WHERE id = 1")
	if err != nil || r.Rows[0][0] != "alice2" {
		t.Fatalf("数据库值: %+v err=%v", r, err)
	}
}
