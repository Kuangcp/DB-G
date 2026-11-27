package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
	"db-g/sql"
)

// settingsSection 是设置窗口左侧导航的一项。
type settingsSection struct {
	label string
	build func(*ui.Context)
}

// settingsSections 是设置窗口的分区（顺序即导航顺序）。
func (a *root) settingsSections() []settingsSection {
	return []settingsSection{
		{locales.SettingsAppearance(), a.appearanceSection},
		{locales.SettingsLanguage(), a.languageSection},
		{locales.SettingsFonts(), a.fontSection},
		{locales.SettingsResults(), a.resultSection},
		{locales.SettingsConsole(), a.consolePrefsSection},
		{locales.SettingsAI(), a.aiSection},
		{locales.SettingsKeymap(), a.keymapSection},
		{locales.SettingsProfiles(), a.profilesSection},
		{locales.SettingsSync(), a.syncSection},
		{locales.SettingsLogs(), a.logSection},
		{locales.SettingsDev(), a.devSection},
	}
}

// languageSection 是界面语言选择：跟随系统 / 简体中文 / English，切完立即生效。
func (a *root) languageSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Text(c, locales.LanguageTitle()).Bold()
	ui.Text(c, locales.LanguageHint()).FontSize(12).TextColor(c.Theme().TextMuted)
	idx := 0
	switch a.settings.LocalePref {
	case app.LocaleZhHans:
		idx = 1
	case app.LocaleEn:
		idx = 2
	}
	if ui.Segmented(c, &idx,
		locales.LanguageSystem(), locales.LanguageChinese(), locales.LanguageEnglish()).
		Radius(cr).Changed() {
		tag := []string{app.LocaleSystem, app.LocaleZhHans, app.LocaleEn}[idx]
		a.setLocalePref(tag)
	}
	a.settingsNoticeLine(c, "locale")
}

// setLocalePref 保存语言选择并让 mygo 立即切换；headless 测试不碰窗口。
func (a *root) setLocalePref(tag string) {
	prefs, warn := app.NormalizeLocalePrefs(app.LocalizationPrefs{Locale: tag})
	a.settings.LocalePref = prefs.Locale
	if warn != "" {
		a.setSettingsNotice("locale", warn, true)
	} else {
		a.setSettingsNotice("locale", "", false)
	}
	if a.settings.LocalePrefsPath != "" {
		if err := app.SaveLocalePrefs(a.settings.LocalePrefsPath, prefs); err != nil {
			a.setSettingsNotice("locale", locales.SettingsSaveLocaleFailed(err.Error()), true)
		}
	}
	if a.win != nil {
		mygo.App.SetLocale(prefs.Locale)
	}
}

// settingsView 是独立设置窗口：左边分区导航，右边内容，左下角版本号。
func (a *root) settingsView(c *ui.Context) {
	a.applyTheme(c)
	sections := a.settingsSections()
	if a.settings.Section < 0 || a.settings.Section >= len(sections) {
		a.settings.Section = 0
	}
	ui.Column(c).Fill().Children(func() {
		a.settingsTitleBar(c)
		// 标题栏占固定高度后，内容行必须用 Grow 取剩余空间；Fill 的 100%
		// 会从标题栏下方再占满整窗高度，底部 38px 被挤出窗口。
		ui.Row(c).FillWidth().Grow(1).Gap(0).Children(func() {
			// 左：分区导航（无按钮外观、行间分隔线、选中高亮），底部版本号。
			ui.Column(c).Width(168).FillHeight().Children(func() {
				for i, sec := range sections {
					row := sideNavRow(c, sec.label, nil, ui.Transparent, i == a.settings.Section)
					if row.Clicked() {
						a.settings.Section = i
					}
				}
				ui.Spacer(c)
				ui.Text(c, app.Version()).FontSize(11).TextColor(c.Theme().TextMuted).Padding(10, 10)
			})
			ui.Box(c).Width(1).FillHeight().Background(c.Theme().Border)
			// 右：当前分区的内容，整块可滚。
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).FillWidth().Padding(14).Gap(10).Children(func() {
					sections[a.settings.Section].build(c)
				})
			})
		})
	})
	// 同步密钥/敏感信息弹窗由设置页触发，必须在这里也渲染；主窗口此时可能在后面。
	a.syncKeyModal(c)
	a.syncFindingsModal(c)
}

