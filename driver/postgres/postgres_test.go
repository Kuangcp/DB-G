package postgres

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"testing"

	"db-g/driver/internal/pagetest"
	"db-g/engine"
)

func TestOpenSessionBuildsDSN(t *testing.T) {
	s, err := openSession(engine.Profile{Kind: "postgres", Host: "db.example", Port: 5433, User: "u", Password: "p", Database: "app"})
	if err != nil {
		t.Fatal(err)
	}
	sess := s.(*session)
	if sess.dsn != "postgres://u:p@db.example:5433/app?sslmode=disable" {
		t.Fatalf("dsn: %q", sess.dsn)
	}
	if !s.Caps().Has(engine.CapEditableResult) || !s.Caps().Has(engine.CapColumns) {
		t.Fatalf("caps: %v", s.Caps())
	}
	if _, ok := s.(engine.Transactional); !ok {
		t.Fatal("postgres must support transactions")
	}
	if _, err := s.(engine.Transactional).Begin(context.Background()); err == nil {
		t.Fatal("Begin without an open session must fail")
	}
}

func TestPostgresIntegration(t *testing.T) {
	host := os.Getenv("DB_G_PG_HOST")
	if host == "" {
		t.Skip("DB_G_PG_HOST 未设置")
	}
	port := 5432
	if v := os.Getenv("DB_G_PG_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			port = n
		}
	}
	s, err := engine.Open(engine.Profile{Kind: "postgres", Host: host, Port: port,
		User: os.Getenv("DB_G_PG_USER"), Password: os.Getenv("DB_G_PG_PASSWORD"), Database: os.Getenv("DB_G_PG_DATABASE")})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Open(ctx); err != nil {
		t.Skipf("postgres 不可用: %v", err)
	}
	defer s.Close()
	if _, err := s.RunStatement(ctx, `CREATE TABLE IF NOT EXISTS pg_smoke (id INT PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if _, err := s.RunStatement(ctx, `INSERT INTO pg_smoke (id, name) VALUES (1, 'a')`); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	cols, err := s.(engine.ColumnLister).Columns(ctx, "pg_smoke")
	if err != nil {
		t.Fatal(err)
	}
	foundPK := false
	for _, c := range cols {
		if c.Name == "id" && c.PrimaryKey {
			foundPK = true
		}
	}
	if !foundPK {
		t.Fatalf("pk not detected: %+v", cols)
	}
}

func TestOpenSessionMergesParams(t *testing.T) {
	s, err := openSession(engine.Profile{
		Kind: "postgres", Host: "db.example", Port: 5433, User: "u", Database: "app",
		Params: "sslmode=require&application_name=db-g",
	})
	if err != nil {
		t.Fatal(err)
	}
	dsn := s.(*session).dsn
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("sslmode"); got != "require" {
		t.Fatalf("用户的参数要覆盖默认的 sslmode=disable，got %q (%q)", got, dsn)
	}
	if got := u.Query().Get("application_name"); got != "db-g" {
		t.Fatalf("application_name: %q (%q)", got, dsn)
	}
}

func TestOpenSessionBadParams(t *testing.T) {
	if _, err := openSession(engine.Profile{Kind: "postgres", Host: "h", Port: 1, Params: "nope"}); err == nil {
		t.Fatal("bad params must fail")
	}
}

// TestPageDoesNotDrain 回归：读满一页后必须取消查询再 Close，否则 pgx 的
// ResultReader.Close 会把剩余结果一直读到 CommandComplete。
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
