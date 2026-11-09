package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAIPrefsDefaultsAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")
	vault, err := LoadVault(storePath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ai.json")

	prefs, warn := LoadAIPrefs(path, vault)
	if warn != "" {
		t.Fatalf("missing file must not warn: %q", warn)
	}
	if prefs.BaseURL != DefaultAIBaseURL {
		t.Fatalf("default base: %q", prefs.BaseURL)
	}
	if prefs.AllowHistory || prefs.AllowSampleRows || prefs.AllowErrorLogs {
		t.Fatalf("egress must default off: %+v", prefs)
	}

	prefs.Model = "qwen2.5-coder:7b"
	prefs.APIKey = "sk-secret"
	prefs.AllowHistory = true
	if err := SaveAIPrefs(path, vault, prefs); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-secret") {
		t.Fatalf("API key must be encrypted on disk: %s", data)
	}
	got, warn := LoadAIPrefs(path, vault)
	if warn != "" {
		t.Fatal(warn)
	}
	if got.APIKey != "sk-secret" || got.Model != prefs.Model || !got.AllowHistory {
		t.Fatalf("round trip: %+v", got)
	}
	if got.APIKeyEnc == "" {
		t.Fatal("encrypted key must be kept")
	}
}

func TestAIConversationStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := AIConversationPath(dir, "ws-1")
	store := AIConversationStore{
		ActiveID: "second",
		Conversations: []AIConversation{
			{ID: "first", Messages: []AIMessageRecord{{Role: "user", Text: "第一条", Done: true}}},
			{ID: "second", Messages: []AIMessageRecord{
				{Role: "user", Text: "hi", Done: true},
				{Role: "assistant", Text: "SELECT 1;", Done: true},
			}},
		},
	}
	if err := SaveAIConversations(path, store); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadAIConversations(path)
	if warn != "" {
		t.Fatal(warn)
	}
	if got.Version != 2 || got.ActiveID != "second" || len(got.Conversations) != 2 {
		t.Fatalf("store: %+v", got)
	}
	if got.Conversations[0].Title != "第一条" {
		t.Fatalf("title: %q", got.Conversations[0].Title)
	}
}

func TestLoadAIConversationsMigratesLegacy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.json")
	legacy := `{"version":1,"messages":[{"role":"user","text":"old chat","done":true}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadAIConversations(path)
	if warn != "" {
		t.Fatal(warn)
	}
	if len(got.Conversations) != 1 || got.Conversations[0].ID != "default" ||
		got.Conversations[0].Title != "old chat" {
		t.Fatalf("migrated: %+v", got)
	}
}
