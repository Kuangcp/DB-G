package app

import "testing"

func TestIsSchemaChangeSQL(t *testing.T) {
	cases := []struct {
		sql  string
		want bool
	}{
		{"ALTER TABLE t ADD COLUMN b TEXT", true},
		{"  alter table t drop column b", true},
		{"-- note\n/* x */\nCREATE TABLE t (id int)", true},
		{"DROP TABLE t", true},
		{"RENAME TABLE a TO b", true},
		{"TRUNCATE TABLE t", true},
		{"CREATE INDEX i ON t(a)", true},
		{"SELECT * FROM t", false},
		{"INSERT INTO t VALUES (1)", false},
		{"UPDATE t SET a = 1", false},
		{"DELETE FROM t", false},
		{"-- only a comment", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsSchemaChangeSQL(c.sql); got != c.want {
			t.Errorf("IsSchemaChangeSQL(%q) = %v, want %v", c.sql, got, c.want)
		}
	}
}

func TestFirstSQLKeyword(t *testing.T) {
	if got := FirstSQLKeyword(" -- c\n /* d */\n select 1"); got != "SELECT" {
		t.Fatalf("first keyword = %q", got)
	}
	if got := FirstSQLKeyword("  "); got != "" {
		t.Fatalf("empty keyword = %q", got)
	}
}
