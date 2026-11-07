package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func newConsoleRoot(t *testing.T) *root {
	t.Helper()
	a := newRoot("")
	a.store = app.LoadStore(filepath.Join(t.TempDir(), "connections.json"))
	a.metaCache = app.NewMetaCache(filepath.Join(t.TempDir(), "meta"))
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
	// 一个数据源只打开一个控制台：标签只显示数据源名。
	if !tt.HasText("本地") {
		t.Fatalf("tab missing: %q", tt.Texts())
	}
	if tt.HasText("查询一") {
		t.Fatalf("single console should omit its name: %q", tt.Texts())
	}
	if !tt.HasText("默认") {
		t.Fatalf("workspace switcher missing: %q", tt.Texts())
	}

	// 同一个数据源再开一个控制台：两个标签都显示「数据源 / 控制台」。
	if _, err := a.consoleMgr().Create(prof.ID, "查询二"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("查询一") || !tt.HasText("查询二") {
		t.Fatalf("console names should show with two open consoles: %q", tt.Texts())
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
	text, err := app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store}.Read(rec.ID)
	if err != nil || text != "SELECT 'saved'" {
		t.Fatalf("Ctrl+S must flush the console: %q %v", text, err)
	}
}

func TestTabBarOverflowMenu(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	var last app.ConsoleRecord
	for i := 1; i <= 10; i++ {
		rec, err := m.Create(prof.ID, fmt.Sprintf("查询%02d", i))
		if err != nil {
			t.Fatal(err)
		}
		last = rec
	}
	tt := ui.NewTester(rootView(a), 760, 700)
	tt.Frame()
	if _, ok := tt.Find("More"); !ok {
		t.Fatalf("窄窗口应出现标签溢出菜单: %q", tt.Texts())
	}
	// 第一个标签仍然可见；工作区切换器不参与标签溢出，也必须可见。
	if first, ok := tt.Find("查询01"); !ok || first.X < 0 || first.X > 760 {
		t.Fatalf("第一个标签应留在可视区: %v %v", first, ok)
	}
	// 标签行不再放「新建控制台」：新建入口只在左侧数据源下。
	if tt.HasText("新建控制台") {
		t.Fatal("tab bar must not carry a new-console button")
	}
	if !tt.HasText("默认") {
		t.Fatalf("workspace switcher must stay visible: %q", tt.Texts())
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	want := "本地 / 查询10"
	found := false
	for _, item := range tt.Menu() {
		if item == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("溢出菜单缺少 %q: %v", want, tt.Menu())
	}
	if err := tt.ChooseMenuItem(want); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.activeConsole != last.ID {
		t.Fatalf("overflow item did not activate: %q", a.activeConsole)
	}
}

// dragTabCtrl 按住 Ctrl 从一个标签拖到另一个标签的指定横向位置（相对文本矩形）。
func dragTabCtrl(tt *ui.Tester, from, to ui.Rect, toX float32) {
	tt.HoldModifiers(ui.Ctrl)
	tt.Frame()
	sx, sy := from.X+from.W/2, from.Y+from.H/2
	tt.Press(sx, sy)
	tt.HoldModifiers(ui.Ctrl)
	tt.Frame()
	for k := 1; k <= 4; k++ {
		f := float32(k) / 4
		tt.Move(sx+(toX-sx)*f, sy)
	}
	tt.Release(toX, sy)
	tt.Frame()
	tt.HoldModifiers(0)
	tt.Frame()
}

// 按住 Ctrl 拖动标签可以调整控制台在工作区内的顺序。
func TestTabDragReordersWorkspace(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	c1, _ := m.Create(prof.ID, "一")
	c2, _ := m.Create(prof.ID, "二")
	c3, _ := m.Create(prof.ID, "三")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	rc1, ok1 := tt.Find("一")
	rc3, ok3 := tt.Find("三")
	if !ok1 || !ok3 {
		t.Fatalf("tabs missing: %v", tt.Texts())
	}
	// 把「一」拖到「三」的右半边（标签右边缘）：插到「三」后面，即工作区末尾。
	dragTabCtrl(tt, rc1, rc3, rc3.X+rc3.W-2)
	ws := mustWorkspace(t, m)
	want := []string{c2.ID, c3.ID, c1.ID}
	if len(ws.Members) != 3 || ws.Members[0] != want[0] || ws.Members[1] != want[1] || ws.Members[2] != want[2] {
		t.Fatalf("after drag: %v want %v", ws.Members, want)
	}
}

// 不按 Ctrl 拖动不会改顺序。
func TestTabDragRequiresCtrl(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	c1, _ := m.Create(prof.ID, "一")
	m.Create(prof.ID, "二")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	r1, _ := tt.Find("一")
	r2, _ := tt.Find("二")
	sx, sy := r2.X+r2.W/2, r2.Y+r2.H/2
	tt.Press(sx, sy)
	tt.Move(r1.X+2, sy)
	tt.Release(r1.X+2, sy)
	tt.Frame()
	ws := mustWorkspace(t, m)
	if len(ws.Members) != 2 || ws.Members[0] != c1.ID {
		t.Fatalf("drag without Ctrl reordered: %v", ws.Members)
	}
}

// Ctrl+Tab 在最近使用的两个控制台之间切换。
func TestCtrlTabSwitchesRecentConsole(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	c1, _ := m.Create(prof.ID, "一")
	c2, _ := m.Create(prof.ID, "二")
	if !a.activateConsole(c1.ID) {
		t.Fatal("activate 1")
	}
	if !a.activateConsole(c2.ID) {
		t.Fatal("activate 2")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Frame()
	tt.Frame()
	if a.activeConsole != c1.ID {
		t.Fatalf("Ctrl+Tab should go back to the previous console: %q", a.activeConsole)
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Frame()
	tt.Frame()
	if a.activeConsole != c2.ID {
		t.Fatalf("Ctrl+Tab again should toggle back: %q", a.activeConsole)
	}
}
