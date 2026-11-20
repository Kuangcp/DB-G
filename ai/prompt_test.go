package ai

import (
	"strings"
	"testing"
)

func TestBuildMessagesFollowsLanguage(t *testing.T) {
	snap := ContextSnapshot{Language: "en", Connection: ConnInfo{Kind: "mysql"}}
	en := BuildMessages(snap, "why slow")
	if !strings.Contains(en[0].Content, "Always answer and explain in English") {
		t.Fatalf("en system prompt missing language instruction: %q", en[0].Content)
	}
	snap.Language = "zh-Hans"
	zh := BuildMessages(snap, "为什么慢")
	if !strings.Contains(zh[0].Content, "始终用简体中文回答") {
		t.Fatalf("zh system prompt missing language instruction: %q", zh[0].Content)
	}
}
