package app

import "testing"

func TestQueryPageSQL(t *testing.T) {
	if body, ok := PageBody("  SELECT 1;  "); !ok || body != "SELECT 1" {
		t.Fatalf("body = %q %v", body, ok)
	}
	if _, ok := PageBody("UPDATE t SET x=1"); ok {
		t.Fatal("non-select must not page")
	}
	sql, ok := PageSQL("SELECT 1", 10, 5)
	if !ok || sql != "SELECT * FROM (SELECT 1) AS dbg LIMIT 5 OFFSET 10" {
		t.Fatalf("page sql = %q %v", sql, ok)
	}
	cnt, ok := PageCountSQL("sqlite", "SELECT 1;")
	if !ok || cnt != "SELECT COUNT(*) FROM (SELECT 1) AS dbg_count" {
		t.Fatalf("count = %q %v", cnt, ok)
	}
}
