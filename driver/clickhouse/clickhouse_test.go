package clickhouse

import (
	"context"
	"os"
	"strconv"
	"testing"

	"db-g/driver/internal/pagetest"
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
	protocol := os.Getenv("DB_G_CK_PROTOCOL")
	port := 9000
	switch protocol {
	case "http":
		port = 8123
	case "https":
		port = 8443
	}
	if v := os.Getenv("DB_G_CK_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			port = n
		}
	}
	s, err := engine.Open(engine.Profile{Kind: "clickhouse", Host: host, Port: port, Protocol: protocol,
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

// TestPageDoesNotDrain 回归：读满一页后必须取消查询再 Close，否则
// clickhouse-go 的 Rows.Close 会把剩余 stream 读完。
func TestPageDoesNotDrain(t *testing.T) {
	db, probe := pagetest.Open(5000)
	t.Cleanup(func() { _ = db.Close() })
	s := &session{db: db}
	ctx := engine.WithPageSize(context.Background(), 10)
	res, err := s.RunStatement(ctx, "SELECT id FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 10 || !res.HasMore {
		t.Fatalf("rows=%d hasMore=%v", len(res.Rows), res.HasMore)
	}
	if !probe.CloseCanceled() {
		t.Fatal("Close 前没有取消 context：剩余结果会被读完")
	}
	if n := probe.Drained(); n != 0 {
		t.Fatalf("Close 排空了 %d 行，应当 0", n)
	}
}
