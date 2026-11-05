package app_test

import (
	"context"
	"testing"

	"db-g/app"
	"db-g/engine"

	_ "db-g/driver/sqlite"
)

func openEditSession(t *testing.T) engine.Session {
	t.Helper()
	s, err := engine.Open(engine.Profile{Kind: "sqlite", Name: "t", DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func tableColumns(t *testing.T, s engine.Session, table string) []app.ColumnInfo {
	t.Helper()
	lister := s.(engine.ColumnLister)
	cols, err := lister.Columns(context.Background(), table)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]app.ColumnInfo, len(cols))
	for i, c := range cols {
		out[i] = app.ColumnInfo{Name: c.Name, Type: c.Type, PrimaryKey: c.PrimaryKey}
	}
	return out
}

func rowsOf(t *testing.T, s engine.Session, sql string) [][]any {
	t.Helper()
	r, err := s.RunStatement(context.Background(), sql)
	if err != nil {
		t.Fatal(err)
	}
	return r.Rows
}

func TestCommitEditsAgainstSQLite(t *testing.T) {
	ctx := context.Background()
	s := openEditSession(t)
	if _, err := s.RunStatement(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, score REAL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(ctx, `INSERT INTO users (id, name, score) VALUES (1,'alice',1.5),(2,'bob',NULL)`); err != nil {
		t.Fatal(err)
	}
	plan, err := app.BuildEditPlan("sqlite", "users", []string{"id", "name", "score"}, tableColumns(t, s, "users"))
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsOf(t, s, `SELECT id, name, score FROM users ORDER BY id`)

	e := app.NewEditSet()
	e.SetCell(0, 1, "alice2") // 改值
	e.SetCell(1, 2, 9.9)      // NULL → 值
	ins := e.AddInsert()
	e.Inserts[ins][0] = int64(3)
	e.Inserts[ins][1] = "carol"
	e.MarkDeleted(1) // bob 行：先改 score 再删，改格应以删为准

	ops, err := e.BuildOps(plan, rows)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.(engine.Transactional).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyOps(ctx, tx, ops); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	got := rowsOf(t, s, `SELECT id, name, score FROM users ORDER BY id`)
	if len(got) != 2 {
		t.Fatalf("rows: %v", got)
	}
	if got[0][1] != "alice2" || got[1][1] != "carol" {
		t.Fatalf("values: %v", got)
	}
}

func TestApplyOpsRollsBackOnConstraint(t *testing.T) {
	ctx := context.Background()
	s := openEditSession(t)
	if _, err := s.RunStatement(ctx, `CREATE TABLE u (id INTEGER PRIMARY KEY, name TEXT UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(ctx, `INSERT INTO u (id, name) VALUES (1,'a'),(2,'b')`); err != nil {
		t.Fatal(err)
	}
	plan, err := app.BuildEditPlan("sqlite", "u", []string{"id", "name"}, tableColumns(t, s, "u"))
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsOf(t, s, `SELECT id, name FROM u ORDER BY id`)
	e := app.NewEditSet()
	e.SetCell(0, 1, "b") // 与第 2 行冲突 → UNIQUE 失败
	ops, err := e.BuildOps(plan, rows)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.(engine.Transactional).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyOps(ctx, tx, ops); err == nil {
		t.Fatal("conflicting update must fail")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	got := rowsOf(t, s, `SELECT id, name FROM u ORDER BY id`)
	if got[0][1] != "a" || got[1][1] != "b" {
		t.Fatalf("rollback failed: %v", got)
	}
}
