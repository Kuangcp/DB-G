package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

// settingsSection 是设置窗口左侧导航的一项。
type settingsSection struct {
	label string
	build func(*ui.Context)
}

// settingsSections 是设置窗口的分区（顺序即导航顺序）。
func (a *root) settingsSections() []settingsSection {
	return []settingsSection{
		{"外观", a.appearanceSection},
		{"字体", a.fontSection},
		{"结果", a.resultSection},
		{"控制台", a.consolePrefsSection},
		{"AI", a.aiSection},
		{"快捷键", a.keymapSection},
		{"连接档案", a.profilesSection},
		{"日志", a.logSection},
		{"开发", a.devSection},
	}
}

// settingsView 是独立设置窗口：左边分区导航，右边内容，左下角版本号。
func (a *root) settingsView(c *ui.Context) {
	a.applyTheme(c)
	sections := a.settingsSections()
	if a.settingsSection < 0 || a.settingsSection >= len(sections) {
		a.settingsSection = 0
	}
	ui.Column(c).Fill().Children(func() {
		a.settingsTitleBar(c)
		// 标题栏占固定高度后，内容行必须用 Grow 取剩余空间；Fill 的 100%
		// 会从标题栏下方再占满整窗高度，底部 38px 被挤出窗口。
		ui.Row(c).FillWidth().Grow(1).Gap(0).Children(func() {
			// 左：分区导航（无按钮外观、行间分隔线、选中高亮），底部版本号。
			ui.Column(c).Width(168).FillHeight().Children(func() {
				for i, sec := range sections {
					row := sideNavRow(c, sec.label, nil, ui.Transparent, i == a.settingsSection)
					if row.Clicked() {
						a.settingsSection = i
					}
				}
				ui.Spacer(c)
				ui.Text(c, app.Version()).FontSize(11).TextColor(c.Theme().TextMuted).Padding(10, 10)
			})
			ui.Box(c).Width(1).FillHeight().Background(c.Theme().Border)
			// 右：当前分区的内容，整块可滚。
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).FillWidth().Padding(14).Gap(10).Children(func() {
					sections[a.settingsSection].build(c)
				})
			})
		})
	})
}

