package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
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
		{"编辑器", a.editorSection},
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
	ui.Row(c).Fill().Gap(0).Children(func() {
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
}

// editorSection 是编辑器显示设置：字号、字体族，带实时预览。
func (a *root) editorSection(c *ui.Context) {
	ui.Text(c, "编辑器").Bold()
	ui.Text(c, "字号").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.NumberInput(c, &a.editorPrefs.FontSize, app.MinFontSize, app.MaxFontSize, 1).
			Label("字号").Width(90).Changed() {
			a.applyEditorPrefs()
		}
		ui.Text(c, fmt.Sprintf("%.0f pt", a.editorPrefs.FontSize)).FontSize(12).
			TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		if ui.Button(c, "恢复默认").Clicked() {
			a.editorPrefs = app.DefaultEditorPrefs()
			a.applyEditorPrefs()
		}
	})

	ui.Text(c, "字体族（留空 = 系统等宽）").FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.editorPrefs.FontFamily).
		Placeholder("例如 JetBrains Mono / Sarasa Mono SC").Changed() {
		a.applyEditorPrefs()
	}

	// 预览：用当前字体渲染一段 SQL。
	ui.Text(c, "预览").FontSize(12).TextColor(c.Theme().TextMuted)
	preview := "SELECT id, name FROM users WHERE id = 1; -- 预览 Preview"
	ui.Box(c).FillWidth().Padding(10).Radius(6).
		Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
		txt := ui.Text(c, preview).FontSize(a.code.state.Font.Size)
		if fam := a.editorPrefs.FontFamily; fam != "" {
			txt.Font(fam)
		} else {
			txt.Font("monospace")
		}
	})
	if a.editorPrefsWarn != "" {
		ui.Text(c, a.editorPrefsWarn).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}
}

// logSection 显示日志目录与打开入口。
func (a *root) logSection(c *ui.Context) {
	ui.Text(c, "日志").Bold()
	ui.Text(c, "一次启动一个文件，纯文本（时间 / 级别 / 标签 / 消息）。").FontSize(12).
		TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, app.LogDir()).FontSize(12).SingleLine().Grow(1)
		if ui.Button(c, "打开日志目录").Clicked() {
			mygo.Shell.ShowItemInFolder(app.LogDir())
		}
	})
}

// devSection 是开发用的编辑器性能面板：HUD、演示预设、补全缓存。
func (a *root) devSection(c *ui.Context) {
	disabled := a.activeConsole == ""
	ui.Text(c, "编辑器性能（开发用）").Bold()
	ui.Text(c, fmt.Sprintf(
		"行数=%d 字节=%d 版本=%d",
		a.code.LineCount(), a.code.ByteLen(), a.code.Version(),
	)).FontSize(12).SingleLine()
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, "刷新").Clicked() {
			a.updateSettingsWindow()
		}
	})

	ui.Text(c, "预设（写入当前激活控制台并立即保存）").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(6).Children(func() {
		for _, n := range []int{3_000, 12_000, 100_000} {
			label := fmt.Sprintf("%dk", n/1_000)
			if ui.Button(c, label).Disabled(disabled).Clicked() {
				a.applyEditorPreset(n)
			}
		}
	})

	ui.Text(c, "补全").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, "刷新补全缓存").Clicked() {
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

// applyEditorPrefs 把设置应用到编辑器并落盘。
func (a *root) applyEditorPrefs() {
	a.code.state.Font = ui.Font{
		Family: a.editorPrefs.FontFamily,
		Size:   float32(a.editorPrefs.FontSize),
	}
	if a.editorPrefsPath == "" {
		return
	}
	if err := app.SaveEditorPrefs(a.editorPrefsPath, a.editorPrefs); err != nil {
		a.editorPrefsWarn = "保存编辑器设置失败: " + err.Error()
	}
	a.updateSettingsWindow()
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
}

// requestSettings 请求打开（或聚焦）设置窗口。
// 有宿主窗口时通过 win.Update 排队到主循环（不在帧构建中途建窗）；
// 没有宿主窗口（headless 测试）时记请求，由帧首 maybeOpenSettings 处理。
func (a *root) requestSettings() {
	if a.win != nil {
		win := a.win
		win.Update(func() { a.openSettings() })
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
		Title:   "设置 — db-g",
		Width:   720,
		Height:  520,
		Content: ui.View(a.settingsView),
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
		win.Update(func() { a.settingsWin = nil })
	})
	return win
}

// updateSettingsWindow 让设置窗口重画（HUD/策略变化后）。
func (a *root) updateSettingsWindow() {
	if w := a.settingsWin; w != nil {
		w.Update(func() {})
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

// appearanceSection 是外观设置：模式 / 强调色 / 密度 / 圆角 / 界面字号 + 预览。
func (a *root) appearanceSection(c *ui.Context) {
	prefs := a.themePrefs
	ui.Text(c, "外观").Bold()
	ui.Text(c, "跟随系统时保留桌面深浅色、强调色与无障碍设置；修改即时生效。").FontSize(12).
		TextColor(c.Theme().TextMuted)

	// 模式
	ui.Text(c, "主题").FontSize(12).TextColor(c.Theme().TextMuted)
	mode := modeIndex(prefs.Mode)
	if ui.Segmented(c, &mode, "跟随系统", "浅色", "深色").Changed() {
		prefs.Mode = themeMode(mode)
		a.setThemePrefs(prefs)
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
			Width(110).Changed() {
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
	if ui.Segmented(c, &density, "紧凑", "标准", "宽松").Changed() {
		prefs.Density = themeDensity(density)
		a.setThemePrefs(prefs)
	}
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "圆角").FontSize(12).TextColor(c.Theme().TextMuted)
		radius := float64(prefs.Radius)
		if ui.NumberInput(c, &radius, app.MinThemeRadius, app.MaxThemeRadius, 1).Label("圆角").
			Width(90).Changed() {
			prefs.Radius = int(radius)
			a.setThemePrefs(prefs)
		}
		ui.Text(c, "界面字号").FontSize(12).TextColor(c.Theme().TextMuted)
		size := float64(prefs.FontSize)
		// 0 = 跟随主题（含桌面缩放），在输入框里显示 0。
		if ui.NumberInput(c, &size, app.DefaultThemeFontSize, app.MaxThemeFontSize, 1).
			Label("界面字号").Width(90).Changed() {
			prefs.FontSize = int(size)
			a.setThemePrefs(prefs)
		}
		ui.Text(c, "0 = 跟随主题").FontSize(11).TextColor(c.Theme().TextMuted)
	})

	// 预览：直接使用当前主题（改动已经是全局的）。
	ui.Text(c, "预览").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Box(c).FillWidth().Padding(12).Radius(c.Theme().Radius).
		Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.PrimaryButton(c, "主按钮")
			ui.Button(c, "次按钮")
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
		if ui.Button(c, "恢复默认").Clicked() {
			a.themeHex, a.themeHexErr = "", ""
			a.setThemePrefs(app.DefaultThemePrefs())
		}
		if ui.Button(c, "导出主题…").Clicked() {
			a.pickExportTheme()
		}
		if ui.Button(c, "导入主题…").Clicked() {
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
		win.Update(func() {
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
		win.Update(func() {
			if err != nil {
				a.themeWarn = "导入主题失败: " + err.Error()
			}
		})
	}()
}
