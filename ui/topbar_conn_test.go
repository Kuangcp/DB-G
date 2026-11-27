package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
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

func clickLabel(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("找不到 %q: %v", label, tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

func TestTopBarClickConnectsAndExpands(t *testing.T) {
	a, prof := topBarTestRoot(t)
	if a.connected(prof.ID) {
		t.Fatal("测试前不应连接")
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if _, ok := tt.Find(locales.SessionDatasource("本地")); !ok {
		t.Fatalf("第二行没有数据源连接图标: %v", tt.Texts())
	}
	clickLabel(t, tt, locales.SessionDatasource("本地"))
	if !a.connected(prof.ID) {
		t.Fatal("单击未连接的数据源应当建立连接")
	}
	if !a.tree.Expand[prof.ID] {
		t.Fatal("单击连接后应展开左侧数据源")
	}
}

func TestTopBarClickDisconnectsAndMarksTreeStale(t *testing.T) {
	a, prof := topBarTestRoot(t)
	a.testSetSession(prof.ID, treeFakeSession{})
	base := objectTreeKey(prof.ID, "")
	groupKey := objectGroupKey(base, engine.ObjectTable)
	a.tree.Data[base] = map[engine.ObjectKind][]engine.DBObject{
		engine.ObjectTable: {{Kind: engine.ObjectTable, Name: "t"}},
	}
	a.tree.PageStarted[groupKey] = true
	a.tree.Counts[base] = map[engine.ObjectKind]int{engine.ObjectTable: 1}

	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	clickLabel(t, tt, locales.SessionDatasource("本地"))
	if a.connected(prof.ID) {
		t.Fatal("单击已连接的数据源应当断开")
	}
	if len(a.session.Err) != 0 {
		t.Fatalf("断开后不该留下连接错误: %v", a.session.Err)
	}
	if !a.tree.GroupStale[groupKey] || a.tree.PageStarted[groupKey] {
		t.Fatal("断开后树分组应标成过期并允许重拉首页")
	}
	if _, ok := a.tree.Counts[base]; ok {
		t.Fatal("断开后计数应清掉，重连时重取")
	}
	if len(a.tree.Data[base][engine.ObjectTable]) != 1 {
		t.Fatal("断开后已加载对象应保留，侧栏不突然空掉")
	}
}
