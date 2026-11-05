package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSwitchingTabsSwapsEditorContent(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	A, err := m.Create(prof.ID, "一是甲")
	if err != nil {
		t.Fatal(err)
	}
	m.SetText(A.ID, "SELECT 'A'")
	if err := m.Flush(A.ID); err != nil {
		t.Fatal(err)
	}
	B, err := m.Create(prof.ID, "二是乙")
	if err != nil {
		t.Fatal(err)
	}
	m.SetText(B.ID, "SELECT 'B'")
	if err := m.Flush(B.ID); err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(A.ID) {
		t.Fatal("activate A")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Frame()
	if got := a.activeText(); got != "SELECT 'A'" {
		t.Fatalf("first frame body: %q", got)
	}
	// 用户点 B 标签
	if err := tt.Click("二是乙"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if got := a.activeText(); got != "SELECT 'B'" {
		t.Fatalf("editor must show B after clicking its tab, got %q", got)
	}
	// 再点回 A
	if err := tt.Click("一是甲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if got := a.activeText(); got != "SELECT 'A'" {
		t.Fatalf("editor must show A after clicking back, got %q", got)
	}
}

func TestCloseActiveSwitchesEditorContent(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	A, _ := m.Create(prof.ID, "甲")
	m.SetText(A.ID, "SELECT 'A'")
	_ = m.Flush(A.ID)
	B, _ := m.Create(prof.ID, "乙")
	m.SetText(B.ID, "SELECT 'B'")
	_ = m.Flush(B.ID)
	if !a.activateConsole(B.ID) {
		t.Fatal("activate B")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Frame()
	a.closeConsole(B.ID)
	tt.Frame()
	tt.Frame()
	if a.activeConsole != A.ID {
		t.Fatalf("closing B must activate A, active=%q", a.activeConsole)
	}
	if got := a.activeText(); got != "SELECT 'A'" {
		t.Fatalf("editor must show A after closing B, got %q", got)
	}
	if !tt.HasText("甲") || tt.HasText("乙") {
		t.Fatalf("tab bar stale: %q", tt.Texts())
	}
}

func TestSwitchPreservesUnsavedEdits(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	A, _ := m.Create(prof.ID, "甲")
	m.SetText(A.ID, "SELECT 'A'")
	_ = m.Flush(A.ID)
	B, _ := m.Create(prof.ID, "乙")
	m.SetText(B.ID, "SELECT 'B'")
	_ = m.Flush(B.ID)
	if !a.activateConsole(A.ID) {
		t.Fatal("activate A")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Frame()

	// 在 A 里改一行尚未到防抖时间，就切到 B
	a.setActiveText("SELECT 'A edited'")
	tt.Frame()
	if err := tt.Click("乙"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if got := a.activeText(); got != "SELECT 'B'" {
		t.Fatalf("editor must show B, got %q", got)
	}
	if err := tt.Click("甲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if got := a.activeText(); got != "SELECT 'A edited'" {
		t.Fatalf("unsaved A edit must survive the switch, got %q", got)
	}
	// B 的内容没被 A 覆盖
	if text, _ := m.Text(B.ID); text != "SELECT 'B'" {
		t.Fatalf("B body corrupted: %q", text)
	}
}
