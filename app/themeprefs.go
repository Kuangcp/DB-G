package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"db-g/locales"
)

// ThemePalette 是一套自定义配色的中性色覆盖；空字段表示沿用 Lobe 主题。
type ThemePalette struct {
	Background string `json:"background,omitempty"`
	Surface    string `json:"surface,omitempty"`
	Border     string `json:"border,omitempty"`
	Text       string `json:"text,omitempty"`
	TextMuted  string `json:"textMuted,omitempty"`
}

// ThemePrefs 是外观设置：模式、配色方案、强调色、间距密度、圆角与界面字号。
// 零值 / 缺省字段按默认值处理（跟随系统、默认配色、主题自带强调色、标准密度）。
type ThemePrefs struct {
	Mode     string `json:"mode"`
	Scheme   string `json:"scheme,omitempty"`
	Accent   string `json:"accent"`
	Density  string `json:"density"`
	Radius   int    `json:"radius"`
	FontSize int    `json:"fontSize"`
	// CustomLight/CustomDark 只在 Scheme == "custom" 时生效；均为空时沿用默认配色。
	CustomLight ThemePalette `json:"customLight,omitempty"`
	CustomDark  ThemePalette `json:"customDark,omitempty"`
	// SoftwareRender 关闭 mygo 的 GPU 渲染，改用 CPU 光栅化；重启后生效。
	SoftwareRender bool `json:"softwareRender,omitempty"`
}

// 外观设置的可选范围与默认值。
const (
	ThemeModeSystem = "system"
	ThemeModeLight  = "light"
	ThemeModeDark   = "dark"

	ThemeSchemeDefault = "default"
	ThemeSchemeOcean   = "ocean"
	ThemeSchemeForest  = "forest"
	ThemeSchemeWarm    = "warm"
	ThemeSchemeCustom  = "custom"

	ThemeDensityCompact = "compact"
	ThemeDensityNormal  = "normal"
	ThemeDensityRoomy   = "roomy"

	DefaultThemeRadius   = 6
	MinThemeRadius       = 0
	MaxThemeRadius       = 12
	MinThemeFontSize     = 12
	MaxThemeFontSize     = 18
	DefaultThemeFontSize = 0 // 0 = 跟随主题（含桌面 TextScale）

	themeFileVersion = 2
	themeFileApp     = "db-g"
)

// DefaultThemePrefs 返回默认外观：跟随系统、默认配色、主题强调色、标准密度、默认圆角。
func DefaultThemePrefs() ThemePrefs {
	return ThemePrefs{Mode: ThemeModeSystem, Scheme: ThemeSchemeDefault, Density: ThemeDensityNormal, Radius: DefaultThemeRadius}
}

// DefaultThemePrefsPath 返回外观设置路径（与连接库同目录）。
func DefaultThemePrefsPath() string {
	return filepath.Join(AppConfigDir(), "theme.json")
}

// ThemePrefsPath 返回给定配置根目录下的外观设置路径。
func ThemePrefsPath(configDir string) string {
	return filepath.Join(configDir, ConfigDirName(), "theme.json")
}

var accentHexPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// accentPresets 是内置强调色名；"default"/空串表示主题或桌面的强调色。
var accentPresets = map[string]bool{
	"": true, "default": true, "blue": true, "violet": true,
	"green": true, "orange": true, "rose": true,
}

// themeSchemes 是内置配色方案；custom 使用 ThemePrefs.CustomLight/CustomDark。
var themeSchemes = map[string]bool{
	ThemeSchemeDefault: true, ThemeSchemeOcean: true,
	ThemeSchemeForest: true, ThemeSchemeWarm: true, ThemeSchemeCustom: true,
}

// ValidThemePaletteColor 报告调色板颜色是否合法；空串表示沿用主题默认。
func ValidThemePaletteColor(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || accentHexPattern.MatchString(strings.ToLower(s))
}

// NormalizeThemePalette 修剪颜色并校验；非法项清空（沿用主题默认）。
func NormalizeThemePalette(p ThemePalette, label string) (ThemePalette, []string) {
	var warn []string
	norm := func(v, field string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			return ""
		}
		if !accentHexPattern.MatchString(v) {
			warn = append(warn, locales.AppThemeFieldInvalid(label, field, v))
			return ""
		}
		return v
	}
	return ThemePalette{
		Background: norm(p.Background, locales.AppPaletteBackground()),
		Surface:    norm(p.Surface, locales.AppPaletteSurface()),
		Border:     norm(p.Border, locales.AppPaletteBorder()),
		Text:       norm(p.Text, locales.AppPaletteText()),
		TextMuted:  norm(p.TextMuted, locales.AppPaletteMutedText()),
	}, warn
}

