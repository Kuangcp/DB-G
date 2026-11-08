package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// FontSpec 是一类区域的字体设置：字体族（逗号分隔回退）与字号。
// Size 为 0 表示跟随主题（结果区还会跟随界面字号）。
type FontSpec struct {
	Family string  `json:"family,omitempty"`
	Size   float64 `json:"size,omitempty"`
}

// FontPrefs 是全局字体设置：编辑区、结果区、功能界面各用一套。
// 功能界面的字号仍由 ThemePrefs.FontSize 统一控制（外观里叫「界面字号」）。
type FontPrefs struct {
	Editor FontSpec `json:"editor"`
	Result FontSpec `json:"result"`
	UI     string   `json:"ui,omitempty"` // 字体族，空 = 系统 UI 字体
}

// DefaultFontPrefs 返回默认设置：编辑区 13pt 系统等宽；结果区与功能界面跟随主题。
func DefaultFontPrefs() FontPrefs {
	return FontPrefs{Editor: FontSpec{Size: 13}}
}

// 字号的可选范围。
const (
	MinFontSize = 8
	MaxFontSize = 24
)

// DefaultFontPrefsPath 返回字体设置路径（与连接库同目录）。
func DefaultFontPrefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "fonts.json"
	}
	return filepath.Join(dir, "db-g", "fonts.json")
}

// NormalizeFontPrefs 把越界字号回退到默认值，并去掉字体族首尾空白。
func NormalizeFontPrefs(p FontPrefs) FontPrefs {
	p.Editor.Family = strings.TrimSpace(p.Editor.Family)
	p.Result.Family = strings.TrimSpace(p.Result.Family)
	p.UI = strings.TrimSpace(p.UI)
	if p.Editor.Size < MinFontSize || p.Editor.Size > MaxFontSize {
		p.Editor.Size = DefaultFontPrefs().Editor.Size
	}
	if p.Result.Size != 0 && (p.Result.Size < MinFontSize || p.Result.Size > MaxFontSize) {
		p.Result.Size = 0
	}
	return p
}

// LoadFontPrefs 读字体设置；文件不存在用默认值（并从旧的 editor.json 迁移），
// 损坏时用默认值 + 警告。
func LoadFontPrefs(path string) (FontPrefs, string) {
	prefs := DefaultFontPrefs()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return migrateLegacyEditorPrefs(path)
		}
		return prefs, "读取字体设置失败: " + err.Error()
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return DefaultFontPrefs(), "字体设置损坏，已用默认值: " + err.Error()
	}
	return NormalizeFontPrefs(prefs), ""
}

// legacyEditorPrefs 是旧的 editor.json 结构，只为迁移保留。
type legacyEditorPrefs struct {
	FontSize   float64 `json:"fontSize,omitempty"`
	FontFamily string  `json:"fontFamily,omitempty"`
}

// migrateLegacyEditorPrefs 把 editor.json 里的编辑区字体搬进 fonts.json。
func migrateLegacyEditorPrefs(path string) (FontPrefs, string) {
	legacyPath := filepath.Join(filepath.Dir(path), "editor.json")
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return DefaultFontPrefs(), ""
	}
	var legacy legacyEditorPrefs
	if err := json.Unmarshal(data, &legacy); err != nil {
		return DefaultFontPrefs(), ""
	}
	prefs := DefaultFontPrefs()
	prefs.Editor = FontSpec{Family: legacy.FontFamily, Size: legacy.FontSize}
	prefs = NormalizeFontPrefs(prefs)
	if err := SaveFontPrefs(path, prefs); err != nil {
		return prefs, "迁移编辑器字体设置失败: " + err.Error()
	}
	return prefs, ""
}

// SaveFontPrefs 写回字体设置（0600，先写临时文件再改名）。
func SaveFontPrefs(path string, prefs FontPrefs) error {
	prefs = NormalizeFontPrefs(prefs)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
