package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// waitStmts 等到「已开始的语句都跑完，且至少 started 条已开始」。
func waitStmts(t *testing.T, a *root, tt *ui.Tester, started int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		done := 0
		for _, run := range a.stmtRuns {
			if run.started && !a.runtimeFor(run.runtimeID).Running() {
				done++
			}
		}
		nStarted := 0
		for _, run := range a.stmtRuns {
			if run.started {
				nStarted++
			}
		}
		if nStarted >= started && done >= nStarted {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("statements did not finish: %+v", a.stmtRuns)
}

func TestMultiStatementTabsAndSequentialRun(t *testing.T) {
	a := openForSelection(t)
	a.setActiveText("SELECT 1 AS a; SELECT 2 AS b")
	a.setSelection(0, len([]rune("SELECT 1 AS a; SELECT 2 AS b")))
	var th *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) { th = c.Theme(); rootView(a)(c) }, 1400, 800)
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, 2)
	tt.Frame()
	tt.Frame()
	if len(a.stmtRuns) != 2 {
		t.Fatalf("expected two statements: %d", len(a.stmtRuns))
	}
	for i, run := range a.stmtRuns {
		if !run.started {
			t.Fatalf("statement %d must run", i+1)
		}
		rt := a.runtimeFor(run.runtimeID)
		if rt.Err() != "" {
			t.Fatalf("statement %d err: %s", i+1, rt.Err())
		}
		if rt.Result() == nil || len(rt.Result().Columns) != 1 {
			t.Fatalf("statement %d result: %+v", i+1, rt.Result())
		}
	}
	img := tt.Image()
	for _, name := range []string{"结果 1 · 成功", "结果 2 · 成功"} {
		r, ok := tt.Find(name)
		if !ok {
			t.Fatalf("result tab %q missing: %q", name, tt.Texts())
		}
		if !rectHasColor(img, r, th.Success) {
			t.Fatalf("tab %q must show a green status dot", name)
		}
	}
}

func TestMultiStatementStopsOnError(t *testing.T) {
	a := openForSelection(t)
	a.setActiveText("SELECT * FROM nope; SELECT 2 AS b; SELECT 3 AS c")
	a.setSelection(0, len([]rune("SELECT * FROM nope; SELECT 2 AS b; SELECT 3 AS c")))
	var th *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) { th = c.Theme(); rootView(a)(c) }, 1400, 800)
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, 1) // 第一条失败即停，只需等它结束
	tt.Frame()
	tt.Frame()
	if got := a.runtimeFor(a.stmtRuns[0].runtimeID).Err(); got == "" {
		t.Fatal("first statement must fail")
	}
	if a.stmtRuns[1].started || a.stmtRuns[2].started {
		t.Fatal("statements after a failure must not run")
	}
	if !tt.HasText("结果 2 · 未执行") {
		t.Fatalf("skipped tab missing: %q", tt.Texts())
	}
	if r, ok := tt.Find("结果 1 · 失败"); !ok {
		t.Fatalf("failed tab missing: %q", tt.Texts())
	} else if !rectHasColor(tt.Image(), r, th.Danger) {
		t.Fatal("failed tab must show a red status dot")
	}
}

func TestSingleStatementKeepsCurrentRuntime(t *testing.T) {
	// 单条语句沿用当前控制台的运行态（既有测试与行为依赖 a.con）。
	a := openForSelection(t)
	a.setActiveText("SELECT 1 AS a; SELECT 2 AS b")
	a.setSelection(0, len([]rune("SELECT 1 AS a")))
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.runSQL()
	pollResult(t, a, tt)
	if len(a.stmtRuns) != 1 || a.stmtRuns[0].runtimeID != "" {
		t.Fatalf("single statement must reuse the scratch runtime: %+v", a.stmtRuns)
	}
	if !strings.Contains(tt.Texts()[len(tt.Texts())-1], "") { // 只是确保渲染不崩
		t.Fatal("unreachable")
	}
}

// 改过的语句不能再保留 gutter 底色：同长度的编辑也要让该语句的颜色消失。
func TestEditingStatementClearsItsBackground(t *testing.T) {
	a, tt := focusEditor(t, "SELECT 1;\nSELECT 2;")
	a.setSelection(0, len([]rune(a.code.Text())))
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, 2)
	tt.Frame()
	if got := len(a.code.state.LineBackgrounds); got != 2 {
		t.Fatalf("两条语句都应着色: %d", got)
	}
	// 只改第一行的 1 → 9，长度不变，第二段的偏移仍然有效。
	a.setSelection(7, 8)
	tt.Frame()
	tt.Type("9")
	tt.Frame()
	if got := a.code.Text(); got != "SELECT 9;\nSELECT 2;" {
		t.Fatalf("text: %q", got)
	}
	lbs := a.code.state.LineBackgrounds
	if len(lbs) != 1 || lbs[0].First != 1 || lbs[0].Last != 1 {
		t.Fatalf("只有未改动的第二段保留底色: %+v", lbs)
	}
}

// 删除一行后，旧语句的行号不能漂移到下面的行上。
func TestDeleteLineClearsStmtBackground(t *testing.T) {
	a, tt := focusEditor(t, "SELECT 1;\nSELECT 2;")
	a.setSelection(0, len([]rune(a.code.Text())))
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, 2)
	tt.Frame()
	a.setSelection(0, 0)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyD)
	tt.Frame()
	if got := a.code.Text(); got != "SELECT 2;" {
		t.Fatalf("text: %q", got)
	}
	if lbs := a.code.state.LineBackgrounds; len(lbs) != 0 {
		t.Fatalf("删行后不能再有旧行号底色: %+v", lbs)
	}
}

// 执行排队后正文被改，后续语句仍按入队时的快照执行，不能拿失效偏移切片。
func TestStartStmtUsesSnapshotAfterEdit(t *testing.T) {
	a, tt := focusEditor(t, "SELECT 1;\nSELECT 2;")
	a.setSelection(0, len([]rune(a.code.Text())))
	tt.Frame()
	a.resetStmtRuns(a.execRanges(a.code.Text()))
	a.setActiveText("x")
	a.startStmt(0) // 旧实现会在这里用新正文切旧偏移而 panic
	if a.lastExecSQL != "SELECT 1" {
		t.Fatalf("应执行快照语句，实际 %q", a.lastExecSQL)
	}
}

// 没有激活控制台的临时缓冲：编辑后同样不能留下旧行底色。
func TestEditingScratchBufferClearsBackground(t *testing.T) {
	a := openForSelection(t)
	a.setActiveText("SELECT 1;\nSELECT 2;")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("编辑器"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.setSelection(0, len([]rune(a.code.Text())))
	tt.Frame()
	a.runSQL()
	waitStmts(t, a, tt, 2)
	tt.Frame()
	if got := len(a.code.state.LineBackgrounds); got != 2 {
		t.Fatalf("两条语句都应着色: %d", got)
	}
	a.setSelection(7, 8)
	tt.Frame()
	tt.Type("9")
	tt.Frame()
	if lbs := a.code.state.LineBackgrounds; len(lbs) != 1 || lbs[0].First != 1 {
		t.Fatalf("只有未改动的第二段保留底色: %+v", lbs)
	}
}