// settingsNumberWidth 是设置页数字输入框的固定宽度：mygo 的 NumberInput
// 内含 −/+ 两个按钮，太窄时文字区会被挤到看不全。
const settingsNumberWidth float32 = 140

// fontSection 是全局字体设置：编辑区、结果区、功能界面各一套。
func (a *root) fontSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.SettingsFontTitle()).Bold()
		ui.Spacer(c)
		if ui.Button(c, locales.SettingsRestoreDefaults()).Radius(cr).Clicked() {
			a.settings.FontPrefs = app.DefaultFontPrefs()
			a.applyFontPrefs()
			p := a.settings.ThemePrefs
			p.FontSize = app.DefaultThemeFontSize
			a.setThemePrefs(p)
		}
	})
	ui.Text(c, locales.SettingsFontHint()).FontSize(12).
		TextColor(c.Theme().TextMuted)

	ui.Text(c, locales.SettingsEditor()).Bold()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.NumberInput(c, &a.settings.FontPrefs.Editor.Size, app.MinFontSize, app.MaxFontSize, 1).
			Label(locales.SettingsEditorFontSize()).Width(settingsNumberWidth).Radius(cr).Changed() {
			a.applyFontPrefs()
		}
		ui.Text(c, fmt.Sprintf("%.0f pt", a.settings.FontPrefs.Editor.Size)).FontSize(12).
			TextColor(c.Theme().TextMuted)
	})
	ui.Text(c, locales.SettingsFontFamilyEditor()).FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.settings.FontPrefs.Editor.Family).
		Placeholder(locales.SettingsFontEditorPlaceholder()).Radius(cr).Changed() {
		a.applyFontPrefs()
	}
	ui.Divider(c)

	ui.Text(c, locales.SettingsResultsArea()).Bold()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.NumberInput(c, &a.settings.FontPrefs.Result.Size, 0, app.MaxFontSize, 1).
			Label(locales.SettingsResultFontSize()).Width(settingsNumberWidth).Radius(cr).Changed() {
			a.applyFontPrefs()
		}
		ui.Text(c, fmt.Sprintf("%.0f pt", a.settings.FontPrefs.Result.Size)).FontSize(12).
			TextColor(c.Theme().TextMuted)
		ui.Text(c, locales.SettingsFollowUiFontSize()).FontSize(11).TextColor(c.Theme().TextMuted)
	})
	ui.Text(c, locales.SettingsFontFamilyResult()).FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.settings.FontPrefs.Result.Family).
		Placeholder(locales.SettingsFontResultPlaceholder()).Radius(cr).Changed() {
		a.applyFontPrefs()
	}
	ui.Divider(c)

	ui.Text(c, locales.SettingsConsoleTabs()).Bold()
	ui.Text(c, locales.SettingsConsoleTabsHint()).FontSize(12).
		TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.NumberInput(c, &a.settings.FontPrefs.TabSize, app.MinTabFontSize, app.MaxTabFontSize, 0.5).
			Label(locales.SettingsTabFontSize()).Width(settingsNumberWidth).Radius(cr).Changed() {
			a.applyFontPrefs()
		}
		ui.Text(c, fmt.Sprintf("%.1f pt", a.settings.FontPrefs.TabSize)).FontSize(12).
			TextColor(c.Theme().TextMuted)
	})

	ui.Divider(c)

	ui.Text(c, locales.SettingsUiFont()).Bold()
	ui.Text(c, locales.SettingsUiFontHint()).FontSize(12).
		TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		size := float64(a.settings.ThemePrefs.FontSize)
		if ui.NumberInput(c, &size, app.DefaultThemeFontSize, app.MaxThemeFontSize, 1).
			Label(locales.SettingsUiFontSize()).Width(settingsNumberWidth).Radius(cr).Changed() {
			p := a.settings.ThemePrefs
			p.FontSize = int(size)
			a.setThemePrefs(p)
		}
		ui.Text(c, locales.SettingsFollowTheme()).FontSize(11).TextColor(c.Theme().TextMuted)
	})
	ui.Text(c, locales.SettingsFontFamilyUI()).FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.settings.FontPrefs.UI).
		Placeholder(locales.SettingsFontUiPlaceholder()).Radius(cr).Changed() {
		a.applyFontPrefs()
	}
	a.settingsNoticeLine(c, "font")
}

