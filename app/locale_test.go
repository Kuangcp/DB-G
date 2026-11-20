package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/i18n"
)

func TestLocalePrefsDefaults(t *testing.T) {
	if got := DefaultLocalePrefs(); got.Locale != "" {
		t.Fatalf("default = %q, want follow system", got.Locale)
	}
	p, _ := NormalizeLocalePrefs(LocalizationPrefs{})
	if p.Locale != "" {
		t.Fatalf("normalize empty = %q", p.Locale)
	}
}

func TestNormalizeLocalePrefs(t *testing.T) {
	cases := map[string]string{
		"":           "",
		" en ":       "en",
		"en-US":      "en",
		"zh":         "zh-Hans",
		"zh-Hans":    "zh-Hans",
		"zh-Hans-CN": "zh-Hans",
		"ZH-HANS":    "zh-Hans",
	}
	for in, want := range cases {
		p, warn := NormalizeLocalePrefs(LocalizationPrefs{Locale: in})
		if p.Locale != want || warn != "" {
			t.Fatalf("%q -> %q warn=%q, want %q", in, p.Locale, warn, want)
		}
	}
	p, warn := NormalizeLocalePrefs(LocalizationPrefs{Locale: "de"})
	if p.Locale != "" || warn == "" {
		t.Fatalf("unknown -> %q warn=%q, want follow system + warning", p.Locale, warn)
	}
}

func TestLocalePrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locale.json")
	if err := SaveLocalePrefs(path, LocalizationPrefs{Locale: "en"}); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadLocalePrefs(path)
	if warn != "" || got.Locale != "en" {
		t.Fatalf("load = %+v warn=%q", got, warn)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestLoadLocalePrefsMissingAndBroken(t *testing.T) {
	p, warn := LoadLocalePrefs(filepath.Join(t.TempDir(), "none.json"))
	if p.Locale != "" || warn != "" {
		t.Fatalf("missing = %+v warn=%q", p, warn)
	}
	path := filepath.Join(t.TempDir(), "locale.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, warn = LoadLocalePrefs(path)
	if p.Locale != "" || warn == "" {
		t.Fatalf("broken = %+v warn=%q", p, warn)
	}
}

func TestAppErrorsFollowLanguage(t *testing.T) {
	prevLang := "zh-Hans"
	i18n.SetLocaleFunc(func() string { return prevLang })
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return prevLang }) })

	if got := validateProfile(ConnProfile{Name: ""}).Error(); got != "连接名称不能为空" {
		t.Fatalf("zh error = %q", got)
	}
	i18n.SetLocaleFunc(func() string { return "en" })
	if got := validateProfile(ConnProfile{Name: ""}).Error(); got != "Connection name cannot be empty" {
		t.Fatalf("en error = %q", got)
	}
}
