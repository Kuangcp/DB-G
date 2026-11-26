package ui

import (
	"testing"

	"db-g/locales"
)

// 顶部 URL 导入：解析后切换类型、填端口/协议/参数，名称空缺时用库名。
func TestConnFormURLImportMySQL(t *testing.T) {
	a, tt := openConnModal(t)
	a.connFormURL = "jdbc:mysql://root:pw@db.local:3307/app?useSSL=false"
	tt.Frame()
	if err := tt.Click(locales.ConnFormURLFill()); err != nil {
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
	a.connFormURL = "jdbc:clickhouse:https://ch.example/analytics?compress=0"
	tt.Frame()
	if err := tt.Click(locales.ConnFormURLFill()); err != nil {
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
	a.connFormURL = "jdbc:sqlite:/tmp/app.db"
	tt.Frame()
	if err := tt.Click(locales.ConnFormURLFill()); err != nil {
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
	a.connFormURL = "jdbc:oracle:thin:@host:1521:xe"
	tt.Frame()
	if err := tt.Click(locales.ConnFormURLFill()); err != nil {
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
