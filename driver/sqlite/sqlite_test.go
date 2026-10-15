package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"db-g/engine"
)

func newMemSession(t *testing.T) engine.Session {
	t.Helper()
	s, err := engine.Open(engine.Profile{Kind: "sqlite", Name: "test", DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)

	res, err := s.RunStatement(ctx, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatal(err)
	}
	if res.IsQuery || res.Affected != 0 {
		t.Fatalf("ddl result: %+v", res)
	}

	insert, err := s.RunStatement(ctx, "INSERT INTO users (name) VALUES ('alice'), ('bob'), ('carol')")
	if err != nil {
		t.Fatal(err)
	}
	if insert.Affected != 3 {
		t.Fatalf("affected = %d, want 3", insert.Affected)
	}

	q, err := s.RunStatement(ctx, "SELECT id, name FROM users ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if !q.IsQuery || q.Truncated {
		t.Fatalf("flags: %+v", q)
	}
	if len(q.Columns) != 2 || q.Columns[0] != "id" || q.Columns[1] != "name" {
		t.Fatalf("columns: %v", q.Columns)
	}
	if len(q.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(q.Rows))
	}
	if q.Rows[1][0].(int64) != 2 || q.Rows[1][1].(string) != "bob" {
		t.Fatalf("row 1: %#v", q.Rows[1])
	}
	if q.Rows[2][1] != "carol" {
		t.Fatalf("row 2: %#v", q.Rows[2])
	}
}

func TestRowLimitAndTruncation(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, "CREATE TABLE big (i INTEGER)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 601; i++ {
		if _, err := s.RunStatement(ctx, "INSERT INTO big (i) VALUES (1)"); err != nil {
			t.Fatal(err)
		}
	}
	q, err := s.RunStatement(ctx, "SELECT i FROM big")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Rows) != 500 {
		t.Fatalf("rows = %d, want 500", len(q.Rows))
	}
	if !q.Truncated {
		t.Fatal("601 rows must mark Truncated")
	}
}

func TestErrorStatement(t *testing.T) {
	s := newMemSession(t)
	if _, err := s.RunStatement(context.Background(), "SELECT * FROM missing"); err == nil {
		t.Fatal("missing table must error")
	}
}

func TestNotOpen(t *testing.T) {
	s, err := engine.Open(engine.Profile{Kind: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(context.Background(), "SELECT 1"); err == nil ||
		!strings.Contains(err.Error(), "not open") {
		t.Fatalf("err = %v, want not open", err)
	}
}

func TestOpenMissingFile(t *testing.T) {
	s, err := engine.Open(engine.Profile{Kind: "sqlite", DSN: filepath.Join(t.TempDir(), "no", "x.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(context.Background()); err == nil {
		t.Fatal("missing file must fail Open")
	}
}

func TestCancelIdle(t *testing.T) {
	if newMemSession(t).Cancel() {
		t.Fatal("idle Cancel must return false")
	}
}

func TestCancelRunning(t *testing.T) {
	s := newMemSession(t)
	if _, err := s.RunStatement(context.Background(), "CREATE TABLE n (i INTEGER)"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.RunStatement(context.Background(),
			"WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c WHERE i<2000000000) SELECT count(*) FROM c")
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !s.Cancel() {
		if time.Now().After(deadline) {
			t.Fatal("Cancel never observed a running statement")
		}
		time.Sleep(2 * time.Millisecond)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled statement must return an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("statement did not stop after Cancel")
	}
}

func TestCellAndResultBudget(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, "CREATE TABLE c (v TEXT)"); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("x", (1<<20)+100)
	if _, err := s.RunStatement(ctx, "INSERT INTO c (v) VALUES ('"+huge+"')"); err != nil {
		t.Fatal(err)
	}
	q, err := s.RunStatement(ctx, "SELECT v FROM c")
	if err != nil {
		t.Fatal(err)
	}
	cell := q.Rows[0][0].(string)
	if len(cell) != 1<<20 {
		t.Fatalf("cell len = %d, want %d", len(cell), 1<<20)
	}
	if !q.Truncated {
		t.Fatal("oversize cell must mark Truncated")
	}

	mb := strings.Repeat("y", 1<<20)
	for i := 0; i < 17; i++ {
		if _, err := s.RunStatement(ctx, "INSERT INTO c (v) VALUES ('"+mb+"')"); err != nil {
			t.Fatal(err)
		}
	}
	q, err = s.RunStatement(ctx, "SELECT v FROM c")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Rows) != 17 {
		t.Fatalf("rows = %d, want 17 (one row overshoots the 16MB budget)", len(q.Rows))
	}
	if !q.Truncated {
		t.Fatal("16MB budget must mark Truncated")
	}
}

func TestObjects(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	lister, ok := s.(engine.ObjectLister)
	if !ok {
		t.Fatal("sqlite session must implement engine.ObjectLister")
	}
	if !s.Caps().Has(engine.CapObjects) {
		t.Fatal("sqlite Caps must include CapObjects")
	}
	stmts := []string{
		"CREATE TABLE a (id INTEGER)",
		"CREATE TABLE b (id INTEGER)",
		"CREATE VIEW v AS SELECT id FROM a",
		"CREATE INDEX ia ON a (id)",
		"CREATE TABLE seq (id INTEGER PRIMARY KEY AUTOINCREMENT)",
	}
	for _, q := range stmts {
		if _, err := s.RunStatement(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	tables, err := lister.Objects(ctx, engine.ObjectTable)
	if err != nil {
		t.Fatal(err)
	}
	if names := objectNames(tables); !equalStrings(names, []string{"a", "b", "seq"}) {
		t.Fatalf("tables = %v, want [a b seq]（sqlite_sequence 已被过滤）", names)
	}
	views, err := lister.Objects(ctx, engine.ObjectView)
	if err != nil {
		t.Fatal(err)
	}
	if names := objectNames(views); !equalStrings(names, []string{"v"}) {
		t.Fatalf("views = %v, want [v]", names)
	}
	indexes, err := lister.Objects(ctx, engine.ObjectIndex)
	if err != nil {
		t.Fatal(err)
	}
	if names := objectNames(indexes); !equalStrings(names, []string{"ia"}) {
		t.Fatalf("indexes = %v, want [ia]", names)
	}
}

func objectNames(objs []engine.DBObject) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.Name
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
