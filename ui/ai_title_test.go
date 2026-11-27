package ui

import (
	"testing"

	"db-g/app"
)

// 标题自动命名不能依赖任何语言的默认文案：英文/中文的旧默认标题都要被首条用户消息替换。
func TestAIChatRetitlesLanguageNeutralDefault(t *testing.T) {
	for _, title := range []string{"New chat", "新会话"} {
		a := newConsoleRoot(t)
		a.aiChat.convID = "c1"
		a.aiChat.store.ActiveID = "c1"
		a.aiChat.store.Conversations = []app.AIConversation{{
			ID:       "c1",
			Title:    title,
			Messages: []app.AIMessageRecord{{Role: "user", Text: "select 1"}},
		}}
		a.aiChat.messages = []aiChatMessage{{role: "user", text: "select 1", done: true}}

		a.syncAIChat()

		if got := a.aiChat.store.Conversations[0].Title; got != "select 1" {
			t.Fatalf("title %q -> %q, want %q", title, got, "select 1")
		}
	}
}
