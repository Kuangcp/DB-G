package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func TestTitleBarRendersAppName(t *testing.T) {
	a := newTestRoot()
	tt := ui.NewTester(func(c *ui.Context) {
		a.applyTheme(c)
		a.titleBar(c)
	}, 800, 80)
	tt.Frame()
	if !tt.HasText(app.DisplayName) {
		t.Fatalf("title bar missing app name: %v", tt.Texts())
	}
	if !tt.HasText("SQL 工作台") {
		t.Fatalf("title bar missing subtitle: %v", tt.Texts())
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
