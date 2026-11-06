package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// addConn 往连接库里加一个指向新建 sqlite 文件的连接，返回存好的档案。
func addConn(t *testing.T, a *root, name string) app.ConnProfile {
	t.Helper()
	if err := a.store.Add(app.ConnProfile{Name: name, Kind: "sqlite", Path: seedConnFile(t)}); err != nil {
		t.Fatal(err)
	}
	list := a.store.List()
	return list[len(list)-1]
}

// 两个数据源可以同时连着：连 B 不会把 A 关掉，A 的树也不会出现「未连接」。
func TestMultipleConnectionsStayOpen(t *testing.T) {
	a := newSidebarRoot(t)
	a1 := addConn(t, a, "甲")
	b1 := addConn(t, a, "乙")
	if err := a.openConn(a1); err != nil {
		t.Fatal(err)
	}
	if err := a.openConn(b1); err != nil {
		t.Fatal(err)
	}
	if !a.connected(a1.ID) || !a.connected(b1.ID) {
		t.Fatalf("两个都该连着: %d", len(a.sessions))
	}
	a.expandConnAndTree(a1)
	a.expandConnAndTree(b1)
	if err := a.treeErr[a1.ID]; err != "" {
		t.Fatalf("甲不该报 %q", err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if tt.HasText("未连接") {
		t.Fatalf("树里不该再出现「未连接」: %q", tt.Texts())
	}
}

// 打开第 7 个时，最久没用的那个被关掉。
func TestLRUEvictsLeastRecentlyUsed(t *testing.T) {
	a := newSidebarRoot(t)
	var conns []app.ConnProfile
	for i := 0; i < maxOpenSessions; i++ {
		p := addConn(t, a, fmt.Sprintf("库%d", i))
		if err := a.openConn(p); err != nil {
			t.Fatal(err)
		}
		conns = append(conns, p)
	}
	if len(a.sessions) != maxOpenSessions {
		t.Fatalf("sessions = %d", len(a.sessions))
	}
	// 把第 1 个再「用」一次，它就变成最新的，该被淘汰的是第 2 个。
	a.sessionFor(conns[0].ID)
	extra := addConn(t, a, "新库")
	if err := a.openConn(extra); err != nil {
		t.Fatal(err)
	}
	if len(a.sessions) != maxOpenSessions {
		t.Fatalf("淘汰后应保持 %d 个，实际 %d", maxOpenSessions, len(a.sessions))
	}
	if !a.connected(conns[0].ID) {
		t.Fatal("刚用过的第 1 个不该被淘汰")
	}
	if a.connected(conns[1].ID) {
		t.Fatal("最久没用的第 2 个应被淘汰")
	}
	if !a.connected(extra.ID) {
		t.Fatal("新连的要在")
	}
}

// 正在跑语句的数据源不会被淘汰。
func TestEvictionSkipsBusyConnection(t *testing.T) {
	a := newSidebarRoot(t)
	p := addConn(t, a, "忙库")
	if err := a.openConn(p); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(p.ID, "跑着")
	if err != nil {
		t.Fatal(err)
	}
	rt := a.runtime(rec.ID)
	if !rt.Run(blockingFake{}, "SELECT 1") {
		t.Fatal("must start")
	}
	if !a.connBusy(p.ID) {
		t.Fatal("有语句在跑就该算忙")
	}
	// 补到上限再连一个新的：忙的那个必须留下，淘汰别人。
	for i := 0; i < maxOpenSessions-1; i++ {
		q := addConn(t, a, fmt.Sprintf("闲库%d", i))
		if err := a.openConn(q); err != nil {
			t.Fatal(err)
		}
	}
	if !a.connected(p.ID) {
		t.Fatal("正在执行的连接不能被关掉")
	}
	if len(a.sessions) != maxOpenSessions {
		t.Fatalf("sessions = %d", len(a.sessions))
	}
	rt.Cancel()
}

// 启动不连任何数据源。
func TestStartupConnectsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	a := newRootWithStore(path)
	if n := len(a.sessions); n != 0 {
		t.Fatalf("启动时不该连任何数据源，实际 %d 个", n)
	}
	if len(a.treeData) != 0 {
		t.Fatalf("启动时不该预载对象树: %d", len(a.treeData))
	}
}

// 圆点：没连是灰的，连上是绿的，打开失败是红的。
func TestConnDotColors(t *testing.T) {
	a := newSidebarRoot(t)
	th := ui.LightTheme()
	p := addConn(t, a, "库")
	if got := a.connDotColor(p.ID, th); got != th.TextMuted {
		t.Fatalf("未连接应为灰色: %+v", got)
	}
	if err := a.openConn(p); err != nil {
		t.Fatal(err)
	}
	if got := a.connDotColor(p.ID, th); got != th.Success {
		t.Fatalf("已连接应为绿色: %+v", got)
	}
	a.sessionErr[p.ID] = "boom"
	if got := a.connDotColor(p.ID, th); got != th.Danger {
		t.Fatalf("失败应为红色: %+v", got)
	}
}

// 点数据源名就连上它（启动时不预连，连接入口只有这一处）。
func TestClickingConnNameConnects(t *testing.T) {
	a := newSidebarRoot(t)
	p := addConn(t, a, "点我来连")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if len(a.sessions) != 0 {
		t.Fatal("还没点就不该连")
	}
	if err := tt.Click("点我来连"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.connected(p.ID) {
		t.Fatal("点数据源名应当连上")
	}
	if !a.treeExpand[p.ID] {
		t.Fatal("点数据源名应当展开")
	}
}

// 「刷新」只清对象树缓存，不替用户连任何数据源。
func TestRefreshDoesNotConnect(t *testing.T) {
	a := newSidebarRoot(t)
	addConn(t, a, "别自动连")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("重载对象树（不连接数据源）"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.sessions) != 0 {
		t.Fatalf("刷新不该连数据源: %d", len(a.sessions))
	}
}
