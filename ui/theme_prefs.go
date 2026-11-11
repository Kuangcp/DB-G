package ui

import (
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	lobe "db-g/ui/lobe"
)

// applyTheme 在帧首应用外观设置；跟随系统且无覆盖时不调 SetTheme。
// 功能界面字体单独于 theme.json，设置后强制 SetTheme（只改字体，不动桌面主题）。
func (a *root) applyTheme(c *ui.Context) {
	t := resolveTheme(c.Theme(), a.themePrefs, c.Preferences()).Theme
	if fam := strings.TrimSpace(a.fontPrefs.UI); fam != "" {
		cp := *t
		cp.Font = fam
		t = &cp
	}
	c.SetTheme(t)
}

// resolvedThemeFor 是 resolvedTheme 的测试友好入口（用当前设置）。
func (a *root) resolvedThemeFor(base *ui.Theme, desk ui.Preferences) resolvedTheme {
	return resolveTheme(base, a.themePrefs, desk)
}

// setThemePrefs 更新外观设置：规范化、持久化（有窗口时防抖）、重画两个窗口。
func (a *root) setThemePrefs(p app.ThemePrefs) {
	prefs, warn := p.Normalize()
	a.themePrefs = prefs
	if strings.HasPrefix(prefs.Accent, "#") {
		a.themeHex = prefs.Accent
	} else {
		a.themeHex = ""
	}
	if len(warn) > 0 {
		a.themeWarn = strings.Join(warn, "；")
	} else {
		a.themeWarn = ""
	}
	a.saveThemePrefs()
	a.updateSettingsWindow()
	if a.win != nil {
		win := a.win
		a.safeUpdate(win, func() {})
	}
}

// saveThemePrefs 写盘：有窗口时延迟 300ms 合并连续改动（滑杆/输入框），
// headless（测试）同步写，便于断言。
func (a *root) saveThemePrefs() {
	path := a.themePath
	if path == "" {
		return
	}
	prefs := a.themePrefs
	if a.win == nil {
		if err := app.SaveThemePrefs(path, prefs); err != nil {
			app.LogError("startup", "theme save failed", "path", path, "err", err.Error())
			a.themeWarn = "保存外观设置失败: " + err.Error()
		}
		return
	}
	seq := a.themeSaveSeq.Add(1)
	win := a.win
	go func() {
		time.Sleep(300 * time.Millisecond)
		if a.themeSaveSeq.Load() != seq {
			return
		}
		if err := app.SaveThemePrefs(path, prefs); err != nil {
			app.LogError("startup", "theme save failed", "path", path, "err", err.Error())
			a.safeUpdate(win, func() { a.themeWarn = "保存外观设置失败: " + err.Error() })
		}
	}()
}

// resolvedTheme 是外观设置解析后的主题。
type resolvedTheme struct {
	Theme *ui.Theme
}

// themeAccentPresets 是内置强调色（浅/深色共用，派生 hover/pressed）。
var themeAccentPresets = map[string]ui.Color{
	"blue":   ui.Hex("#2563eb"),
	"violet": ui.Hex("#7c3aed"),
	"green":  ui.Hex("#16a34a"),
	"orange": ui.Hex("#ea580c"),
	"rose":   ui.Hex("#e11d48"),
}

// resolveTheme 按外观设置算出这一帧要用的主题。
//
// 基底是 Lobe UI 的设计 token（随浅/深色切换），再覆盖用户的外观设置；
// base 只用来判断系统模式下的深浅色。
func resolveTheme(base *ui.Theme, p app.ThemePrefs, desk ui.Preferences) resolvedTheme {
	prefs, _ := p.Normalize()
	dark := base.Dark
	switch prefs.Mode {
	case app.ThemeModeLight:
		dark = false
	case app.ThemeModeDark:
		dark = true
	}
	t := lobeUITheme(dark)
	if prefs.FontSize > 0 {
		t.FontSize = float32(prefs.FontSize)
	} else {
		scale := desk.TextScale
		if scale <= 0 {
			scale = 1
		}
		t.FontSize *= scale
	}
	if color, ok := themeAccent(prefs.Accent, &t, desk); ok {
		applyThemeAccent(&t, color)
	}
	t.Radius = float32(prefs.Radius)
	switch prefs.Density {
	case app.ThemeDensityCompact:
		t.Spacing = 3
	case app.ThemeDensityRoomy:
		t.Spacing = 5
	default:
		t.Spacing = 4
	}
	return resolvedTheme{Theme: &t}
}

// lobeUITheme 把 Lobe UI 的 token 映射成 mygo 的 ui.Theme。
func lobeUITheme(dark bool) ui.Theme {
	var tokens *lobe.Tokens
	if dark {
		tokens = lobe.Dark()
	} else {
		tokens = lobe.Light()
	}
	return *tokens.Theme()
}

// themeAccent 返回要用的强调色：设置里的预设/自定义优先；"默认"时优先用
// 桌面强调色，桌面没有则返回 false（保留主题自带的强调色）。
func themeAccent(name string, t *ui.Theme, desk ui.Preferences) (ui.Color, bool) {
	if c, ok := themeAccentPresets[name]; ok {
		return c, true
	}
	if strings.HasPrefix(name, "#") {
		return ui.Hex(name), true
	}
	if desk.Accent.A != 0 {
		return desk.Accent, true
	}
	return ui.Color{}, false
}

// applyThemeAccent 写入强调色并派生 hover/pressed/对比文字/焦点与选区。
func applyThemeAccent(t *ui.Theme, accent ui.Color) {
	hover, pressed, text := themeAccentPalette(accent, t.Dark)
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = accent, hover, pressed, text
	t.Focus = accent.Alpha(0.55)
	t.Selection = accent.Alpha(0.25)
}

// themeAccentPalette 派生强调色的 hover/pressed 与对比文字色。
func themeAccentPalette(accent ui.Color, dark bool) (hover, pressed, text ui.Color) {
	black, white := ui.RGB(0, 0, 0), ui.RGB(255, 255, 255)
	if dark {
		hover = accent.MixOklab(white, 0.18)
		pressed = accent.MixOklab(black, 0.22)
	} else {
		hover = accent.MixOklab(black, 0.14)
		pressed = accent.MixOklab(black, 0.24)
	}
	if themeLuminance(accent) > 0.55 {
		text = ui.Hex("#111827")
	} else {
		text = ui.Hex("#ffffff")
	}
	return hover, pressed, text
}

// themeLuminance 返回 sRGB 相对亮度（0–1，WCAG 定义）。
func themeLuminance(c ui.Color) float32 {
	lin := func(v uint8) float32 {
		x := float32(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return float32(math.Pow(float64((x+0.055)/1.055), 2.4))
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}
