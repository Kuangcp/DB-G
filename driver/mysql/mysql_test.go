package mysql

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"

	"db-g/engine"

	"github.com/go-sql-driver/mysql"
)

func TestOpenSessionBuildsConfig(t *testing.T) {
	s, err := openSession(engine.Profile{
		Kind: "mysql", Name: "n",
		Host: "db.example", Port: 3307, User: "root", Password: "p@ss/w", Database: "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess := s.(*session)
	if sess.cfg.User != "root" || sess.cfg.Passwd != "p@ss/w" {
		t.Fatalf("creds: %+v", sess.cfg)
	}
	if sess.cfg.Addr != "db.example:3307" || sess.cfg.Net != "tcp" {
		t.Fatalf("addr: %q net: %q", sess.cfg.Addr, sess.cfg.Net)
	}
	if sess.cfg.DBName != "app" || !sess.cfg.MultiStatements || !sess.cfg.ParseTime {
		t.Fatalf("dbname=%q multi=%v parseTime=%v", sess.cfg.DBName, sess.cfg.MultiStatements, sess.cfg.ParseTime)
	}
	if sess.cfg.Params["charset"] != "utf8mb4" {
		t.Fatalf("params: %+v", sess.cfg.Params)
	}
	dsn := sess.cfg.FormatDSN()
	if dsn == "" {
		t.Fatal("FormatDSN empty")
	}
}

func TestIsQueryLikeMySQL(t *testing.T) {
	yes := []string{"SELECT 1", " with x", "\nEXPLAIN t", "show tables", "DESCRIBE t", "desc t", "VALUES (1)"}
	no := []string{"INSERT INTO t VALUES (1)", "update t set a=1", "delete from t", "CREATE TABLE a(b int)"}
	for _, q := range yes {
		if !isQueryLike(q) {
			t.Errorf("expected query-like: %q", q)
		}
	}
	for _, q := range no {
		if isQueryLike(q) {
			t.Errorf("expected exec-like: %q", q)
		}
	}
}

func TestObjectsErrors(t *testing.T) {
	s := &session{cfg: newTestConfig()}
	if _, err := s.Objects(context.Background(), engine.ObjectTable); err == nil || err.Error() != "mysql: session not open" {
		t.Fatalf("want session not open, got %v", err)
	}
	db, err := sql.Open("mysql", "root@tcp(127.0.0.1:1)/")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s2 := &session{cfg: newTestConfig(), db: db}
	if _, err := s2.Objects(context.Background(), engine.ObjectTable); err == nil || err.Error() != "mysql: 未选择数据库" {
		t.Fatalf("want 未选择数据库, got %v", err)
	}
	s2.cfg.DBName = "app"
	if _, err := s2.Objects(context.Background(), engine.ObjectKind(99)); err == nil {
		t.Fatal("unknown kind must error")
	}
}

func TestMySQLIntegration(t *testing.T) {
	host := os.Getenv("DB_G_MYSQL_HOST")
	if host == "" {
		t.Skip("DB_G_MYSQL_HOST 未设置")
	}
	port := 3306
	if v := os.Getenv("DB_G_MYSQL_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatal(err)
		}
		port = p
	}
	s, err := openSession(engine.Profile{
		Kind: "mysql", Name: "it",
		Host: host, Port: port,
		User: os.Getenv("DB_G_MYSQL_USER"), Password: os.Getenv("DB_G_MYSQL_PASSWORD"),
		Database: os.Getenv("DB_G_MYSQL_DATABASE"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, err := s.RunStatement(ctx, "SELECT 1 AS a")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsQuery || len(res.Rows) != 1 {
		t.Fatalf("SELECT 1: %+v", res)
	}
	lister, ok := s.(engine.ObjectLister)
	if !ok {
		t.Fatal("mysql session must implement engine.ObjectLister")
	}
	if _, err := lister.Objects(ctx, engine.ObjectTable); err != nil {
		t.Fatalf("Objects: %v", err)
	}
}

func newTestConfig() *mysql.Config {
	return mysql.NewConfig()
}
