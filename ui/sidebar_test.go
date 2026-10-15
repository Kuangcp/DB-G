package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"

	_ "db-g/driver/sqlite"
)

func newSidebarRoot(t *testing.T) *root {
	t.Helper()
	a := newTestRoot()
	a.store = app.LoadStore(filepath.Join(t.TempDir(), "connections.json"))
	return a
}

func seedConnFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	s, err := engine.Open(engine.Profile{Kind: "sqlite", Name: "seed", DSN: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(t.Context(), "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	return path
}

func TestSidebarEmptyHint(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("新建连接") {
		t.Fatalf("empty hint missing: %q", tt.Texts())
	}
}

func TestSidebarCreateConnFlow(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.connFormOpen {
		t.Fatal("modal must open")
	}
	a.connFormName = "本地库"
	a.connFormPath = path
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if a.connFormOpen {
		t.Fatal("modal must close after save")
	}
	if len(a.store.List()) != 1 || a.store.List()[0].Name != "本地库" {
		t.Fatalf("store: %+v", a.store.List())
	}
	tt.Frame()
	if !tt.HasText("本地库") {
		t.Fatalf("conn row not rendered: %q", tt.Texts())
	}
}

func TestSidebarCreateConnValidation(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.connFormName = "无路径"
	a.connFormPath = filepath.Join(t.TempDir(), "missing.db")
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if !a.connFormOpen || a.connFormErr == "" {
		t.Fatalf("invalid form must stay open with error: open=%v err=%q", a.connFormOpen, a.connFormErr)
	}
	if len(a.store.List()) != 0 {
		t.Fatal("invalid form must not persist")
	}
}

func TestOpenConnAndRunEndToEnd(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "本地库", Kind: "sqlite", Path: path}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	if err := a.openConn(stored); err != nil {
		t.Fatal(err)
	}
	if a.con.Session() == nil {
		t.Fatal("openConn must install a session")
	}
	if a.connName != "本地库" || a.connErr != "" {
		t.Fatalf("connName=%q connErr=%q", a.connName, a.connErr)
	}
	if act, ok := a.store.Active(); !ok || act.ID != stored.ID {
		t.Fatalf("active: %+v %v", act, ok)
	}
	a.ed.Buf.Set("SELECT name FROM users")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("执行"); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		if a.con.Result() != nil || a.con.Err() != "" {
			break
		}
		tt.Frame()
	}
	if a.con.Err() != "" {
		t.Fatalf("exec err: %s", a.con.Err())
	}
	if a.con.Result() == nil {
		t.Fatal("expected result")
	}
}

func TestOpenConnFailureKeepsState(t *testing.T) {
	a := newSidebarRoot(t)
	good := app.ConnProfile{Name: "good", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(good); err != nil {
		t.Fatal(err)
	}
	if err := a.openConn(a.store.List()[0]); err != nil {
		t.Fatal(err)
	}
	bad := app.ConnProfile{Name: "bad", Kind: "sqlite", Path: filepath.Join(t.TempDir(), "gone.db")}
	if err := a.openConn(bad); err == nil {
		t.Fatal("missing file must fail")
	}
	if a.connErr == "" {
		t.Fatal("connErr must be set")
	}
	if a.con.Session() == nil || a.connName != "good" {
		t.Fatal("failed open must keep previous session")
	}
}

func TestRemoveActiveConnClearsSession(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "临时", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	if err := a.openConn(stored); err != nil {
		t.Fatal(err)
	}
	a.removeConn(stored.ID)
	if a.con.Session() != nil || a.connName != "" {
		t.Fatalf("session=%v connName=%q", a.con.Session(), a.connName)
	}
	if _, ok := a.store.Get(stored.ID); ok {
		t.Fatal("profile must be removed")
	}
}