// consolePrefsSection 是控制台正文自动保存设置。
func (a *root) consolePrefsSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.SettingsConsoleTitle()).Bold()
		ui.Spacer(c)
		if ui.Button(c, locales.SettingsRestoreDefaults()).Radius(cr).Clicked() {
			a.setConsolePrefs(app.DefaultConsolePrefs())
		}
	})
	ui.Text(c, locales.SettingsConsoleSaveHint()).
		FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		secs := float64(a.settings.ConsolePrefs.SaveDelaySeconds)
		if ui.NumberInput(c, &secs, float64(app.MinConsoleSaveDelaySeconds), float64(app.MaxConsoleSaveDelaySeconds), 1).
			Label(locales.SettingsSaveDelay()).Width(settingsNumberWidth).Radius(cr).Changed() {
			p := a.settings.ConsolePrefs
			p.SaveDelaySeconds = int(secs)
			a.setConsolePrefs(p)
		}
		ui.Text(c, locales.SettingsSeconds(a.settings.ConsolePrefs.SaveDelaySeconds)).FontSize(12).
			TextColor(c.Theme().TextMuted)
	})
	a.settingsNoticeLine(c, "console")
}

// setConsolePrefs 更新控制台设置并应用到当前管理器。
func (a *root) setConsolePrefs(p app.ConsolePrefs) {
	p = app.NormalizeConsolePrefs(p)
	a.settings.ConsolePrefs = p
	a.applyConsolePrefs()
	if a.settings.ConsolePrefsPath == "" {
		return
	}
	if err := app.SaveConsolePrefs(a.settings.ConsolePrefsPath, p); err != nil {
		a.setSettingsNotice("console", locales.SettingsSaveConsoleFailed(err.Error()), true)
	} else {
		a.setSettingsNotice("console", "", false)
	}
	a.updateSettingsWindow()
}

// applyConsolePrefs 把保存延迟应用到控制台管理器。
func (a *root) applyConsolePrefs() {
	if m := a.consoleMgr(); m != nil {
		m.SetSaveDelay(time.Duration(a.settings.ConsolePrefs.SaveDelaySeconds) * time.Second)
	}
}

// resultSection 是查询结果的加载预算设置。
func (a *root) resultSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.SettingsResultsTitle()).Bold()
		ui.Spacer(c)
		if ui.Button(c, locales.SettingsRestoreDefaults()).Radius(cr).Clicked() {
			a.setResultPrefs(app.DefaultResultPrefs())
		}
	})
	ui.Text(c, locales.SettingsPageSizeHint()).
		FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		mb := float64(a.settings.ResultPrefs.BudgetMB)
		if ui.NumberInput(c, &mb, float64(app.MinResultBudgetMB), float64(app.MaxResultBudgetMB), 1).
			Label(locales.SettingsBudget()).Width(settingsNumberWidth).Radius(cr).Changed() {
			p := a.settings.ResultPrefs
			p.BudgetMB = int(mb)
			a.setResultPrefs(p)
		}
		ui.Text(c, locales.SettingsBudgetValue(a.settings.ResultPrefs.BudgetMB)).FontSize(12).
			TextColor(c.Theme().TextMuted)
	})
	ui.Text(c, locales.SettingsBudgetHint()).
		FontSize(11).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		size := float64(a.settings.ResultPrefs.PageSize)
		if ui.NumberInput(c, &size, float64(app.MinResultPageSize), float64(app.MaxResultPageSize), 50).
			Label(locales.SettingsRowsPerPage()).Width(settingsNumberWidth).Radius(cr).Changed() {
			p := a.settings.ResultPrefs
			p.PageSize = int(size)
			a.setResultPrefs(p)
		}
		ui.Text(c, locales.SettingsRowsPerPageValue(
			a.settings.ResultPrefs.PageSize, app.MinResultPageSize, app.MaxResultPageSize)).
			FontSize(12).TextColor(c.Theme().TextMuted)
	})
	a.settingsNoticeLine(c, "result")
}

