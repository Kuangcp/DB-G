package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// tabBox 是标签文字周围的一圈，用来找激活标签底部的 accent 线。
func tabBox(r ui.Rect) ui.Rect {
	return ui.Rect{X: r.X - 6, Y: r.Y - 6, W: r.W + 12, H: r.H + 18}
}

// newTabRoot 建一个有两个控制台、第一个已激活的根状态。
func newTabRoot(t *testing.T) (*root, *ui.Tester, ui.Color) {
	t.Helper()
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	first, err := a.consoleMgr().Create(prof.ID, "查询一")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.consoleMgr().Create(prof.ID, "查询二"); err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(first.ID) {
		t.Fatal("activate")
	}
	var accent ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		accent = c.Theme().Accent
		rootView(a)(c)
	}, 1200, 700)
	tt.Frame()
	return a, tt, accent
}

// 标签旁边不再有关闭按钮：关闭走右键菜单。
func TestTabBarHasNoCloseButton(t *testing.T) {
	_, tt, _ := newTabRoot(t)
	if _, ok := tt.Find("关闭控制台"); ok {
		t.Fatalf("标签上不该再有关闭按钮: %q", tt.Texts())
	}
	// 关闭走右键菜单，且真的能关掉。
	r, ok := tt.Find("查询一")
	if !ok {
		t.Fatal("tab missing")
	}
	if err := tt.RightClick("查询一"); err != nil {
		t.Fatalf("右键标签 (%v): %v", r, err)
	}
	if err := tt.ChooseMenuItem("关闭"); err != nil {
		t.Fatalf("右键菜单里应有「关闭」: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find("查询一"); ok {
		t.Fatalf("右键「关闭」之后标签应该没了: %q", tt.Texts())
	}
}

// 激活标签的底部有 accent 线，未激活的没有。
func TestTabBarActiveTabHasAccentLine(t *testing.T) {
	_, tt, accent := newTabRoot(t)
	img := tt.Image()
	active, ok := tt.Find("查询一")
	if !ok {
		t.Fatal("active tab missing")
	}
	inactive, ok := tt.Find("查询二")
	if !ok {
		t.Fatal("inactive tab missing")
	}
	if !rectHasColor(img, tabBox(active), accent) {
		t.Fatal("active tab should show an accent line")
	}
	if rectHasColor(img, tabBox(inactive), accent) {
		t.Fatal("inactive tab should have no accent line")
	}
}

// 每个标签都预留了底部线的位置：切换激活项时标签尺寸与位置不动，不会跳。
func TestTabBarActiveTabKeepsLayout(t *testing.T) {
	a, tt, accent := newTabRoot(t)
	before1, _ := tt.Find("查询一")
	before2, _ := tt.Find("查询二")

	// 换成激活第二个。
	for _, rec := range a.consoleMgr().Open() {
		if rec.Name == "查询二" {
			a.pendingActivate = rec.ID
		}
	}
	tt.Frame()
	after1, _ := tt.Find("查询一")
	after2, _ := tt.Find("查询二")
	if before1 != after1 || before2 != after2 {
		t.Fatalf("切换激活标签后布局动了:\n before %v %v\n after  %v %v", before1, before2, after1, after2)
	}
	img := tt.Image()
	if !rectHasColor(img, tabBox(after2), accent) {
		t.Fatal("second tab should now show the accent line")
	}
	if rectHasColor(img, tabBox(after1), accent) {
		t.Fatal("first tab should no longer show the accent line")
	}
}
