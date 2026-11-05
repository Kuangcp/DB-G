package ui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func newConsoleRoot(t *testing.T) *root {
	t.Helper()
	a := newRoot("")
	a.store = app.LoadStore(filepath.Join(t.TempDir(), "connections.json"))
	a.consoles, a.lastVer = map[string]*app.Console{}, map[string]uint64{}
	return a
}

func mustWorkspace(t *testing.T, m *app.ConsoleManager) app.WorkspaceRecord {
	t.Helper()
	ws, ok := m.ActiveWorkspace()
	if !ok {
		t.Fatal("no active workspace")
	}
	return ws
}

func seedConsoleConn(t *testing.T, a *root, name string) app.ConnProfile {
	t.Helper()
	p := app.ConnProfile{Name: name, Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	return a.store.List()[len(a.store.List())-1]
}

func TestTabBarShowsWorkspaceMembers(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if _, err := a.consoleMgr().Create(prof.ID, "查询一"); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("查询一") {
		t.Fatalf("tab missing: %q", tt.Texts())
	}
	if !tt.HasText("默认") {
		t.Fatalf("workspace switcher missing: %q", tt.Texts())
	}
}

func TestTabClickSwitchesBufferAndCaret(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	first, _ := m.Create(prof.ID, "A")
	m.SetText(first.ID, "SELECT 1")
	second, _ := m.Create(prof.ID, "B")
	m.SetText(second.ID, "SELECT 2")
	m.SetCaret(second.ID, 3, 3)

	if !a.activateConsole(first.ID) {
		t.Fatal("activate A")
	}
	if got := a.activeText(); got != "SELECT 1" {
		t.Fatalf("buffer: %q", got)
	}
	if !a.activateConsole(second.ID) {
		t.Fatal("activate B")
	}
	if got := a.activeText(); got != "SELECT 2" {
		t.Fatalf("buffer: %q", got)
	}
	if s, e := a.code.CaretRunes(); s != 3 || e != 3 {
		t.Fatalf("caret of B: %d,%d", s, e)
	}
	// 失焦后再激活 A：A 的光标也应从元数据恢复（0/0）
	if !a.activateConsole(first.ID) {
		t.Fatal("reactivate A")
	}
	if s, e := a.code.CaretRunes(); s != 0 || e != 0 {
		t.Fatalf("caret of A: %d,%d", s, e)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if s, e := a.code.CaretRunes(); s != 0 || e != 0 {
		t.Fatalf("frames must not move the caret: %d,%d", s, e)
	}
}

func TestTabDirtyMarker(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, _ := a.consoleMgr().Create(prof.ID, "脏")
	a.consoleMgr().SetText(rec.ID, "SELECT 1")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("●") {
		t.Fatalf("dirty dot missing: %q", tt.Texts())
	}
	a.consoleMgr().Flush(rec.ID)
	tt.Frame()
	if tt.HasText("● 脏") {
		t.Fatalf("dirty dot must clear after flush: %q", tt.Texts())
	}
}

func TestTabCreateButtonNeedsConnection(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	// mygo 的 headless Click 不会因 Disabled 报错，因此断言副作用：没有连接时点＋不得建工作区/控制台。
	_ = tt.Click("＋")
	if m := a.consoleMgr(); len(m.Workspaces()) != 0 || len(m.Open()) != 0 {
		t.Fatalf("＋ must not create anything without a connection: ws=%d open=%d", len(m.Workspaces()), len(m.Open()))
	}
}

func TestCaretClampedAfterExternalShorten(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	rec, _ := m.Create(prof.ID, "c")
	m.SetText(rec.ID, "SELECT 1")
	m.SetCaret(rec.ID, 500, 500) // 超出正文长度
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Frame()
	limit := len([]rune("SELECT 1"))
	if s, e := a.code.CaretRunes(); s > limit || e > limit {
		t.Fatalf("caret not clamped: %d %d", s, e)
	}
}

func TestEmptyStateHintWithoutConsole(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("在左侧数据源下新建或打开控制台") {
		t.Fatalf("hint missing: %q", tt.Texts())
	}
}

func TestWorkspaceCreateAndSwitch(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	recA, err := m.Create(prof.ID, "A")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(recA.ID) {
		t.Fatal("activate A")
	}
	firstWS := mustWorkspace(t, m)

	a.openModal("ws-new", "", "B")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("新建工作区") {
		t.Fatalf("modal missing: %q", tt.Texts())
	}
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	ws, ok := m.ActiveWorkspace()
	if !ok || ws.Name != "B" {
		t.Fatalf("active workspace: %+v %v", ws, ok)
	}
	if len(m.Open()) != 0 {
		t.Fatalf("new workspace must start empty: %+v", m.Open())
	}
	if a.activeConsole != "" {
		t.Fatalf("editor must be in empty state, active=%q", a.activeConsole)
	}
	if !tt.HasText("在左侧数据源下新建或打开控制台") {
		t.Fatalf("empty hint missing: %q", tt.Texts())
	}

	a.pendingSwitchWS = firstWS.ID
	tt.Frame()
	tt.Frame()
	if a.activeConsole != recA.ID {
		t.Fatalf("switching back must restore A: %q", a.activeConsole)
	}
	if got := a.activeText(); got != "" {
		t.Fatalf("A body: %q", got)
	}
}

func TestWorkspaceDeleteKeepsConsoles(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	rec, _ := m.Create(prof.ID, "A")
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate A")
	}
	ws := mustWorkspace(t, m)
	a.openModal("ws-delete", ws.ID, ws.Name)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := m.ActiveWorkspace(); ok {
		t.Fatal("workspace must be deleted")
	}
	if a.activeConsole != "" {
		t.Fatalf("editor must be empty: %q", a.activeConsole)
	}
	if recs := m.ConsolesOf(prof.ID); len(recs) != 1 {
		t.Fatalf("console must survive: %+v", recs)
	}
}

func TestSwitchKeepsCaretWithWideRunes(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	A, _ := m.Create(prof.ID, "甲")
	m.SetText(A.ID, "SELECT '你好😀'")
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
	a.setSelection(9, 9)
	tt.Frame()
	if err := tt.Click("乙"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if err := tt.Click("甲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if got := a.activeText(); got != "SELECT '你好😀'" {
		t.Fatalf("body: %q", got)
	}
	if s, e := a.code.CaretRunes(); s != 9 || e != 9 {
		t.Fatalf("caret must come back at the same rune: %d,%d", s, e)
	}
}

func TestCtrlSSavesActiveConsole(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	rec, _ := m.Create(prof.ID, "保存")
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Click("编辑器")
	tt.Frame()
	a.setActiveText("SELECT 'saved'")
	tt.Frame() // 帧首同步正文版本
	tt.Key(ui.Ctrl, ui.KeyS)
	tt.Frame()
	text, err := app.ConsoleFiles{Dir: a.store.ConsolesDir()}.Read(rec.ID)
	if err != nil || text != "SELECT 'saved'" {
		t.Fatalf("Ctrl+S must flush the console: %q %v", text, err)
	}
}
