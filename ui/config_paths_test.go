package ui

import (
	"os"
	"path/filepath"
	"testing"

	"db-g/app"
)

// newRootWithStore 的所有偏好/元数据/历史都必须从连接库所在目录派生，
// 测试传临时目录时不能读到开发机的真实配置。
func TestNewRootWithStoreDerivesAllPaths(t *testing.T) {
	dir := t.TempDir()
	a := newRootWithStore(filepath.Join(dir, "connections.json"))
	want := func(name string) string { return filepath.Join(dir, name) }
	cases := map[string]string{
		"keymap":  a.settings.KeymapPath,
		"fonts":   a.settings.FontPrefsPath,
		"results": a.settings.ResultPrefsPath,
		"console": a.settings.ConsolePrefsPath,
		"theme":   a.settings.ThemePath,
		"locale":  a.settings.LocalePrefsPath,
		"ai":      a.ai.PrefsPath,
		"sync":    a.gitSync.PrefsPath,
	}
	for name, got := range cases {
		if got != want(name+".json") {
			t.Fatalf("%s path = %q, want %q", name, got, want(name+".json"))
		}
	}
}

// 即使开发机真实配置目录里有 fonts/theme/keymap/history，测试用的 root 也不能读或写它们。
func TestNewRootWithStoreIgnoresUserConfig(t *testing.T) {
	user := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	cfg := filepath.Join(user, app.ConfigDirName())
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "fonts.json"), []byte(`{"editor":{"size":22}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "theme.json"), []byte(`{"mode":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "keymap.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newRootWithStore(filepath.Join(t.TempDir(), "connections.json"))
	if got := a.settings.FontPrefs.Editor.Size; got == 22 {
		t.Fatal("read the real user's fonts.json")
	}
	if a.settings.ThemePrefs.Mode == app.ThemeModeDark {
		t.Fatal("read the real user's theme.json")
	}
	if _, ok := a.settingsNoticeOf("keymap"); ok {
		t.Fatal("read the real user's keymap.json")
	}
	if _, err := os.Stat(filepath.Join(cfg, "history")); !os.IsNotExist(err) {
		t.Fatalf("created history in the real user config: %v", err)
	}
}
