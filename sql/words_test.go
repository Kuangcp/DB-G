package sql

import "testing"

func TestWordTables(t *testing.T) {
	words := Words()
	if len(words) < 80 {
		t.Fatalf("words = %d", len(words))
	}
	seen := map[string]WordKind{}
	for _, w := range words {
		if w.Text == "" {
			t.Fatal("empty word")
		}
		if _, dup := seen[w.Text]; dup {
			t.Fatalf("duplicate %q", w.Text)
		}
		seen[w.Text] = w.Kind
	}
	if seen["SELECT"] != WordKeyword || seen["select"] != WordKeyword {
		t.Fatalf("SELECT kind = %v", seen["SELECT"])
	}
	if seen["integer"] != WordType {
		t.Fatalf("integer kind = %v", seen["integer"])
	}
	if seen["count"] != WordFunction {
		t.Fatalf("count kind = %v", seen["count"])
	}

	terms := DialectTerms("mysql")
	kinds := map[string]WordKind{}
	for _, w := range terms {
		kinds[w.Text] = w.Kind
	}
	if kinds["auto_increment"] != WordKeyword || kinds["json"] != WordType {
		t.Fatalf("mysql terms = %+v", kinds["auto_increment"])
	}
	if _, ok := kinds["prewhere"]; ok {
		t.Fatal("clickhouse-only term leaked into mysql")
	}
	if _, ok := DialectTermMap("clickhouse")["prewhere"]; !ok {
		t.Fatal("missing clickhouse term")
	}
	if !IsExtraKeyword("truncate") || IsExtraKeyword("select") {
		t.Fatal("extra keyword check")
	}
}
