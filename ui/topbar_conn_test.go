package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

func topBarTestRoot(t *testing.T) (*root, app.ConnProfile) {
	t.Helper()
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "查询")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}
	return a, prof
}

func doubleClickLabel(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("找不到 %q: %v", label, tt.Texts())
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
}

func TestTopBarDoubleClickConnectsAndExpands(t *testing.T) {
	a, prof := topBarTestRoot(t)
	if a.connected(prof.ID) {
		t.Fatal("测试前不应连接")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("本地") {
		t.Fatalf("顶栏没有数据源名: %v", tt.Texts())
	}
	doubleClickLabel(t, tt, "数据源 本地")
	if !a.connected(prof.ID) {
		t.Fatal("双击未连接的数据源应当建立连接")
	}
	if !a.treeExpand[prof.ID] {
		t.Fatal("双击连接后应展开左侧数据源")
	}
}

func TestTopBarDoubleClickDisconnectsAndMarksTreeStale(t *testing.T) {
	a, prof := topBarTestRoot(t)
	a.testSetSession(prof.ID, treeFakeSession{})
	base := objectTreeKey(prof.ID, "")
	groupKey := objectGroupKey(base, engine.ObjectTable)
	a.treeData[base] = map[engine.ObjectKind][]engine.DBObject{
		engine.ObjectTable: {{Kind: engine.ObjectTable, Name: "t"}},
	}
	a.treePageStarted[groupKey] = true
	a.treeCounts[base] = map[engine.ObjectKind]int{engine.ObjectTable: 1}

	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	doubleClickLabel(t, tt, "数据源 本地")
	if a.connected(prof.ID) {
		t.Fatal("双击已连接的数据源应当断开")
	}
	if len(a.sessionErr) != 0 {
		t.Fatalf("断开后不该留下连接错误: %v", a.sessionErr)
	}
	if !a.treeGroupStale[groupKey] || a.treePageStarted[groupKey] {
		t.Fatal("断开后树分组应标成过期并允许重拉首页")
	}
	if _, ok := a.treeCounts[base]; ok {
		t.Fatal("断开后计数应清掉，重连时重取")
	}
	if len(a.treeData[base][engine.ObjectTable]) != 1 {
		t.Fatal("断开后已加载对象应保留，侧栏不突然空掉")
	}
}
