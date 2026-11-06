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
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (name) VALUES ('alice')"); err != nil {
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
