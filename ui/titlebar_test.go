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
}