// fontSection 是全局字体设置：编辑区、结果区、功能界面各一套。
func (a *root) fontSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "字体").Bold()
		ui.Spacer(c)
		if ui.Button(c, "恢复默认").Radius(cr).Clicked() {
			a.fontPrefs = app.DefaultFontPrefs()
			a.applyFontPrefs()
			p := a.themePrefs
			p.FontSize = app.DefaultThemeFontSize
			a.setThemePrefs(p)
		}
	})
	ui.Text(c, "编辑区、结果区、控制台标签、功能界面各用一套字体；修改即时生效并保存到 fonts.json，直接看主窗口即可。").FontSize(12).
		TextColor(c.Theme().TextMuted)

	ui.Text(c, "编辑区").Bold()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.NumberInput(c, &a.fontPrefs.Editor.Size, app.MinFontSize, app.MaxFontSize, 1).
			Label("编辑区字号").Width(90).Radius(cr).Changed() {
			a.applyFontPrefs()
		}
		ui.Text(c, fmt.Sprintf("%.0f pt", a.fontPrefs.Editor.Size)).FontSize(12).
			TextColor(c.Theme().TextMuted)
	})
	ui.Text(c, "字体族（留空 = 系统等宽）").FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.fontPrefs.Editor.Family).
		Placeholder("例如 JetBrains Mono / Sarasa Mono SC").Radius(cr).Changed() {
		a.applyFontPrefs()
	}
	ui.Divider(c)

	ui.Text(c, "结果区").Bold()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.NumberInput(c, &a.fontPrefs.Result.Size, 0, app.MaxFontSize, 1).
			Label("结果区字号").Width(90).Radius(cr).Changed() {
			a.applyFontPrefs()
		}
		ui.Text(c, fmt.Sprintf("%.0f pt", a.fontPrefs.Result.Size)).FontSize(12).
			TextColor(c.Theme().TextMuted)
		ui.Text(c, "0 = 跟随界面字号").FontSize(11).TextColor(c.Theme().TextMuted)
	})
	ui.Text(c, "字体族（留空 = 跟随功能界面）").FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.fontPrefs.Result.Family).
		Placeholder("例如 JetBrains Mono / monospace").Radius(cr).Changed() {
		a.applyFontPrefs()
	}
	ui.Divider(c)

	ui.Text(c, "控制台标签").Bold()
	ui.Text(c, "控制台标签栏的数据源名与控制台名字号；控制台名自动小 2pt。").FontSize(12).
		TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.NumberInput(c, &a.fontPrefs.TabSize, app.MinTabFontSize, app.MaxTabFontSize, 0.5).
			Label("控制台标签字号").Width(90).Radius(cr).Changed() {
			a.applyFontPrefs()
		}
		ui.Text(c, fmt.Sprintf("%.1f pt", a.fontPrefs.TabSize)).FontSize(12).
			TextColor(c.Theme().TextMuted)
	})

	ui.Divider(c)

	ui.Text(c, "功能界面").Bold()
	ui.Text(c, "设置、控制台名、菜单、按钮等编辑区与结果区以外的文字。").FontSize(12).
		TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		size := float64(a.themePrefs.FontSize)
		if ui.NumberInput(c, &size, app.DefaultThemeFontSize, app.MaxThemeFontSize, 1).
			Label("界面字号").Width(90).Radius(cr).Changed() {
			p := a.themePrefs
			p.FontSize = int(size)
			a.setThemePrefs(p)
		}
		ui.Text(c, "0 = 跟随主题").FontSize(11).TextColor(c.Theme().TextMuted)
	})
	ui.Text(c, "字体族（留空 = 系统 UI 字体）").FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.fontPrefs.UI).
		Placeholder("例如 Noto Sans CJK SC").Radius(cr).Changed() {
		a.applyFontPrefs()
	}
	if a.fontPrefsWarn != "" {
		ui.Text(c, a.fontPrefsWarn).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}
}

// consolePrefsSection 是控制台正文自动保存设置。
func (a *root) consolePrefsSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "控制台").Bold()
		ui.Spacer(c)
		if ui.Button(c, "恢复默认").Radius(cr).Clicked() {
			a.setConsolePrefs(app.DefaultConsolePrefs())
		}
	})
	ui.Text(c, "停止输入后自动保存正文的延迟；切换标签、执行、关闭窗口或退出时仍会立即保存。").
		FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		secs := float64(a.consolePrefs.SaveDelaySeconds)
		if ui.NumberInput(c, &secs, float64(app.MinConsoleSaveDelaySeconds), float64(app.MaxConsoleSaveDelaySeconds), 1).
			Label("自动保存延迟").Width(90).Radius(cr).Changed() {
			p := a.consolePrefs
			p.SaveDelaySeconds = int(secs)
			a.setConsolePrefs(p)
		}
		ui.Text(c, fmt.Sprintf("%d 秒", a.consolePrefs.SaveDelaySeconds)).FontSize(12).
			TextColor(c.Theme().TextMuted)
	})
	if a.consolePrefsWarn != "" {
		ui.Text(c, a.consolePrefsWarn).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}
}

// setConsolePrefs 更新控制台设置并应用到当前管理器。
func (a *root) setConsolePrefs(p app.ConsolePrefs) {
	p = app.NormalizeConsolePrefs(p)
	a.consolePrefs = p
	a.applyConsolePrefs()
	if a.consolePrefsPath == "" {
		return
	}
	if err := app.SaveConsolePrefs(a.consolePrefsPath, p); err != nil {
		a.consolePrefsWarn = "保存控制台设置失败: " + err.Error()
	} else {
		a.consolePrefsWarn = ""
	}
	a.updateSettingsWindow()
}

