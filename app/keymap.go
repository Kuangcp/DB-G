package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultKeymapPath 返回快捷键配置路径（与连接库同目录）。
func DefaultKeymapPath() string {
	return filepath.Join(AppConfigDir(), "keymap.json")
}

// keymapFile 是 keymap.json 的结构：action → 键位串（逗号分隔多个，
// 空串表示用户特意解绑）。只存用户改过的动作，没出现的用默认值。
type keymapFile struct {
	Version  int               `json:"version"`
	Bindings map[string]string `json:"bindings"`
}

// LoadKeymap 读取用户改过的键位；文件不存在返回空表，损坏时返回空表 + 警告。
func LoadKeymap(path string) (map[string]string, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, ""
		}
		return map[string]string{}, "读取快捷键配置失败: " + err.Error()
	}
	var f keymapFile
	if err := json.Unmarshal(data, &f); err != nil {
		return map[string]string{}, "快捷键配置损坏，已用默认值（原文件保留）: " + err.Error()
	}
	if f.Bindings == nil {
		f.Bindings = map[string]string{}
	}
	return f.Bindings, ""
}

// SaveKeymap 写回用户改过的键位（0600，先写临时文件再改名）。
func SaveKeymap(path string, bindings map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(keymapFile{Version: 1, Bindings: bindings}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
