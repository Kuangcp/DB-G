package app

import (
	"path/filepath"
	"testing"
)

func TestConfigDirSplitsDevAndRelease(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	old := releaseBuild
	t.Cleanup(func() { releaseBuild = old })

	releaseBuild = ""
	if got := AppConfigDir(); got != filepath.Join(root, "db-g.dev") {
		t.Fatalf("开发构建目录 = %q", got)
	}
	if got := DefaultStorePath(); got != filepath.Join(root, "db-g.dev", "connections.json") {
		t.Fatalf("开发构建连接库 = %q", got)
	}

	releaseBuild = "1"
	if got := AppConfigDir(); got != filepath.Join(root, "db-g") {
		t.Fatalf("发布构建目录 = %q", got)
	}
	if got := DefaultLogDir(); got != filepath.Join(root, "db-g", "logs") {
		t.Fatalf("发布构建日志目录 = %q", got)
	}
}

func TestVersionDropsVPrefix(t *testing.T) {
	old := buildVersion
	t.Cleanup(func() { buildVersion = old })

	for _, c := range []struct{ in, want string }{
		{"v1.0.0-4-g7e186e8", "1.0.0-4-g7e186e8"},
		{"1.0.0-4-g7e186e8", "1.0.0-4-g7e186e8"},
		{"v1.0.0-4-g7e186e8-dirty", "1.0.0-4-g7e186e8-dirty"},
	} {
		buildVersion = c.in
		if got := Version(); got != c.want {
			t.Errorf("Version() with %q = %q, want %q", c.in, got, c.want)
		}
	}
}
