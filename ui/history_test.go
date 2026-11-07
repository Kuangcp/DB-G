package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func TestExecRecordsHistory(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	a.setActiveText("SELECT 1 AS x")
	a.setSelection(0, len([]rune("SELECT 1 AS x")))
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	a.runSQL()
	pollResult(t, a, tt)
	deadline := len(a.history.List(a.activeConnID(), 1))
	_ = deadline
	list := a.history.List(a.activeConnID(), 10)
	if len(list) != 1 {
		t.Fatalf("history entries: %d", len(list))
	}
	if list[0].SQL != "SELECT 1 AS x" || !list[0].OK {
		t.Fatalf("entry: %+v", list[0])
	}
}

func TestHistoryPanelInsertAtCursor(t *testing.T) {
	a, tt := focusEditor(t, "")
	a.history = app.OpenHistory(t.TempDir())
	if err := a.history.Add(app.HistoryEntry{ConnID: a.activeConnID(), SQL: "SELECT 42 AS answer", OK: true}); err != nil {
		t.Fatal(err)
	}
	a.historyOpen = true
	tt.Frame()
	if !tt.HasText("SQL 历史") {
		t.Fatalf("panel missing: %q", tt.Texts())
	}
	if err := tt.Click("插入"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.activeText(); !strings.Contains(got, "SELECT 42 AS answer") {
		t.Fatalf("insert into editor failed: %q", got)
	}
	if a.historyOpen {
		t.Fatal("panel must close after insert")
	}
}