// setResultPrefs 更新结果预算：立即作用于后续查询，并写入 results.json。
func (a *root) setResultPrefs(p app.ResultPrefs) {
	p = app.NormalizeResultPrefs(p)
	a.settings.ResultPrefs = p
	a.applyResultLimits()
	if a.settings.ResultPrefsPath == "" {
		return
	}
	if err := app.SaveResultPrefs(a.settings.ResultPrefsPath, p); err != nil {
		a.setSettingsNotice("result", locales.SettingsSaveResultFailed(err.Error()), true)
	} else {
		a.setSettingsNotice("result", "", false)
	}
	a.updateSettingsWindow()
}

// logSection 显示日志目录与打开入口。
func (a *root) logSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Text(c, locales.SettingsLogTitle()).Bold()
	ui.Text(c, locales.SettingsLogHint()).FontSize(12).
		TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, app.LogDir()).FontSize(12).SingleLine().Grow(1)
		if ui.Button(c, locales.SettingsOpenLogFolder()).Radius(cr).Clicked() {
			mygo.Shell.ShowItemInFolder(app.LogDir())
		}
	})
}

// devSection 是开发用的编辑器性能面板：HUD、演示预设、补全缓存。
func (a *root) devSection(c *ui.Context) {
	cr := controlRadius(c)
	disabled := a.tabs.ActiveConsole == ""
	ui.Text(c, locales.SettingsDevPerf()).Bold()
	ui.Text(c, locales.SettingsPerfStats(
		a.code.LineCount(), a.code.ByteLen(), fmt.Sprint(a.code.Version()),
	)).FontSize(12).SingleLine()
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, locales.SettingsRefresh()).Radius(cr).Clicked() {
			a.updateSettingsWindow()
		}
	})

	ui.Text(c, locales.SettingsPresets()).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(6).Children(func() {
		for _, n := range []int{3_000, 12_000, 100_000} {
			label := fmt.Sprintf("%dk", n/1_000)
			if ui.Button(c, label).Radius(cr).Disabled(disabled).Clicked() {
				a.applyEditorPreset(n)
			}
		}
	})

	ui.Text(c, locales.SettingsCompletions()).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, locales.SettingsRefreshCompletions()).Radius(cr).Clicked() {
			a.refreshCompletions()
		}
	})
	if disabled {
		ui.Text(c, locales.SettingsNoActiveConsole()).FontSize(12).TextColor(c.Theme().TextMuted)
	}
	if a.consoleErr != "" {
		ui.Text(c, a.consoleErr).TextColor(c.Theme().Danger).FontSize(12)
	}
}

// applyFontPrefs 把编辑区字体应用到编辑器，并把全部字体设置落盘。
// 结果区/功能界面的字体由各自的渲染路径读取，不需要缓存副本。
func (a *root) applyFontPrefs() {
	a.code.state.Font = ui.Font{
		Family: strings.TrimSpace(a.settings.FontPrefs.Editor.Family),
		Size:   float32(a.settings.FontPrefs.Editor.Size),
	}
	if a.settings.FontPrefsPath == "" {
		return
	}
	if err := app.SaveFontPrefs(a.settings.FontPrefsPath, a.settings.FontPrefs); err != nil {
		a.setSettingsNotice("font", locales.SettingsSaveFontFailed(err.Error()), true)
	} else {
		a.setSettingsNotice("font", "", false)
	}
	a.updateSettingsWindow()
}

