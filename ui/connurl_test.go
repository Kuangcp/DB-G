package ui

import (
	"testing"

	"db-g/locales"
)

// URL 导入：解析后切换类型、填端口/协议/参数，名称空缺时用库名。
func TestConnFormURLImportMySQL(t *testing.T) {
	a, tt := openConnModal(t)
	tt.SetClipboard("jdbc:mysql://root:pw@db.local:3307/app?useSSL=false")
	if err := tt.Click(locales.ConnFormURLParseClipboard()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connForm.Kind != "mysql" || a.connForm.Host != "db.local" || a.connForm.Port != "3307" ||
		a.connForm.User != "root" || a.connForm.Pass != "pw" || a.connForm.DB != "app" ||
		a.connForm.Params != "useSSL=false" {
		t.Fatalf("导入字段: kind=%q host=%q port=%q user=%q pass=%q db=%q params=%q",
			a.connForm.Kind, a.connForm.Host, a.connForm.Port, a.connForm.User, a.connForm.Pass, a.connForm.DB, a.connForm.Params)
	}
	if a.connForm.Name != "app" {
		t.Fatalf("空名称应建议为库名: %q", a.connForm.Name)
	}
	if a.connForm.URL != "" {
		t.Fatalf("导入后应清空含密码的 URL: %q", a.connForm.URL)
	}
}

func TestConnFormURLImportClickHouse(t *testing.T) {
	a, tt := openConnModal(t)
	tt.SetClipboard("jdbc:clickhouse:https://ch.example/analytics?compress=0")
	if err := tt.Click(locales.ConnFormURLParseClipboard()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connForm.Kind != "clickhouse" || a.connProto != 2 || a.connForm.Port != "8443" ||
		a.connForm.DB != "analytics" || a.connForm.Params != "compress=0" {
		t.Fatalf("ClickHouse 导入: kind=%q proto=%d port=%q db=%q params=%q",
			a.connForm.Kind, a.connProto, a.connForm.Port, a.connForm.DB, a.connForm.Params)
	}
}

func TestConnFormURLImportSQLite(t *testing.T) {
	a, tt := openConnModal(t)
	tt.SetClipboard("jdbc:sqlite:/tmp/app.db")
	if err := tt.Click(locales.ConnFormURLParseClipboard()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connForm.Kind != "sqlite" || a.connForm.Path != "/tmp/app.db" ||
		a.connForm.Host != "" || a.connForm.Port != "" {
		t.Fatalf("SQLite 导入: kind=%q path=%q host=%q port=%q",
			a.connForm.Kind, a.connForm.Path, a.connForm.Host, a.connForm.Port)
	}
}

// 解析失败只提示，不覆盖已经填好的字段。
func TestConnFormURLImportFailureKeepsFields(t *testing.T) {
	a, tt := openConnModal(t)
	a.connForm.Kind, a.connForm.Host = "postgres", "keep.example"
	// 直接改表单字段后先重排一帧：按钮在底栏，点击坐标取自上一帧布局。
	tt.Frame()
	tt.SetClipboard("jdbc:oracle:thin:@host:1521:xe")
	if err := tt.Click(locales.ConnFormURLParseClipboard()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connForm.Kind != "postgres" || a.connForm.Host != "keep.example" {
		t.Fatalf("失败不应改表单: kind=%q host=%q", a.connForm.Kind, a.connForm.Host)
	}
	if a.connForm.URLErr == "" {
		t.Fatal("应显示导入错误")
	}
}

// 解析按钮在底栏、紧挨着「复制」右侧，不再占据表单顶部的大按钮位置。
func TestConnFormURLButtonBesideCopy(t *testing.T) {
	_, tt := openConnModal(t)
	copyRect, ok := tt.Find(locales.ConnFormCopy())
	if !ok {
		t.Fatalf("找不到「复制」按钮: %v", tt.Texts())
	}
	urlRect, ok := tt.Find(locales.ConnFormURLParseClipboard())
	if !ok {
		t.Fatalf("找不到「解析剪贴板连接URL」按钮: %v", tt.Texts())
	}
	if dy := urlRect.Y - copyRect.Y; dy < -2 || dy > 2 {
		t.Fatalf("解析按钮应与「复制」同一行: copy.y=%v url.y=%v", copyRect.Y, urlRect.Y)
	}
	if urlRect.X < copyRect.X+copyRect.W {
		t.Fatalf("解析按钮应在「复制」右侧: copy=[%v,%v] url.x=%v",
			copyRect.X, copyRect.X+copyRect.W, urlRect.X)
	}
}
