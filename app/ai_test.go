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

func TestAIConversationRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := AIConversationPath(dir, "ws-1")
	conv := AIConversation{Messages: []AIMessageRecord{
		{Role: "user", Text: "hi", Done: true},
		{Role: "assistant", Text: "```sql\nSELECT 1;\n```", Done: true},
	}}
	if err := SaveAIConversation(path, conv); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadAIConversation(path)
	if warn != "" {
		t.Fatal(warn)
	}
	if len(got.Messages) != 2 || got.Messages[1].Text != "```sql\nSELECT 1;\n```" {
		t.Fatalf("conversation: %+v", got)
	}
	if got.Version != 1 {
		t.Fatalf("version: %d", got.Version)
	}
}
