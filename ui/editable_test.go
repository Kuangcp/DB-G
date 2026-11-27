package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

func runEditQuery(t *testing.T, a *root, sql string) *ui.Tester {
	t.Helper()
	a.setActiveText(sql)
	a.setSelection(0, len([]rune(sql)))
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.runSQL()
	pollResult(t, a, tt)
	tt.Frame()
	return tt
}

func TestEditPlanGating(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	if a.grid.plan == nil {
		t.Fatalf("single table with pk must be editable: %q", a.grid.readonly)
	}
	// JOIN → 只读
	if _, err := a.testSession(t).RunStatement(t.Context(), "CREATE TABLE other (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	tt = runEditQuery(t, a, "SELECT u.id, u.name FROM users u JOIN other o ON o.id = u.id")
	tt.Frame()
	if a.grid.plan != nil || !strings.Contains(a.grid.readonly, "单表") {
		t.Fatalf("join must be read-only: plan=%v reason=%q", a.grid.plan, a.grid.readonly)
	}
	// 视图 → 只读（无主键）
	if _, err := a.testSession(t).RunStatement(t.Context(), "CREATE VIEW v_users AS SELECT id, name FROM users"); err != nil {
		t.Fatal(err)
	}
	tt = runEditQuery(t, a, "SELECT id, name FROM v_users")
	tt.Frame()
	if a.grid.plan != nil {
		t.Fatalf("view must be read-only: %+v", a.grid.plan)
	}
}

func TestEditCellCommitThroughUI(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1, 'alice')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	if a.grid.plan == nil {
		t.Fatalf("not editable: %q", a.grid.readonly)
	}
	a.setCellEdit(0, 1, "alice2")
	lines, err := a.previewEdits()
	if err != nil || len(lines) != 1 || !strings.Contains(lines[0], "alice2") {
		t.Fatalf("preview: %v err=%v", lines, err)
	}
	if err := a.applyEditsSync(); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	r, err := a.testSession(t).RunStatement(t.Context(), "SELECT name FROM users WHERE id = 1")
	if err != nil || r.Rows[0][0] != "alice2" {
		t.Fatalf("db value: %+v err=%v", r, err)
	}
	if a.editSet().Count() != 0 {
		t.Fatal("overlay must be cleared after commit")
	}
}

func TestEditInsertAndDeleteThroughUI(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1, 'alice')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	_ = tt
	ins := a.addInsertRow(a.currentRuntime().Result())
	a.editSet().Inserts[ins][0] = int64(2)
	a.editSet().Inserts[ins][1] = "bob"
	a.deleteRow(0)
	if err := a.applyEditsSync(); err != nil {
		t.Fatal(err)
	}
	r, err := a.testSession(t).RunStatement(t.Context(), "SELECT id, name FROM users ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 1 || r.Rows[0][0] != int64(2) || r.Rows[0][1] != "bob" {
		t.Fatalf("rows: %v", r.Rows)
	}
}

// 新增行必须能逐格编辑（包括主键列），不能只是画一行空文本。
func TestEditInsertRowCellsThroughUI(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "DELETE FROM users"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	r := a.currentRuntime().Result()
	ins := a.addInsertRow(r)
	if ins != 0 || !a.edit.InlineOn || a.edit.InlineRow != -1 || a.edit.InlineCol != 1 {
		t.Fatalf("add row should edit first non-pk column: ins=%d inline=%v row=%d col=%d",
			ins, a.edit.InlineOn, a.edit.InlineRow, a.edit.InlineCol)
	}
	a.edit.InlineText = "bob"
	a.commitInlineEdit()
	if got, ok := a.editSet().Inserts[0][1]; !ok || got != "bob" {
		t.Fatalf("insert name: %v %v", got, ok)
	}
	// 主键列对已有行只读，但对新增行必须可填。
	a.beginEditAt(r, -1, 0)
	if !a.edit.InlineOn || a.edit.InlineRow != -1 || a.edit.InlineCol != 0 {
		t.Fatalf("insert pk editor: %v %d %d", a.edit.InlineOn, a.edit.InlineRow, a.edit.InlineCol)
	}
	a.edit.InlineText = "2"
	a.commitInlineEdit()
	if got, ok := a.editSet().Inserts[0][0]; !ok || got != int64(2) {
		t.Fatalf("insert pk: %v %v", got, ok)
	}
	lines, err := a.previewEdits()
	if err != nil || len(lines) != 1 || !strings.Contains(lines[0], `"id"`) || !strings.Contains(lines[0], `"name"`) {
		t.Fatalf("preview: %v err=%v", lines, err)
	}
	if err := a.applyEditsSync(); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	got, err := a.testSession(t).RunStatement(t.Context(), "SELECT id, name FROM users")
	if err != nil || len(got.Rows) != 1 || got.Rows[0][0] != int64(2) || got.Rows[0][1] != "bob" {
		t.Fatalf("inserted row: %+v err=%v", got, err)
	}
}

