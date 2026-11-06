package app

import (
	"strings"
	"testing"
)

func TestFormatSQLBasic(t *testing.T) {
	got, err := FormatSQL("select a,b from t where x=1 and y='z' order by a desc")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT a,b\nFROM t\nWHERE x=1 AND y='z'\nORDER BY a DESC"
	if got != want {
		t.Fatalf("format:\n%q\nwant\n%q", got, want)
	}
}

func TestFormatSQLIdempotent(t *testing.T) {
	inputs := []string{
		"select 1",
		"select a,b from t where x=1",
		"insert into t (a) values (1)",
		"select * from a join b on a.id=b.id where a.x in (select y from c)",
		`CREATE TABLE "category" ("id" integer PRIMARY KEY autoincrement,"created_at" datetime,"name" varchar(255),"type_id" integer )`,
		"create table if not exists t (a integer, b text not null default 'x,y', primary key (a, b))",
		"CREATE TABLE t AS SELECT (a+b) FROM x",
	}
	for _, in := range inputs {
		once, _ := FormatSQL(in)
		twice, _ := FormatSQL(once)
		if once != twice {
			t.Fatalf("not idempotent for %q:\n%q\n%q", in, once, twice)
		}
	}
}

func TestFormatSQLKeepsStringsAndComments(t *testing.T) {
	in := "select 'select from where', \"FROM\" -- select x\nfrom t /* from y */ where a = 1"
	got, err := FormatSQL(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "'select from where'") || !strings.Contains(got, `"FROM"`) {
		t.Fatalf("string literals changed: %q", got)
	}
	if !strings.Contains(got, "-- select x") || !strings.Contains(got, "/* from y */") {
		t.Fatalf("comments changed: %q", got)
	}
}

func TestFormatSQLUnbalancedReturnsOriginal(t *testing.T) {
	for _, in := range []string{"select (1", "select 'x"} {
		got, err := FormatSQL(in)
		if err != nil {
			t.Fatal(err)
		}
		if got != in {
			t.Fatalf("%q must stay as is, got %q", in, got)
		}
	}
}

// sqlite 的建表 DDL 常常是一整行、逗号后不带空格：列定义要断行并缩进。
func TestFormatSQLCreateTableColumns(t *testing.T) {
	in := `CREATE TABLE "category" ("id" integer PRIMARY KEY autoincrement,"created_at" datetime,"name" varchar(255),"type_id" integer )`
	want := `CREATE TABLE "category" (
  "id" integer PRIMARY KEY autoincrement,
  "created_at" datetime,
  "name" varchar(255),
  "type_id" integer
)`
	got, err := FormatSQL(in)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("format:\n%s\nwant:\n%s", got, want)
	}
	// 表约束也当一行处理
	got, _ = FormatSQL("create table t (a integer, b text, primary key (a, b))")
	want = "CREATE TABLE t (\n  a integer,\n  b text,\n  PRIMARY KEY (a, b)\n)"
	if got != want {
		t.Fatalf("constraint:\n%s\nwant:\n%s", got, want)
	}
}

// CTAS 与查询里的括号不能被当成列定义拆开。
func TestFormatSQLCreateTableAsSelect(t *testing.T) {
	got, err := FormatSQL("CREATE TABLE t AS SELECT (a+b) FROM x")
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE TABLE t AS\nSELECT (a+b)\nFROM x"
	if got != want {
		t.Fatalf("ctas:\n%s\nwant:\n%s", got, want)
	}
	if got, _ := FormatSQL("select * from t where a in (1,2,3)"); !strings.Contains(got, "IN (1,2,3)") {
		t.Fatalf("inline parens should stay: %q", got)
	}
}
