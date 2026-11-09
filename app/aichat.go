package app

import (
	"crypto/rand"
	"encoding/hex"
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

// AIConversation 是一段 AI 会话。
type AIConversation struct {
	ID        string            `json:"id"`
	Title     string            `json:"title,omitempty"`
	CreatedAt int64             `json:"createdAt,omitempty"`
	UpdatedAt int64             `json:"updatedAt,omitempty"`
	Messages  []AIMessageRecord `json:"messages"`
}

// AIConversationStore 是一个工作区的全部会话。
type AIConversationStore struct {
	Version       int              `json:"version"`
	ActiveID      string           `json:"activeId,omitempty"`
	Conversations []AIConversation `json:"conversations"`
}

const (
	// aiConversationVersion 是会话文件格式版本；2 起支持多个会话。
	aiConversationVersion = 2
	// aiConversationKeep 是单个会话保留的消息条数。
	aiConversationKeep = 200
	// aiConversationsKeep 是单个工作区保留的会话数。
	aiConversationsKeep = 50
)

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

// NewAIConversationID 生成一个短会话 id。
func NewAIConversationID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "c" + time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(b[:])
}

// AIConversationTitle 从第一条用户消息生成标题；没有消息时返回空串。
func AIConversationTitle(messages []AIMessageRecord) string {
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		text := strings.Join(strings.Fields(m.Text), " ")
		if text == "" {
			continue
		}
		r := []rune(text)
		if len(r) > 32 {
			return string(r[:32]) + "…"
		}
		return text
	}
	return ""
}

// LoadAIConversations 读取工作区会话；不存在返回空 store。第二个返回值是警告。
// 兼容版本 1 的单会话文件：自动包成一个 id 为 default 的会话。
func LoadAIConversations(path string) (AIConversationStore, string) {
	if strings.TrimSpace(path) == "" {
		return AIConversationStore{}, ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AIConversationStore{}, ""
		}
		return AIConversationStore{}, "读取 AI 会话失败：" + err.Error()
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return AIConversationStore{}, "AI 会话格式错误：" + err.Error()
	}
	if _, ok := raw["conversations"]; !ok {
		var legacy struct {
			Messages []AIMessageRecord `json:"messages"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return AIConversationStore{}, "AI 会话格式错误：" + err.Error()
		}
		if len(legacy.Messages) == 0 {
			return AIConversationStore{}, ""
		}
		now := time.Now().UnixMilli()
		conv := AIConversation{ID: "default", Messages: legacy.Messages, CreatedAt: now, UpdatedAt: now}
		conv.Title = AIConversationTitle(conv.Messages)
		return AIConversationStore{
			Version: aiConversationVersion, ActiveID: conv.ID,
			Conversations: []AIConversation{conv},
		}, ""
	}
	var store AIConversationStore
	if err := json.Unmarshal(data, &store); err != nil {
		return AIConversationStore{}, "AI 会话格式错误：" + err.Error()
	}
	store.Version = aiConversationVersion
	seen := map[string]bool{}
	for i := range store.Conversations {
		conv := &store.Conversations[i]
		if conv.ID == "" || seen[conv.ID] {
			conv.ID = NewAIConversationID()
		}
		seen[conv.ID] = true
		if conv.Title == "" {
			conv.Title = AIConversationTitle(conv.Messages)
		}
	}
	if !seen[store.ActiveID] {
		store.ActiveID = ""
		if len(store.Conversations) > 0 {
			store.ActiveID = store.Conversations[0].ID
		}
	}
	return store, ""
}

// SaveAIConversations 原子保存工作区会话，裁剪消息与会话数量。
func SaveAIConversations(path string, store AIConversationStore) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	store.Version = aiConversationVersion
	now := time.Now().UnixMilli()
	for i := range store.Conversations {
		conv := &store.Conversations[i]
		if conv.ID == "" {
			conv.ID = NewAIConversationID()
		}
		if len(conv.Messages) > aiConversationKeep {
			conv.Messages = conv.Messages[len(conv.Messages)-aiConversationKeep:]
		}
		for j := range conv.Messages {
			if conv.Messages[j].Time == 0 {
				conv.Messages[j].Time = now
			}
		}
		if conv.CreatedAt == 0 {
			conv.CreatedAt = now
		}
		if conv.Title == "" {
			conv.Title = AIConversationTitle(conv.Messages)
		}
	}
	if len(store.Conversations) > aiConversationsKeep {
		keep := make([]AIConversation, 0, aiConversationsKeep)
		active := AIConversation{}
		for _, conv := range store.Conversations {
			if conv.ID == store.ActiveID {
				active = conv
				continue
			}
			keep = append(keep, conv)
		}
		if len(keep) > aiConversationsKeep-1 {
			keep = keep[len(keep)-(aiConversationsKeep-1):]
		}
		if active.ID != "" {
			keep = append([]AIConversation{active}, keep...)
		}
		store.Conversations = keep
	}
	data, err := json.MarshalIndent(store, "", "  ")
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
