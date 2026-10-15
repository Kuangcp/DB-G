package ui

import "strings"

// buildSQL returns "lines" lines of line-oriented SQL. Each line is a
// self-contained statement fragment with keywords, identifiers, numbers,
// quoted strings (incl. non-ASCII) and a trailing -- comment, so line-level
// tokenization is exact. Block comments / multi-line strings are out of
// scope by design (see plan notes).
func buildSQL(lines int) string {
	var b strings.Builder
	cols := []string{"id", "customer_name", "region", "amount", "created_at", "status"}
	for i := 0; i < lines; i++ {
		if i%8 == 0 || i%8 == 4 {
			b.WriteString("INSERT INTO orders (")
			for j, c := range cols {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(c)
			}
			b.WriteString(") VALUES (1001, 2002, '华东大区', 99.99, '2026-01-01', 'open')")
			b.WriteString("; -- 用户")
		} else {
			b.WriteString("SELECT id, customer_name, amount FROM orders")
			b.WriteString(" WHERE status = 'open' AND amount > ")
			b.WriteByte(byte('0' + i%10))
			b.WriteString(".00; -- row 用户")
		}
		b.WriteByte('\n')
	}
	return b.String()
}