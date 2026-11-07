package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
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
	if a.editPlan == nil {
		t.Fatalf("single table with pk must be editable: %q", a.readonlyReason)
	}
	// JOIN → 只读
	if _, err := a.testSession(t).RunStatement(t.Context(), "CREATE TABLE other (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	tt = runEditQuery(t, a, "SELECT u.id, u.name FROM users u JOIN other o ON o.id = u.id")
	tt.Frame()
	if a.editPlan != nil || !strings.Contains(a.readonlyReason, "单表") {
		t.Fatalf("join must be read-only: plan=%v reason=%q", a.editPlan, a.readonlyReason)
	}
	// 视图 → 只读（无主键）
	if _, err := a.testSession(t).RunStatement(t.Context(), "CREATE VIEW v_users AS SELECT id, name FROM users"); err != nil {
		t.Fatal(err)
	}
	tt = runEditQuery(t, a, "SELECT id, name FROM v_users")
	tt.Frame()
	if a.editPlan != nil {
		t.Fatalf("view must be read-only: %+v", a.editPlan)
	}
}

func TestEditCellCommitThroughUI(t *testing.T) {
	a := openForSelection(t)
	a.history = app.OpenHistory(t.TempDir())
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (id, name) VALUES (1, 'alice')"); err != nil {
		t.Fatal(err)
	}
	tt := runEditQuery(t, a, "SELECT id, name FROM users")
	if a.editPlan == nil {
		t.Fatalf("not editable: %q", a.readonlyReason)
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
	ins := a.addInsertRow()
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
	if !a.previewOpen || !tt.HasText("提交 1 条改动") {
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
