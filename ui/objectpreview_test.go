package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

func TestPreviewObjectCreatesConsoleWithoutRunning(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (name) VALUES ('alice')"); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.previewObject(prof, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	for range 3 {
		tt.Frame()
	}
	want := `SELECT * FROM "users" LIMIT 200;`
	if got := a.activeText(); got != want {
		t.Fatalf("preview sql: %q", got)
	}
	if r := a.currentRuntime().Result(); r != nil {
		t.Fatalf("预览只插入 SQL，不能自动执行: %+v", r)
	}
}

// 已有控制台时不再新建：追加到第一个控制台保存的光标处，并跳过去。
func TestPreviewObjectReusesFirstConsole(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	other := seedConsoleConn(t, a, "另一个")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	m := a.consoleMgr()
	rec, err := m.Create(prof.ID, "我的")
	if err != nil {
		t.Fatal(err)
	}
	const before = "SELECT 1;"
	m.SetText(rec.ID, before)
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	m.SetCaret(rec.ID, len([]rune(before)), len([]rune(before)))

	// 当前激活的是另一个数据源的控制台：仍要回到本数据源的第一个控制台。
	otherRec, err := m.Create(other.ID, "别处")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(otherRec.ID) {
		t.Fatal("activate other console")
	}

	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.previewObject(prof, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	tt.Frame()
	tt.Frame()

	if got, want := len(m.ConsolesOf(prof.ID)), 1; got != want {
		t.Fatalf("复用控制台时不该新建: %d", got)
	}
	if a.activeConsole != rec.ID {
		t.Fatalf("应跳到本数据源的控制台: %q", a.activeConsole)
	}
	want := before + "\n" + `SELECT * FROM "users" LIMIT 200;`
	if got := a.activeText(); got != want {
		t.Fatalf("追加位置不对:\n got %q\nwant %q", got, want)
	}
}

// 当前激活的就是本数据源的控制台：直接在它的光标处追加，不切换。
func TestPreviewObjectAppendsToActiveConsole(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	m := a.consoleMgr()
	rec, err := m.Create(prof.ID, "我的")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	const before = "SELECT 1;\nSELECT 2;"
	a.setActiveText(before)
	a.setSelection(len([]rune("SELECT 1;")), len([]rune("SELECT 1;")))
	if err := a.flushActive(); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.previewObject(prof, engine.DBObject{Kind: engine.ObjectTable, Name: "users"})
	tt.Frame()
	tt.Frame()

	if got, want := len(m.ConsolesOf(prof.ID)), 1; got != want {
		t.Fatalf("激活控制台时不该新建: %d", got)
	}
	if a.activeConsole != rec.ID {
		t.Fatalf("不该切走: %q", a.activeConsole)
	}
	sql := `SELECT * FROM "users" LIMIT 200;`
	want := "SELECT 1;\n" + sql + "\nSELECT 2;"
	if got := a.activeText(); got != want {
		t.Fatalf("追加位置不对:\n got %q\nwant %q", got, want)
	}
	// 光标要停在新追加的 SQL 上，紧接着的执行才会命中它。
	start, _ := a.code.CaretRunes()
	if got := string([]rune(a.activeText())[:start]); !strings.HasSuffix(got, sql) {
		t.Fatalf("插入后光标不在新 SQL 末尾: %q", got)
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

// DDL 弹窗：正文格式化 + SQL 高亮 + 可选中部分复制。
func TestDDLPanelFormatsAndSupportsPartialCopy(t *testing.T) {
	a := newConsoleRoot(t)
	raw := "create table users ( id integer primary key, name text )"
	a.setDDL("users", raw)
	if a.ddlText != raw {
		t.Fatalf("raw ddl must be kept for copy-all: %q", a.ddlText)
	}
	formatted := a.ddlState.Buffer.String()
	if !strings.Contains(formatted, "CREATE TABLE") || !strings.Contains(formatted, "PRIMARY KEY") {
		t.Fatalf("ddl must be formatted (keywords uppercased): %q", formatted)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("DDL：users") {
		t.Fatalf("panel title missing: %q", tt.Texts())
	}
	// 选中前 6 个字符再 Ctrl+C：剪贴板只该有选区内容。
	if n := len(formatted); n > 6 {
		a.ddlState.Select(0, 6)
		tt.Frame()
		tt.Key(ui.Ctrl, ui.KeyC)
		tt.Frame()
		if got := tt.Clipboard(); got != formatted[:6] {
			t.Fatalf("partial copy: %q want %q", got, formatted[:6])
		}
	}
	// 「复制全部」拿完整（格式化后的）DDL。
	if err := tt.Click("复制全部"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != formatted {
		t.Fatalf("copy all: %q", got)
	}
}
