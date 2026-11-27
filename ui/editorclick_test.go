package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

// editorDDLRoot 准备一个连着 sqlite 的控制台，正文可选；返回 root 与 tester。
func editorDDLRoot(t *testing.T, sql string) (*root, *ui.Tester) {
	t.Helper()
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	rec, err := a.mgr.Create(prof.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	a.hasSelObj = false
	if sql != "" {
		a.setActiveText(sql)
	}
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	return a, tt
}

// 编辑器里 Ctrl+点击表名（mygo 把 Cmd/Ctrl+click 交给 State.OnDefinition）
// 与 Ctrl+Q 等价：打开该表的 DDL 弹窗。
func TestEditorCtrlClickTableOpensDDL(t *testing.T) {
	const sql = "select * from users where id = 1;"
	a, tt := editorDDLRoot(t, sql)
	if a.code.state.OnDefinition == nil {
		t.Fatal("编辑器没接 Ctrl+点击（State.OnDefinition 为空）")
	}
	off := strings.Index(sql, "users") + 2 // 点在词中间也算整词
	a.code.state.OnDefinition(off)
	tt.Frame()
	if !a.ddlOpen {
		t.Fatal("Ctrl+点击表名应打开 DDL 弹窗")
	}
	if !strings.Contains(a.ddlTitle, "users") {
		t.Fatalf("ddl title: %q", a.ddlTitle)
	}
	if !strings.Contains(a.ddlText, "CREATE TABLE") {
		t.Fatalf("ddl text: %q", a.ddlText)
	}
}

// 对象树/目录已知时，点在关键字上不弹 DDL；点真正的表名才弹（与 Ctrl+Q 同一过滤）。
func TestEditorCtrlClickNonTableDoesNothing(t *testing.T) {
	const sql = "select * from users;"
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	a.tree.Data = map[string]map[engine.ObjectKind][]engine.DBObject{
		objectTreeKey(prof.ID, ""): {
			engine.ObjectTable: {{Kind: engine.ObjectTable, Name: "users"}},
		},
	}
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	rec, err := a.mgr.Create(prof.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	a.hasSelObj = false
	a.setActiveText(sql)
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()

	a.code.state.OnDefinition(1) // "select" 中间
	tt.Frame()
	if a.ddlOpen {
		t.Fatalf("点关键字不该弹 DDL: %q", a.ddlTitle)
	}
	a.code.state.OnDefinition(strings.Index(sql, "users"))
	tt.Frame()
	if !a.ddlOpen || !strings.Contains(a.ddlTitle, "users") {
		t.Fatalf("点表名应弹 DDL: open=%v title=%q", a.ddlOpen, a.ddlTitle)
	}
}

// 点击偏移取词：词首/词中/词末都取整词，非标识符位置取空。
func TestEditorWordAtOffsets(t *testing.T) {
	a, _ := editorDDLRoot(t, "")
	const sql = "select * from users;"
	a.setActiveText(sql)
	base := strings.Index(sql, "users")
	for _, off := range []int{base, base + 2, base + len("users") - 1} {
		if got := editorWordAt(a.code, off); got != "users" {
			t.Fatalf("editorWordAt(%d) = %q, want users", off, got)
		}
	}
	star := strings.Index(sql, "*")
	if got := editorWordAt(a.code, star); got != "" {
		t.Fatalf("editorWordAt(%d) = %q, want empty", star, got)
	}
	if got := editorWordAt(a.code, len(sql)+5); got != "" {
		t.Fatalf("out of range word = %q, want empty", got)
	}
}
