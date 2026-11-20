package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// LocalizationPrefs 是界面语言偏好。
// Locale 为空表示跟随系统；目前只交付 en 与 zh-Hans 两种。
type LocalizationPrefs struct {
	Locale string `json:"locale"`
}

// 界面语言的可选值："" 跟随系统。
const (
	LocaleSystem = ""
	LocaleEn     = "en"
	LocaleZhHans = "zh-Hans"
)

// DefaultLocalePrefs 返回默认语言偏好：跟随系统。
func DefaultLocalePrefs() LocalizationPrefs {
	return LocalizationPrefs{Locale: LocaleSystem}
}

// NormalizeLocalePrefs 归一化语言偏好：空串保留，已知语言按 canonical 标签返回，
// 未知值回退跟随系统并给出说明。
func NormalizeLocalePrefs(p LocalizationPrefs) (LocalizationPrefs, string) {
	tag := strings.TrimSpace(p.Locale)
	if tag == "" {
		return LocalizationPrefs{Locale: LocaleSystem}, ""
	}
	lower := strings.ToLower(tag)
	switch {
	case lower == "en" || strings.HasPrefix(lower, "en-"):
		return LocalizationPrefs{Locale: LocaleEn}, ""
	case lower == "zh" || lower == "zh-hans" || strings.HasPrefix(lower, "zh-hans-"):
		return LocalizationPrefs{Locale: LocaleZhHans}, ""
	}
	return DefaultLocalePrefs(), "未知界面语言 " + tag + "，已改为跟随系统"
}

// DefaultLocalePrefsPath 返回语言偏好文件路径（与连接库同目录）。
func DefaultLocalePrefsPath() string {
	return filepath.Join(AppConfigDir(), "locale.json")
}

// LoadLocalePrefs 读语言偏好；文件不存在或损坏时用默认值（跟随系统）。
func LoadLocalePrefs(path string) (LocalizationPrefs, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultLocalePrefs(), ""
		}
		return DefaultLocalePrefs(), "读取语言设置失败: " + err.Error()
	}
	p := DefaultLocalePrefs()
	if err := json.Unmarshal(data, &p); err != nil {
		return DefaultLocalePrefs(), "语言设置损坏，已用默认值: " + err.Error()
	}
	prefs, warn := NormalizeLocalePrefs(p)
	return prefs, warn
}

// SaveLocalePrefs 写回语言偏好（0600，先写临时文件再改名）。
func SaveLocalePrefs(path string, p LocalizationPrefs) error {
	prefs, _ := NormalizeLocalePrefs(p)
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
