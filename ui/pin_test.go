package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// setAndRun 设置整段正文并全选后执行（多语句时按分号切分）。
func setAndRun(t *testing.T, a *root, tt *ui.Tester, sql string, want int) {
	t.Helper()
	a.setActiveText(sql)
	a.setSelection(0, len([]rune(sql)))
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, want)
	tt.Frame()
	tt.Frame()
}

// TestPinMovesTabToLeftAndSurvivesRerun 验证 db-k 的语义：pin 后该 tab 立即移到最左并选中；
// 再次执行时 pinned 结果保留在最左，新结果追加到右侧。
func TestPinMovesTabToLeftAndSurvivesRerun(t *testing.T) {
	a := openForSelection(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()

	setAndRun(t, a, tt, "SELECT 1 AS a; SELECT 2 AS b", 2)
	if len(a.stmtRuns) != 2 {
		t.Fatalf("two statements expected: %+v", a.stmtRuns)
	}
	secondNo := a.stmtRuns[1].tabNo

	// 激活第二个结果并钉住：应移到最左（索引 0）并成为当前 tab。
	a.activeStmt = 1
	a.togglePin()
	if a.activeStmt != 0 {
		t.Fatalf("pinned tab must move to left, active=%d", a.activeStmt)
	}
	if !a.stmtRuns[0].pinned || a.stmtRuns[0].tabNo != secondNo {
		t.Fatalf("pinned run must stay at front and keep its number: %+v", a.stmtRuns)
	}
	if a.stmtRuns[1].pinned {
		t.Fatalf("only the selected result must be pinned: %+v", a.stmtRuns)
	}
	if got := a.runtimeFor(a.stmtRuns[0].runtimeID).Result(); got == nil || got.Columns[0] != "b" {
		t.Fatalf("pinned snapshot must keep its own result")
	}
	// 渲染层验证：钉住的结果 tab 必须移到最左（X 更小）。
	tt.Frame()
	r2, ok2 := tt.Find("结果 2")
	r1, ok1 := tt.Find("结果 1")
	if !ok1 || !ok2 {
		t.Fatalf("result tabs missing after pin: %q", tt.Texts())
	}
	if r2.X >= r1.X {
		t.Fatalf("pinned tab must render left of the others: 结果2.X=%v 结果1.X=%v", r2.X, r1.X)
	}

	// 新执行：pinned 留在最左，新结果在右侧，且当前选中新结果。
	setAndRun(t, a, tt, "SELECT 3 AS c", 1)
	if len(a.stmtRuns) != 2 {
		t.Fatalf("pinned + new result expected: %+v", a.stmtRuns)
	}
	if !a.stmtRuns[0].pinned || a.stmtRuns[0].tabNo != secondNo {
		t.Fatalf("pinned result must survive a new execution: %+v", a.stmtRuns)
	}
	if a.stmtRuns[1].pinned || a.activeStmt != 1 {
		t.Fatalf("new result must append right and be active: %+v active=%d", a.stmtRuns, a.activeStmt)
	}
	if got := a.runtimeFor(a.stmtRuns[0].runtimeID).Result(); got == nil || got.Columns[0] != "b" {
		t.Fatalf("pinned result must not be overwritten by new execution")
	}
	if got := a.runtimeFor(a.stmtRuns[1].runtimeID).Result(); got == nil || got.Columns[0] != "c" {
		t.Fatalf("new result must render its own data")
	}
}

// TestUnpinMovesAfterPinnedBlock 验证取消钉住后该 tab 落到 pin 区块之后的第一个位置。
func TestUnpinMovesAfterPinnedBlock(t *testing.T) {
	a := openForSelection(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()

	setAndRun(t, a, tt, "SELECT 1 AS a; SELECT 2 AS b", 2)
	// 两个结果都钉住：顺序 [结果1, 结果2]。
	a.activeStmt = 0
	a.togglePin()
	a.activeStmt = 1
	a.togglePin()
	if len(a.stmtRuns) != 2 || !a.stmtRuns[0].pinned || !a.stmtRuns[1].pinned {
		t.Fatalf("both must be pinned: %+v", a.stmtRuns)
	}
	firstNo := a.stmtRuns[0].tabNo
	secondNo := a.stmtRuns[1].tabNo

	// 取消第一个：它应移到剩余 pinned（结果2）之后。
	a.activeStmt = 0
	a.togglePin()
	if a.activeStmt != 1 {
		t.Fatalf("unpinned tab must land after pinned block, active=%d", a.activeStmt)
	}
	if !a.stmtRuns[0].pinned || a.stmtRuns[0].tabNo != secondNo {
		t.Fatalf("remaining pinned keeps the left slot: %+v", a.stmtRuns)
	}
	if a.stmtRuns[1].pinned || a.stmtRuns[1].tabNo != firstNo {
		t.Fatalf("unpinned tab keeps its number and moves right: %+v", a.stmtRuns)
	}
}

// TestPinIgnoresNonQueryResult 验证只有查询结果可钉住（db-k 的限制）。
func TestPinIgnoresNonQueryResult(t *testing.T) {
	a := openForSelection(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()

	setAndRun(t, a, tt, "CREATE TABLE IF NOT EXISTS pin_probe (id INTEGER)", 1)
	if a.runtimeFor(a.stmtRuns[0].runtimeID).Result().IsQuery {
		t.Fatal("DDL must not produce a query result")
	}
	a.togglePin()
	if a.stmtRuns[0].pinned {
		t.Fatal("non-query results must not be pinned")
	}
}

// TestPinnedTabsKeptPerConsole 对照 db-k：每个控制台各自保留结果 tab，切走再切回 pinned 仍在最左。
func TestPinnedTabsKeptPerConsole(t *testing.T) {
	a := newSidebarRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	first, err := a.consoleMgr().Create(prof.ID, "甲")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.consoleMgr().Create(prof.ID, "乙")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(first.ID) {
		t.Fatal("activate first console")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()

	setAndRun(t, a, tt, "SELECT 1 AS a; SELECT 2 AS b", 2)
	a.activeStmt = 1
	a.togglePin()
	pinnedNo := a.stmtRuns[0].tabNo
	if !a.stmtRuns[0].pinned {
		t.Fatalf("pin failed: %+v", a.stmtRuns)
	}

	// 切到另一个控制台：它没有结果 tab。
	if !a.activateConsole(second.ID) {
		t.Fatal("activate second console")
	}
	if len(a.stmtRuns) != 0 {
		t.Fatalf("other console must not inherit tabs: %+v", a.stmtRuns)
	}

	// 切回来：pinned 结果仍在最左且数据没变。
	if !a.activateConsole(first.ID) {
		t.Fatal("reactivate first console")
	}
	if len(a.stmtRuns) != 2 || !a.stmtRuns[0].pinned || a.stmtRuns[0].tabNo != pinnedNo {
		t.Fatalf("pinned tab must be restored on the left: %+v", a.stmtRuns)
	}
	if got := a.runtimeFor(a.stmtRuns[0].runtimeID).Result(); got == nil || got.Columns[0] != "b" {
		t.Fatal("restored pinned tab must keep its result")
	}
}
