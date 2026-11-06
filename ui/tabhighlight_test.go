package ui

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

// borderPixel 取标签按钮左边框上的像素：文字左缘往左 11px
// （按钮内边距 10px + 边框 2px 的内侧一列）。
func borderPixel(img *image.RGBA, r ui.Rect) (uint8, uint8, uint8) {
	p := img.RGBAAt(int(r.X)-11, int(r.Y)+int(r.H)/2)
	return p.R, p.G, p.B
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

// 激活的标签有 accent 边框，未激活的没有。
func TestTabBarActiveTabHasAccentBorder(t *testing.T) {
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
	r, g, b := borderPixel(img, active)
	if uint8(r) != accent.R || uint8(g) != accent.G || uint8(b) != accent.B {
		t.Fatalf("激活标签的边框应是 accent(%d,%d,%d)，实际 (%d,%d,%d)", accent.R, accent.G, accent.B, r, g, b)
	}
	if r, g, b := borderPixel(img, inactive); uint8(r) == accent.R && uint8(g) == accent.G && uint8(b) == accent.B {
		t.Fatalf("未激活标签不该有 accent 边框，实际 (%d,%d,%d)", r, g, b)
	}
}

// 每个标签都预留了边框的位置：切换激活项时标签尺寸与位置不动，不会跳。
func TestTabBarActiveBorderKeepsLayout(t *testing.T) {
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
	// 现在第二个才是高亮的，第一个回到无边框。
	img := tt.Image()
	if r, g, b := borderPixel(img, after2); uint8(r) != accent.R || uint8(g) != accent.G || uint8(b) != accent.B {
		t.Fatalf("切换后第二个标签应是 accent 边框，实际 (%d,%d,%d)", r, g, b)
	}
	if r, g, b := borderPixel(img, after1); uint8(r) == accent.R && uint8(g) == accent.G && uint8(b) == accent.B {
		t.Fatalf("第一个标签已不是激活项，不该有 accent 边框")
	}
}