// withResultFont 给结果区的元素套上结果区字体；字号 0 时保留继承的字号。
func (a *root) withResultFont(el ui.Element) ui.Element {
	if fam := strings.TrimSpace(a.settings.FontPrefs.Result.Family); fam != "" {
		el = el.Font(fam)
	}
	if a.settings.FontPrefs.Result.Size > 0 {
		el = el.FontSize(float32(a.settings.FontPrefs.Result.Size))
	}
	return el
}

// applyEditorPreset 把 n 行的演示 SQL 写进当前激活控制台并立即落盘（spike 回归用）。
func (a *root) applyEditorPreset(n int) {
	if a.tabs.ActiveConsole == "" {
		return
	}
	a.setActiveText(sql.BuildSQL(n))
	a.syncActiveBuffer()
	_ = a.flushActive()
}

// settingsWin 是设置窗口需要的最小能力（便于 headless 测试注入假窗口）。
type settingsWin interface {
	Focus()
	Update(func())
	Minimize()
	IsMaximized() bool
	ToggleMaximize()
	Close()
	SetTitle(string)
	SetBackgroundColor(string) error
}

// requestSettings 请求打开（或聚焦）设置窗口。
// 有宿主窗口时通过 win.Update 排队到主循环（不在帧构建中途建窗）；
// 没有宿主窗口（headless 测试）时记请求，由帧首 maybeOpenSettings 处理。
func (a *root) requestSettings() {
	if a.win != nil {
		win := a.win
		a.safeUpdate(win, func() { a.openSettings() })
		return
	}
	a.settings.Requested = true
}

// openSettings 打开（或聚焦）设置窗口（单例）。
func (a *root) openSettings() {
	a.ai.ShowKey = false // 每次打开设置都回到隐藏状态
	if a.settings.Win != nil {
		a.settings.Win.Focus()
		return
	}
	open := a.settings.OpenWin
	if open == nil {
		open = a.newSettingsWindow
	}
	a.settings.Win = open(mygo.WindowOptions{
		Title:           locales.SettingsTitle() + " — " + app.DisplayName,
		Width:           760,
		Height:          600,
		StateKey:        "settings",
		Frameless:       true,
		CornerRadius:    a.settings.ThemePrefs.Radius + 4,
		BackgroundColor: WindowBackgroundColor(a.settings.ThemePrefs),
		Content:         ui.View(func(c *ui.Context) { a.guardView(func() { a.settingsView(c) }) }),
	})
}

// maybeOpenSettings 消费 headless 的「打开设置」请求。
func (a *root) maybeOpenSettings() {
	if !a.settings.Requested {
		return
	}
	a.settings.Requested = false
	if a.settings.OpenWin == nil && a.win == nil {
		return
	}
	a.openSettings()
}

// newSettingsWindow 是生产实现的窗口工厂：建窗并在关闭后清空单例指针。
func (a *root) newSettingsWindow(opts mygo.WindowOptions) settingsWin {
	win := mygo.NewWindow(opts)
	win.OnClose(func(*mygo.CloseEvent) {
		a.safeUpdate(win, func() { a.settings.Win = nil })
	})
	return win
}

// updateSettingsWindow 让设置窗口重画（HUD/策略变化后）。
func (a *root) updateSettingsWindow() {
	if w := a.settings.Win; w != nil {
		a.safeUpdateFunc(w.Update, func() {})
	}
}

// 外观分区的强调色选项：值对应 app.ThemePrefs.Accent（空 = 主题/桌面强调色）。
type themeAccentOption struct {
	label func() string
	value string
}

var themeAccentOptions = []themeAccentOption{
	{locales.ThemeDefault, ""},
	{locales.ThemeBlue, "blue"},
	{locales.ThemeViolet, "violet"},
	{locales.ThemeGreen, "green"},
	{locales.ThemeOrange, "orange"},
	{locales.ThemeRose, "rose"},
}

