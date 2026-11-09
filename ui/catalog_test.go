package ui

import (
	"strings"
	"testing"
	"time"

	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/engine"
)

func TestCatalogCacheHitAndTTL(t *testing.T) {
	c := newCatalogCache()
	clock := time.Unix(1000, 0)
	c.now = func() time.Time { return clock }
	if !c.begin("conn") {
		t.Fatal("first load must start")
	}
	if c.begin("conn") {
		t.Fatal("concurrent load must not start twice")
	}
	c.finish("conn", []string{"orders"}, []string{"v_orders"}, "")
	items, ok := c.get("conn")
	if !ok || len(items) == 0 {
		t.Fatalf("cache miss after finish: %v", ok)
	}
	var hasOrders, hasView bool
	for _, it := range items {
		if it.Label == "orders" && it.Detail == "表" {
			hasOrders = true
		}
		if it.Label == "v_orders" && it.Detail == "视图" {
			hasView = true
		}
	}
	if !hasOrders || !hasView {
		t.Fatalf("items: %+v", items)
	}
	clock = clock.Add(6 * time.Minute)
	if _, ok := c.get("conn"); ok {
		t.Fatal("expired entry must miss")
	}
	if !c.begin("conn") {
		t.Fatal("expired entry must allow a reload")
	}
}

func TestCatalogIsolatesConnections(t *testing.T) {
	c := newCatalogCache()
	c.begin("a")
	c.finish("a", []string{"only_a"}, nil, "")
	if _, ok := c.get("b"); ok {
		t.Fatal("connections must not share a catalog")
	}
	items, _ := c.get("a")
	found := false
	for _, it := range items {
		if it.Label == "only_a" {
			found = true
		}
	}
	if !found {
		t.Fatal("conn a lost its tables")
	}
}

func TestCatalogFailureAllowsRetry(t *testing.T) {
	c := newCatalogCache()
	if !c.begin("x") {
		t.Fatal("begin")
	}
	c.fail("x")
	if _, ok := c.get("x"); ok {
		t.Fatal("failure must not be cached")
	}
	if !c.begin("x") {
		t.Fatal("failure must allow retry")
	}
}

func TestCatalogInvalidate(t *testing.T) {
	c := newCatalogCache()
	c.begin("a")
	c.finish("a", []string{"t"}, nil, "")
	c.invalidate("a")
	if _, ok := c.get("a"); ok {
		t.Fatal("invalidate must drop the entry")
	}
}

func TestBuildCatalogItemsKeepsWordsFirst(t *testing.T) {
	words := sqlWordItems()
	items := buildCatalogItems([]string{"orders"}, []string{"v"}, "")
	if len(items) <= len(words) {
		t.Fatalf("tables/views missing: %d", len(items))
	}
	if items[0].Label != words[0].Label {
		t.Fatal("words must come first")
	}
	last := items[len(items)-1]
	if last.Label != "v" || last.Detail != "视图" {
		t.Fatalf("last item: %+v", last)
	}
	// 返回的是副本：改动结果不得污染共享词表
	items[0].Label = "MUTATED"
	if sqlWordItems()[0].Label == "MUTATED" {
		t.Fatal("catalog items must not alias the shared word list")
	}
}

func TestCatalogHasRelationAndColumns(t *testing.T) {
	c := newCatalogCache()
	clock := time.Unix(2000, 0)
	c.now = func() time.Time { return clock }
	c.begin("conn")
	c.finish("conn", []string{"users"}, []string{"v_users"}, "")
	if !c.hasRelation("conn", "users") || !c.hasRelation("conn", "v_users") {
		t.Fatal("tables and views must be relations")
	}
	if c.hasRelation("conn", "nope") || c.hasRelation("other", "users") {
		t.Fatal("unknown relation or connection must miss")
	}
	if !c.hasRelation("conn", "USERS") {
		t.Fatal("relation lookup must ignore case, as SQL identifiers do")
	}
	if !c.beginColumns("conn", "users") {
		t.Fatal("first column load must start")
	}
	if c.beginColumns("conn", "users") {
		t.Fatal("column load must dedupe")
	}
	c.finishColumns("conn", "users", []engine.Column{
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "name", Type: "TEXT", Nullable: true},
	}, "")
	items, ok := c.getColumns("conn", "users")
	if !ok {
		t.Fatal("columns must be cached")
	}
	var id, name bool
	for _, it := range items {
		if it.Label == "id" && it.Detail == "INTEGER" && it.Kind == myeditor.CompletionField {
			id = true
		}
		if it.Label == "name" {
			name = true
		}
	}
	if !id || !name {
		t.Fatalf("items: %+v", items)
	}
	clock = clock.Add(6 * time.Minute)
	if _, ok := c.getColumns("conn", "users"); ok {
		t.Fatal("expired columns must miss")
	}
}