// applyConsolePrefs 把保存延迟应用到控制台管理器。
func (a *root) applyConsolePrefs() {
	if m := a.consoleMgr(); m != nil {
		m.SetSaveDelay(time.Duration(a.consolePrefs.SaveDelaySeconds) * time.Second)
	}
}

// resultSection 是查询结果的加载预算设置。
func (a *root) resultSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "结果").Bold()
		ui.Spacer(c)
		if ui.Button(c, "恢复默认").Radius(cr).Clicked() {
			a.setResultPrefs(app.DefaultResultPrefs())
		}
	})
	ui.Text(c, "超过预算后停止加载后续行，并在结果区显示「结果已截断」；最多返回 500 行。").FontSize(12).
		TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		mb := float64(a.resultPrefs.BudgetMB)
		if ui.NumberInput(c, &mb, float64(app.MinResultBudgetMB), float64(app.MaxResultBudgetMB), 1).
			Label("结果大小预算").Width(90).Radius(cr).Changed() {
			p := a.resultPrefs
			p.BudgetMB = int(mb)
			a.setResultPrefs(p)
		}
		ui.Text(c, fmt.Sprintf("MB（当前 %d MB）", a.resultPrefs.BudgetMB)).FontSize(12).
			TextColor(c.Theme().TextMuted)
	})
	ui.Text(c, "预算按结果集文本/二进制单元格的字节数累计；单个单元格另有 1 MiB 驱动上限。").FontSize(11).
		TextColor(c.Theme().TextMuted)
	if a.resultPrefsWarn != "" {
		ui.Text(c, a.resultPrefsWarn).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}
}

// setResultPrefs 更新结果预算：立即作用于后续查询，并写入 results.json。
func (a *root) setResultPrefs(p app.ResultPrefs) {
	p = app.NormalizeResultPrefs(p)
	a.resultPrefs = p
	engine.SetResultBudgetMB(p.BudgetMB)
	if a.resultPrefsPath == "" {
		return
	}
	if err := app.SaveResultPrefs(a.resultPrefsPath, p); err != nil {
		a.resultPrefsWarn = "保存结果设置失败: " + err.Error()
	} else {
		a.resultPrefsWarn = ""
	}
	a.updateSettingsWindow()
}

// logSection 显示日志目录与打开入口。
func (a *root) logSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Text(c, "日志").Bold()
	ui.Text(c, "一次启动一个文件，纯文本（时间 / 级别 / 标签 / 消息）。").FontSize(12).
		TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, app.LogDir()).FontSize(12).SingleLine().Grow(1)
		if ui.Button(c, "打开日志目录").Radius(cr).Clicked() {
			mygo.Shell.ShowItemInFolder(app.LogDir())
		}
	})
}

// devSection 是开发用的编辑器性能面板：HUD、演示预设、补全缓存。
func (a *root) devSection(c *ui.Context) {
	cr := controlRadius(c)
	disabled := a.activeConsole == ""
	ui.Text(c, "编辑器性能（开发用）").Bold()
	ui.Text(c, fmt.Sprintf(
		"行数=%d 字节=%d 版本=%d",
		a.code.LineCount(), a.code.ByteLen(), a.code.Version(),
	)).FontSize(12).SingleLine()
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, "刷新").Radius(cr).Clicked() {
			a.updateSettingsWindow()
		}
	})

	ui.Text(c, "预设（写入当前激活控制台并立即保存）").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(6).Children(func() {
		for _, n := range []int{3_000, 12_000, 100_000} {
			label := fmt.Sprintf("%dk", n/1_000)
			if ui.Button(c, label).Radius(cr).Disabled(disabled).Clicked() {
				a.applyEditorPreset(n)
			}
		}
	})

	ui.Text(c, "补全").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, "刷新补全缓存").Radius(cr).Clicked() {
			a.refreshCompletions()
		}
	})
	if disabled {
		ui.Text(c, "没有激活的控制台：预设不可用").FontSize(12).TextColor(c.Theme().TextMuted)
	}
	if a.consoleErr != "" {
		ui.Text(c, a.consoleErr).TextColor(c.Theme().Danger).FontSize(12)
	}
}

