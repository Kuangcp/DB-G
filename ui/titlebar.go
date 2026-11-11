package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// TitleBarHeight 是自绘标题栏的高度（DIP）：窗口内容顶部留出的一条，
// 整条作为拖拽区，右侧放窗口控制按钮。
const TitleBarHeight = 38

// titleBar 是自绘标题栏：整条可拖动，双击最大化；右侧是窗口控制按钮。
// 只在有原生窗口时显示按钮，headless 测试仍能看到应用名与拖拽区。
func (a *root) titleBar(c *ui.Context) {
	t := c.Theme()
	bar := ui.Row(c).Height(TitleBarHeight).FillWidth().Padding(0, 12).Gap(8).
		AlignItems(ui.Center).DragWindow().Background(t.Surface)
	bar.Children(func() {
		ui.Text(c, app.DisplayName).Bold().FontSize(12.5)
		ui.Text(c, "SQL 工作台").FontSize(11).TextColor(t.TextMuted)
		ui.Spacer(c)
		if a.win != nil {
			if iconButtonPlain(c, iconMinus, "最小化", t.TextMuted).Clicked() {
				a.win.Minimize()
			}
			maximized := a.win.IsMaximized()
			icon, label := iconExpandAll, "最大化"
			if maximized {
				icon, label = iconCollapseAll, "还原"
			}
			if iconButtonPlain(c, icon, label, t.TextMuted).Clicked() {
				a.win.ToggleMaximize()
			}
			if iconButtonPlain(c, iconClose, "关闭窗口", t.TextMuted).Clicked() {
				a.win.Close()
			}
		}
	})
}
