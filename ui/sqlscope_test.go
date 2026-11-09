package ui

import (
	"strings"
	"testing"
	"time"
)

func TestStmtScopeAt(t *testing.T) {
	text := "select * from a; select id, na from b where id in (select x from c); -- tail"
	inner := stmtScopeAt(text, strings.Index(text, "select x"))
	if !strings.HasPrefix(inner, "select x from c") {
		t.Fatalf("inner scope: %q", inner)
	}
	outer := stmtScopeAt(text, strings.Index(text, "select id"))
	// 外层作用域是整条语句（CTE 定义要可见），但 relationsIn 只取本层关系：
	if got := relationsOf(outer); strings.Join(got, ",") != "b" {
		t.Fatalf("outer scope relations must exclude the subquery: %v (scope=%q)", got, outer)
	}
	if got := stmtScopeAt("select 1; select 2", strings.Index("select 1; select 2", "select 2")); got != "select 2" {
		t.Fatalf("statement split: %q", got)
	}
	if got := stmtScopeAt("select ';' from a", 10); !strings.Contains(got, "select ';' from a") {
		t.Fatalf("semicolon in string must not split: %q", got)
	}
}

func relationsOf(sql string) []string {
	var out []string
	for _, r := range relationsIn(sql) {
		if r.Table != "" {
			out = append(out, r.Table)
		}
	}
	return out
}

func relationsOfRefs(sql string) []relationRef { return relationsIn(sql) }

func TestRelationsBasicClauses(t *testing.T) {
	for _, c := range []struct {
		sql  string
		want []string
	}{
		{"select id, name from med_school", []string{"med_school"}},
		{"select * from a join b on a.id = b.id join c on c.id = b.id", []string{"a", "b", "c"}},
		{"select * from a, b where a.id = b.id", []string{"a", "b"}},
		{"select * from a left outer join b using (id)", []string{"a", "b"}},
		{"update med_school set name = 1 where id = 2", []string{"med_school"}},
		{"insert into med_school (name) values ('x')", []string{"med_school"}},
		{"select * from app.med_school", []string{"med_school"}},
		{"select * from `med_school`", []string{"med_school"}},
	} {
		got := relationsOf(c.sql)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("%q → %v want %v", c.sql, got, c.want)
		}
	}
}

func TestRelationsStringsAndCommentsIgnored(t *testing.T) {
	sql := `select * from real_t where note = 'from fake_t' -- from also_fake
	/* from block_fake */ and x = 1`
	got := relationsOf(sql)
	if strings.Join(got, ",") != "real_t" {
		t.Fatalf("got %v", got)
	}
}

func TestRelationsCTE(t *testing.T) {
	sql := `with x as (select * from a), y as (select * from x join b on true)
	select * from y`
	got := relationsOf(sql)
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("cte chain: %v", got)
	}
	_ = relationsOf(`with r as (select * from r) select * from r`)
}

func TestRelationsDerivedTableSkipped(t *testing.T) {
	got := relationsOf(`select * from (select * from inner_t) d join real_t on true`)
	if strings.Join(got, ",") != "real_t" {
		t.Fatalf("got %v", got)
	}
}

func TestAliasMap(t *testing.T) {
	rels := relationsOfRefs("select * from med_school m join b on true join c")
	m := aliasMap(rels)
	if m["m"] != "med_school" || m["b"] != "b" || m["c"] != "c" {
		t.Fatalf("alias map: %v", m)
	}
	if _, ok := m["d"]; ok {
		t.Fatal("derived table alias must not resolve to a column scope")
	}
}

func TestStmtScopeAtIsBounded(t *testing.T) {
	// 大文件下补全不能全文扫描：100k 行（11MB）也要在毫秒级完成。
	text := buildSQL(100_000)
	start := time.Now()
	scope := stmtScopeAt(text, len(text)/2)
	d := time.Since(start)
	if scope == "" {
		t.Fatal("empty scope")
	}
	if d > 2*time.Millisecond {
		t.Fatalf("stmtScopeAt on %d bytes took %v (must stay bounded)", len(text), d)
	}
	if got := relationsOf(scope); len(got) == 0 {
		t.Fatalf("relations lost in the windowed scan: %q", scope)
	}
}

