package sqlite

import (
	"context"
	"strings"
	"testing"

	"db-g/engine"
)

func TestObjectStats(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	for _, q := range []string{
		"CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)",
		"CREATE INDEX idx_t_name ON t(name)",
		"INSERT INTO t (name) VALUES ('a'), ('b')",
	} {
		if _, err := s.RunStatement(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	statter, ok := s.(engine.SchemaObjectStatter)
	if !ok || !s.Caps().Has(engine.CapObjectStats) {
		t.Fatal("sqlite must provide object stats")
	}
	stats, err := statter.ObjectStatsIn(ctx, "", engine.ObjectTable, "t")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rows != nil {
		t.Fatalf("rows must stay unknown before ANALYZE: %v", *stats.Rows)
	}
	if !strings.Contains(stats.SQL[0], "sqlite_stat1") {
		t.Fatalf("row estimate must use sqlite_stat1: %q", stats.SQL)
	}
	if stats.Columns == nil || *stats.Columns != 2 {
		t.Fatalf("columns = %v", stats.Columns)
	}
	if stats.DataBytes == nil || *stats.DataBytes <= 0 {
		t.Fatalf("data bytes = %v", stats.DataBytes)
	}
	if stats.IndexBytes == nil || *stats.IndexBytes <= 0 {
		t.Fatalf("index bytes = %v", stats.IndexBytes)
	}
	if !strings.Contains(stats.ExactRowsSQL, "COUNT(*)") {
		t.Fatalf("exact rows sql = %q", stats.ExactRowsSQL)
	}
	for _, q := range stats.SQL {
		if strings.Contains(strings.ToUpper(q), `COUNT(*) FROM "T"`) {
			t.Fatalf("statistics must not scan the table: %q", q)
		}
	}
	// ANALYZE 之后 sqlite_stat1 才有估算值。
	if _, err := s.RunStatement(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}
	stats, err = statter.ObjectStatsIn(ctx, "", engine.ObjectTable, "t")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rows == nil || *stats.Rows != 2 {
		t.Fatalf("estimated rows after ANALYZE = %v", stats.Rows)
	}
	if _, err := statter.ObjectStatsIn(ctx, "", engine.ObjectView, "t"); err == nil {
		t.Fatal("non-table must be rejected")
	}
}