func TestEditDeleteInsertRow(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	runEditQuery(t, a, "SELECT id, name FROM users")
	r := a.currentRuntime().Result()
	a.addInsertRow(r)
	if len(a.editSet().Inserts) != 1 {
		t.Fatal("insert row missing")
	}
	a.grid.row = len(a.grid.view.apply(r)) // 新增行
	a.deleteSelectedRow(r)
	if len(a.editSet().Inserts) != 0 || a.editSet().Count() != 0 {
		t.Fatalf("insert row must be removed: %+v", a.editSet())
	}
}

func TestEditCommitFailureKeepsOverlay(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1,'alice'),(2,'bob')"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.testSession(t).RunStatement(t.Context(), "CREATE UNIQUE INDEX ux_users_name ON users(name)"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users ORDER BY id")
	tt.Frame()
	a.setCellEdit(0, 1, "bob") // 冲突
	if err := a.applyEditsSync(); err == nil {
		t.Fatal("conflict must fail")
	}
	if a.editSet().Count() == 0 {
		t.Fatal("failed commit must keep the overlay")
	}
	r, _ := a.testSession(t).RunStatement(t.Context(), "SELECT name FROM users ORDER BY id")
	if r.Rows[0][0] != "alice" || r.Rows[1][0] != "bob" {
		t.Fatalf("rollback must keep data: %v", r.Rows)
	}
}

func TestCommitThroughPreviewModal(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1,'alice')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	tt.Frame()
	a.setCellEdit(0, 1, "alice3")
	tt.Frame()
	if err := tt.Click("提交 (1)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.edit.PreviewOpen || !tt.HasText("提交 1 条改动") {
		t.Fatalf("preview missing: %q", tt.Texts())
	}
	if err := tt.Click("确认提交"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	r, err := a.testSession(t).RunStatement(t.Context(), "SELECT name FROM users WHERE id = 1")
	if err != nil || r.Rows[0][0] != "alice3" {
		t.Fatalf("db value: %+v err=%v", r, err)
	}
}

func TestReadonlyReasonShownInToolbar(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "CREATE TABLE other2 (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT u.id FROM users u JOIN other2 o ON o.id = u.id")
	tt.Frame()
	found := false
	for _, s := range tt.Texts() {
		if strings.Contains(s, "只读") {
			found = true
		}
	}
	if !found {
		t.Fatalf("read-only hint missing: %q", tt.Texts())
	}
}