// modeIndex 把设置里的模式换成 Segmented 的下标。
func modeIndex(mode string) int {
	switch mode {
	case app.ThemeModeLight:
		return 1
	case app.ThemeModeDark:
		return 2
	}
	return 0
}

// themeMode 是 modeIndex 的反向：0 跟随系统、1 浅色、2 深色。
func themeMode(idx int) string {
	switch idx {
	case 1:
		return app.ThemeModeLight
	case 2:
		return app.ThemeModeDark
	}
	return app.ThemeModeSystem
}

// densityIndex 把密度换成 Segmented 下标。
func densityIndex(d string) int {
	switch d {
	case app.ThemeDensityCompact:
		return 0
	case app.ThemeDensityRoomy:
		return 2
	}
	return 1
}

// themeDensity 是 densityIndex 的反向。
func themeDensity(idx int) string {
	switch idx {
	case 0:
		return app.ThemeDensityCompact
	case 2:
		return app.ThemeDensityRoomy
	}
	return app.ThemeDensityNormal
}

// paletteEditor 是自定义配色的五组颜色输入；留空沿用默认。
func (a *root) paletteEditor(c *ui.Context, cr float32, prefs app.ThemePrefs) {
	t := c.Theme()
	ui.Text(c, locales.SettingsCustomPalette()).FontSize(12).TextColor(t.TextMuted)
	fields := []struct {
		label       func() string
		light, dark *string
	}{
		{locales.PaletteBackground, &a.settings.PaletteLight.Background, &a.settings.PaletteDark.Background},
		{locales.PaletteSurface, &a.settings.PaletteLight.Surface, &a.settings.PaletteDark.Surface},
		{locales.PaletteBorder, &a.settings.PaletteLight.Border, &a.settings.PaletteDark.Border},
		{locales.PaletteText, &a.settings.PaletteLight.Text, &a.settings.PaletteDark.Text},
		{locales.PaletteMutedText, &a.settings.PaletteLight.TextMuted, &a.settings.PaletteDark.TextMuted},
	}
	changed := false
	for _, f := range fields {
		f := f
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			name := f.label()
			ui.Text(c, name).Width(68).FontSize(12).TextColor(t.TextMuted)
			if ui.TextInput(c, f.light).Placeholder(locales.PaletteLightField(name)).Label(locales.PaletteLightField(name)).
				Radius(cr).Fill().Changed() {
				changed = true
			}
			if ui.TextInput(c, f.dark).Placeholder(locales.PaletteDarkField(name)).Label(locales.PaletteDarkField(name)).
				Radius(cr).Fill().Changed() {
				changed = true
			}
		})
	}
	if changed {
		a.applyPaletteDrafts(prefs)
	}
	if a.settings.PaletteErr != "" {
		ui.Text(c, a.settings.PaletteErr).FontSize(12).TextColor(t.Danger).SingleLine()
	}
	if ui.Button(c, locales.SettingsResetPalette()).Radius(cr).Clicked() {
		a.settings.PaletteLight, a.settings.PaletteDark = defaultCustomPalettes()
		a.settings.PaletteErr = ""
		a.applyPaletteDrafts(prefs)
	}
}

