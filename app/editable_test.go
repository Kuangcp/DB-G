package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var editTestCols = []ColumnInfo{
	{Name: "id", Type: "INTEGER", PrimaryKey: true},
	{Name: "name", Type: "TEXT"},
	{Name: "score", Type: "REAL"},
}

func TestBuildEditPlanOK(t *testing.T) {
	plan, err := BuildEditPlan("sqlite", "users", []string{"id", "name"}, editTestCols)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Table != "users" || len(plan.PKIdx) != 1 || plan.PKIdx[0] != 0 || plan.PKNames[0] != "id" {
		t.Fatalf("plan: %+v", plan)
	}
	if plan.Editable[0] || !plan.Editable[1] {
		t.Fatalf("pk must be read-only: %+v", plan.Editable)
	}
}

func TestBuildEditPlanRejects(t *testing.T) {
	cases := []struct {
		name  string
		table string
		cols  []string
	}{
		{"no primary key", "users", []string{"name"}},
		{"missing pk column", "users", []string{"name", "score"}},
		{"unknown column", "users", []string{"id", "unknown_col"}},
		{"duplicate result column", "users", []string{"id", "id"}},
	}
	for _, c := range cases {
		if _, err := BuildEditPlan("sqlite", "users", c.cols, editTestCols); err == nil {
			t.Fatalf("%s must be rejected", c.name)
		}
	}
	tcols := []ColumnInfo{{Name: "name", Type: "TEXT"}}
	if _, err := BuildEditPlan("sqlite", "v", []string{"name"}, tcols); err == nil {
		t.Fatal("a table without primary key must be rejected")
	}
}

func TestBuildOpsUpdateInsertDelete(t *testing.T) {
	plan, err := BuildEditPlan("sqlite", "users", []string{"id", "name", "score"}, editTestCols)
	if err != nil {
		t.Fatal(err)
	}
	rows := [][]any{
		{int64(1), "alice", 1.5},
		{int64(2), "bob", nil},
	}
	e := NewEditSet()
	e.SetCell(0, 1, "alice2") // name
	e.SetCell(1, 2, nil)      // score 本来就是 NULL：不应产生 UPDATE
	e.SetCell(0, 1, "alice")  // 改回原值：也不应产生 UPDATE
	ins := e.AddInsert()
	e.Inserts[ins][1] = "carol"
	e.MarkDeleted(1)

	ops, err := e.BuildOps(plan, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 {
		t.Fatalf("ops: %+v", ops)
	}
	if ops[0].SQL != `INSERT INTO "users" ("name") VALUES (?)` || ops[0].Args[0] != "carol" || ops[0].Want != 1 {
		t.Fatalf("insert op: %+v", ops[0])
	}
	if ops[1].SQL != `DELETE FROM "users" WHERE "id" = ?` || ops[1].Args[0] != int64(2) {
		t.Fatalf("delete op: %+v", ops[1])
	}

	// 改值 → UPDATE（参数顺序：SET 值、主键值）
	e2 := NewEditSet()
	e2.SetCell(0, 1, "z")
	ops, err = e2.BuildOps(plan, rows)
	if err != nil || len(ops) != 1 {
		t.Fatalf("update ops: %+v err=%v", ops, err)
	}
	if ops[0].SQL != `UPDATE "users" SET "name" = ? WHERE "id" = ?` {
		t.Fatalf("update sql: %q", ops[0].SQL)
	}
	if len(ops[0].Args) != 2 || ops[0].Args[0] != "z" || ops[0].Args[1] != int64(1) {
		t.Fatalf("update args: %+v", ops[0].Args)
	}

	// NULL → 值的参数
	e3 := NewEditSet()
	e3.SetCell(1, 2, 9.9)
	ops, _ = e3.BuildOps(plan, rows)
	if ops[0].Args[0] != 9.9 {
		t.Fatalf("null→value args: %+v", ops[0].Args)
	}
}

func TestBuildOpsSkipsUpdatesOnDeletedRows(t *testing.T) {
	plan, _ := BuildEditPlan("sqlite", "users", []string{"id", "name"}, editTestCols)
	rows := [][]any{{int64(1), "a"}}
	e := NewEditSet()
	e.SetCell(0, 1, "changed")
	e.MarkDeleted(0)
	ops, err := e.BuildOps(plan, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || !strings.HasPrefix(ops[0].SQL, "DELETE") {
		t.Fatalf("deleted row must not produce an UPDATE: %+v", ops)
	}
}

func TestPreviewMatchesOps(t *testing.T) {
	plan, _ := BuildEditPlan("sqlite", "users", []string{"id", "name"}, editTestCols)
	rows := [][]any{{int64(1), "a"}}
	e := NewEditSet()
	e.SetCell(0, 1, "O'Reilly")
	ops, err := e.BuildOps(plan, rows)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := e.Preview(plan, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != len(ops) {
		t.Fatalf("preview/ops mismatch: %v vs %v", preview, ops)
	}
	if preview[0] != `UPDATE "users" SET "name" = 'O''Reilly' WHERE "id" = 1` {
		t.Fatalf("preview: %q", preview[0])
	}
}

func TestApplyOpsChecksRowsAffected(t *testing.T) {
	tx := &fakeTx{counts: []int64{1}}
	if err := ApplyOps(context.Background(), tx, []Op{{SQL: "x", Want: 1}}); err != nil {
		t.Fatal(err)
	}
	tx = &fakeTx{counts: []int64{0}}
	if err := ApplyOps(context.Background(), tx, []Op{{SQL: "x", Want: 1}}); err == nil {
		t.Fatal("rows affected mismatch must fail")
	}
	tx = &fakeTx{counts: []int64{2}}
	if err := ApplyOps(context.Background(), tx, []Op{{SQL: "x", Want: 1}}); err == nil {
		t.Fatal("rows affected 2 must fail")
	}
	tx = &fakeTx{errs: []error{errors.New("boom")}}
	if err := ApplyOps(context.Background(), tx, []Op{{SQL: "x", Want: 1}}); err == nil {
		t.Fatal("exec error must fail")
	}
}

type fakeTx struct {
	counts []int64
	errs   []error
	i      int
}

func (f *fakeTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	i := f.i
	f.i++
	if i < len(f.errs) && f.errs[i] != nil {
		return 0, f.errs[i]
	}
	if i < len(f.counts) {
		return f.counts[i], nil
	}
	return 1, nil
}

func TestBuildOpsPostgresPlaceholders(t *testing.T) {
	plan, err := BuildEditPlan("postgres", "users", []string{"id", "name", "score"}, editTestCols)
	if err != nil {
		t.Fatal(err)
	}
	rows := [][]any{{int64(1), "a", 1.0}}
	e := NewEditSet()
	e.SetCell(0, 1, "b")
	ops, err := e.BuildOps(plan, rows)
	if err != nil {
		t.Fatal(err)
	}
	want := `UPDATE "users" SET "name" = $1 WHERE "id" = $2`
	if ops[0].SQL != want {
		t.Fatalf("sql: %q", ops[0].SQL)
	}
	preview, _ := e.Preview(plan, rows)
	if preview[0] != `UPDATE "users" SET "name" = 'b' WHERE "id" = 1` {
		t.Fatalf("preview: %q", preview[0])
	}
}
