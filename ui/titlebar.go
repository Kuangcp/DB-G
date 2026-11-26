package ui

import (
	"math"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
)

// TitleBarHeight 是主窗口右列顶栏的高度（DIP）：右侧编辑区顶部留出的一条，
// 整条作为拖拽区，右侧放窗口控制按钮。左侧数据源列表从窗口顶端开始，不占这条。
const TitleBarHeight = 38

// titleWindow 是自绘标题栏需要的窗口控制能力（主窗口与设置窗口共用）。
type titleWindow interface {
	Minimize()
	IsMaximized() bool
	ToggleMaximize()
	Close()
}

// 窗口控制图标沿用 git-k：正方形表示最大化，两个叠放方框表示还原，比折叠箭头直观。
var (
	windowMaximizeIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>`))
	windowRestoreIcon  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="5" y="8" width="11" height="11" rx="1"/><path d="M9 8V5h10v10h-3"/></svg>`))
)

// windowButtons 画最小化/最大化/关闭；调用方负责在它前面留空隙。
func windowButtons(c *ui.Context, win titleWindow) {
	if win == nil {
		return
	}
	t := c.Theme()
	if iconButtonPlain(c, iconMinus, locales.WindowMinimize(), t.TextMuted).Clicked() {
		win.Minimize()
	}
	maximized := win.IsMaximized()
	icon, label := windowMaximizeIcon, locales.WindowMaximize()
	if maximized {
		icon, label = windowRestoreIcon, locales.WindowRestore()
	}
	if iconButtonPlain(c, icon, label, t.TextMuted).Clicked() {
		win.ToggleMaximize()
	}
	if iconButtonPlain(c, iconClose, locales.WindowClose(), t.TextMuted).Clicked() {
		win.Close()
	}
}

// settingsTitleBar 是设置窗口的自绘标题栏：与主窗口同样的高度、拖拽区与窗口按钮。
func (a *root) settingsTitleBar(c *ui.Context) {
	t := c.Theme()
	bar := ui.Row(c).Height(TitleBarHeight).FillWidth().Padding(0, 12).Gap(8).
		AlignItems(ui.Center).DragWindow().Background(t.Surface)
	bar.Children(func() {
		ui.Text(c, locales.SettingsTitle()).Bold().FontSize(12.5)
		ui.Text(c, app.DisplayName).FontSize(11).TextColor(t.TextMuted)
		ui.Spacer(c)
		ui.Box(c).Width(6)
		if a.settingsWin != nil {
			windowButtons(c, a.settingsWin)
		}
	})
}

// topBar 是主窗口右列顶栏：整条可拖动，双击最大化。左边是当前数据源名与状态、
// 数据库（schema）切换和提示信息，右边是应用操作与窗口控制按钮。
// 左侧数据源列表改成通顶全高后，这条只覆盖编辑区一侧。
func (a *root) topBar(c *ui.Context) {
	t := c.Theme()
	bar := ui.Row(c).Height(TitleBarHeight).FillWidth().Padding(0, 12).Gap(8).
		AlignItems(ui.Center).DragWindow().Background(t.Surface)
	bar.Children(func() {
		if a.sidebarHidden {
			if iconButtonPlain(c, iconPanelLeftOpen, locales.SidebarShow(), t.TextMuted).Clicked() {
				a.sidebarHidden = false
			}
		}
		connID := a.activeConnID()
		if connID != "" && a.store != nil {
			if p, ok := a.store.Get(connID); ok {
				a.topBarConn(c, p)
			}
		}
		a.schemaPicker(c)
		if a.connErr != "" {
			ui.Text(c, a.connErr).TextColor(t.Danger).SingleLine()
		}
		if a.consoleErr != "" {
			if ui.Button(c, a.consoleErr).TextColor(t.Danger).SingleLine().Clicked() {
				a.consoleErr = ""
			}
		}
		if a.extMsg != "" {
			if ui.Button(c, a.extMsg).FontSize(12).TextColor(t.TextMuted).SingleLine().Clicked() {
				a.extMsg = ""
			}
		}
		if a.installNeed.NeedInstall {
			label := locales.InstallTitle()
			switch {
			case a.installNeed.Upgrade:
				label = locales.TopUpgradeApp(a.installNeed.Running)
			case a.installNeed.Repair:
				label = locales.InstallRepairTitle()
			}
			if iconButton(c, iconUpgrade, label, t.Accent).Clicked() {
				a.installOpen = true
				a.installErr = ""
			}
		}
		if a.installNote != "" {
			ui.Text(c, a.installNote).FontSize(12).TextColor(t.TextMuted).SingleLine()
		}
		ui.Spacer(c)
		ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
			if iconButtonPlain(c, iconFolderOpen, locales.TopOpenSQLFile(), t.TextMuted).Clicked() {
				a.pickOpenSQLFiles()
			}
			if iconButtonPlain(c, iconHistory, locales.TopHistory(), t.TextMuted).Clicked() {
				a.historyOpen = true
			}
			aiColor := t.TextMuted
			if a.aiChat.open {
				aiColor = t.Accent
			}
			if iconButtonPlain(c, iconSparkles, locales.TopAIAssistant(), aiColor).Clicked() {
				a.aiChat.open = !a.aiChat.open
				if a.aiChat.open {
					a.loadAIChat()
					a.aiChat.scroll.Y = math.MaxFloat32
				} else {
					a.cancelAIPrepare()
					a.stopAI()
				}
			}
			if iconButtonPlain(c, iconSettings, locales.SettingsTitle(), t.TextMuted).Clicked() {
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