func TestCtrlDoubleClickEditsShortCellInline(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1, 'alice')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	if a.grid.plan == nil {
		t.Fatalf("not editable: %q", a.grid.readonly)
	}
	rect, ok := tt.Find("alice")
	if !ok {
		t.Fatalf("cell missing: %q", tt.Texts())
	}
	tt.HoldModifiers(ui.Ctrl)
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+2)
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+2)
	tt.Frame()
	tt.Frame()
	tt.HoldModifiers(0)
	if !a.edit.InlineOn || a.edit.CellOpen {
		t.Fatalf("short cell must edit inline: inline=%v modal=%v view=%v row=%d cursor=%q plan=%v err=%q", a.edit.InlineOn, a.edit.CellOpen, a.cellView.Open, a.grid.row, a.grid.state.Cells.Cursor, a.grid.plan, a.consoleErr)
	}
	if a.edit.InlineRow != 0 || a.edit.InlineCol != 1 {
		t.Fatalf("inline target = (%d,%d)", a.edit.InlineRow, a.edit.InlineCol)
	}
	tt.Type("2")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.edit.InlineOn {
		t.Fatal("Enter must finish inline editing")
	}
	if a.cellView.Open || a.edit.CellOpen {
		t.Fatalf("Enter must not open another panel: view=%v modal=%v", a.cellView.Open, a.edit.CellOpen)
	}
	got, ok := a.editSet().Cells[app.CellKey{Row: 0, Col: 1}]
	if !ok || got != "alice2" {
		t.Fatalf("pending edit = %#v, %v", got, ok)
	}
	tt.Frame()
	if !tt.HasText("alice2") {
		t.Fatalf("grid must show the pending value: %q", tt.Texts())
	}
}

func TestCtrlDoubleClickLongCellOpensModal(t *testing.T) {
	a := openForSelection(t)
	long := strings.Repeat("长", inlineEditMaxRunes+1)
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1, '"+long+"')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	if a.grid.plan == nil {
		t.Fatalf("not editable: %q", a.grid.readonly)
	}
	label := ""
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, "长") {
			label = text
			break
		}
	}
	rect, ok := tt.Find(label)
	if !ok {
		t.Fatalf("cell missing: %q", tt.Texts())
	}
	tt.HoldModifiers(ui.Ctrl)
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+2)
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+2)
	tt.Frame()
	tt.Frame()
	tt.HoldModifiers(0)
	if !a.edit.CellOpen || a.edit.InlineOn {
		t.Fatalf("long cell must open modal: modal=%v inline=%v view=%v row=%d cursor=%q plan=%v err=%q", a.edit.CellOpen, a.edit.InlineOn, a.cellView.Open, a.grid.row, a.grid.state.Cells.Cursor, a.grid.plan, a.consoleErr)
	}
	if a.edit.CellText != long {
		t.Fatalf("modal text = %q", a.edit.CellText)
	}
	if !tt.HasText("编辑 name · 第 1 行") {
		t.Fatalf("modal title missing: %q", tt.Texts())
	}
	a.edit.CellText = long + "改"
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.edit.CellOpen {
		t.Fatal("save must close the edit modal")
	}
	got, ok := a.editSet().Cells[app.CellKey{Row: 0, Col: 1}]
	if !ok || got != long+"改" {
		t.Fatalf("saved edit = %#v, %v", got, ok)
	}
}

func TestCellViewerCloseIcon(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true,
		Columns: []string{"v"},
		Rows:    [][]any{{"hello"}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	a.openCellViewer(a.con.Result(), 0, 0)
	tt.Frame()
	if !a.cellView.Open || !tt.HasText("hello") {
		t.Fatalf("viewer missing: %v %q", a.cellView.Open, tt.Texts())
	}
	if _, ok := tt.Find("复制原文"); !ok {
		t.Fatalf("copy icon missing: %q", tt.Texts())
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.cellView.Open {
		t.Fatal("close icon must close the viewer")
	}
}

func TestCtrlShiftNSetsSelectedCellsNull(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1,'alice'),(2,'bob')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users ORDER BY id")
	a.grid.row = 0
	a.grid.state.Cells = ui.ColumnRange{Cursor: "name", Anchor: "name"}
	a.grid.sel.Clear()
	a.grid.sel.Add(0)
	a.grid.sel.Add(1)
	tt.Frame()
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyN)
	tt.Frame()

	for row := 0; row < 2; row++ {
		v, ok := a.editSet().Cells[app.CellKey{Row: row, Col: 1}]
		if !ok || v != nil {
			t.Fatalf("row %d name must be pending NULL, got %v (set=%v)", row, v, ok)
		}
	}
	lines, err := a.previewEdits()
	if err != nil || len(lines) != 2 {
		t.Fatalf("preview: %v err=%v", lines, err)
	}
	for _, line := range lines {
		if !strings.Contains(line, `SET "name" = NULL`) {
			t.Fatalf("preview must set NULL: %q", line)
		}
	}
}

