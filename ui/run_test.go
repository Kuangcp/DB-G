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
	if len(a.sessions) != 1 {
		t.Fatalf("openSQLite must install one session, got %d", len(a.sessions))
	}
	a.setActiveText("SELECT id, name FROM users ORDER BY id")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	a.setSelection(0, len([]rune("SELECT id, name FROM users ORDER BY id")))
	a.runSQL()
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
	a.setActiveText("SELECT * FROM nope")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	a.setSelection(0, len([]rune("SELECT * FROM nope")))
	a.runSQL()
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

func waitRerun(t *testing.T, a *root, tt *ui.Tester) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		rt := a.currentRuntime()
		if !rt.Running() && rt.Result() != nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("rerun did not finish")
}

func TestRefreshRerunsCurrentQueryOnly(t *testing.T) {
	a := openForSelection(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()

	a.setActiveText("SELECT COUNT(*) AS n FROM users")
	a.setSelection(0, len([]rune("SELECT COUNT(*) AS n FROM users")))
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, 1)
	tt.Frame()
	if res := a.currentRuntime().Result(); res == nil || !res.IsQuery || cellString(res.Rows[0][0]) != "0" {
		t.Fatalf("initial result: %+v", res)
	}
	if _, ok := tt.Find("重新执行此查询"); !ok {
		t.Fatalf("refresh icon missing for query result: %q", tt.Texts())
	}
	refresh, _ := tt.Find("重新执行此查询")
	fetch, _ := tt.Find("取更多行")
	if refresh.X >= fetch.X {
		t.Fatalf("refresh must sit left of 取更多行: refresh=%v fetch=%v", refresh, fetch)
	}

	if _, err := a.activeSession().RunStatement(t.Context(), "INSERT INTO users (name) VALUES ('x')"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("重新执行此查询"); err != nil {
		t.Fatal(err)
	}
	waitRerun(t, a, tt)
	tt.Frame()
	if res := a.currentRuntime().Result(); res == nil || cellString(res.Rows[0][0]) != "1" {
		t.Fatalf("refreshed result: %+v", res)
	}

	// 写语句没有结果集，不出现刷新按钮。
	a.setActiveText("INSERT INTO users (name) VALUES ('y')")
	a.setSelection(0, len([]rune("INSERT INTO users (name) VALUES ('y')")))
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, 1)
	tt.Frame()
	if _, ok := tt.Find("重新执行此查询"); ok {
		t.Fatal("refresh icon must not show for non-query results")
	}
}

func TestRefreshRerunsActiveStatementOnly(t *testing.T) {
	a := openForSelection(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	text := "SELECT 1 AS a; SELECT 2 AS b"
	a.setActiveText(text)
	a.setSelection(0, len([]rune(text)))
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, 2)
	a.activeStmt = 1
	tt.Frame()
	if err := tt.Click("重新执行此查询"); err != nil {
		t.Fatal(err)
	}
	waitRerun(t, a, tt)
	tt.Frame()
	if got := a.currentRuntime().Result(); got == nil || got.Columns[0] != "b" {
		t.Fatalf("refresh must rerun the active result's SQL: %+v", got)
	}
	if a.activeStmt != 1 || len(a.stmtRuns) != 2 {
		t.Fatalf("refresh must not re-plan statements: active=%d runs=%+v", a.activeStmt, a.stmtRuns)
	}
}
