package ui

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

// DDL 成功后自动失效列缓存：ALTER 加列后不用重启/手动刷新就能拿到新列。
func TestDDLRefreshCatalogCache(t *testing.T) {
	a := newSidebarRoot(t)
	p := seedConsoleConn(t, a, "本地")
	if err := a.openConn(p); err != nil {
		t.Fatal(err)
	}
	sess := a.sessionFor(p.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := sess.RunStatement(ctx, "CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT)"); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(p.ID, "查询")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	key, _ := a.completionScope(p.ID)
	a.session.Catalog.finishColumns(key, "t", []engine.Column{{Name: "id", PrimaryKey: true}}, p.Kind)

	text := "ALTER TABLE t ADD COLUMN b TEXT;"
	a.setActiveText(text)
	a.setSelection(0, len([]rune(text)))
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, 1)
	tt.Frame()
	if _, ok := a.session.Catalog.getColumns(key, "t"); ok {
		t.Fatal("DDL 成功后旧列缓存应失效")
	}

	a.ensureColumns(p.ID, "t")
	cols, ok := a.session.Catalog.columns(key, "t")
	if !ok {
		t.Fatal("失效后应能重新加载列")
	}
	found := false
	for _, c := range cols {
		if c.Name == "b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("重新加载的列里应有新列 b: %+v", cols)
	}
}
