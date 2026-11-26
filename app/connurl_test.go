package app

import (
	"errors"
	"strings"
	"testing"
)

func TestParseConnURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want ParsedConnURL
	}{
		{
			name: "jdbc mysql with query overrides",
			raw:  "jdbc:mysql://root:pw@db.local:3306/app?useSSL=false&user=admin&password=p%40ss&database=sales",
			want: ParsedConnURL{Kind: "mysql", Host: "db.local", Port: 3306, User: "admin", Password: "p@ss", Database: "sales", Params: "useSSL=false", Name: "sales"},
		},
		{
			name: "jdbc mariadb maps to mysql",
			raw:  "jdbc:mariadb://host/app",
			want: ParsedConnURL{Kind: "mysql", Host: "host", Database: "app", Name: "app"},
		},
		{
			name: "postgres uri ipv6 and escaped password",
			raw:  "postgresql://u:p%40ss@[::1]:5432/db?sslmode=require",
			want: ParsedConnURL{Kind: "postgres", Host: "::1", Port: 5432, User: "u", Password: "p@ss", Database: "db", Params: "sslmode=require", Name: "db"},
		},
		{
			name: "sqlalchemy driver suffix ignored",
			raw:  "mysql+pymysql://u:p@host/db?charset=utf8mb4",
			want: ParsedConnURL{Kind: "mysql", Host: "host", User: "u", Password: "p", Database: "db", Params: "charset=utf8mb4", Name: "db"},
		},
		{
			name: "clickhouse port 8123 means http",
			raw:  "jdbc:clickhouse://ch:8123/analytics?compress=0",
			want: ParsedConnURL{Kind: "clickhouse", Protocol: "http", Host: "ch", Port: 8123, Database: "analytics", Params: "compress=0", Name: "analytics"},
		},
		{
			name: "clickhouse explicit https",
			raw:  "jdbc:clickhouse:https://ch/db",
			want: ParsedConnURL{Kind: "clickhouse", Protocol: "https", Host: "ch", Database: "db", Name: "db"},
		},
		{
			name: "clickhouse native",
			raw:  "jdbc:clickhouse://ch:9000/db",
			want: ParsedConnURL{Kind: "clickhouse", Protocol: "native", Host: "ch", Port: 9000, Database: "db", Name: "db"},
		},
		{
			name: "clickhouse sqlalchemy http",
			raw:  "clickhouse+http://ch:8123/db",
			want: ParsedConnURL{Kind: "clickhouse", Protocol: "http", Host: "ch", Port: 8123, Database: "db", Name: "db"},
		},
		{
			name: "clickhouse database query",
			raw:  "jdbc:clickhouse://ch:8123?database=metrics&user=u",
			want: ParsedConnURL{Kind: "clickhouse", Protocol: "http", Host: "ch", Port: 8123, User: "u", Database: "metrics", Name: "metrics"},
		},
		{
			name: "sqlite jdbc path",
			raw:  "jdbc:sqlite:/tmp/a.db",
			want: ParsedConnURL{Kind: "sqlite", Path: "/tmp/a.db", Name: "a.db"},
		},
		{
			name: "sqlite memory",
			raw:  "jdbc:sqlite::memory:",
			want: ParsedConnURL{Kind: "sqlite", Path: ":memory:", Name: "memory"},
		},
		{
			name: "sqlite windows path",
			raw:  `jdbc:sqlite:C:\data\a.db`,
			want: ParsedConnURL{Kind: "sqlite", Path: `C:\data\a.db`, Name: "a.db"},
		},
		{
			name: "sqlite uri",
			raw:  "sqlite:///tmp/a.db",
			want: ParsedConnURL{Kind: "sqlite", Path: "/tmp/a.db", Name: "a.db"},
		},
		{
			name: "file uri",
			raw:  "file:/tmp/a.db",
			want: ParsedConnURL{Kind: "sqlite", Path: "/tmp/a.db", Name: "a.db"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseConnURL(c.raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got != c.want {
				t.Fatalf("parsed:\n got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestParseConnURLErrors(t *testing.T) {
	for _, raw := range []string{"", "not a url", "mysql://", "mysql://host:badport/db"} {
		if _, err := ParseConnURL(raw); !errors.Is(err, ErrConnURLInvalid) {
			t.Errorf("%q: want invalid, got %v", raw, err)
		}
	}
	for _, raw := range []string{"jdbc:oracle:thin:@host:1521:xe", "sqlserver://host/db"} {
		if _, err := ParseConnURL(raw); !errors.Is(err, ErrConnURLUnsupported) {
			t.Errorf("%q: want unsupported, got %v", raw, err)
		}
	}
	_, err := ParseConnURL("jdbc:mysql://u:pass@/db")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error must not echo the URL: %v", err)
	}
}