// applyFontPrefs 把编辑区字体应用到编辑器，并把全部字体设置落盘。
// 结果区/功能界面的字体由各自的渲染路径读取，不需要缓存副本。
func (a *root) applyFontPrefs() {
	a.code.state.Font = ui.Font{
		Family: strings.TrimSpace(a.fontPrefs.Editor.Family),
		Size:   float32(a.fontPrefs.Editor.Size),
	}
	if a.fontPrefsPath == "" {
		return
	}
	if err := app.SaveFontPrefs(a.fontPrefsPath, a.fontPrefs); err != nil {
		a.fontPrefsWarn = "保存字体设置失败: " + err.Error()
	} else {
		a.fontPrefsWarn = ""
	}
	a.updateSettingsWindow()
}

// withResultFont 给结果区的元素套上结果区字体；字号 0 时保留继承的字号。
func (a *root) withResultFont(el ui.Element) ui.Element {
	if fam := strings.TrimSpace(a.fontPrefs.Result.Family); fam != "" {
		el = el.Font(fam)
	}
	if a.fontPrefs.Result.Size > 0 {
		el = el.FontSize(float32(a.fontPrefs.Result.Size))
	}
	return el
}

// applyEditorPreset 把 n 行的演示 SQL 写进当前激活控制台并立即落盘（spike 回归用）。
func (a *root) applyEditorPreset(n int) {
	if a.activeConsole == "" {
		return
	}
	a.setActiveText(buildSQL(n))
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
	a.settingsRequested = true
}

// openSettings 打开（或聚焦）设置窗口（单例）。
func (a *root) openSettings() {
	if a.settingsWin != nil {
		a.settingsWin.Focus()
		return
	}
	open := a.openSettingsWin
	if open == nil {
		open = a.newSettingsWindow
	}
	a.settingsWin = open(mygo.WindowOptions{
		Title:           "设置 — " + app.DisplayName,
		Width:           760,
		Height:          600,
		StateKey:        "settings",
		Frameless:       true,
		CornerRadius:    a.themePrefs.Radius + 4,
		BackgroundColor: WindowBackgroundColor(a.themePrefs),
		Content:         ui.View(func(c *ui.Context) { a.guardView(func() { a.settingsView(c) }) }),
	})
}

// maybeOpenSettings 消费 headless 的「打开设置」请求。
func (a *root) maybeOpenSettings() {
	if !a.settingsRequested {
		return
	}
	a.settingsRequested = false
	if a.openSettingsWin == nil && a.win == nil {
		return
	}
	a.openSettings()
}

// newSettingsWindow 是生产实现的窗口工厂：建窗并在关闭后清空单例指针。
func (a *root) newSettingsWindow(opts mygo.WindowOptions) settingsWin {
	win := mygo.NewWindow(opts)
	win.OnClose(func(*mygo.CloseEvent) {
		a.safeUpdate(win, func() { a.settingsWin = nil })
	})
	return win
}

// updateSettingsWindow 让设置窗口重画（HUD/策略变化后）。
func (a *root) updateSettingsWindow() {
	if w := a.settingsWin; w != nil {
		a.safeUpdateFunc(w.Update, func() {})
	}
}

// 外观分区的强调色选项：值对应 app.ThemePrefs.Accent（空 = 主题/桌面强调色）。
type themeAccentOption struct {
	label string
	value string
}

