package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/locales"
)

// 标题栏搜索框打开命令面板；搜控制台内 SQL 后点击跳到对应控制台。
func TestPaletteSearchesSQLAndSwitchesConsole(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	first, err := m.Create(prof.ID, "查询一")
	if err != nil {
		t.Fatal(err)
	}
	m.SetText(first.ID, "SELECT id FROM users")
	second, err := m.Create(prof.ID, "查询二")
	if err != nil {
		t.Fatal(err)
	}
	m.SetText(second.ID, "SELECT 1")
	if !a.activateConsole(second.ID) {
		t.Fatal("activate")
	}

	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click(locales.PaletteOpen()); err != nil {
		t.Fatalf("点击标题栏搜索框: %v", err)
	}
	tt.Frame()
	if !a.paletteOpen {
		t.Fatal("搜索框应打开命令面板")
	}
	if !tt.HasText(locales.PaletteSectionConsoles()) {
		t.Fatalf("面板应列出控制台: %v", tt.Texts())
	}
	tt.Type("users")
	tt.Frame()
	if !tt.HasText(locales.PaletteSectionSQL()) {
		t.Fatalf("面板应列出 SQL 匹配: %v", tt.Texts())
	}
	if err := tt.Click("本地 / 查询一"); err != nil {
		t.Fatalf("点击 SQL 匹配行: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if a.paletteOpen {
		t.Fatal("选中后面板应关闭")
	}
	if a.activeConsole != first.ID {
		t.Fatalf("应跳到匹配的控制台: got %q want %q", a.activeConsole, first.ID)
	}
}

// Ctrl+P 打开命令面板，空查询时列出推荐操作，输入可按操作名过滤。
func TestPaletteShortcutAndActionSearch(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyP)
	tt.Frame()
	if !a.paletteOpen {
		t.Fatal("Ctrl+P 应打开命令面板")
	}
	if !tt.HasText(locales.KeySaveConsole()) {
		t.Fatalf("空查询应列出推荐操作: %v", tt.Texts())
	}
	tt.Type("保存")
	tt.Frame()
	if !tt.HasText(locales.KeySaveConsole()) {
		t.Fatalf("按操作名过滤后应保留保存控制台: %v", tt.Texts())
	}
	if tt.HasText(locales.KeyCopyCell()) {
		t.Fatalf("不匹配的操作应被过滤掉: %v", tt.Texts())
	}
}
