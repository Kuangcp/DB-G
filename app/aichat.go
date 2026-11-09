package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AIMessageRecord 是持久化的 AI 消息；不保存上下文快照和样本行。
type AIMessageRecord struct {
	Role string `json:"role"`
	Text string `json:"text"`
	Done bool   `json:"done"`
	Err  string `json:"err,omitempty"`
	Time int64  `json:"time,omitempty"`
}

// AIConversation 是一个工作区的 AI 会话。
type AIConversation struct {
	Version  int               `json:"version"`
	Messages []AIMessageRecord `json:"messages"`
}

// aiConversationVersion 是会话文件格式版本。
const aiConversationVersion = 1

// aiConversationKeep 是单个工作区保留的消息条数。
const aiConversationKeep = 200

// AIConversationPath 返回工作区的 AI 会话文件路径。
func AIConversationPath(configDir, workspaceID string) string {
	name := strings.TrimSpace(workspaceID)
	if name == "" {
		name = "default"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name = b.String()
	if name == "" || name == "." || name == ".." {
		name = "default"
	}
	return filepath.Join(configDir, "ai", "conversations", name+".json")
}

// LoadAIConversation 读取会话；不存在返回空会话。第二个返回值是警告。
func LoadAIConversation(path string) (AIConversation, string) {
	if strings.TrimSpace(path) == "" {
		return AIConversation{}, ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AIConversation{}, ""
		}
		return AIConversation{}, "读取 AI 会话失败：" + err.Error()
	}
	var conv AIConversation
	if err := json.Unmarshal(data, &conv); err != nil {
		return AIConversation{}, "AI 会话格式错误：" + err.Error()
	}
	return conv, ""
}

// SaveAIConversation 原子保存会话，保留最近 aiConversationKeep 条消息。
func SaveAIConversation(path string, conv AIConversation) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	conv.Version = aiConversationVersion
	if len(conv.Messages) > aiConversationKeep {
		conv.Messages = conv.Messages[len(conv.Messages)-aiConversationKeep:]
	}
	now := time.Now().UnixMilli()
	for i := range conv.Messages {
		if conv.Messages[i].Time == 0 {
			conv.Messages[i].Time = now
		}
	}
	data, err := json.MarshalIndent(conv, "", "  ")
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
