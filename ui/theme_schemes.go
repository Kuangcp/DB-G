package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// themeSchemeOption 是设置界面里的一个配色方案选项。
type themeSchemeOption struct {
	key   string
	label string
}

var themeSchemeOptions = []themeSchemeOption{
	{app.ThemeSchemeDefault, "默认"},
	{app.ThemeSchemeOcean, "海洋"},
	{app.ThemeSchemeForest, "森林"},
	{app.ThemeSchemeWarm, "暖阳"},
	{app.ThemeSchemeCustom, "自定义"},
}

// themeSchemePalette 是一套配色方案在浅色/深色下的中性色覆盖。
type themeSchemePalette struct {
	light app.ThemePalette
	dark  app.ThemePalette
}

var defaultPalettePair = themeSchemePalette{
	light: app.ThemePalette{
		Background: "#f8f8f8", Surface: "#ffffff", Border: "#e3e3e3",
		Text: "#1f1f1f", TextMuted: "#666666",
	},
	dark: app.ThemePalette{
		Background: "#1a1a1a", Surface: "#242424", Border: "#343434",
		Text: "#e6e6e6", TextMuted: "#b4b4b4",
	},
}

var themeSchemePalettes = map[string]themeSchemePalette{
	app.ThemeSchemeDefault: defaultPalettePair,
	app.ThemeSchemeOcean: {
		light: app.ThemePalette{
			Background: "#edf4fb", Surface: "#ffffff", Border: "#d3e2f2",
			Text: "#17212b", TextMuted: "#5b6b7c",
		},
		dark: app.ThemePalette{
			Background: "#0f1822", Surface: "#172433", Border: "#2a3d52",
			Text: "#e6f0fa", TextMuted: "#9fb2c6",
		},
	},
	app.ThemeSchemeForest: {
		light: app.ThemePalette{
			Background: "#eef5ef", Surface: "#ffffff", Border: "#d2e3d5",
			Text: "#15231a", TextMuted: "#5a6c5f",
		},
		dark: app.ThemePalette{
			Background: "#0e1712", Surface: "#16241c", Border: "#2a3e32",
			Text: "#e2efe6", TextMuted: "#9db2a3",
		},
	},
	app.ThemeSchemeWarm: {
		light: app.ThemePalette{
			Background: "#faf4ec", Surface: "#fffdf9", Border: "#e8d9c8",
			Text: "#241b12", TextMuted: "#7a6958",
		},
		dark: app.ThemePalette{
			Background: "#1a1410", Surface: "#251d16", Border: "#3b3026",
			Text: "#f1e7dc", TextMuted: "#b6a695",
		},
	},
}

// schemePalette 返回当前配色方案在指定深浅模式下的覆盖。
func schemePalette(p app.ThemePrefs, dark bool) app.ThemePalette {
	if p.Scheme == app.ThemeSchemeCustom {
		if dark {
			if p.CustomDark == (app.ThemePalette{}) {
				return defaultPalettePair.dark
			}
			return p.CustomDark
		}
		if p.CustomLight == (app.ThemePalette{}) {
			return defaultPalettePair.light
		}
		return p.CustomLight
	}
	pair, ok := themeSchemePalettes[p.Scheme]
	if !ok {
		pair = defaultPalettePair
	}
	if dark {
		return pair.dark
	}
	return pair.light
}

// applyThemePalette 把非空的中性色覆盖写进主题。
func applyThemePalette(t *ui.Theme, p app.ThemePalette) {
	set := func(dst *ui.Color, hex string) {
		if hex = strings.TrimSpace(hex); hex != "" {
			*dst = ui.Hex(hex)
		}
	}
	set(&t.Background, p.Background)
	set(&t.Surface, p.Surface)
	set(&t.Border, p.Border)
	set(&t.Text, p.Text)
	set(&t.TextMuted, p.TextMuted)
}

// defaultCustomPalettes 是自定义配色的初始值（即可编辑的默认方案）。
func defaultCustomPalettes() (app.ThemePalette, app.ThemePalette) {
	return defaultPalettePair.light, defaultPalettePair.dark
}

// WindowBackgroundColor 返回无边框窗口的底色（允许 light-dark 语法）。
func WindowBackgroundColor(p app.ThemePrefs) string {
	light := defaultPalettePair.light.Background
	dark := defaultPalettePair.dark.Background
	if bg := schemePalette(p, false).Background; bg != "" {
		light = bg
	}
	if bg := schemePalette(p, true).Background; bg != "" {
		dark = bg
	}
	switch p.Mode {
	case app.ThemeModeLight:
		return light
	case app.ThemeModeDark:
		return dark
	}
	return "light-dark(" + light + ", " + dark + ")"
}

// themeSchemeIndex 返回配色方案在 Segmented 里的下标。
func themeSchemeIndex(key string) int {
	for i, opt := range themeSchemeOptions {
		if opt.key == key {
			return i
		}
	}
	return 0
}

// loadPaletteDrafts 把自定义配色从 prefs 装进编辑草稿；整块为空时用默认方案。
func (a *root) loadPaletteDrafts(prefs app.ThemePrefs) {
	defLight, defDark := defaultCustomPalettes()
	a.paletteLight, a.paletteDark = prefs.CustomLight, prefs.CustomDark
	if a.paletteLight == (app.ThemePalette{}) {
		a.paletteLight = defLight
	}
	if a.paletteDark == (app.ThemePalette{}) {
		a.paletteDark = defDark
	}
	a.paletteLoaded, a.paletteErr = true, ""
}

// applyPaletteDrafts 校验草稿并应用；非法颜色只提示，不落盘。
func (a *root) applyPaletteDrafts(prefs app.ThemePrefs) {
	for _, p := range []app.ThemePalette{a.paletteLight, a.paletteDark} {
		for _, color := range []string{p.Background, p.Surface, p.Border, p.Text, p.TextMuted} {
			if !app.ValidThemePaletteColor(color) {
				a.paletteErr = "颜色格式应为 #rrggbb（留空沿用默认）"
				return
			}
		}
	}
	a.paletteErr = ""
	prefs.Scheme = app.ThemeSchemeCustom
	prefs.CustomLight, prefs.CustomDark = a.paletteLight, a.paletteDark
	a.setThemePrefs(prefs)
}
