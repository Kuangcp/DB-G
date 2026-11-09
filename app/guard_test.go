package app

import "testing"

func TestCheckSQLRisks(t *testing.T) {
	cases := []struct {
		sql   string
		rules []string
	}{
		{"UPDATE users SET name = 'x'", []string{"UPDATE-without-where"}},
		{"DELETE FROM users", []string{"DELETE-without-where"}},
		{"DELETE FROM users WHERE 1=1", []string{"DELETE-tautology"}},
		{"UPDATE users SET name = 'x' WHERE id = 1", nil},
		{"DELETE FROM users WHERE id = 1", nil},
		{"DROP TABLE users", []string{"drop"}},
		{"TRUNCATE TABLE users", []string{"truncate"}},
		{"SELECT * FROM users WHERE 1=1", nil},
		{"UPDATE users SET note = 'WHERE 1=1'", []string{"UPDATE-without-where"}},
	}
	for _, c := range cases {
		got := CheckSQLRisks(c.sql)
		if len(got) != len(c.rules) {
			t.Fatalf("%q: got %+v", c.sql, got)
		}
		for i, r := range c.rules {
			if got[i].Rule != r {
				t.Fatalf("%q: got %+v want %v", c.sql, got, c.rules)
			}
		}
	}
}
