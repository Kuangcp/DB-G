package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBuildNumber(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"1.0.0", 10000, true},
		{"v1.1.1", 10101, true},
		{"2.5.8", 20508, true},
		{"v0.0.1", 1, true},
		{"1.0.0-dirty", 10000, true},
		{"1.0.0+abc", 10000, true},
		{"1.0", 0, false},
		{"1.0.100", 0, false},
		{"1.100.0", 0, false},
		{"x1.0.0", 0, false},
		{"", 0, false},
		{"dev-abc1234", 0, false},
	} {
		got, ok := ParseBuildNumber(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseBuildNumber(%q) = %d,%v; want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// installEnv 把 XDG 目录都指到临时目录，并伪造发布版本号。
func installEnv(t *testing.T) (cfg, data string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	old := buildVersion
	buildVersion = "v1.2.3"
	t.Cleanup(func() { buildVersion = old })
	return filepath.Join(root, "config", "db-g"), filepath.Join(root, "data")
}

func TestInstallSelf(t *testing.T) {
	cfg, data := installEnv(t)
	st := CheckInstall()
	if !st.NeedInstall || st.Upgrade {
		t.Fatalf("首次应提示安装: %+v", st)
	}
	if st.BinPath != filepath.Join(cfg, "bin", "db-g") {
		t.Fatalf("bin path: %s", st.BinPath)
	}
	st, err := InstallSelf([]byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	if st.NeedInstall || st.Installed != "1.2.3" || st.InstalledBuild != 10203 {
		t.Fatalf("安装后状态: %+v", st)
	}
	if info, err := os.Stat(st.BinPath); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("bin 不存在或不可执行: %v %v", info, err)
	}
	if got, err := os.ReadFile(st.VersionPath); err != nil || string(got) != "[db-g]\nbuild=10203\nversion=1.2.3\n" {
		t.Fatalf("version 文件: %q %v", got, err)
	}
	desktop, err := os.ReadFile(filepath.Join(data, "applications", "db-g.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(desktop), "Exec=\""+st.BinPath+"\"") || !strings.Contains(string(desktop), "Icon=db-g") {
		t.Fatalf("desktop: %s", desktop)
	}
	if got, err := os.ReadFile(filepath.Join(data, "icons", "hicolor", "256x256", "apps", "db-g.png")); err != nil || string(got) != "PNG" {
		t.Fatalf("图标: %q %v", got, err)
	}
	// 再装一次（升级/重装）不能报错。
	if _, err := InstallSelf([]byte("PNG")); err != nil {
		t.Fatalf("重复安装: %v", err)
	}
}

func TestCheckInstallVersionGates(t *testing.T) {
	cfg, _ := installEnv(t)
	if _, err := InstallSelf(nil); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cfg, "bin", "db-g")

	// 同版本：不提示。
	if st := CheckInstall(); st.NeedInstall {
		t.Fatalf("同版本不应提示: %+v", st)
	}
	// 旧版本启动：不提示（不能把旧包当成升级）。
	buildVersion = "v1.2.2"
	if st := CheckInstall(); st.NeedInstall {
		t.Fatalf("旧版本不应提示: %+v", st)
	}
	// 新版本：提示升级。
	buildVersion = "v1.2.4"
	if st := CheckInstall(); !st.NeedInstall || !st.Upgrade {
		t.Fatalf("新版本应提示升级: %+v", st)
	}
	// version 文件丢失：即使同版本也要提示安装。
	buildVersion = "v1.2.3"
	if err := os.Remove(filepath.Join(cfg, "version")); err != nil {
		t.Fatal(err)
	}
	if st := CheckInstall(); !st.NeedInstall || st.Upgrade {
		t.Fatalf("缺 version 文件应提示安装: %+v", st)
	}
	// bin 丢失：提示安装。
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if st := CheckInstall(); !st.NeedInstall {
		t.Fatalf("缺 bin 应提示安装: %+v", st)
	}
}

func TestVersionFileBadContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "version")
	for _, body := range []string{
		"[db-g]\nversion=1.0.0\n",            // 缺 build
		"[db-g]\nbuild=10000\n",              // 缺 version
		"[db-g]\nbuild=abc\nversion=1.0.0\n", // build 非法
		"",                                   // 空文件
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadVersionFile(path); err == nil {
			t.Fatalf("坏内容应报错: %q", body)
		}
	}
}
