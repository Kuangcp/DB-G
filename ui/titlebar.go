package ui

import (
	"math"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// TitleBarHeight 是自绘标题栏的高度（DIP）：窗口内容顶部留出的一条，
// 整条作为拖拽区，右侧放窗口控制按钮。
const TitleBarHeight = 38

// titleWindow 是自绘标题栏需要的窗口控制能力（主窗口与设置窗口共用）。
type titleWindow interface {
	Minimize()
	IsMaximized() bool
	ToggleMaximize()
	Close()
}

// windowButtons 画最小化/最大化/关闭；调用方负责在它前面留空隙。
func windowButtons(c *ui.Context, win titleWindow) {
	if win == nil {
		return
	}
	t := c.Theme()
	if iconButtonPlain(c, iconMinus, "最小化", t.TextMuted).Clicked() {
		win.Minimize()
	}
	maximized := win.IsMaximized()
	icon, label := iconExpandAll, "最大化"
	if maximized {
		icon, label = iconCollapseAll, "还原"
	}
	if iconButtonPlain(c, icon, label, t.TextMuted).Clicked() {
		win.ToggleMaximize()
	}
	if iconButtonPlain(c, iconClose, "关闭窗口", t.TextMuted).Clicked() {
		win.Close()
	}
}

// settingsTitleBar 是设置窗口的自绘标题栏：与主窗口同样的高度、拖拽区与窗口按钮。
func (a *root) settingsTitleBar(c *ui.Context) {
	t := c.Theme()
	bar := ui.Row(c).Height(TitleBarHeight).FillWidth().Padding(0, 12).Gap(8).
		AlignItems(ui.Center).DragWindow().Background(t.Surface)
	bar.Children(func() {
		ui.Text(c, "设置").Bold().FontSize(12.5)
		ui.Text(c, app.DisplayName).FontSize(11).TextColor(t.TextMuted)
		ui.Spacer(c)
		ui.Box(c).Width(6)
		if a.settingsWin != nil {
			windowButtons(c, a.settingsWin)
		}
	})
}

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
		ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
			if iconButtonPlain(c, iconFolderOpen, "打开 SQL 文件", t.TextMuted).Clicked() {
				a.pickOpenSQLFiles()
			}
			if iconButtonPlain(c, iconHistory, "历史", t.TextMuted).Clicked() {
				a.historyOpen = true
			}
			aiColor := t.TextMuted
			if a.aiChat.open {
				aiColor = t.Accent
			}
			if iconButtonPlain(c, iconSparkles, "AI 助手", aiColor).Clicked() {
				a.aiChat.open = !a.aiChat.open
				if a.aiChat.open {
					a.loadAIChat()
					a.aiChat.scroll.Y = math.MaxFloat32
				} else {
					a.stopAI()
				}
			}
			if iconButtonPlain(c, iconSettings, "设置", t.TextMuted).Clicked() {
				a.requestSettings()
			}
		})
		// 应用操作与窗口控制之间留一点空白，避免误点关闭。
		ui.Box(c).Width(6)
		if a.win != nil {
			windowButtons(c, a.win)
		}
	})
}
