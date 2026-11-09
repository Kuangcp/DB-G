package plan

import (
	"strings"
	"testing"
)

func TestParsePostgres(t *testing.T) {
	raw := `[{"Plan":{"Node Type":"Hash Join","Total Cost":42.5,"Plan Rows":10,"Actual Total Time":1.25,"Plans":[{"Node Type":"Seq Scan","Relation Name":"users","Total Cost":10,"Plan Rows":100}]}}]`
	nodes, err := Parse("postgres", []string{"QUERY PLAN"}, [][]any{{raw}})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Op != "Hash Join" || len(nodes[0].Children) != 1 {
		t.Fatalf("nodes: %+v", nodes)
	}
	if nodes[0].Cost != 42.5 || nodes[0].Children[0].Relation != "users" {
		t.Fatalf("values: %+v", nodes)
	}
	if !strings.Contains(Text(nodes), "Seq Scan") {
		t.Fatal("Text must render children")
	}
}

func TestParseMySQLJSON(t *testing.T) {
	raw := `{"query_block":{"cost_info":{"query_cost":"12.5"},"nested_loop":[{"table":{"table_name":"orders","access_type":"ALL","cost_info":{"read_cost":"3.0","eval_cost":"1.0"},"rows_examined_per_scan":9}}]}}`
	nodes, err := Parse("mysql", []string{"EXPLAIN"}, [][]any{{raw}})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || len(nodes[0].Children) != 1 || len(nodes[0].Children[0].Children) != 1 {
		t.Fatalf("nodes: %+v", nodes)
	}
	table := nodes[0].Children[0].Children[0]
	if table.Relation != "orders" || table.Rows != 9 || table.Cost != 4 {
		t.Fatalf("table: %+v", table)
	}
}

func TestParseSQLite(t *testing.T) {
	cols := []string{"id", "parent", "notused", "detail"}
	rows := [][]any{
		{int64(2), int64(0), int64(0), "SCAN users"},
		{int64(3), int64(2), int64(0), "SEARCH orders USING INDEX"},
	}
	nodes, err := Parse("sqlite", cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Op != "SCAN users" || len(nodes[0].Children) != 1 {
		t.Fatalf("nodes: %+v", nodes)
	}
}

func TestParseMySQLAnalyzeTree(t *testing.T) {
	raw := "-> Nested loop inner join  (cost=12.5 rows=3) (actual time=0.1 rows=3 loops=1)\n    -> Table scan on orders  (cost=3 rows=9) (actual time=0.05 rows=9 loops=1)"
	nodes, err := Parse("mysql", []string{"EXPLAIN"}, [][]any{{raw}})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || len(nodes[0].Children) != 1 {
		t.Fatalf("nodes: %+v", nodes)
	}
	if nodes[0].Cost != 12.5 || nodes[0].Children[0].Rows != 9 {
		t.Fatalf("tree values: %+v", nodes)
	}
}
