package ui

import (
	"errors"
	"path/filepath"
	"testing"

	"db-g/locales"
)

func TestAISettingsFetchesModelList(t *testing.T) {
	a := newConsoleRoot(t)
	a.ai.TestProvider = &fakeAIProvider{models: []string{"qwen2.5-coder:7b", "deepseek-chat"}}
	a.ai.Prefs.BaseURL = "http://127.0.0.1:11434/v1"
	a.ai.Prefs.APIKey = ""
	tt := openSettingsSection(t, a, "AI")
	tt.Frame()
	if len(a.ai.Models) != 2 {
		t.Fatalf("models = %v", a.ai.Models)
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
	a.ai.TestProvider = &fakeAIProvider{modelErr: errors.New("404 no models")}
	a.ai.Prefs.BaseURL = "https://api.example.com/v1"
	a.ai.Prefs.APIKey = "sk-test"
	tt := openSettingsSection(t, a, "AI")
	tt.Frame()
	if a.ai.ModelsErr == "" {
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
	a.ai.TestProvider = &fakeAIProvider{models: []string{"remote-model"}}
	a.ai.Prefs.BaseURL = "https://api.example.com/v1"
	a.ai.Prefs.APIKey = ""
	tt := openSettingsSection(t, a, "AI")
	tt.Frame()
	if len(a.ai.Models) != 0 {
		t.Fatalf("remote without key must not auto fetch: %v", a.ai.Models)
	}
	if err := tt.Click("获取模型列表"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.ai.Models) != 1 || a.ai.Models[0] != "remote-model" {
		t.Fatalf("models after refresh = %v", a.ai.Models)
	}
}

// AI API Key 输入框右侧的眼睛可以切换明文显示。
func TestAISettingsAPIKeyEye(t *testing.T) {
	a := newConsoleRoot(t)
	a.ai.PrefsPath = filepath.Join(t.TempDir(), "ai.json")
	tt := openSettingsSection(t, a, "AI")
	if a.ai.ShowKey {
		t.Fatal("API Key 默认应隐藏")
	}
	if err := tt.Click(locales.ConnFormShowPassword()); err != nil {
		t.Fatalf("API Key 输入框右侧应有显示密码按钮: %v", err)
	}
	tt.Frame()
	if !a.ai.ShowKey {
		t.Fatal("点击眼睛后应显示明文")
	}
	if err := tt.Click(locales.ConnFormHidePassword()); err != nil {
		t.Fatalf("显示后按钮应变为隐藏密码: %v", err)
	}
	tt.Frame()
	if a.ai.ShowKey {
		t.Fatal("再次点击后应恢复隐藏")
	}
}