func TestCatalogColumnFailureAllowsRetry(t *testing.T) {
	c := newCatalogCache()
	c.beginColumns("c", "t")
	c.failColumns("c", "t")
	if _, ok := c.getColumns("c", "t"); ok {
		t.Fatal("failure must not be cached")
	}
	if !c.beginColumns("c", "t") {
		t.Fatal("failure must allow retry")
	}
}

func TestCatalogInvalidateDropsColumns(t *testing.T) {
	c := newCatalogCache()
	c.beginColumns("c", "t")
	c.finishColumns("c", "t", []engine.Column{{Name: "x"}}, "")
	c.invalidate("c")
	if _, ok := c.getColumns("c", "t"); ok {
		t.Fatal("invalidate must drop columns too")
	}
}

func TestBuildColumnItemsColumnsFirst(t *testing.T) {
	items := buildColumnItems([]engine.Column{{Name: "zz_col", Type: "TEXT"}, {Name: "id", Type: "INTEGER"}}, "")
	if len(items) < 3 {
		t.Fatalf("items: %d", len(items))
	}
	if items[0].Label != "id" || items[1].Label != "zz_col" {
		t.Fatalf("columns must come first, sorted: %+v %+v", items[0], items[1])
	}
	if items[0].Kind != myeditor.CompletionField || items[0].Detail != "INTEGER" {
		t.Fatalf("column item: %+v", items[0])
	}
}

func TestBuildStatementItemsMultiTable(t *testing.T) {
	items := buildStatementItems([]string{"a", "b"}, [][]engine.Column{
		{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}},
		{{Name: "id", Type: "INTEGER"}, {Name: "bcol", Type: "TEXT"}},
	}, "")
	var name, bcol, ids []string
	for _, it := range items {
		switch it.Label {
		case "name":
			name = append(name, it.Detail)
		case "bcol":
			bcol = append(bcol, it.Detail)
		case "id":
			ids = append(ids, it.Detail)
		}
	}
	if len(name) != 1 || !strings.Contains(name[0], "a") {
		t.Fatalf("name detail: %v", name)
	}
	if len(bcol) != 1 || !strings.Contains(bcol[0], "b") {
		t.Fatalf("bcol detail: %v", bcol)
	}
	if len(ids) != 2 {
		t.Fatalf("same column in two tables must show both: %v", ids)
	}
	if items[0].Kind != myeditor.CompletionField {
		t.Fatalf("columns must come first: %+v", items[0])
	}
}

func TestBuildStatementItemsSingleTableDetail(t *testing.T) {
	items := buildStatementItems([]string{"a"}, [][]engine.Column{{{Name: "name", Type: "TEXT"}}}, "")
	if items[0].Label != "name" || items[0].Detail != "TEXT" {
		t.Fatalf("single table detail must be the type: %+v", items[0])
	}
}

func TestGetColumnsMany(t *testing.T) {
	c := newCatalogCache()
	c.finishColumns("c", "a", []engine.Column{{Name: "x"}}, "")
	cols, ok := c.getColumnsMany("c", []string{"a"})
	if !ok || len(cols) != 1 || cols[0][0].Name != "x" {
		t.Fatalf("cols=%v ok=%v", cols, ok)
	}
	if _, ok := c.getColumnsMany("c", []string{"a", "b"}); ok {
		t.Fatal("missing table must report not-all-hit")
	}
}