func TestCtrlShiftNSkipsNotNullColumn(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	s := a.testSession(t)
	if _, err := s.RunStatement(t.Context(), "CREATE TABLE strict (id INTEGER PRIMARY KEY, code TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(t.Context(), "INSERT INTO strict (id, code) VALUES (1,'x')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, code FROM strict")
	a.grid.row = 0
	a.grid.state.Cells = ui.ColumnRange{Cursor: "code", Anchor: "code"}
	a.grid.sel.Clear()
	a.grid.sel.Add(0)
	tt.Frame()
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyN)
	tt.Frame()
	if _, ok := a.editSet().Cells[app.CellKey{Row: 0, Col: 1}]; ok {
		t.Fatal("NOT NULL column must not be marked as NULL")
	}
	if !strings.Contains(a.consoleErr, "不可为空") {
		t.Fatalf("expected NOT NULL hint, got %q", a.consoleErr)
	}
}

func TestEditingNullStartsWithEmptyText(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1, NULL)"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	_ = tt
	r := a.currentRuntime().Result()
	if r == nil || a.grid.plan == nil {
		t.Fatalf("precondition: result=%v plan=%v reason=%q", r, a.grid.plan, a.grid.readonly)
	}
	a.beginEditAt(r, 0, 1)
	if !a.edit.InlineOn || a.edit.InlineText != "" {
		t.Fatalf("NULL cell must open an empty editor, got inline=%v text=%q", a.edit.InlineOn, a.edit.InlineText)
	}
	a.commitInlineEdit()
	if _, ok := a.editSet().Cells[app.CellKey{Row: 0, Col: 1}]; ok {
		t.Fatal("saving an empty editor over NULL must not create a fake change")
	}
}

func TestParseEditValueKeepsDatetimeStrings(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"2026-10-09 19:17:45", "2026-10-09 19:17:45"},
		{"2026-10-09T19:17:45+08:00", "2026-10-09T19:17:45+08:00"},
		{"42", int64(42)},
		{"-7", int64(-7)},
		{"3.5", 3.5},
		{"12abc", "12abc"},
		{"NULL", nil},
	}
	for _, c := range cases {
		if got := parseEditValue(c.in); got != c.want {
			t.Fatalf("parseEditValue(%q) = %#v (%T), want %#v", c.in, got, got, c.want)
		}
	}
	if got := parseEditedValue(nil, ""); got != nil {
		t.Fatalf("empty editor over NULL must stay NULL, got %#v", got)
	}
	if got := parseEditedValue("x", ""); got != "" {
		t.Fatalf("empty editor over text must become empty string, got %#v", got)
	}
}

func TestNewExecutionDiscardsEditState(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1,'alice'),(2,'bob')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users ORDER BY id")
	a.setCellEdit(0, 1, "alice2")
	a.grid.view.sortBy(a.currentRuntime().Result(), 1, true)
	a.grid.view.setFilter(1, "ali")
	if a.editSet().Count() == 0 {
		t.Fatal("precondition: pending edit")
	}
	tt = runEditQuery(t, a, "SELECT id, name FROM users WHERE id = 2")
	tt.Frame()
	if got := a.editSet().Count(); got != 0 {
		t.Fatalf("new execution must discard the old edit set, got %d pending changes", got)
	}
	if _, ok := a.editSet().Cells[app.CellKey{Row: 0, Col: 1}]; ok {
		t.Fatal("old pending cell leaked into the new result")
	}
	if a.grid.view.sortCol != -1 || len(a.grid.view.filters) != 0 {
		t.Fatalf("view cache leaked: sort=%d filters=%v", a.grid.view.sortCol, a.grid.view.filters)
	}
}

