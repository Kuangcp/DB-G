package ui

import (
	"testing"

	myeditor "github.com/egoist/mygo/ui/editor"
)

func TestSQLWordItems(t *testing.T) {
	items := sqlWordItems()
	if len(items) < 80 {
		t.Fatalf("词表太小: %d", len(items))
	}
	seen := map[string]bool{}
	byLabel := map[string]myeditor.CompletionKind{}
	for _, it := range items {
		if it.Label == "" {
			t.Fatal("empty label")
		}
		if seen[it.Label] {
			t.Fatalf("重复词条: %q", it.Label)
		}
		seen[it.Label] = true
		byLabel[it.Label] = it.Kind
	}
	for _, want := range []string{"SELECT", "select", "FROM", "WHERE", "JOIN", "NULL"} {
		if !seen[want] {
			t.Fatalf("缺少 %q", want)
		}
	}
	if byLabel["count"] != myeditor.CompletionFunction || byLabel["COUNT"] != myeditor.CompletionFunction {
		t.Fatalf("count 应为函数: %v", byLabel["COUNT"])
	}
	if byLabel["varchar"] != myeditor.CompletionType && byLabel["VARCHAR"] != myeditor.CompletionType {
		t.Fatalf("varchar 应为类型: %v", byLabel["VARCHAR"])
	}
}
