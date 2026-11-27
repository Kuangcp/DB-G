package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
)

// TestMain 固定 headless 测试的界面语言为简体中文，避免测试机 locale 影响断言。
// 需要跑英文用例时用 MYGO_LOCALE=en go test ./ui -run TestI18nEnglish。
func TestMain(m *testing.M) {
	if os.Getenv("MYGO_LOCALE") == "" {
		_ = os.Setenv("MYGO_LOCALE", "zh-Hans")
	}
	os.Exit(m.Run())
}

func TestI18nCatalogChinese(t *testing.T) {
	if got := locales.In("zh-Hans").SettingsAppearance(); got != "外观" {
		t.Fatalf("SettingsAppearance = %q, want 外观", got)
	}
	if got := locales.In("en").SettingsAppearance(); got != "Appearance" {
		t.Fatalf("en SettingsAppearance = %q", got)
	}
}

func TestLanguageSectionSwitchesAndPersists(t *testing.T) {
	a := newConsoleRoot(t)
	a.settings.LocalePrefsPath = filepath.Join(t.TempDir(), "locale.json")
	tt := openSettingsSection(t, a, "语言")
	if !tt.HasText("跟随系统") || !tt.HasText("English") {
		t.Fatalf("language section missing options: %q", tt.Texts())
	}
	if err := tt.Click("English"); err != nil {
		t.Fatalf("click English: %v", err)
	}
	if a.settings.LocalePref != app.LocaleEn {
		t.Fatalf("localePref = %q, want en", a.settings.LocalePref)
	}
	prefs, warn := app.LoadLocalePrefs(a.settings.LocalePrefsPath)
	if warn != "" || prefs.Locale != app.LocaleEn {
		t.Fatalf("persisted = %+v warn=%q", prefs, warn)
	}
	// 切回跟随系统
	tt.Frame()
	if err := tt.Click("跟随系统"); err != nil {
		t.Fatalf("click system: %v", err)
	}
	prefs, _ = app.LoadLocalePrefs(a.settings.LocalePrefsPath)
	if prefs.Locale != app.LocaleSystem {
		t.Fatalf("persisted = %q, want follow system", prefs.Locale)
	}
}

func TestLanguageSectionRendersEnglish(t *testing.T) {
	if os.Getenv("MYGO_LOCALE") != "en" {
		t.Skip("run with MYGO_LOCALE=en")
	}
	a := newConsoleRoot(t)
	tt := ui.NewTester(a.settingsView, 720, 520)
	tt.Frame()
	for _, want := range []string{"Settings", "Language", "Appearance"} {
		if !tt.HasText(want) {
			t.Fatalf("english settings missing %q: %q", want, tt.Texts())
		}
	}
}

func TestI18nEnglishMainWindow(t *testing.T) {
	if os.Getenv("MYGO_LOCALE") != "en" {
		t.Skip("run with MYGO_LOCALE=en")
	}
	a := newConsoleRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	for _, want := range []string{"New Connection", "Run (Ctrl+Enter)", "No datasource connected"} {
		if !tt.HasText(want) {
			t.Fatalf("english main window missing %q: %q", want, tt.Texts())
		}
	}
}

func TestI18nEnglishAIPanelAndSync(t *testing.T) {
	if os.Getenv("MYGO_LOCALE") != "en" {
		t.Skip("run with MYGO_LOCALE=en")
	}
	a := newConsoleRoot(t)
	a.ai.Chat.open = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	for _, want := range []string{"AI Panel", "Conversation History", "Send"} {
		if !tt.HasText(want) {
			t.Fatalf("english AI panel missing %q: %q", want, tt.Texts())
		}
	}
	idx := -1
	for i, sec := range a.settingsSections() {
		if sec.label == "Sync" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("sync settings section missing")
	}
	a.settings.Section = idx
	st := ui.NewTester(a.settingsView, 760, 600)
	st.Frame()
	for _, want := range []string{"Git Sync", "Sync Key", "Repository URL"} {
		if !st.HasText(want) {
			t.Fatalf("english sync settings missing %q: %q", want, st.Texts())
		}
	}
}
