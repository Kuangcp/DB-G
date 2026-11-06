package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// EditorPrefs 是编辑器的显示设置（字号 / 字体族）。
type EditorPrefs struct {
	FontSize   float64 `json:"fontSize,omitempty"`   // 0 = 用 mygo 的默认 13
	FontFamily string  `json:"fontFamily,omitempty"` // "" = 系统等宽
}

// DefaultEditorPrefs 返回默认设置。
func DefaultEditorPrefs() EditorPrefs { return EditorPrefs{FontSize: 13} }

// 字号的可选范围。
const (
	MinFontSize = 8
	MaxFontSize = 24
)

// DefaultEditorPrefsPath 返回编辑器设置路径（与连接库同目录）。
func DefaultEditorPrefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "editor.json"
	}
	return filepath.Join(dir, "db-g", "editor.json")
}

// LoadEditorPrefs 读编辑器设置；文件不存在用默认值，损坏时用默认值 + 警告。
func LoadEditorPrefs(path string) (EditorPrefs, string) {
	prefs := DefaultEditorPrefs()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return prefs, ""
		}
		return prefs, "读取编辑器设置失败: " + err.Error()
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return DefaultEditorPrefs(), "编辑器设置损坏，已用默认值: " + err.Error()
	}
	if prefs.FontSize < MinFontSize || prefs.FontSize > MaxFontSize {
		prefs.FontSize = DefaultEditorPrefs().FontSize
	}
	return prefs, ""
}

// SaveEditorPrefs 写回编辑器设置（0600，先写临时文件再改名）。
func SaveEditorPrefs(path string, prefs EditorPrefs) error {
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
