package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func TestResolveThemeSystemNoOverrides(t *testing.T) {
	base := ui.DarkTheme()
	got := resolveTheme(base, app.DefaultThemePrefs(), ui.Preferences{})
	if got.Apply || got.Theme != base {
		t.Fatalf("system without overrides must hand over to mygo: %+v", got)
	}
}

func TestResolveThemeSystemWithOverrides(t *testing.T) {
	base := ui.DarkTheme()
	p := app.DefaultThemePrefs()
	p.Accent = "violet"
	got := resolveTheme(base, p, ui.Preferences{})
	if !got.Apply || !got.Theme.Dark {
		t.Fatalf("system with overrides must keep dark: %+v", got)
	}
	if got.Theme.Accent != themeAccentPresets["violet"] {
		t.Fatalf("accent: %v", got.Theme.Accent)
	}
	if got.Theme.AccentText != ui.Hex("#ffffff") {
		t.Fatalf("violet takes white text: %v", got.Theme.AccentText)
	}
}

func TestResolveThemeExplicitLightDark(t *testing.T) {
	light := resolveTheme(ui.DarkTheme(), app.ThemePrefs{Mode: "light"}, ui.Preferences{})
	if !light.Apply || light.Theme.Dark {
		t.Fatal("explicit light mode must be light")
	}
	dark := resolveTheme(ui.LightTheme(), app.ThemePrefs{Mode: "dark"}, ui.Preferences{})
	if !dark.Apply || !dark.Theme.Dark {
		t.Fatal("explicit dark mode must be dark")
	}
	// 默认强调色：桌面有强调色时用它。
	desk := ui.Preferences{Accent: ui.Hex("#ff8800")}
	got := resolveTheme(ui.LightTheme(), app.ThemePrefs{Mode: "dark"}, desk)
	if got.Theme.Accent != desk.Accent {
		t.Fatalf("desktop accent: %v want %v", got.Theme.Accent, desk.Accent)
	}
	// 桌面没有强调色：保留主题自带蓝色。
	got = resolveTheme(ui.LightTheme(), app.ThemePrefs{Mode: "dark"}, ui.Preferences{})
	if got.Theme.Accent != ui.DarkTheme().Accent {
		t.Fatalf("theme accent fallback: %v", got.Theme.Accent)
	}
}

func TestResolveThemeAccentContrast(t *testing.T) {
	// 亮黄强调色需要深色文字。
	got := resolveTheme(ui.LightTheme(), app.ThemePrefs{Mode: "light", Accent: "#facc15"}, ui.Preferences{})
	if got.Theme.AccentText.R > 40 {
		t.Fatalf("bright accent needs dark text: %+v", got.Theme.AccentText)
	}
	// 深色强调色用白字。
	got = resolveTheme(ui.LightTheme(), app.ThemePrefs{Mode: "light", Accent: "#1d4ed8"}, ui.Preferences{})
	if got.Theme.AccentText != ui.Hex("#ffffff") {
		t.Fatalf("dark accent needs white text: %+v", got.Theme.AccentText)
	}
	if got.Theme.AccentHover == got.Theme.Accent || got.Theme.AccentPressed == got.Theme.Accent {
		t.Fatal("hover/pressed must be derived")
	}
}

func TestResolveThemeDensityRadiusFontSize(t *testing.T) {
	p := app.ThemePrefs{Mode: "dark", Density: "compact", Radius: 10, FontSize: 16}
	got := resolveTheme(ui.LightTheme(), p, ui.Preferences{TextScale: 1.5})
	if got.Theme.Spacing != 3 || got.Theme.Radius != 10 {
		t.Fatalf("density/radius: spacing=%v radius=%v", got.Theme.Spacing, got.Theme.Radius)
	}
	if got.Theme.FontSize != 16 {
		t.Fatalf("font size: %v", got.Theme.FontSize)
	}
	// 字号 0 = 跟随主题并乘桌面 TextScale。
	p.FontSize = 0
	p.Density = "roomy"
	got = resolveTheme(ui.LightTheme(), p, ui.Preferences{TextScale: 1.5})
	want := ui.DarkTheme().FontSize * 1.5
	if got.Theme.FontSize != want || got.Theme.Spacing != 5 {
		t.Fatalf("scale: font=%v want %v spacing=%v", got.Theme.FontSize, want, got.Theme.Spacing)
	}
}

func TestResolveThemeSystemKeepsBaseMetrics(t *testing.T) {
	base := ui.LightTheme()
	p := app.DefaultThemePrefs()
	// 仅调圆角：其他字段保持主题原值。
	p.Radius = 2
	got := resolveTheme(base, p, ui.Preferences{})
	if !got.Apply || got.Theme.Radius != 2 {
		t.Fatalf("radius override: %+v", got)
	}
	if got.Theme.Spacing != base.Spacing || got.Theme.FontSize != base.FontSize {
		t.Fatal("unrelated metrics must keep the theme's")
	}
}