func TestPinnedResultKeepsEditState(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1,'alice'),(2,'bob')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users ORDER BY id")
	a.setCellEdit(0, 1, "alice2")
	a.togglePin()
	if len(a.tabs.StmtRuns) != 1 || !a.tabs.StmtRuns[0].pinned {
		t.Fatalf("pin failed: %+v", a.tabs.StmtRuns)
	}
	pinID := a.tabs.StmtRuns[0].runtimeID
	if v, ok := a.editSet().Cells[app.CellKey{Row: 0, Col: 1}]; !ok || v != "alice2" {
		t.Fatalf("pending edit must follow the pin, got %v (set=%v)", v, ok)
	}

	tt = runEditQuery(t, a, "SELECT id, name FROM users WHERE id = 2")
	if got := a.editSet().Count(); got != 0 {
		t.Fatalf("new result must start clean, got %d pending changes", got)
	}

	found := false
	for i, run := range a.tabs.StmtRuns {
		if run.runtimeID == pinID {
			a.tabs.ActiveStmt = i
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pinned run missing: %+v", a.tabs.StmtRuns)
	}
	tt.Frame()
	if a.grid.plan == nil {
		t.Fatalf("pinned result must stay editable: %q", a.grid.readonly)
	}
	if v, ok := a.editSet().Cells[app.CellKey{Row: 0, Col: 1}]; !ok || v != "alice2" {
		t.Fatalf("switching back must restore the pending edit, got %v (set=%v)", v, ok)
	}
}

func TestCommitSuccessStatusShownInResultToolbar(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1,'alice')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	a.setCellEdit(0, 1, "alice2")
	a.applyConfirmedEdits()
	for i := 0; i < 200; i++ {
		if !a.currentRuntime().Running() && a.currentRuntime().Result() != nil {
			break
		}
		tt.Frame()
	}
	tt.Frame()
	if a.consoleErr != "" {
		t.Fatalf("提交成功不应占用顶栏错误: %q", a.consoleErr)
	}
	if a.edit.Status != "提交成功" {
		t.Fatalf("edit status = %q", a.edit.Status)
	}
	if !tt.HasText("提交成功") {
		t.Fatalf("结果工具条缺少提交状态: %q", tt.Texts())
	}
}

// 双击预览/编辑的手势可配置：互换后普通双击编辑，Ctrl+双击预览。
func swapCellGestures(t *testing.T) (*root, *ui.Tester, ui.Rect) {
	t.Helper()
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1, 'alice')"); err != nil {
		t.Fatal(err)
	}
	a.setMouseBinding(mouseCellEdit, 0) // 默认预览↔编辑互换
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	if a.grid.plan == nil {
		t.Fatalf("not editable: %q", a.grid.readonly)
	}
	rect, ok := tt.Find("alice")
	if !ok {
		t.Fatalf("cell missing: %q", tt.Texts())
	}
	return a, tt, rect
}

func TestMouseDoubleClickPlainEditsWhenSwapped(t *testing.T) {
	a, tt, rect := swapCellGestures(t)
	tt.ClickAt(rect.X+2, rect.Y+2)
	tt.ClickAt(rect.X+2, rect.Y+2)
	tt.Frame()
	tt.Frame()
	if !a.edit.InlineOn || a.cellView.Open {
		t.Fatalf("普通双击应变成编辑: inline=%v view=%v", a.edit.InlineOn, a.cellView.Open)
	}
}

func TestMouseDoubleClickCtrlPreviewsWhenSwapped(t *testing.T) {
	a, tt, rect := swapCellGestures(t)
	tt.HoldModifiers(ui.Ctrl)
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+2)
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+2)
	tt.Frame()
	tt.Frame()
	tt.HoldModifiers(0)
	if a.resultClickMods != ui.Ctrl {
		t.Fatalf("应记录双击时的 Ctrl 修饰键，得到 %v", a.resultClickMods)
	}
	if !a.cellView.Open || a.edit.InlineOn {
		t.Fatalf("Ctrl+双击应变成预览: view=%v inline=%v", a.cellView.Open, a.edit.InlineOn)
	}
}
