package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"db-g/locales"
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
	AllowHistory    bool   `json:"allowHistory"`
	AllowErrorLogs  bool   `json:"allowErrorLogs"`
	AllowSampleRows bool   `json:"allowSampleRows"`
	// AllowDDL 为 true 时发送当前 SQL 引用表的 DDL（默认关）。
	AllowDDL bool `json:"allowDDL"`
	// AITitle 为 true 时用模型生成会话标题；默认用首条消息截取。
	AITitle bool `json:"aiTitle,omitempty"`
	// ContextWindow 是模型上下文窗口（token）；0 = 使用 ai.DefaultContextWindow。
	ContextWindow int `json:"contextWindow,omitempty"`
	// 任务级模型：空串表示继承主 Model。
	CompletionModel string `json:"completionModel,omitempty"`
	FixModel        string `json:"fixModel,omitempty"`
	// 任务级生成参数；0 在读取时补默认值。
	ChatTemperature     float64 `json:"chatTemperature,omitempty"`
	ChatMaxTokens       int     `json:"chatMaxTokens,omitempty"`
	CompletionMaxTokens int     `json:"completionMaxTokens,omitempty"`
	FixMaxTokens        int     `json:"fixMaxTokens,omitempty"`
	RewriteMaxTokens    int     `json:"rewriteMaxTokens,omitempty"`
}

// DefaultAIPrefs 返回默认配置：本地 Ollama，默认只发表结构、当前 SQL 与结果形状。
func DefaultAIPrefs() AIPrefs {
	return AIPrefs{
		BaseURL:             DefaultAIBaseURL,
		ChatTemperature:     0.2,
		ChatMaxTokens:       1200,
		CompletionMaxTokens: 160,
		FixMaxTokens:        1200,
		RewriteMaxTokens:    1600,
	}
}

// NormalizeAIPrefs 给旧 ai.json 缺失的任务参数补默认值；不覆盖已填值。
func NormalizeAIPrefs(p AIPrefs) AIPrefs {
	if p.ChatTemperature < 0.1 || p.ChatTemperature > 2 {
		p.ChatTemperature = 0.2
	}
	if p.ChatMaxTokens <= 0 {
		p.ChatMaxTokens = 1200
	}
	if p.CompletionMaxTokens <= 0 {
		p.CompletionMaxTokens = 160
	}
	if p.FixMaxTokens <= 0 {
		p.FixMaxTokens = 1200
	}
	if p.RewriteMaxTokens <= 0 {
		p.RewriteMaxTokens = 1600
	}
	return p
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
		return prefs, locales.AppAIReadFailed(err.Error())
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return DefaultAIPrefs(), locales.AppAICorrupt(err.Error())
	}
	prefs = NormalizeAIPrefs(prefs)
	if prefs.BaseURL == "" {
		prefs.BaseURL = DefaultAIBaseURL
	}
	if prefs.APIKeyEnc != "" {
		key, err := vault.Decrypt(prefs.APIKeyEnc)
		if err != nil {
			return prefs, locales.AppAIKeyDecryptFailed(err.Error())
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
	prefs = NormalizeAIPrefs(prefs)
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
