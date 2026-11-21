package ui

import (
	"strings"
	"testing"
	"time"

	"db-g/app"
	"db-g/engine"
)

func newSelfCheckRoot(t *testing.T) (*root, app.ConnProfile) {
	t.Helper()
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "lint")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.catalog.finish(prof.ID, []string{"users", "orders"}, nil, "")
	a.catalog.finishColumns(prof.ID, "users", []engine.Column{
		{Name: "id", Type: "INTEGER", PrimaryKey: true}, {Name: "name", Type: "TEXT"},
	}, "")
	a.catalog.finishColumns(prof.ID, "orders", []engine.Column{
		{Name: "id", Type: "INTEGER"}, {Name: "user_id", Type: "INTEGER"},
	}, "")
	return a, prof
}

func TestAISQLLint(t *testing.T) {
	a, _ := newSelfCheckRoot(t)
	none := func(sql string) {
		t.Helper()
		if got := a.aiSQLLint(sql); len(got) != 0 {
			t.Fatalf("unexpected issues for %q: %+v", sql, got)
		}
	}
	none(`select u.id, u.name from users u where u.id = 1`)
	none(`select count(*), now() from users`)
	none(`select o.id, u.name from users u join orders o on o.user_id = u.id`)
	none(`select * from other.users`)
	if got := a.aiSQLLint(`with x as (select id from users) select * from x`); len(got) != 0 {
		t.Fatalf("CTE should not be treated as a missing table: %+v", got)
	}

	if got := a.aiSQLLint(`select * from missing_t`); len(got) != 1 || got[0].Table != "missing_t" {
		t.Fatalf("missing table: %+v", got)
	}
	if got := a.aiSQLLint(`select u.missing_col from users u`); len(got) != 1 || got[0].Column != "missing_col" || got[0].Table != "users" {
		t.Fatalf("missing qualified column: %+v", got)
	}
	if got := a.aiSQLLint(`select missing_col from users`); len(got) != 1 || got[0].Column != "missing_col" {
		t.Fatalf("missing bare column: %+v", got)
	}
	if rep := a.aiSQLCheck(`delete from users`); len(rep.Risks) == 0 || !rep.HasRisk() {
		t.Fatalf("high-risk rule missing: %+v", rep)
	}
	if rep := a.aiSQLCheck(`drop table users`); len(rep.Risks) == 0 {
		t.Fatalf("drop rule missing: %+v", rep)
	}
}

func TestInsertAISQLRunsSelfCheck(t *testing.T) {
	a, _ := newSelfCheckRoot(t)
	if !a.insertAISQL("```sql\nselect u.missing_col from users u;\n```") {
		t.Fatal("insert failed")
	}
	if !strings.Contains(a.aiChat.lint.Summary(), "missing_col") {
		t.Fatalf("self-check warning missing: %q", a.aiChat.lint.Summary())
	}
	if !strings.Contains(a.code.Text(), "missing_col") {
		t.Fatalf("SQL not inserted: %q", a.code.Text())
	}
}

func TestConnectionAIStrictPolicy(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "policy")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.catalog.finish(prof.ID, []string{"users"}, nil, "")
	a.catalog.finishColumns(prof.ID, "users", []engine.Column{{Name: "id", Type: "INTEGER"}}, "")
	a.setActiveText("select id from users")
	a.aiPrefs.AllowHistory = true
	a.aiPrefs.AllowErrorLogs = true
	a.aiPrefs.AllowSampleRows = true
	a.aiPrefs.AllowDDL = true
	a.history = app.OpenHistory(t.TempDir())
	if err := a.history.Add(app.HistoryEntry{ConnID: prof.ID, SQL: "select 1"}); err != nil {
		t.Fatal(err)
	}
	a.consoleErr = "boom"
	a.aiDDL = map[string]aiDDLEntry{
		aiDDLKey(prof.ID, "", "users"): {ddl: "CREATE TABLE users (id INT)", at: time.Now()},
	}
	a.runtimeFor(rec.ID).SetOutcome(&engine.QueryResult{
		IsQuery: true, Columns: []string{"id"}, Rows: [][]any{{int64(1)}},
	}, "")

	if snap := a.aiSnapshot(); len(snap.History) != 1 || len(snap.Errors) != 1 {
		t.Fatalf("global policy should allow history/errors: %+v", snap)
	}
	if rc := a.aiResultContext(); rc == nil || len(rc.Sample) == 0 {
		t.Fatalf("global policy should allow samples: %+v", rc)
	}
	if snap := a.chatSnapshot(); len(snap.Tables) != 1 || snap.Tables[0].DDL == "" {
		t.Fatalf("global policy should allow DDL: %+v", snap.Tables)
	}

	prof.AIContextStrict = true
	if err := a.store.Update(prof); err != nil {
		t.Fatal(err)
	}
	if snap := a.aiSnapshot(); len(snap.History) != 0 || len(snap.Errors) != 0 {
		t.Fatalf("strict policy leaked history/errors: %+v", snap)
	}
	if rc := a.aiResultContext(); rc == nil || len(rc.Sample) != 0 {
		t.Fatalf("strict policy leaked samples: %+v", rc)
	}
	if snap := a.chatSnapshot(); len(snap.Tables) != 1 || snap.Tables[0].DDL != "" {
		t.Fatalf("strict policy leaked DDL: %+v", snap.Tables)
	}
}

func TestConnFormSavesAIStrictPolicy(t *testing.T) {
	a := newSidebarRoot(t)
	a.openNewConnForm("")
	a.connFormName = "strict_prod"
	a.connFormPath = seedConnFile(t)
	a.connFormAIStrict = true
	a.saveConnForm()
	if a.connFormErr != "" {
		t.Fatal(a.connFormErr)
	}
	list := a.store.List()
	if len(list) != 1 || !list[0].AIContextStrict {
		t.Fatalf("profile: %+v", list)
	}
}
