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
