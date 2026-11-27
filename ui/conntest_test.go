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

func TestConnFormSummaryIncludesPassword(t *testing.T) {
	a, _ := openConnModal(t)
	a.connForm.Kind = "mysql"
	a.connForm.Name = "远程库"
	a.connForm.Host = "10.0.0.1"
	a.connForm.Port = "3306"
	a.connForm.User = "root"
	a.connForm.Pass = "secret"
	a.connForm.DB = "app"
	p, err := a.connFormProfile()
	if err != nil {
		t.Fatal(err)
	}
	got := connFormSummary(p)
	want := "名称: 远程库\n类型: MySQL\n主机: 10.0.0.1\n端口: 3306\n数据库: app\n用户: root\n密码: secret"
	if got != want {
		t.Fatalf("summary:\n%q\nwant:\n%q", got, want)
	}
}

func TestConnFormTestConnectionOK(t *testing.T) {
	a, tt := openConnModal(t)
	a.connForm.Kind, a.connForm.Path = "sqlite", seedConnFile(t)
	tt.Frame()
	if err := tt.Click("测试连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connForm.TestErr || !strings.HasPrefix(a.connForm.TestMsg, "连接成功") {
		t.Fatalf("test connection: %q (err=%v)", a.connForm.TestMsg, a.connForm.TestErr)
	}
	if !tt.HasText("连接成功") {
		t.Fatalf("结果没显示在弹窗里: %q", tt.Texts())
	}
}

func TestConnFormTestConnectionFails(t *testing.T) {
	a, tt := openConnModal(t)
	a.connForm.Kind, a.connForm.Path = "sqlite", filepath.Join(t.TempDir(), "missing.db")
	tt.Frame()
	if err := tt.Click("测试连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.connForm.TestErr || !strings.HasPrefix(a.connForm.TestMsg, "失败：") {
		t.Fatalf("test connection should fail: %q (err=%v)", a.connForm.TestMsg, a.connForm.TestErr)
	}
	if !tt.HasText("失败：") {
		t.Fatalf("失败原因没显示: %q", tt.Texts())
	}
}

func TestConnFormCopyToClipboard(t *testing.T) {
	a, tt := openConnModal(t)
	a.connForm.Kind, a.connForm.Path = "sqlite", "/tmp/app.db"
	a.connForm.Name = "本地"
	tt.Frame()
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); !strings.Contains(got, "名称: 本地") || !strings.Contains(got, "文件: /tmp/app.db") {
		t.Fatalf("clipboard: %q", got)
	}
	if a.connForm.TestErr || !strings.Contains(a.connForm.TestMsg, "已复制") {
		t.Fatalf("copy feedback: %q", a.connForm.TestMsg)
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
	// 四个按钮必须在同一行（历史 bug：外层 Row 丢了，按钮竖排）。
	for _, name := range []string{"复制", "取消"} {
		r, _ := tt.Find(name)
		if r.Y != save.Y {
			t.Fatalf("%s 与 保存 不在同一行: %v vs %v", name, r, save)
		}
	}
}

func TestConnFormCopyIncludesPassword(t *testing.T) {
	a, tt := openConnModal(t)
	a.connForm.Kind = "mysql"
	a.connForm.Name = "远程库"
	a.connForm.Host = "10.0.0.1"
	a.connForm.Port = "3306"
	a.connForm.User = "root"
	a.connForm.Pass = "secret"
	a.connForm.DB = "app"
	tt.Frame()
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); !strings.Contains(got, "密码: secret") {
		t.Fatalf("clipboard must include plaintext password: %q", got)
	}
	if !strings.Contains(a.connForm.TestMsg, "含明文密码") {
		t.Fatalf("copy feedback: %q", a.connForm.TestMsg)
	}
}

