package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// DefaultAIBaseURL 是本地 Ollama 的 OpenAI-compatible 地址。
const DefaultAIBaseURL = "http://127.0.0.1:11434/v1"

// AIPrefs 是 AI 助手配置；APIKey 只在内存里明文，落盘走 Vault 加密。
type AIPrefs struct {
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
	// APIKey 是运行时明文；不直接序列化。
	APIKey string `json:"-"`
	// APIKeyEnc 是落盘的 enc:v1:… 密文。
	APIKeyEnc       string `json:"apiKey,omitempty"`
	AllowSampleRows bool   `json:"allowSampleRows"`
	AllowHistory    bool   `json:"allowHistory"`
	AllowErrorLogs  bool   `json:"allowErrorLogs"`
	// AITitle 为 true 时用模型生成会话标题；默认用首条消息截取。
	AITitle bool `json:"aiTitle,omitempty"`
}

// DefaultAIPrefs 返回默认配置：本地 Ollama，不发送样本/历史/错误日志。
func DefaultAIPrefs() AIPrefs {
	return AIPrefs{BaseURL: DefaultAIBaseURL}
}

// DefaultAIPrefsPath 返回 ai.json 路径。
func DefaultAIPrefsPath() string {
	return filepath.Join(AppConfigDir(), "ai.json")
}

// LoadAIPrefs 读取配置；文件不存在返回默认值。第二个返回值是需要展示的警告。
func LoadAIPrefs(path string, vault *Vault) (AIPrefs, string) {
	prefs := DefaultAIPrefs()
	if strings.TrimSpace(path) == "" {
		return prefs, ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return prefs, ""
		}
		return prefs, "读取 AI 配置失败：" + err.Error()
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return DefaultAIPrefs(), "AI 配置格式错误：" + err.Error()
	}
	if prefs.BaseURL == "" {
		prefs.BaseURL = DefaultAIBaseURL
	}
	if prefs.APIKeyEnc != "" {
		key, err := vault.Decrypt(prefs.APIKeyEnc)
		if err != nil {
			return prefs, "AI API Key 解密失败：" + err.Error()
		}
		prefs.APIKey = key
	}
	return prefs, ""
}

// SaveAIPrefs 保存配置；API Key 用 Vault 加密后写盘。
func SaveAIPrefs(path string, vault *Vault, prefs AIPrefs) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if prefs.APIKey != "" {
		enc, err := vault.Encrypt(prefs.APIKey)
		if err != nil {
			return err
		}
		prefs.APIKeyEnc = enc
	} else {
		prefs.APIKeyEnc = ""
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
