package ui

import (
	"math"
	"strings"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// resolvedTheme 是外观设置解析后的主题：Apply=false 表示完全交给 mygo
// 的默认主题（跟随系统且没有任何覆盖项）。
type resolvedTheme struct {
	Theme *ui.Theme
	Apply bool
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
//	base 是 mygo 的默认主题（已跟随系统与桌面偏好）；
//	desk 是桌面偏好（提供强调色与 TextScale）。
//
// mode=system 且无覆盖项时返回 Apply=false，确保 HighContrast 等桌面行为不丢。
func resolveTheme(base *ui.Theme, p app.ThemePrefs, desk ui.Preferences) resolvedTheme {
	prefs, _ := p.Normalize()
	var t ui.Theme
	switch prefs.Mode {
	case app.ThemeModeLight, app.ThemeModeDark:
		if prefs.Mode == app.ThemeModeDark {
			t = *ui.DarkTheme()
		} else {
			t = *ui.LightTheme()
		}
		// 显式模式不再经过 mygo 的内部 follow，手动应用桌面字号缩放。
		if prefs.FontSize > 0 {
			t.FontSize = float32(prefs.FontSize)
		} else {
			scale := desk.TextScale
			if scale <= 0 {
				scale = 1
			}
			t.FontSize *= scale
		}
	case app.ThemeModeSystem:
		t = *base
		if !themeHasOverrides(prefs) {
			return resolvedTheme{Theme: base, Apply: false}
		}
		if prefs.FontSize > 0 {
			t.FontSize = float32(prefs.FontSize)
		}
	default:
		t = *base
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
	return resolvedTheme{Theme: &t, Apply: true}
}

// themeHasOverrides 报告设置里是否有任何覆盖主题的项。
func themeHasOverrides(p app.ThemePrefs) bool {
	return p.Accent != "" ||
		p.Density != app.ThemeDensityNormal ||
		p.Radius != app.DefaultThemeRadius ||
		p.FontSize != app.DefaultThemeFontSize
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
