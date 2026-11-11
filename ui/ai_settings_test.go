package ui

import (
	"errors"
	"testing"
)

func TestAISettingsFetchesModelList(t *testing.T) {
	a := newConsoleRoot(t)
	a.aiTestProvider = &fakeAIProvider{models: []string{"qwen2.5-coder:7b", "deepseek-chat"}}
	a.aiPrefs.BaseURL = "http://127.0.0.1:11434/v1"
	a.aiPrefs.APIKey = ""
	tt := openSettingsSection(t, a, "AI")
	tt.Frame()
	if len(a.aiModels) != 2 {
		t.Fatalf("models = %v", a.aiModels)
	}
	if !tt.HasText("可直接选择") {
		t.Fatalf("model list hint missing: %q", tt.Texts())
	}
	if _, ok := tt.Find("AI 模型"); !ok {
		t.Fatalf("model combobox missing: %q", tt.Texts())
	}
}

func TestAISettingsModelFetchFailureFallsBackToInput(t *testing.T) {
	a := newConsoleRoot(t)
	a.aiTestProvider = &fakeAIProvider{modelErr: errors.New("404 no models")}
	a.aiPrefs.BaseURL = "https://api.example.com/v1"
	a.aiPrefs.APIKey = "sk-test"
	tt := openSettingsSection(t, a, "AI")
	tt.Frame()
	if a.aiModelsErr == "" {
		t.Fatal("model fetch error must be recorded")
	}
	if !tt.HasText("获取失败，可手动输入") {
		t.Fatalf("fallback hint missing: %q", tt.Texts())
	}
	if _, ok := tt.Find("AI 模型"); !ok {
		t.Fatalf("manual model input missing: %q", tt.Texts())
	}
}

func TestAISettingsManualRefreshModels(t *testing.T) {
	a := newConsoleRoot(t)
	a.aiTestProvider = &fakeAIProvider{models: []string{"remote-model"}}
	a.aiPrefs.BaseURL = "https://api.example.com/v1"
	a.aiPrefs.APIKey = ""
	tt := openSettingsSection(t, a, "AI")
	tt.Frame()
	if len(a.aiModels) != 0 {
		t.Fatalf("remote without key must not auto fetch: %v", a.aiModels)
	}
	if err := tt.Click("获取模型列表"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.aiModels) != 1 || a.aiModels[0] != "remote-model" {
		t.Fatalf("models after refresh = %v", a.aiModels)
	}
}