var themeAccentOptions = []themeAccentOption{
	{"默认", ""},
	{"蓝", "blue"},
	{"紫", "violet"},
	{"绿", "green"},
	{"橙", "orange"},
	{"玫红", "rose"},
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
	ui.Text(c, "自定义配色（颜色留空则沿用默认）").FontSize(12).TextColor(t.TextMuted)
	fields := []struct {
		label       string
		light, dark *string
	}{
		{"背景", &a.paletteLight.Background, &a.paletteDark.Background},
		{"容器", &a.paletteLight.Surface, &a.paletteDark.Surface},
		{"边框", &a.paletteLight.Border, &a.paletteDark.Border},
		{"正文", &a.paletteLight.Text, &a.paletteDark.Text},
		{"次要文字", &a.paletteLight.TextMuted, &a.paletteDark.TextMuted},
	}
	changed := false
	for _, f := range fields {
		f := f
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, f.label).Width(68).FontSize(12).TextColor(t.TextMuted)
			if ui.TextInput(c, f.light).Placeholder("浅色 " + f.label).Label("浅色" + f.label).
				Radius(cr).Fill().Changed() {
				changed = true
			}
			if ui.TextInput(c, f.dark).Placeholder("深色 " + f.label).Label("深色" + f.label).
				Radius(cr).Fill().Changed() {
				changed = true
			}
		})
	}
	if changed {
		a.applyPaletteDrafts(prefs)
	}
	if a.paletteErr != "" {
		ui.Text(c, a.paletteErr).FontSize(12).TextColor(t.Danger).SingleLine()
	}
	if ui.Button(c, "从默认配色重置").Radius(cr).Clicked() {
		a.paletteLight, a.paletteDark = defaultCustomPalettes()
		a.paletteErr = ""
		a.applyPaletteDrafts(prefs)
	}
}

// appearanceSection 是外观设置：模式 / 强调色 / 密度 / 圆角 / 界面字号 + 预览。
func (a *root) appearanceSection(c *ui.Context) {
	cr := controlRadius(c)
	prefs := a.themePrefs
	ui.Text(c, "外观").Bold()
	ui.Text(c, "跟随系统时保留桌面深浅色、强调色与无障碍设置；修改即时生效。").FontSize(12).
		TextColor(c.Theme().TextMuted)

	// 模式
	ui.Text(c, "主题").FontSize(12).TextColor(c.Theme().TextMuted)
	mode := modeIndex(prefs.Mode)
	if ui.Segmented(c, &mode, "跟随系统", "浅色", "深色").Radius(cr).Changed() {
		prefs.Mode = themeMode(mode)
		a.setThemePrefs(prefs)
	}

	// 配色方案
	ui.Text(c, "配色方案").FontSize(12).TextColor(c.Theme().TextMuted)
	schemeLabels := make([]string, len(themeSchemeOptions))
	for i, opt := range themeSchemeOptions {
		schemeLabels[i] = opt.label
	}
	scheme := themeSchemeIndex(prefs.Scheme)
	if ui.Segmented(c, &scheme, schemeLabels...).Radius(cr).Changed() {
		prefs.Scheme = themeSchemeOptions[scheme].key
		if prefs.Scheme == app.ThemeSchemeCustom {
			a.loadPaletteDrafts(a.themePrefs)
		}
		a.setThemePrefs(prefs)
	}
	if prefs.Scheme == app.ThemeSchemeCustom {
		if !a.paletteLoaded {
			a.loadPaletteDrafts(prefs)
		}
		a.paletteEditor(c, cr, prefs)
	}

	// 强调色
	ui.Text(c, "强调色").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		for _, opt := range themeAccentOptions {
			opt := opt
			sw := ui.ButtonBase(c).Label("强调色："+opt.label).Size(20, 20).Radius(10)
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
				a.themeHex = ""
				a.themeHexErr = ""
				a.setThemePrefs(prefs)
			}
		}
		if ui.TextInput(c, &a.themeHex).Placeholder("#rrggbb").Label("自定义强调色").
			Width(110).Radius(cr).Changed() {
			want := strings.ToLower(strings.TrimSpace(a.themeHex))
			if want == "" {
				a.themeHexErr = ""
				prefs.Accent = ""
				a.setThemePrefs(prefs)
			} else if p, warn := (app.ThemePrefs{
				Mode: prefs.Mode, Accent: want, Density: prefs.Density,
				Radius: prefs.Radius, FontSize: prefs.FontSize,
			}).Normalize(); len(warn) > 0 {
				a.themeHexErr = "颜色格式应为 #rrggbb"
			} else {
				a.themeHexErr = ""
				a.setThemePrefs(p)
			}
		}
		ui.Text(c, "修改即应用").FontSize(11).TextColor(c.Theme().TextMuted)
	})
	if a.themeHexErr != "" {
		ui.Text(c, a.themeHexErr).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}

	// 密度 / 圆角 / 字号
	ui.Text(c, "密度").FontSize(12).TextColor(c.Theme().TextMuted)
	density := densityIndex(prefs.Density)
	if ui.Segmented(c, &density, "紧凑", "标准", "宽松").Radius(cr).Changed() {
		prefs.Density = themeDensity(density)
		a.setThemePrefs(prefs)
	}
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "圆角").FontSize(12).TextColor(c.Theme().TextMuted)
		radius := float64(prefs.Radius)
		if ui.NumberInput(c, &radius, app.MinThemeRadius, app.MaxThemeRadius, 1).Label("圆角").
			Width(90).Radius(cr).Changed() {
			prefs.Radius = int(radius)
			a.setThemePrefs(prefs)
		}
	})

	// 预览：直接使用当前主题（改动已经是全局的）。
	ui.Text(c, "预览").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Box(c).FillWidth().Padding(12).Radius(c.Theme().Radius).
		Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.PrimaryButton(c, "主按钮").Radius(cr)
			ui.Button(c, "次按钮").Radius(cr)
			ui.Badge(c, "徽标")
			ui.Text(c, "次要文字").FontSize(12).TextColor(c.Theme().TextMuted)
		})
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, "选中行效果").Padding(4, 8).Radius(c.Theme().Radius).
				Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
			ui.Text(c, "普通行").Padding(4, 8).TextColor(c.Theme().Text)
		})
	})

	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, "恢复默认").Radius(cr).Clicked() {
			a.themeHex, a.themeHexErr = "", ""
			a.paletteLoaded, a.paletteErr = false, ""
			a.setThemePrefs(app.DefaultThemePrefs())
		}
		if ui.Button(c, "导出主题…").Radius(cr).Clicked() {
			a.pickExportTheme()
		}
		if ui.Button(c, "导入主题…").Radius(cr).Clicked() {
			a.pickImportTheme()
		}
	})
	if a.themeWarn != "" {
		ui.Text(c, a.themeWarn).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}
}

