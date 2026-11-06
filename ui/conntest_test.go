package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// openConnModal 打开新建连接弹窗，返回根状态和 tester。
func openConnModal(t *testing.T) (*root, *ui.Tester) {
	t.Helper()
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	return a, tt
}

func TestConnFormSummarySQLite(t *testing.T) {
	got := connFormSummary(app.ConnProfile{Name: "本地", Kind: "sqlite", Path: "/tmp/app.db"})
	want := "名称: 本地\n类型: SQLite\n文件: /tmp/app.db"
	if got != want {
		t.Fatalf("summary:\n%q\nwant:\n%q", got, want)
	}
}

func TestConnFormSummaryNoPassword(t *testing.T) {
	a, _ := openConnModal(t)
	a.connFormKind = "mysql"
	a.connFormName = "远程库"
	a.connFormHost = "10.0.0.1"
	a.connFormPort = "3306"
	a.connFormUser = "root"
	a.connFormPass = "secret"
	a.connFormDB = "app"
	p, err := a.connFormProfile()
	if err != nil {
		t.Fatal(err)
	}
	got := connFormSummary(p)
	want := "名称: 远程库\n类型: MySQL\n主机: 10.0.0.1\n端口: 3306\n数据库: app\n用户: root"
	if got != want {
		t.Fatalf("summary:\n%q\nwant:\n%q", got, want)
	}
	if strings.Contains(got, "secret") {
		t.Fatal("summary must not include the password")
	}
}

func TestConnFormTestConnectionOK(t *testing.T) {
	a, tt := openConnModal(t)
	a.connFormKind, a.connFormPath = "sqlite", seedConnFile(t)
	tt.Frame()
	if err := tt.Click("测试连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connTestErr || !strings.HasPrefix(a.connTestMsg, "连接成功") {
		t.Fatalf("test connection: %q (err=%v)", a.connTestMsg, a.connTestErr)
	}
	if !tt.HasText("连接成功") {
		t.Fatalf("结果没显示在弹窗里: %q", tt.Texts())
	}
}

func TestConnFormTestConnectionFails(t *testing.T) {
	a, tt := openConnModal(t)
	a.connFormKind, a.connFormPath = "sqlite", filepath.Join(t.TempDir(), "missing.db")
	tt.Frame()
	if err := tt.Click("测试连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.connTestErr || !strings.HasPrefix(a.connTestMsg, "失败：") {
		t.Fatalf("test connection should fail: %q (err=%v)", a.connTestMsg, a.connTestErr)
	}
	if !tt.HasText("失败：") {
		t.Fatalf("失败原因没显示: %q", tt.Texts())
	}
}

func TestConnFormCopyToClipboard(t *testing.T) {
	a, tt := openConnModal(t)
	a.connFormKind, a.connFormPath = "sqlite", "/tmp/app.db"
	a.connFormName = "本地"
	tt.Frame()
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); !strings.Contains(got, "名称: 本地") || !strings.Contains(got, "文件: /tmp/app.db") {
		t.Fatalf("clipboard: %q", got)
	}
	if a.connTestErr || !strings.Contains(a.connTestMsg, "已复制") {
		t.Fatalf("copy feedback: %q", a.connTestMsg)
	}
}

// 弹窗里四个按钮要能塞进右侧表单列，不挤出去。
func TestConnFormButtonsFit(t *testing.T) {
	_, tt := openConnModal(t)
	for _, name := range []string{"测试连接", "复制", "取消", "保存"} {
		if _, ok := tt.Find(name); !ok {
			t.Fatalf("button %q missing from the form", name)
		}
	}
	save, _ := tt.Find("保存")
	test, _ := tt.Find("测试连接")
	if test.X >= save.X {
		t.Fatalf("测试连接 must sit left of 保存: test=%v save=%v", test, save)
	}
}
