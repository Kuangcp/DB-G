package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"db-g/sync"
)

// SyncPrefs 是 git 同步配置；不存密钥本身（密钥在同目录 sync.key）。
type SyncPrefs struct {
	RepoURL          string   `json:"repoURL"`
	Branch           string   `json:"branch,omitempty"`
	SSHKeyPath       string   `json:"sshKeyPath,omitempty"`
	KeyKCV           string   `json:"keyKCV,omitempty"`
	AcceptedFindings []string `json:"acceptedFindings,omitempty"`
	LastSyncAt       int64    `json:"lastSyncAt,omitempty"`
}

// DefaultSyncPrefs 返回默认配置。
func DefaultSyncPrefs() SyncPrefs { return SyncPrefs{Branch: "main"} }

// DefaultSyncPrefsPath 返回 sync.json 路径。
func DefaultSyncPrefsPath() string { return filepath.Join(AppConfigDir(), "sync.json") }

// SyncKeyPath 返回与 sync.json 同目录的 sync.key。
func SyncKeyPath(prefsPath string) string {
	return filepath.Join(filepath.Dir(prefsPath), "sync.key")
}

// SyncWorkTree 返回工作树目录（随配置路径隔离，测试不会碰真实目录）。
func SyncWorkTree(prefsPath string) string {
	return filepath.Join(filepath.Dir(prefsPath), "sync", "repo")
}

// LoadSyncPrefs 读取配置；不存在返回默认值，第二个返回值是警告。
func LoadSyncPrefs(path string) (SyncPrefs, string) {
	prefs := DefaultSyncPrefs()
	if strings.TrimSpace(path) == "" {
		return prefs, ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return prefs, ""
		}
		return prefs, "读取同步配置失败：" + err.Error()
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return DefaultSyncPrefs(), "同步配置格式错误：" + err.Error()
	}
	if strings.TrimSpace(prefs.Branch) == "" {
		prefs.Branch = "main"
	}
	prefs.SSHKeyPath = expandHome(strings.TrimSpace(prefs.SSHKeyPath))
	return prefs, ""
}

// SaveSyncPrefs 原子写入同步配置（0600）。
func SaveSyncPrefs(path string, prefs SyncPrefs) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if strings.TrimSpace(prefs.Branch) == "" {
		prefs.Branch = "main"
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadSyncKey 读取本机同步密钥；不存在时 ok=false。
func LoadSyncKey(prefsPath string) ([]byte, bool) {
	key, err := sync.LoadKeyFile(SyncKeyPath(prefsPath))
	return key, err == nil
}

// SaveSyncKey 保存密钥并更新 prefs 里的 KCV。
func SaveSyncKey(prefsPath string, key []byte) error {
	if err := sync.SaveKeyFile(SyncKeyPath(prefsPath), key); err != nil {
		return err
	}
	prefs, _ := LoadSyncPrefs(prefsPath)
	prefs.KeyKCV = sync.KCV(key)
	return SaveSyncPrefs(prefsPath, prefs)
}

// ClearSyncKey 删除本机同步密钥并清掉 prefs 里的 KCV。
func ClearSyncKey(prefsPath string) error {
	if err := os.Remove(SyncKeyPath(prefsPath)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	prefs, _ := LoadSyncPrefs(prefsPath)
	prefs.KeyKCV = ""
	return SaveSyncPrefs(prefsPath, prefs)
}

// expandHome 展开开头的 ~/。
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
