package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRunSQLWarnsRiskyStatement(t *testing.T) {
	a := newConsoleRoot(t)
	seedConsoleConn(t, a, "本地")
	a.setActiveText("update users set name = 'x'")
	a.runSQL()
	if !a.guardOpen || a.guardBlocked || len(a.guardRisks) != 1 {
		t.Fatalf("guard: %+v", a)
	}
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("高危 SQL 提醒") || !tt.HasText("仍然执行") {
		t.Fatalf("guard modal missing: %q", tt.Texts())
	}
}

func TestRunSQLBlocksRiskyStatementOnConnection(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "删除")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	p, _ := a.store.Get(prof.ID)
	p.GuardBlock = true
	if err := a.store.Update(p); err != nil {
		t.Fatal(err)
	}
	a.setActiveText("delete from users")
	a.runSQL()
	if !a.guardOpen || !a.guardBlocked || a.consoleErr == "" {
		t.Fatalf("blocked: open=%v blocked=%v err=%q", a.guardOpen, a.guardBlocked, a.consoleErr)
	}
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("已阻断高危 SQL") || tt.HasText("仍然执行") {
		t.Fatalf("blocked modal: %q", tt.Texts())
	}
}
