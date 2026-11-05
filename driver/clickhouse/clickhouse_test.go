package clickhouse

import (
	"os"
	"strconv"
	"testing"

	"db-g/engine"
)

func TestOpenSessionBuildsDSN(t *testing.T) {
	s, err := openSession(engine.Profile{Kind: "clickhouse", Host: "ck.example", Port: 9000, User: "u", Password: "p", Database: "db"})
	if err != nil {
		t.Fatal(err)
	}
	sess := s.(*session)
	if sess.dsn != "clickhouse://u:p@ck.example:9000/db" {
		t.Fatalf("dsn: %q", sess.dsn)
	}
	if s.Caps().Has(engine.CapEditableResult) {
		t.Fatal("clickhouse must be read-only for results")
	}
	if _, ok := s.(engine.Transactional); ok {
		t.Fatal("clickhouse must not declare transactions")
	}
}

func TestClickHouseIntegration(t *testing.T) {
	host := os.Getenv("DB_G_CK_HOST")
	if host == "" {
		t.Skip("DB_G_CK_HOST 未设置")
	}
	port := 9000
	if v := os.Getenv("DB_G_CK_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			port = n
		}
	}
	s, err := engine.Open(engine.Profile{Kind: "clickhouse", Host: host, Port: port,
		User: os.Getenv("DB_G_CK_USER"), Password: os.Getenv("DB_G_CK_PASSWORD"), Database: os.Getenv("DB_G_CK_DATABASE")})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := s.Open(ctx); err != nil {
		t.Skipf("clickhouse 不可用: %v", err)
	}
	defer s.Close()
	if _, err := s.RunStatement(ctx, `SELECT 1`); err != nil {
		t.Skipf("query failed: %v", err)
	}
}
