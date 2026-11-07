package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func installRoot(t *testing.T) *root {
	t.Helper()
	a := newConsoleRoot(t)
	a.selfInstall = true
	return a
}

func TestSelfInstallIconOnlyWhenNeeded(t *testing.T) {
	a := installRoot(t)
	a.installNeed = app.InstallStatus{NeedInstall: false}
	tt := ui.NewTester(rootView(a), 1200, 700)
	tt.Frame()
	if _, ok := tt.Find("安装到应用列表"); ok {
		t.Fatalf("已安装且同版本不应显示安装图标: %q", tt.Texts())
	}
	a.installNeed = app.InstallStatus{NeedInstall: true, Running: "v1.2.3"}
	tt.Frame()
	if _, ok := tt.Find("安装到应用列表"); !ok {
		t.Fatalf("需要安装时应显示图标: %q", tt.Texts())
	}
}

func TestSelfInstallConfirmWritesIcon(t *testing.T) {
	a := installRoot(t)
	a.appIconPNG = []byte("PNG")
	a.installNeed = app.InstallStatus{
		NeedInstall: true,
		Running:     "v1.2.3",
		BinPath:     "/home/u/.config/db-g/bin/db-g",
	}
	var gotIcon []byte
	calls := 0
	a.installFn = func(icon []byte) (app.InstallStatus, error) {
		calls++
		gotIcon = icon
		return app.InstallStatus{Running: "v1.2.3", Installed: "1.2.3", BinExists: true}, nil
	}
	tt := ui.NewTester(rootView(a), 1200, 700)
	tt.Frame()
	if err := tt.Click("安装到应用列表"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("/home/u/.config/db-g/bin/db-g") {
		t.Fatalf("弹窗缺少安装路径: %q", tt.Texts())
	}
	if err := tt.Click("安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if calls != 1 || string(gotIcon) != "PNG" {
		t.Fatalf("安装调用: calls=%d icon=%q", calls, gotIcon)
	}
	if a.installOpen || a.installNeed.NeedInstall {
		t.Fatalf("安装成功后应关闭弹窗并隐藏图标: open=%v need=%+v", a.installOpen, a.installNeed)
	}
	if !strings.Contains(a.installNote, "v1.2.3") {
		t.Fatalf("安装成功提示: %q", a.installNote)
	}
	if _, ok := tt.Find("安装到应用列表"); ok {
		t.Fatal("安装成功后顶栏图标应消失")
	}
}

func TestSelfInstallUpgradeLabelAndFailure(t *testing.T) {
	a := installRoot(t)
	a.installNeed = app.InstallStatus{
		NeedInstall: true,
		Upgrade:     true,
		Running:     "v1.2.4",
		Installed:   "1.2.3",
		BinPath:     "/home/u/.config/db-g/bin/db-g",
	}
	a.installFn = func([]byte) (app.InstallStatus, error) { return app.InstallStatus{}, errors.New("磁盘已满") }
	tt := ui.NewTester(rootView(a), 1200, 700)
	tt.Frame()
	if err := tt.Click("升级应用（v1.2.4）"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("已安装版本 1.2.3") {
		t.Fatalf("升级弹窗缺少已安装版本: %q", tt.Texts())
	}
	if err := tt.Click("升级"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.installOpen || a.installErr == "" {
		t.Fatalf("失败后弹窗应保持并显示原因: open=%v err=%q", a.installOpen, a.installErr)
	}
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "磁盘已满") {
		t.Fatalf("失败原因不可见: %q", tt.Texts())
	}
}

// 已安装同版本但图标丢失：顶栏应提示修复，确认后重写菜单项与图标。
func TestSelfInstallRepairLabel(t *testing.T) {
	a := installRoot(t)
	a.installNeed = app.InstallStatus{
		NeedInstall: true,
		Repair:      true,
		Running:     "v1.2.3",
		Installed:   "1.2.3",
		BinPath:     "/home/u/.config/db-g/bin/db-g",
	}
	calls := 0
	a.installFn = func([]byte) (app.InstallStatus, error) {
		calls++
		return app.InstallStatus{Running: "v1.2.3", Installed: "1.2.3", BinExists: true}, nil
	}
	tt := ui.NewTester(rootView(a), 1200, 700)
	tt.Frame()
	if err := tt.Click("修复应用菜单"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("修复"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if calls != 1 || a.installNeed.NeedInstall {
		t.Fatalf("修复流程: calls=%d need=%+v", calls, a.installNeed)
	}
}
