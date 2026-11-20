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
	statter, ok := s.(engine.ObjectStatter)
	if !ok || !s.Caps().Has(engine.CapObjectStats) {
		t.Fatal("sqlite must provide object stats")
	}
	stats, err := statter.ObjectStats(ctx, engine.ObjectTable, "t")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rows == nil || *stats.Rows != 2 {
		t.Fatalf("rows = %v", stats.Rows)
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
	if len(stats.SQL) < 4 || !strings.Contains(stats.SQL[0], "COUNT(*)") {
		t.Fatalf("sql = %q", stats.SQL)
	}
	if _, err := statter.ObjectStats(ctx, engine.ObjectView, "t"); err == nil {
		t.Fatal("non-table must be rejected")
	}
}