// appearanceSection 是外观设置：模式 / 强调色 / 密度 / 圆角 / 界面字号 + 预览。
func (a *root) appearanceSection(c *ui.Context) {
	cr := controlRadius(c)
	prefs := a.settings.ThemePrefs
	ui.Text(c, locales.SettingsAppearance()).Bold()
	ui.Text(c, locales.SettingsAppearanceHint()).FontSize(12).
		TextColor(c.Theme().TextMuted)

	// 模式
	ui.Text(c, locales.SettingsTheme()).FontSize(12).TextColor(c.Theme().TextMuted)
	mode := modeIndex(prefs.Mode)
	if ui.Segmented(c, &mode, locales.LanguageSystem(), locales.SettingsModeLight(), locales.SettingsModeDark()).Radius(cr).Changed() {
		prefs.Mode = themeMode(mode)
		a.setThemePrefs(prefs)
	}

	// 配色方案
	ui.Text(c, locales.SettingsScheme()).FontSize(12).TextColor(c.Theme().TextMuted)
	schemeLabels := make([]string, len(themeSchemeOptions))
	for i, opt := range themeSchemeOptions {
		schemeLabels[i] = opt.label()
	}
	scheme := themeSchemeIndex(prefs.Scheme)
	if ui.Segmented(c, &scheme, schemeLabels...).Radius(cr).Changed() {
		prefs.Scheme = themeSchemeOptions[scheme].key
		if prefs.Scheme == app.ThemeSchemeCustom {
			a.loadPaletteDrafts(a.settings.ThemePrefs)
		}
		a.setThemePrefs(prefs)
	}
	if prefs.Scheme == app.ThemeSchemeCustom {
		if !a.settings.PaletteLoaded {
			a.loadPaletteDrafts(prefs)
		}
		a.paletteEditor(c, cr, prefs)
	}

	// 强调色
	ui.Text(c, locales.SettingsAccent()).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		for _, opt := range themeAccentOptions {
			opt := opt
			sw := ui.ButtonBase(c).Label(locales.SettingsAccentLabel(opt.label())).Size(20, 20).Radius(10)
			if opt.value == "" {
				sw.Background(c.Theme().Accent)
			} else {
				sw.Background(themeAccentPresets[opt.value])
			}
			if opt.value == prefs.Accent {
				sw.Border(2, c.Theme().Text)
			}
			if sw.Clicked() {
				prefs.Accent = opt.value
				a.settings.ThemeHex = ""
				a.settings.ThemeHexErr = ""
				a.setThemePrefs(prefs)
			}
		}
		if ui.TextInput(c, &a.settings.ThemeHex).Placeholder("#rrggbb").Label(locales.SettingsCustomAccent()).
			Width(110).Radius(cr).Changed() {
			want := strings.ToLower(strings.TrimSpace(a.settings.ThemeHex))
			if want == "" {
				a.settings.ThemeHexErr = ""
				prefs.Accent = ""
				a.setThemePrefs(prefs)
			} else if p, warn := (app.ThemePrefs{
				Mode: prefs.Mode, Accent: want, Density: prefs.Density,
				Radius: prefs.Radius, FontSize: prefs.FontSize,
			}).Normalize(); len(warn) > 0 {
				a.settings.ThemeHexErr = locales.SettingsHexError()
			} else {
				a.settings.ThemeHexErr = ""
				a.setThemePrefs(p)
			}
		}
		ui.Text(c, locales.SettingsApplies()).FontSize(11).TextColor(c.Theme().TextMuted)
	})
	if a.settings.ThemeHexErr != "" {
		ui.Text(c, a.settings.ThemeHexErr).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}

	// 渲染方式：与外观一起保存，下次启动生效。
	gpu := !prefs.SoftwareRender
	if ui.Checkbox(c, &gpu, locales.SettingsGpu()).Radius(cr).Changed() {
		prefs.SoftwareRender = !gpu
		a.setThemePrefs(prefs)
	}
	ui.Text(c, locales.SettingsGpuHint()).
		FontSize(11).TextColor(c.Theme().TextMuted)

	// 密度 / 圆角 / 字号
	ui.Text(c, locales.SettingsDensity()).FontSize(12).TextColor(c.Theme().TextMuted)
	density := densityIndex(prefs.Density)
	if ui.Segmented(c, &density,
		locales.SettingsDensityCompact(), locales.SettingsDensityNormal(), locales.SettingsDensityRoomy()).
		Radius(cr).Changed() {
		prefs.Density = themeDensity(density)
		a.setThemePrefs(prefs)
	}
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.SettingsRadius()).FontSize(12).TextColor(c.Theme().TextMuted)
		radius := float64(prefs.Radius)
		if ui.NumberInput(c, &radius, app.MinThemeRadius, app.MaxThemeRadius, 1).Label(locales.SettingsRadius()).
			Width(90).Radius(cr).Changed() {
			prefs.Radius = int(radius)
			a.setThemePrefs(prefs)
		}
	})

	// 预览：直接使用当前主题（改动已经是全局的）。
	ui.Text(c, locales.SettingsPreview()).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Box(c).FillWidth().Padding(12).Radius(c.Theme().Radius).
		Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.PrimaryButton(c, locales.SettingsPrimaryButton()).Radius(cr)
			ui.Button(c, locales.SettingsSecondaryButton()).Radius(cr)
			ui.Badge(c, locales.SettingsBadge())
			ui.Text(c, locales.PaletteMutedText()).FontSize(12).TextColor(c.Theme().TextMuted)
		})
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, locales.SettingsSelectedRow()).Padding(4, 8).Radius(c.Theme().Radius).
				Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
			ui.Text(c, locales.SettingsNormalRow()).Padding(4, 8).TextColor(c.Theme().Text)
		})
	})

	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, locales.SettingsRestoreDefaults()).Radius(cr).Clicked() {
			a.settings.ThemeHex, a.settings.ThemeHexErr = "", ""
			a.settings.PaletteLoaded, a.settings.PaletteErr = false, ""
			a.setThemePrefs(app.DefaultThemePrefs())
		}
		if ui.Button(c, locales.SettingsExportTheme()).Radius(cr).Clicked() {
			a.pickExportTheme()
		}
		if ui.Button(c, locales.SettingsImportTheme()).Radius(cr).Clicked() {
			a.pickImportTheme()
		}
	})
	a.settingsNoticeLine(c, "theme")
}