// Normalize 把缺省/非法字段回退为默认值，并返回被忽略项的说明。
// 返回的 prefs 一定是可直接持久化的规范值。
func (p ThemePrefs) Normalize() (ThemePrefs, []string) {
	out := DefaultThemePrefs()
	out.SoftwareRender = p.SoftwareRender
	var warn []string
	switch strings.ToLower(strings.TrimSpace(p.Mode)) {
	case "", ThemeModeSystem:
		out.Mode = ThemeModeSystem
	case ThemeModeLight:
		out.Mode = ThemeModeLight
	case ThemeModeDark:
		out.Mode = ThemeModeDark
	default:
		warn = append(warn, locales.AppThemeUnknownMode(p.Mode))
	}
	accent := strings.ToLower(strings.TrimSpace(p.Accent))
	if !accentPresets[accent] && !accentHexPattern.MatchString(accent) {
		warn = append(warn, locales.AppThemeBadAccent(p.Accent))
		accent = ""
	}
	if accent == "default" {
		accent = ""
	}
	out.Accent = accent
	scheme := strings.ToLower(strings.TrimSpace(p.Scheme))
	if scheme == "" {
		scheme = ThemeSchemeDefault
	}
	if !themeSchemes[scheme] {
		warn = append(warn, locales.AppThemeUnknownScheme(p.Scheme))
		scheme = ThemeSchemeDefault
	}
	out.Scheme = scheme
	var pw []string
	out.CustomLight, pw = NormalizeThemePalette(p.CustomLight, locales.AppThemeLightPalette())
	warn = append(warn, pw...)
	out.CustomDark, pw = NormalizeThemePalette(p.CustomDark, locales.AppThemeDarkPalette())
	warn = append(warn, pw...)
	switch strings.ToLower(strings.TrimSpace(p.Density)) {
	case "", ThemeDensityNormal:
		out.Density = ThemeDensityNormal
	case ThemeDensityCompact:
		out.Density = ThemeDensityCompact
	case ThemeDensityRoomy:
		out.Density = ThemeDensityRoomy
	default:
		warn = append(warn, locales.AppThemeUnknownDensity(p.Density))
	}
	if p.Radius < MinThemeRadius || p.Radius > MaxThemeRadius {
		warn = append(warn, locales.AppThemeRadiusRange(p.Radius, MinThemeRadius, MaxThemeRadius))
		out.Radius = DefaultThemeRadius
	} else {
		out.Radius = p.Radius
	}
	if p.FontSize != 0 && (p.FontSize < MinThemeFontSize || p.FontSize > MaxThemeFontSize) {
		warn = append(warn, locales.AppThemeFontRange(p.FontSize, MinThemeFontSize, MaxThemeFontSize))
		out.FontSize = DefaultThemeFontSize
	} else {
		out.FontSize = p.FontSize
	}
	return out, warn
}

// LoadThemePrefs 读外观设置；文件不存在用默认值，损坏时用默认值 + 警告。
func LoadThemePrefs(path string) (ThemePrefs, string) {
	p := DefaultThemePrefs()
	found, err := loadJSONFile(path, &p)
	if err != nil {
		if !found {
			return DefaultThemePrefs(), locales.AppThemeReadFailed(err.Error())
		}
		return DefaultThemePrefs(), locales.AppThemeCorrupt(err.Error())
	}
	if !found {
		return DefaultThemePrefs(), ""
	}
	prefs, warn := p.Normalize()
	if len(warn) > 0 {
		return prefs, locales.AppThemeCorrected(strings.Join(warn, "；"))
	}
	return prefs, ""
}

// themeFile 是 theme.json 与导出文件的共同结构。
type themeFile struct {
	App     string `json:"app,omitempty"`
	Version int    `json:"version,omitempty"`
	ThemePrefs
}

// SaveThemePrefs 写回外观设置（0600，先写临时文件再改名）。
func SaveThemePrefs(path string, p ThemePrefs) error {
	prefs, _ := p.Normalize()
	return writeThemeFile(path, themeFile{ThemePrefs: prefs})
}

// ExportThemePrefs 导出可分享的主题文件（带 app/version 标记）。
func ExportThemePrefs(path string, p ThemePrefs) error {
	prefs, _ := p.Normalize()
	return writeThemeFile(path, themeFile{App: themeFileApp, Version: themeFileVersion, ThemePrefs: prefs})
}

func writeThemeFile(path string, f themeFile) error {
	return saveJSONFile(path, f, 0o755)
}

// ImportThemePrefs 读取主题文件并校验：app 必须是 db-g（缺省容忍），
// version 不能高于本版本；非法字段回退默认并返回说明。
func ImportThemePrefs(path string) (ThemePrefs, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ThemePrefs{}, nil, err
	}
	f := themeFile{ThemePrefs: DefaultThemePrefs()}
	if err := json.Unmarshal(data, &f); err != nil {
		return ThemePrefs{}, nil, errors.New(locales.AppThemeInvalidJSON(err.Error()))
	}
	if f.App != "" && f.App != themeFileApp {
		return ThemePrefs{}, nil, errors.New(locales.AppThemeWrongApp(f.App))
	}
	if f.Version > themeFileVersion {
		return ThemePrefs{}, nil, errors.New(locales.AppThemeVersionTooNew(f.Version, themeFileVersion))
	}
	prefs, warn := f.ThemePrefs.Normalize()
	return prefs, warn, nil
}
