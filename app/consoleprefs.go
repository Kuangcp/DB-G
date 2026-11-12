package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ConsolePrefs 是控制台编辑体验设置。
type ConsolePrefs struct {
	// SaveDelaySeconds 是停止输入后自动保存正文的延迟（秒）。
	SaveDelaySeconds int `json:"saveDelaySeconds"`
}

const (
	DefaultConsoleSaveDelaySeconds = 5
	MinConsoleSaveDelaySeconds     = 1
	MaxConsoleSaveDelaySeconds     = 60
)

// DefaultConsolePrefs 返回默认控制台设置：自动保存延迟 5 秒。
func DefaultConsolePrefs() ConsolePrefs {
	return ConsolePrefs{SaveDelaySeconds: DefaultConsoleSaveDelaySeconds}
}

// NormalizeConsolePrefs 把缺省/越界的保存延迟回退到默认值或允许范围。
func NormalizeConsolePrefs(p ConsolePrefs) ConsolePrefs {
	if p.SaveDelaySeconds == 0 {
		p.SaveDelaySeconds = DefaultConsoleSaveDelaySeconds
	}
	if p.SaveDelaySeconds < MinConsoleSaveDelaySeconds {
		p.SaveDelaySeconds = MinConsoleSaveDelaySeconds
	}
	if p.SaveDelaySeconds > MaxConsoleSaveDelaySeconds {
		p.SaveDelaySeconds = MaxConsoleSaveDelaySeconds
	}
	return p
}

// DefaultConsolePrefsPath 返回控制台设置路径（与连接库同目录）。
func DefaultConsolePrefsPath() string {
	return filepath.Join(AppConfigDir(), "console.json")
}

// LoadConsolePrefs 读控制台设置；文件不存在或损坏时用默认值。
func LoadConsolePrefs(path string) (ConsolePrefs, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConsolePrefs(), ""
		}
		return DefaultConsolePrefs(), "读取控制台设置失败: " + err.Error()
	}
	prefs := DefaultConsolePrefs()
	if err := json.Unmarshal(data, &prefs); err != nil {
		return DefaultConsolePrefs(), "控制台设置损坏，已用默认值: " + err.Error()
	}
	return NormalizeConsolePrefs(prefs), ""
}

// SaveConsolePrefs 写回控制台设置（0600，先写临时文件再改名）。
func SaveConsolePrefs(path string, prefs ConsolePrefs) error {
	prefs = NormalizeConsolePrefs(prefs)
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
