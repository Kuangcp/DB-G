package ui

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func TestTitleBarRendersIconAndActions(t *testing.T) {
	a := newTestRoot()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	a.SetAppIconPNG(buf.Bytes())
	tt := ui.NewTester(func(c *ui.Context) {
		a.applyTheme(c)
		a.titleBar(c)
	}, 800, 80)
	tt.Frame()
	if !tt.HasText(app.DisplayName) {
		t.Fatalf("title bar missing app name: %v", tt.Texts())
	}
	if tt.HasText("SQL 工作台") {
		t.Fatalf("title bar must not show subtitle: %v", tt.Texts())
	}
	for _, label := range []string{"打开 SQL 文件", "历史", "AI 助手", "设置"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("title bar action %q missing: %v", label, tt.Texts())
		}
	}
	open, _ := tt.Find("打开 SQL 文件")
	settings, _ := tt.Find("设置")
	if open.X >= settings.X {
		t.Fatalf("title bar actions out of order: open=%v settings=%v", open, settings)
	}
}

type fakeTitleWindow struct{ maximized bool }

func (w *fakeTitleWindow) Minimize()         {}
func (w *fakeTitleWindow) IsMaximized() bool { return w.maximized }
func (w *fakeTitleWindow) ToggleMaximize()   { w.maximized = !w.maximized }
func (w *fakeTitleWindow) Close()            {}

func TestWindowButtonsMaximizeAndRestore(t *testing.T) {
	a := newTestRoot()
	for _, max := range []bool{false, true} {
		w := &fakeTitleWindow{maximized: max}
		tt := ui.NewTester(func(c *ui.Context) {
			a.applyTheme(c)
			windowButtons(c, w)
		}, 300, 80)
		tt.Frame()
		want := "最大化"
		if max {
			want = "还原"
		}
		if _, ok := tt.Find(want); !ok {
			t.Fatalf("window button %q missing: %v", want, tt.Texts())
		}
	}
}
