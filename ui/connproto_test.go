package ui

import (
	"strings"
	"testing"

	"db-g/app"
)

func TestClickHouseProtocolSwitchesPort(t *testing.T) {
	a, tt := openConnModal(t)
	if err := tt.Click("ClickHouse"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connFormPort != "9000" {
		t.Fatalf("native 默认端口: %q", a.connFormPort)
	}
	// 点 HTTP：端口跟着换成 8123
	if err := tt.Click("HTTP"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connProto != 1 {
		t.Fatalf("协议下标: %d", a.connProto)
	}
	if a.connFormPort != "8123" {
		t.Fatalf("HTTP 端口: %q", a.connFormPort)
	}
	if err := tt.Click("HTTPS"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connFormPort != "8443" {
		t.Fatalf("HTTPS 端口: %q", a.connFormPort)
	}
	// 自己填过的端口不被覆盖
	a.connFormPort = "12345"
	a.connProto = 0
	a.applyConnProto()
	if a.connFormPort != "12345" {
		t.Fatalf("自定义端口不该被改: %q", a.connFormPort)
	}
}

func TestClickHouseProfileCarriesProtocolAndParams(t *testing.T) {
	a, _ := openConnModal(t)
	a.connFormKind = "clickhouse"
	a.connFormName = "Langfuse"
	a.connFormHost = "ck.example"
	a.connFormPort = "8123"
	a.connProto = 1
	a.connFormParams = "compress=0"
	p, err := a.connFormProfile()
	if err != nil {
		t.Fatal(err)
	}
	if p.Protocol != "http" || p.Params != "compress=0" {
		t.Fatalf("profile: %+v", p)
	}
	ep := engineProfile(p)
	if ep.Protocol != "http" || ep.Params != "compress=0" {
		t.Fatalf("engine profile: %+v", ep)
	}
	sum := connFormSummary(p)
	if !strings.Contains(sum, "协议: http") || !strings.Contains(sum, "参数: compress=0") {
		t.Fatalf("summary: %q", sum)
	}
}

func TestProtocolOnlyForClickHouse(t *testing.T) {
	a, tt := openConnModal(t)
	tt.Frame()
	if tt.HasText("协议") {
		t.Fatalf("SQLite 表单不该出现协议选项: %q", tt.Texts())
	}
	a.connFormKind = "mysql"
	tt.Frame()
	if tt.HasText("协议") {
		t.Fatalf("MySQL 表单不该出现协议选项: %q", tt.Texts())
	}
	a.connFormKind = "clickhouse"
	tt.Frame()
	if !tt.HasText("协议") {
		t.Fatalf("ClickHouse 表单要有协议选项: %q", tt.Texts())
	}
}

func TestConnProfileParamsRoundTrip(t *testing.T) {
	a, tt := openConnModal(t)
	p := app.ConnProfile{Name: "CK", Kind: "clickhouse", Host: "h", Port: 8123, Protocol: "http", Params: "compress=0"}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	if stored.Protocol != "http" || stored.Params != "compress=0" {
		t.Fatalf("存储没带上协议/参数: %+v", stored)
	}
	// 编辑回填
	a.connFormOpen, a.connFormID = true, stored.ID
	a.connFormKind, a.connFormName, a.connFormHost = stored.Kind, stored.Name, stored.Host
	a.connFormPort = "8123"
	a.connFormParams = stored.Params
	a.connProto = connProtoIndex(stored.Protocol)
	tt.Frame()
	if a.connProto != 1 {
		t.Fatalf("回填协议下标: %d", a.connProto)
	}
}
