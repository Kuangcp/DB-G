package ui

import (
	"path/filepath"
	"testing"
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