// 函数/表达式括号不切作用域：光标在 sum( 里仍能看到整条语句的表。
func TestScopeFunctionParenKeepsStatementRelations(t *testing.T) {
	for _, text := range []string{
		"select sum(amount) from record",
		"select sum(amount from record",               // 括号还没闭合
		"select coalesce(sum(amount), 0) from record", // 嵌套函数
		"select count(*) from record where (amount > 0 and id = 1)",
	} {
		offset := strings.Index(text, "amount") + 3
		if strings.Contains(text, "count(*)") {
			offset = strings.Index(text, "amount")
		}
		if got := relationsOf(stmtScopeAt(text, offset)); strings.Join(got, ",") != "record" {
			t.Fatalf("%q → relations %v (scope %q)", text, got, stmtScopeAt(text, offset))
		}
	}
	// 函数里的子查询仍按子查询作用域解析。
	text := "select max((select zz from inner_t)) from record"
	offset := strings.Index(text, "zz") + 2
	if got := relationsOf(stmtScopeAt(text, offset)); strings.Join(got, ",") != "inner_t" {
		t.Fatalf("subquery in function: %v", got)
	}
}

func TestRelationPositionAt(t *testing.T) {
	cases := []struct {
		sql  string
		want bool
	}{
		{"select * from |", true},
		{"select * from us|ers", true},
		{"select * from users |", false},
		{"select * from users, |", true},
		{"select * from users u join |", true},
		{"select * from users u join orders o on |", false},
		{"update | set", true},
		{"update users set |", false},
		{"insert into |", true},
		{"insert into users (|", false},
		{"delete from |", true},
		{"truncate table |", true},
		{"select | from users", false},
		{"select * from schema.|", true},
		{"select a.| from users", false},
	}
	for _, c := range cases {
		at := strings.Index(c.sql, "|")
		if at < 0 {
			t.Fatalf("missing marker: %q", c.sql)
		}
		text := strings.Replace(c.sql, "|", "", 1)
		if got := relationPositionAt(text, at); got != c.want {
			t.Errorf("%q: got %v want %v", c.sql, got, c.want)
		}
	}
}

func TestStmtScopeAtCutsFollowingStatementWithoutSemicolon(t *testing.T) {
	text := "select * from channel_config_bak0915;\n\nselect * from m\n\nselect * from channel_config_bench;"
	offset := strings.Index(text, "select * from m") + len("select * from m")
	got := stmtScopeAt(text, offset)
	if got != "select * from m" {
		t.Fatalf("scope must stop at the next line-leading SELECT: %q", got)
	}
	if rels := relationsOf(got); strings.Join(rels, ",") != "m" {
		t.Fatalf("relations: %v", rels)
	}
}

func TestStmtScopeAtSoftBoundaryGuards(t *testing.T) {
	cases := []struct {
		name   string
		sql    string
		offset int
		want   string
	}{
		{
			name:   "union",
			sql:    "select a from t\nunion\nselect b from u",
			offset: strings.Index("select a from t\nunion\nselect b from u", "union"),
			want:   "select a from t\nunion\nselect b from u",
		},
		{
			name:   "cte main select",
			sql:    "with x as (select 1)\nselect * from x",
			offset: strings.Index("with x as (select 1)\nselect * from x", "select * from x"),
			want:   "with x as (select 1)\nselect * from x",
		},
		{
			name:   "insert select",
			sql:    "insert into t\nselect * from u",
			offset: strings.Index("insert into t\nselect * from u", "select * from u"),
			want:   "insert into t\nselect * from u",
		},
		{
			name:   "create as select",
			sql:    "create view v as\nselect * from u",
			offset: strings.Index("create view v as\nselect * from u", "select * from u"),
			want:   "create view v as\nselect * from u",
		},
		{
			name:   "new create statement",
			sql:    "select * from t\n\ncreate table x (id int)",
			offset: strings.Index("select * from t\n\ncreate table x (id int)", "\n\ncreate"),
			want:   "select * from t",
		},
	}
	for _, c := range cases {
		if got := stmtScopeAt(c.sql, c.offset); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestTokenizeSQLUnclosedQuoteDoesNotPanic(t *testing.T) {
	for _, s := range []string{`select "`, "select `", `select "x`} {
		_ = tokenizeSQL(s)
		_ = stmtScopeAt(s, len(s))
		_ = relationPositionAt(s, len(s))
	}
}