// exportThemeTo 把当前外观写到文件（对话框之外的实现；测试直接调用）。
func (a *root) exportThemeTo(path string) error {
	return app.ExportThemePrefs(path, a.themePrefs)
}

// importThemeFrom 读入主题文件并应用；返回被忽略项说明。
func (a *root) importThemeFrom(path string) ([]string, error) {
	prefs, warn, err := app.ImportThemePrefs(path)
	if err != nil {
		return nil, err
	}
	a.themeHex, a.themeHexErr = "", ""
	a.paletteLoaded, a.paletteErr = false, ""
	a.setThemePrefs(prefs)
	if len(warn) > 0 {
		a.themeWarn = "已导入（" + strings.Join(warn, "；") + "）"
	}
	app.LogInfo("startup", "theme imported", "path", path)
	return warn, nil
}

// pickExportTheme 弹保存对话框导出主题。
func (a *root) pickExportTheme() {
	win := a.win
	if win == nil {
		return
	}
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: win, Title: "导出主题", DefaultPath: "db-g-theme.json",
		})
		if err != nil || path == "" {
			return
		}
		err = a.exportThemeTo(path)
		a.safeUpdate(win, func() {
			if err != nil {
				a.themeWarn = "导出主题失败: " + err.Error()
				return
			}
			a.themeWarn = "已导出主题 → " + path
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
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "导入主题"})
		if err != nil || len(paths) == 0 {
			return
		}
		_, err = a.importThemeFrom(paths[0])
		a.safeUpdate(win, func() {
			if err != nil {
				a.themeWarn = "导入主题失败: " + err.Error()
			}
		})
	}()
}
