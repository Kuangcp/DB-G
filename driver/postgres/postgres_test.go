package postgres

import (
	"context"
	"os"
	"strconv"
	"testing"

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
