package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

func TestPreviewObjectCreatesConsoleAndRuns(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, err := a.sess.RunStatement(t.Context(), "INSERT INTO users (name) VALUES ('alice')"); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.previewObject(prof, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	tt.Frame()
	tt.Frame()
	if got := a.activeText(); !strings.HasPrefix(got, `SELECT * FROM "users" LIMIT 200`) {
		t.Fatalf("preview sql: %q", got)
	}
	deadline := 0
	for deadline < 200 {
		tt.Frame()
		if r := a.currentRuntime().Result(); r != nil {
			break
		}
		deadline++
	}
	r := a.currentRuntime().Result()
	if r == nil || len(r.Rows) != 1 {
		t.Fatalf("preview result: %+v", r)
	}
}

func TestShowDDLOpensPanel(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.showDDL(prof, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	tt.Frame()
	if !a.ddlOpen || !strings.Contains(strings.ToUpper(a.ddlText), "CREATE TABLE") {
		t.Fatalf("ddl: open=%v text=%q", a.ddlOpen, a.ddlText)
	}
	if !tt.HasText("DDL：users") {
		t.Fatalf("panel title missing: %q", tt.Texts())
	}
}
