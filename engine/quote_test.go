package engine

import "testing"

func TestQuoteIdentAndString(t *testing.T) {
	cases := []struct {
		kind, name, want string
	}{
		{"mysql", "a`b", "`a``b`"},
		{"clickhouse", "a`b", "`a``b`"},
		{"postgres", `a"b`, `"a""b"`},
		{"sqlite", `a"b`, `"a""b"`},
	}
	for _, c := range cases {
		if got := QuoteIdent(c.kind, c.name); got != c.want {
			t.Fatalf("%s ident = %q, want %q", c.kind, got, c.want)
		}
	}
	if got := QuoteIdentFunc("mysql")("x"); got != "`x`" {
		t.Fatalf("QuoteIdentFunc = %q", got)
	}
	if got := QuoteString("mysql", "a\\b'c\nd"); got != `'a\\b''c\nd'` {
		t.Fatalf("mysql literal = %q", got)
	}
	if got := QuoteString("postgres", "a\\b'c\nd"); got != `'a\b''c
d'` {
		t.Fatalf("pg literal = %q", got)
	}
}
