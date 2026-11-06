package clickhouse

import (
	"net/url"
	"testing"

	"db-g/engine"
)

// openDSN 用给的表单建会话并返回拼出来的 DSN。
func openDSN(t *testing.T, p engine.Profile) string {
	t.Helper()
	s, err := openSession(p)
	if err != nil {
		t.Fatal(err)
	}
	return s.(*session).dsn
}

func TestDSNProtocolAndParams(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		params   string
		want     string
	}{
		{
			name: "native 默认",
			want: "clickhouse://u:p@ck.example:9000/db",
		},
		{
			name:     "http 端口 + 关闭压缩",
			protocol: "http",
			params:   "compress=0",
			want:     "http://u:p@ck.example:9000/db?compress=0",
		},
		{
			name:     "https 自动补 secure",
			protocol: "https",
			want:     "https://u:p@ck.example:9000/db?secure=true",
		},
		{
			name:     "native 也能带参数",
			protocol: "native",
			params:   "compress=0&dial_timeout=3s",
			want:     "clickhouse://u:p@ck.example:9000/db?compress=0&dial_timeout=3s",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := openDSN(t, engine.Profile{
				Kind: "clickhouse", Host: "ck.example", Port: 9000, User: "u", Password: "p",
				Database: "db", Protocol: c.protocol, Params: c.params,
			})
			if got != c.want {
				t.Fatalf("dsn:\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestDSNParamsAreURLSafe(t *testing.T) {
	got := openDSN(t, engine.Profile{
		Kind: "clickhouse", Host: "ck.example", Port: 8123, User: "u", Database: "db",
		Protocol: "http", Params: "custom=1/2&x=1",
	})
	if _, err := url.Parse(got); err != nil {
		t.Fatalf("dsn must parse: %v (%q)", err, got)
	}
	if got != "http://u@ck.example:8123/db?custom=1%2F2&x=1" {
		t.Fatalf("dsn: %q", got)
	}
}

func TestDSNRejectsBadParams(t *testing.T) {
	if _, err := openSession(engine.Profile{Kind: "clickhouse", Host: "ck.example", Port: 9000, Params: "bogus"}); err == nil {
		t.Fatal("bad params must fail the session")
	}
}
