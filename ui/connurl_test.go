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
	if a.connFormKind != "mysql" || a.connFormHost != "db.local" || a.connFormPort != "3307" ||
		a.connFormUser != "root" || a.connFormPass != "pw" || a.connFormDB != "app" ||
		a.connFormParams != "useSSL=false" {
		t.Fatalf("导入字段: kind=%q host=%q port=%q user=%q pass=%q db=%q params=%q",
			a.connFormKind, a.connFormHost, a.connFormPort, a.connFormUser, a.connFormPass, a.connFormDB, a.connFormParams)
	}
	if a.connFormName != "app" {
		t.Fatalf("空名称应建议为库名: %q", a.connFormName)
	}
	if a.connFormURL != "" {
		t.Fatalf("导入后应清空含密码的 URL: %q", a.connFormURL)
	}
}

func TestConnFormURLImportClickHouse(t *testing.T) {
	a, tt := openConnModal(t)
	tt.SetClipboard("jdbc:clickhouse:https://ch.example/analytics?compress=0")
	if err := tt.Click(locales.ConnFormURLParseClipboard()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connFormKind != "clickhouse" || a.connProto != 2 || a.connFormPort != "8443" ||
		a.connFormDB != "analytics" || a.connFormParams != "compress=0" {
		t.Fatalf("ClickHouse 导入: kind=%q proto=%d port=%q db=%q params=%q",
			a.connFormKind, a.connProto, a.connFormPort, a.connFormDB, a.connFormParams)
	}
}

func TestConnFormURLImportSQLite(t *testing.T) {
	a, tt := openConnModal(t)
	tt.SetClipboard("jdbc:sqlite:/tmp/app.db")
	if err := tt.Click(locales.ConnFormURLParseClipboard()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connFormKind != "sqlite" || a.connFormPath != "/tmp/app.db" ||
		a.connFormHost != "" || a.connFormPort != "" {
		t.Fatalf("SQLite 导入: kind=%q path=%q host=%q port=%q",
			a.connFormKind, a.connFormPath, a.connFormHost, a.connFormPort)
	}
}

// 解析失败只提示，不覆盖已经填好的字段。
func TestConnFormURLImportFailureKeepsFields(t *testing.T) {
	a, tt := openConnModal(t)
	a.connFormKind, a.connFormHost = "postgres", "keep.example"
	// 直接改表单字段后先重排一帧：按钮在底栏，点击坐标取自上一帧布局。
	tt.Frame()
	tt.SetClipboard("jdbc:oracle:thin:@host:1521:xe")
	if err := tt.Click(locales.ConnFormURLParseClipboard()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connFormKind != "postgres" || a.connFormHost != "keep.example" {
		t.Fatalf("失败不应改表单: kind=%q host=%q", a.connFormKind, a.connFormHost)
	}
	if a.connFormURLErr == "" {
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
