package sql

import "testing"

// 纯 SQL 词法/作用域逻辑必须能在不依赖 mygo/ui 的叶包里使用。
func TestScopeAPIIsPure(t *testing.T) {
	text := "with c as (select * from base) select u.id from users u join orders o on o.uid = u.id"
	scope, _ := StatementScope(text, len(text))
	if scope == "" {
		t.Fatal("empty scope")
	}
	rels := Relations(scope)
	if len(rels) != 2 {
		t.Fatalf("relations = %+v, want users/orders", rels)
	}
	alias := AliasMap(rels)
	if alias["u"] != "users" || alias["o"] != "orders" {
		t.Fatalf("alias map = %+v", alias)
	}
	if !RelationPositionAt("select * from ", len("select * from ")) {
		t.Fatal("relation position expected after FROM")
	}
	if toks := Tokenize("select 1 -- c\nfrom t"); len(toks) != 3 {
		t.Fatalf("tokens = %+v", toks)
	}
	if !IsIdentStart('a') || !IsIdentPart('1') || FirstWord(" select 1") != "select" {
		t.Fatal("identifier helpers")
	}
}
