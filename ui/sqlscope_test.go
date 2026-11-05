package ui

import (
	"strings"
	"testing"
)

func TestStmtScopeAt(t *testing.T) {
	text := "select * from a; select id, na from b where id in (select x from c); -- tail"
	inner := stmtScopeAt(text, strings.Index(text, "select x"))
	if !strings.HasPrefix(inner, "select x from c") {
		t.Fatalf("inner scope: %q", inner)
	}
	outer := stmtScopeAt(text, strings.Index(text, "select id"))
	if strings.Contains(outer, "select x") {
		t.Fatalf("outer scope must stop at the parenthesis: %q", outer)
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
