package ui

import (
	"testing"

	"db-g/engine"
)

// 控制台与表视图各自持有自己的 gridState，切换标签只换指针，不复制字段。
func TestGridStateSwitchingKeepsIndependentState(t *testing.T) {
	a := newRoot("")
	a.grid.row = 3 // 控制台的网格状态

	rt := a.newConsole()
	rt.SetOutcome(&engine.QueryResult{IsQuery: true, Columns: []string{"id"}}, "")
	tab := &viewerTab{
		id: "v1", connID: "c1", page: viewerData, rt: rt,
		grid: newGridState(),
	}
	tab.grid.row = 7
	a.tabs.ViewerTabs = map[string]*viewerTab{"v1": tab}
	a.tabs.ViewerOrder = []string{"v1"}

	a.activateViewer("v1")
	if a.grid != tab.grid || a.grid.row != 7 {
		t.Fatalf("viewer grid not active: same=%v row=%d", a.grid == tab.grid, a.grid.row)
	}
	a.grid.row = 9

	a.leaveViewer()
	if a.grid != a.tabs.ConsoleGrid || a.grid.row != 3 {
		t.Fatalf("console grid not restored: same=%v row=%d", a.grid == a.tabs.ConsoleGrid, a.grid.row)
	}
	if tab.grid.row != 9 {
		t.Fatalf("viewer state must stay in its tab: %d", tab.grid.row)
	}

	a.activateViewer("v1")
	if a.grid.row != 9 {
		t.Fatalf("viewer state lost on re-activation: %d", a.grid.row)
	}
}
