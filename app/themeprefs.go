package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ThemePrefs 是外观设置：模式、强调色、间距密度、圆角与界面字号。
// 零值 / 缺省字段按默认值处理（跟随系统、主题自带强调色、标准密度）。
type ThemePrefs struct {
	Mode     string `json:"mode"`
	Accent   string `json:"accent"`
	Density  string `json:"density"`
	Radius   int    `json:"radius"`
	FontSize int    `json:"fontSize"`
}

// 外观设置的可选范围与默认值。
const (
	ThemeModeSystem = "system"
	ThemeModeLight  = "light"
	ThemeModeDark   = "dark"

	ThemeDensityCompact = "compact"
	ThemeDensityNormal  = "normal"
	ThemeDensityRoomy   = "roomy"

	DefaultThemeRadius   = 6
	MinThemeRadius       = 0
	MaxThemeRadius       = 12
	MinThemeFontSize     = 12
	MaxThemeFontSize     = 18
	DefaultThemeFontSize = 0 // 0 = 跟随主题（含桌面 TextScale）

	themeFileVersion = 1
	themeFileApp     = "db-g"
)

// DefaultThemePrefs 返回默认外观：跟随系统、主题强调色、标准密度、默认圆角。
func DefaultThemePrefs() ThemePrefs {
	return ThemePrefs{Mode: ThemeModeSystem, Density: ThemeDensityNormal, Radius: DefaultThemeRadius}
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

// Normalize 把缺省/非法字段回退为默认值，并返回被忽略项的说明。
// 返回的 prefs 一定是可直接持久化的规范值。
func (p ThemePrefs) Normalize() (ThemePrefs, []string) {
	out := DefaultThemePrefs()
	var warn []string
	switch strings.ToLower(strings.TrimSpace(p.Mode)) {
	case "", ThemeModeSystem:
		out.Mode = ThemeModeSystem
	case ThemeModeLight:
		out.Mode = ThemeModeLight
	case ThemeModeDark:
		out.Mode = ThemeModeDark
	default:
		warn = append(warn, fmt.Sprintf("未知主题模式 %q，已用跟随系统", p.Mode))
	}
	accent := strings.ToLower(strings.TrimSpace(p.Accent))
	if !accentPresets[accent] && !accentHexPattern.MatchString(accent) {
		warn = append(warn, fmt.Sprintf("强调色 %q 无效，已用默认", p.Accent))
		accent = ""
	}
	if accent == "default" {
		accent = ""
	}
	out.Accent = accent
	switch strings.ToLower(strings.TrimSpace(p.Density)) {
	case "", ThemeDensityNormal:
		out.Density = ThemeDensityNormal
	case ThemeDensityCompact:
		out.Density = ThemeDensityCompact
	case ThemeDensityRoomy:
		out.Density = ThemeDensityRoomy
	default:
		warn = append(warn, fmt.Sprintf("未知密度 %q，已用标准", p.Density))
	}
	if p.Radius < MinThemeRadius || p.Radius > MaxThemeRadius {
		warn = append(warn, fmt.Sprintf("圆角 %d 超出 %d–%d，已用默认", p.Radius, MinThemeRadius, MaxThemeRadius))
		out.Radius = DefaultThemeRadius
	} else {
		out.Radius = p.Radius
	}
	if p.FontSize != 0 && (p.FontSize < MinThemeFontSize || p.FontSize > MaxThemeFontSize) {
		warn = append(warn, fmt.Sprintf("界面字号 %d 超出 %d–%d，已用默认", p.FontSize, MinThemeFontSize, MaxThemeFontSize))
		out.FontSize = DefaultThemeFontSize
	} else {
		out.FontSize = p.FontSize
	}
	return out, warn
}

// LoadThemePrefs 读外观设置；文件不存在用默认值，损坏时用默认值 + 警告。
func LoadThemePrefs(path string) (ThemePrefs, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultThemePrefs(), ""
		}
		return DefaultThemePrefs(), "读取外观设置失败: " + err.Error()
	}
	p := DefaultThemePrefs()
	if err := json.Unmarshal(data, &p); err != nil {
		return DefaultThemePrefs(), "外观设置损坏，已用默认值: " + err.Error()
	}
	prefs, warn := p.Normalize()
	if len(warn) > 0 {
		return prefs, "外观设置已修正: " + strings.Join(warn, "；")
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
		return ThemePrefs{}, nil, errors.New("主题文件不是合法 JSON: " + err.Error())
	}
	if f.App != "" && f.App != themeFileApp {
		return ThemePrefs{}, nil, fmt.Errorf("不是 db-g 主题文件（app=%q）", f.App)
	}
	if f.Version > themeFileVersion {
		return ThemePrefs{}, nil, fmt.Errorf("主题文件版本 %d 高于支持的 %d", f.Version, themeFileVersion)
	}
	prefs, warn := f.ThemePrefs.Normalize()
	return prefs, warn, nil
}
