package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThemePrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theme.json")
	want := ThemePrefs{Mode: ThemeModeDark, Accent: "#7c3aed", Density: ThemeDensityCompact, Radius: 8, FontSize: 15}
	if err := SaveThemePrefs(path, want); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadThemePrefs(path)
	if warn != "" {
		t.Fatalf("warn: %s", warn)
	}
	if got != want {
		t.Fatalf("round trip: %+v want %+v", got, want)
	}
}

func TestThemePrefsNormalize(t *testing.T) {
	// 从默认值出发的 prefs 原样通过。
	got, warn := DefaultThemePrefs().Normalize()
	if len(warn) != 0 || got != DefaultThemePrefs() {
		t.Fatalf("default prefs: %+v warn=%v", got, warn)
	}
	// 非法值逐项回退并给出说明。
	bad := ThemePrefs{Mode: "purple", Accent: "#12", Density: "huge", Radius: 99, FontSize: 30}
	got, warn = bad.Normalize()
	if len(warn) != 5 {
		t.Fatalf("warnings: %v", warn)
	}
	wantDefault := DefaultThemePrefs()
	wantDefault.Radius = DefaultThemeRadius
	if got != wantDefault {
		t.Fatalf("invalid prefs should fall back: %+v", got)
	}
	// "default" 强调色等价于空（主题/桌面强调色）。
	got, _ = ThemePrefs{Accent: "default"}.Normalize()
	if got.Accent != "" {
		t.Fatalf("accent default: %+v", got)
	}
	// 圆角 0 是合法值（直角），不能被当成缺省。
	got, _ = ThemePrefs{Radius: 0}.Normalize()
	if got.Radius != 0 {
		t.Fatalf("radius 0 must stay sharp: %+v", got)
	}
}

func TestThemePrefsLoadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theme.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadThemePrefs(path)
	if warn == "" || got != DefaultThemePrefs() {
		t.Fatalf("corrupt file: %+v warn=%q", got, warn)
	}
	// 合法 JSON 但字段非法：修正并提示，不整体丢弃。
	if err := os.WriteFile(path, []byte(`{"mode":"light","radius":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warn = LoadThemePrefs(path)
	if got.Mode != ThemeModeLight || got.Radius != DefaultThemeRadius || warn == "" {
		t.Fatalf("partial fix: %+v warn=%q", got, warn)
	}
}

func TestThemePrefsExportImport(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "export.json")
	want := ThemePrefs{Mode: ThemeModeLight, Accent: "violet", Density: ThemeDensityRoomy, Radius: 3, FontSize: 17}
	if err := ExportThemePrefs(src, want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"app": "db-g"`) || !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("export file: %s", data)
	}
	got, warn, err := ImportThemePrefs(src)
	if err != nil || len(warn) != 0 || got != want {
		t.Fatalf("import: %+v warn=%v err=%v", got, warn, err)
	}
}

func TestThemePrefsImportRejectsOtherAppOrVersion(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "other.json")
	if err := os.WriteFile(other, []byte(`{"app":"other-app","version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ImportThemePrefs(other); err == nil {
		t.Fatal("other app must be rejected")
	}
	newer := filepath.Join(dir, "newer.json")
	if err := os.WriteFile(newer, []byte(`{"app":"db-g","version":99,"mode":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ImportThemePrefs(newer); err == nil {
		t.Fatal("newer version must be rejected")
	}
	// 没有 app/version 的裸主题也接受（便于手写）。
	bare := filepath.Join(dir, "bare.json")
	if err := os.WriteFile(bare, []byte(`{"mode":"dark","accent":"#112233"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warn, err := ImportThemePrefs(bare)
	if err != nil || len(warn) != 0 || got.Mode != ThemeModeDark || got.Accent != "#112233" {
		t.Fatalf("bare import: %+v warn=%v err=%v", got, warn, err)
	}
}
