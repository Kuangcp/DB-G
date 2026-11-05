package ui

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

// completionLabels 取某连接的候选标签（completionItems 对未知连接也返回词表）。
func completionLabels(t *testing.T, a *root) []string {
	t.Helper()
	var out []string
	for _, it := range a.completionItems(a.sessConnID) {
		out = append(out, it.Label)
	}
	return out
}

func TestCompletionWithoutSessionHasOnlyWords(t *testing.T) {
	a := newConsoleRoot(t)
	labels := completionLabels(t, a)
	if len(labels) == 0 {
		t.Fatal("keywords must always be available")
	}
	for _, l := range labels {
		if l == "users" {
			t.Fatal("no session → no tables")
		}
	}
}

func TestCompletionIncludesTablesAndViews(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	tables, _, err := a.loadCatalog(a.sessConnID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 || tables[0] != "users" {
		t.Fatalf("tables: %v", tables)
	}
	a.ensureCatalog(a.sessConnID) // headless：同步加载，便于断言
	labels := completionLabels(t, a)
	found := false
	for _, l := range labels {
		if l == "users" {
			found = true
		}
	}
	if !found {
		t.Fatal("users must appear after the catalog loads")
	}
}

func TestCompletionMenuShowsTable(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "补全")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Click("编辑器")
	tt.Frame()
	tt.TypeKey(0, ui.KeyU, "u")
	tt.TypeKey(0, ui.KeyS, "s")
	for range 5 {
		tt.Frame()
	}
	if !tt.HasText("users") {
		t.Fatalf("completion menu must list the table: %q", tt.Texts())
	}
}

func TestRefreshCompletionsInvalidatesCache(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.loadCatalog(prof.ID); err != nil {
		t.Fatal(err)
	}
	a.ensureCatalog(prof.ID)
	if _, ok := a.catalog.get(prof.ID); !ok {
		t.Fatal("catalog must be cached after a load")
	}
	a.refreshCompletions()
	if _, ok := a.catalog.get(prof.ID); !ok {
		t.Fatal("refresh must repopulate the cache")
	}
}

func TestOpenConnInvalidatesCatalog(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	a.catalog.begin(prof.ID)
	a.catalog.finish(prof.ID, []string{"stale"}, nil)
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.catalog.get(prof.ID); ok {
		t.Fatal("openConn must invalidate the connection's catalog")
	}
}

func typeText(tt *ui.Tester, s string) {
	for _, ch := range s {
		tt.TypeKey(0, 0, string(ch))
	}
}

func TestCompletionColumnsAfterDot(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, err := a.sess.RunStatement(t.Context(), `CREATE TABLE a3_t (id INTEGER PRIMARY KEY, zz_col TEXT)`); err != nil {
		t.Fatal(err)
	}
	a.catalog.invalidate(prof.ID)
	rec, err := a.consoleMgr().Create(prof.ID, "列补全")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Click("编辑器")
	tt.Frame()
	typeText(tt, "select * from a3_t.")
	for range 6 {
		tt.Frame()
	}
	if !tt.HasText("zz_col") {
		t.Fatalf("column completion missing: %q", tt.Texts())
	}
}

func TestCompletionUnknownTableAfterDotKeepsKeywords(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	a.catalog.invalidate(prof.ID)
	rec, _ := a.consoleMgr().Create(prof.ID, "列补全")
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Click("编辑器")
	tt.Frame()
	typeText(tt, "select * from nosuch.")
	for range 6 {
		tt.Frame()
	}
	if !tt.HasText("select") && !tt.HasText("SELECT") {
		t.Fatalf("keywords must remain for unknown relations: %q", tt.Texts())
	}
}

type noColumnsSession struct {
	// 只声明 CapObjects：旧驱动/第三方实现的形态。
}

func (s noColumnsSession) Open(context.Context) error { return nil }
func (s noColumnsSession) Close() error               { return nil }
func (s noColumnsSession) Cancel() bool               { return false }
func (s noColumnsSession) Caps() engine.Capabilities  { return engine.CapObjects }
func (s noColumnsSession) RunStatement(context.Context, string) (*engine.QueryResult, error) {
	return &engine.QueryResult{}, nil
}
func (s noColumnsSession) Objects(context.Context, engine.ObjectKind) ([]engine.DBObject, error) {
	return []engine.DBObject{{Name: "t"}}, nil
}

func TestEnsureColumnsDegradesWithoutCapability(t *testing.T) {
	a := newConsoleRoot(t)
	a.setSession("c1", noColumnsSession{})
	a.ensureColumns("c1", "t") // 不得 panic，也不得写缓存
	if _, ok := a.catalog.getColumns("c1", "t"); ok {
		t.Fatal("session without CapColumns must not produce column items")
	}
	if found := a.completionItems("c1"); len(found) == 0 {
		t.Fatal("keywords must remain available")
	}
}

func TestShowCompletionsAtCursorLoadsColumnsAfterCatalog(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, err := a.sess.RunStatement(t.Context(), `CREATE TABLE a3_u (zz_two TEXT)`); err != nil {
		t.Fatal(err)
	}
	a.catalog.invalidate(prof.ID)
	rec, _ := a.consoleMgr().Create(prof.ID, "列补全")
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	// 模拟真实路径：编辑器有焦点时，目录加载完成的重弹会顺手把列取上。
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Click("编辑器")
	tt.Frame()
	a.ensureCatalog(prof.ID)
	a.setActiveText("select * from a3_u.")
	n := len([]rune("select * from a3_u."))
	a.code.SetSelection(n, n)
	a.showCompletionsAtCursor()
	if _, ok := a.catalog.getColumns(prof.ID, "a3_u"); !ok {
		t.Fatal("dot context must trigger a column load")
	}
}