// exportThemeFile 只把主题写到文件，不读 root。
func exportThemeFile(path string, prefs app.ThemePrefs) error {
	return app.ExportThemePrefs(path, prefs)
}

// importThemeFile 只解析主题文件，不应用；warn 是被忽略项说明。
func importThemeFile(path string) (app.ThemePrefs, []string, error) {
	prefs, warn, err := app.ImportThemePrefs(path)
	if err != nil {
		return app.ThemePrefs{}, nil, err
	}
	return prefs, warn, nil
}

// applyImportedTheme 在 UI 线程应用导入的主题并给出提示。
func (a *root) applyImportedTheme(prefs app.ThemePrefs, warn []string) {
	a.settings.ThemeHex, a.settings.ThemeHexErr = "", ""
	a.settings.PaletteLoaded, a.settings.PaletteErr = false, ""
	a.setThemePrefs(prefs)
	if len(warn) > 0 {
		a.setSettingsNotice("theme", locales.SettingsImported(strings.Join(warn, "；")), true)
	}
	app.LogInfo("startup", "theme imported")
}

// pickExportTheme 弹保存对话框导出主题。
func (a *root) pickExportTheme() {
	win := a.win
	if win == nil {
		return
	}
	prefs := a.settings.ThemePrefs
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: win, Title: locales.SettingsExportThemeTitle(), DefaultPath: "db-g-theme.json",
		})
		if err != nil || path == "" {
			return
		}
		err = exportThemeFile(path, prefs)
		a.safeUpdate(win, func() {
			if err != nil {
				a.reportSettingsResult("theme", locales.SettingsExportFailed(err.Error()), true)
				return
			}
			a.reportSettingsResult("theme", locales.SettingsExported(path), false)
		})
	}()
}

// pickImportTheme 弹选择对话框导入主题。
func (a *root) pickImportTheme() {
	win := a.win
	if win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: locales.SettingsImportThemeTitle()})
		if err != nil || len(paths) == 0 {
			return
		}
		prefs, warn, err := importThemeFile(paths[0])
		a.safeUpdate(win, func() {
			if err != nil {
				a.reportSettingsResult("theme", locales.SettingsImportFailed(err.Error()), true)
				return
			}
			a.applyImportedTheme(prefs, warn)
		})
	}()
}