// 复制数据源走数据源右键菜单，每次只新增一条，重复操作名称也要各自唯一
// （历史 bug：死循环落盘几千条）。
func TestDuplicateConnAddsExactlyOneCopy(t *testing.T) {
	a := newSidebarRoot(t)
	src := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(src); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.RightClick("本地"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("复制数据源"); err != nil {
		t.Fatal(err)
	}
	if got := len(a.store.List()); got != 2 {
		t.Fatalf("复制一次应新增 1 条，得到 %d 条", got)
	}
	// 再复制一次：不得撞名，也不得再多出别的记录
	tt.Frame()
	if err := tt.RightClick("本地"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("复制数据源"); err != nil {
		t.Fatal(err)
	}
	list := a.store.List()
	if len(list) != 3 {
		t.Fatalf("复制两次应共 3 条，得到 %d 条", len(list))
	}
	seen := map[string]bool{}
	for _, p := range list {
		if seen[p.Name] {
			t.Fatalf("连接名称重复: %q", p.Name)
		}
		seen[p.Name] = true
		if p.Kind != "sqlite" || p.Path != src.Path {
			t.Fatalf("副本应继承原数据源字段: %+v", p)
		}
	}
	if len(a.toasts.Queue) != 0 {
		t.Fatalf("复制不应报错: %v", a.toasts.Queue)
	}
	var copies int
	for _, p := range list {
		if strings.HasPrefix(p.Name, "副本：") || strings.Contains(p.Name, "(副本") {
			copies++
		}
	}
	if copies != 2 {
		t.Fatalf("应有两个副本，得到 %d", copies)
	}
}

// 数据源行右键菜单：低频操作（复制/删除）不占行内图标，删除仍需二次确认。
func TestConnRowContextMenu(t *testing.T) {
	a := newSidebarRoot(t)
	src := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(src); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.RightClick("本地"); err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, item := range tt.Menu() {
		has[item] = true
	}
	if !has["复制数据源"] || !has["删除连接…"] {
		t.Fatalf("右键菜单缺少条目: %v", tt.Menu())
	}
	if err := tt.ChooseMenuItem("删除连接…"); err != nil {
		t.Fatal(err)
	}
	if !a.connForm.DeleteOpen || a.connForm.Deleting != a.store.List()[0].ID {
		t.Fatalf("删除应打开确认弹窗: open=%v id=%q", a.connForm.DeleteOpen, a.connForm.Deleting)
	}
	if len(a.store.List()) != 1 {
		t.Fatal("确认前不得删除连接")
	}
}

// 测试连接进行中按钮变成「取消测试」，点了能自己取消。
func TestCancelConnTest(t *testing.T) {
	a, tt := openConnModal(t)
	a.connForm.TestBusy, a.connForm.TestMsg = true, "测试中…"
	tt.Frame()
	if !tt.HasText("取消测试") {
		t.Fatalf("busy 时应显示取消按钮: %v", tt.Texts())
	}
	if err := tt.Click("取消测试"); err != nil {
		t.Fatal(err)
	}
	if a.connForm.TestBusy {
		t.Fatal("取消后不能再处于测试中")
	}
	if a.connForm.TestMsg != "已取消测试" {
		t.Fatalf("取消反馈: %q", a.connForm.TestMsg)
	}
}

// 连接弹窗的查询超时：解析、校验、落盘与执行侧换算。
func TestConnFormQueryTimeout(t *testing.T) {
	a, tt := openConnModal(t)
	a.connForm.Timeout = "120"
	p, err := a.connFormProfile()
	if err != nil || p.QueryTimeout != 120 {
		t.Fatalf("profile: %+v err=%v", p, err)
	}
	if !strings.Contains(connFormSummary(p), "查询超时: 120 秒") {
		t.Fatalf("summary: %q", connFormSummary(p))
	}
	a.connForm.Timeout = "abc"
	if _, err := a.connFormProfile(); err == nil {
		t.Fatal("non-numeric timeout must fail")
	}
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if a.connForm.Err == "" || !a.connForm.Open {
		t.Fatalf("invalid timeout must keep the form open with an error: %q", a.connForm.Err)
	}
	a.connForm.Timeout = "3600"
	a.connForm.Kind, a.connForm.Path = "sqlite", seedConnFile(t)
	a.connForm.Name = "本地"
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if a.connForm.Open {
		t.Fatalf("form should close: %q", a.connForm.Err)
	}
	stored := a.store.List()[0]
	if stored.QueryTimeout != 3600 {
		t.Fatalf("stored timeout: %+v", stored)
	}
	a.store.SetActive(stored.ID)
	a.touchSession(stored.ID) // 执行时一定有会话；这里模拟「刚用过该数据源」
	if got := a.queryTimeoutFor(); got.Seconds() != 3600 {
		t.Fatalf("queryTimeoutFor: %v", got)
	}
}
