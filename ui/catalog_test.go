package ui

import (
	"testing"
	"time"
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
	c.finish("conn", []string{"orders"}, []string{"v_orders"})
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
	c.finish("a", []string{"only_a"}, nil)
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
	c.finish("a", []string{"t"}, nil)
	c.invalidate("a")
	if _, ok := c.get("a"); ok {
		t.Fatal("invalidate must drop the entry")
	}
}

func TestBuildCatalogItemsKeepsWordsFirst(t *testing.T) {
	words := sqlWordItems()
	items := buildCatalogItems([]string{"orders"}, []string{"v"})
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
