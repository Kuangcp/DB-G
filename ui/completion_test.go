package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
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
