package sqlite

import (
	"context"
	"errors"
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

func TestColumns(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, score REAL)`); err != nil {
		t.Fatal(err)
	}
	lister, ok := s.(engine.ColumnLister)
	if !ok || !s.Caps().Has(engine.CapColumns) {
		t.Fatal("sqlite must list columns")
	}
	cols, err := lister.Columns(ctx, "users")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 {
		t.Fatalf("cols: %+v", cols)
	}
	if cols[0].Name != "id" || !cols[0].PrimaryKey || cols[0].Type == "" {
		t.Fatalf("pk column: %+v", cols[0])
	}
	if cols[1].Name != "name" || cols[1].Nullable {
		t.Fatalf("not-null column: %+v", cols[1])
	}
}

func TestColumnsQuotesIdentifier(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, `CREATE TABLE "weird""name" (c TEXT)`); err != nil {
		t.Fatal(err)
	}
	cols, err := s.(engine.ColumnLister).Columns(ctx, `weird"name`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || cols[0].Name != "c" {
		t.Fatalf("cols: %+v", cols)
	}
}

func TestColumnsUnknownTable(t *testing.T) {
	cols, err := newMemSession(t).(engine.ColumnLister).Columns(context.Background(), "nope")
	if err != nil {
		t.Fatal("unknown table must not error:", err)
	}
	if len(cols) != 0 {
		t.Fatalf("cols: %+v", cols)
	}
}

func TestStreamRows(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, `CREATE TABLE t (id INTEGER, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(ctx, `INSERT INTO t (id, name) VALUES (1,'a'),(2,'b'),(3,'c')`); err != nil {
		t.Fatal(err)
	}
	streamer, ok := s.(engine.RowStreamer)
	if !ok {
		t.Fatal("sqlite must stream rows")
	}
	var cols []string
	var rows [][]any
	err := streamer.Stream(ctx, "SELECT id, name FROM t ORDER BY id",
		func(c []string) error { cols = c; return nil },
		func(row []any) error {
			cp := append([]any(nil), row...)
			rows = append(rows, cp)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 || cols[1] != "name" {
		t.Fatalf("cols: %v", cols)
	}
	if len(rows) != 3 || rows[2][1] != "c" {
		t.Fatalf("rows: %v", rows)
	}
	// 回调报错要中止并返回该错误
	sentinel := errors.New("stop")
	err = streamer.Stream(ctx, "SELECT id FROM t", nil, func([]any) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("callback error must propagate: %v", err)
	}
}

func TestObjectDDL(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, `CREATE TABLE ddl_t (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(ctx, `CREATE VIEW ddl_v AS SELECT id FROM ddl_t`); err != nil {
		t.Fatal(err)
	}
	lister, ok := s.(engine.ObjectDDLer)
	if !ok || !s.Caps().Has(engine.CapObjectDDL) {
		t.Fatal("sqlite must provide DDL")
	}
	ddl, err := lister.ObjectDDL(ctx, engine.ObjectTable, "ddl_t")
	if err != nil || !strings.HasPrefix(strings.ToUpper(ddl), "CREATE TABLE") {
		t.Fatalf("table ddl: %q err=%v", ddl, err)
	}
	ddl, err = lister.ObjectDDL(ctx, engine.ObjectView, "ddl_v")
	if err != nil || !strings.Contains(strings.ToUpper(ddl), "CREATE VIEW") {
		t.Fatalf("view ddl: %q err=%v", ddl, err)
	}
	if _, err := lister.ObjectDDL(ctx, engine.ObjectTable, "nope"); err == nil {
		t.Fatal("missing object must error")
	}
}

func TestTransactionalCommitAndRollback(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	if _, err := s.RunStatement(ctx, `CREATE TABLE tx_t (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	tr, ok := s.(engine.Transactional)
	if !ok || !s.Caps().Has(engine.CapEditableResult) {
		t.Fatal("sqlite must support transactions")
	}
	// 提交可见
	tx, err := tr.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, err := tx.Exec(ctx, `INSERT INTO tx_t (id, name) VALUES (?, ?)`, 1, "a")
	if err != nil || n != 1 {
		t.Fatalf("exec: n=%d err=%v", n, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if r, err := s.RunStatement(ctx, `SELECT count(*) FROM tx_t`); err != nil || r.Rows[0][0] != int64(1) {
		t.Fatalf("committed row missing: %+v", r)
	}
	// 回滚不可见
	tx, err = tr.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tx_t (id, name) VALUES (?, ?)`, 2, "b"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if r, err := s.RunStatement(ctx, `SELECT count(*) FROM tx_t`); err != nil || r.Rows[0][0] != int64(1) {
		t.Fatalf("rolled back row visible: %+v", r)
	}
	// 影响行数要如实返回
	tx, err = tr.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if n, err := tx.Exec(ctx, `UPDATE tx_t SET name = ? WHERE id = ?`, "z", 999); err != nil || n != 0 {
		t.Fatalf("no-match update: n=%d err=%v", n, err)
	}
}
