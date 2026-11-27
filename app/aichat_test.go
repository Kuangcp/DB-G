package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAIConversationsMigratesTitles(t *testing.T) {
	msgs := []AIMessageRecord{{Role: "user", Text: "select id from users"}}
	derived := AIConversationTitle(msgs)
	store := AIConversationStore{Version: aiConversationVersion, Conversations: []AIConversation{
		{ID: "empty", Messages: msgs},
		{ID: "legacy", Title: "新会话", Messages: msgs},
		{ID: "auto", Title: derived, Messages: msgs},
		{ID: "manual", Title: "改写：只查 id", Messages: msgs},
	}}
	path := filepath.Join(t.TempDir(), "conv.json")
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, warn := LoadAIConversations(path)
	if warn != "" {
		t.Fatal(warn)
	}
	want := map[string]struct {
		title  string
		locked bool
	}{
		"empty":  {derived, false},
		"legacy": {derived, false},
		"auto":   {derived, false},
		"manual": {"改写：只查 id", true},
	}
	for _, conv := range got.Conversations {
		w := want[conv.ID]
		if conv.Title != w.title || conv.TitleLocked != w.locked {
			t.Fatalf("%s: title=%q locked=%v, want %q %v", conv.ID, conv.Title, conv.TitleLocked, w.title, w.locked)
		}
	}
}
