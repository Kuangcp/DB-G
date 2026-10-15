package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/engine"

	_ "db-g/driver/sqlite"
)

func seedDB(t *testing.T) string {
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
	defer s.Close()
	stmts := []string{
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
		"INSERT INTO users (name) VALUES ('alice')",
		"INSERT INTO users (name) VALUES ('bob')",
	}
	for _, q := range stmts {
		if _, err := s.RunStatement(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestOpenSQLiteErrors(t *testing.T) {
	a := newTestRoot()
	if err := a.openSQLite(filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Fatal("missing file must fail")
	}
	if a.connErr == "" {
		t.Fatal("connErr must be set")
	}
}

func TestRunSQLiteEndToEnd(t *testing.T) {
	path := seedDB(t)
	a := newTestRoot()
	if err := a.openSQLite(path); err != nil {
		t.Fatal(err)
	}
	if a.connName != "app.db" {
		t.Fatalf("connName = %q", a.connName)
	}
	a.ed.Buf.Set("SELECT id, name FROM users ORDER BY id")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if err := tt.Click("执行"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.con.Result() != nil || a.con.Err() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	tt.Frame()
	if a.con.Err() != "" {
		t.Fatalf("exec err: %s", a.con.Err())
	}
	res := a.con.Result()
	if res == nil || len(res.Rows) != 2 {
		t.Fatalf("result: %+v", res)
	}
	if !tt.HasText("alice") || !tt.HasText("bob") {
		t.Fatalf("rows not rendered: %q", tt.Texts())
	}
}

func TestRunErrorEndToEnd(t *testing.T) {
	path := seedDB(t)
	a := newTestRoot()
	if err := a.openSQLite(path); err != nil {
		t.Fatal(err)
	}
	a.ed.Buf.Set("SELECT * FROM nope")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if err := tt.Click("执行"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.con.Err() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	tt.Frame()
	if a.con.Err() == "" {
		t.Fatal("expected error")
	}
	if !tt.HasText("nope") {
		t.Fatalf("error not rendered: %q", tt.Texts())
	}
}
