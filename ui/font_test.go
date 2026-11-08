package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

// 结果区字体设置要真的落到单元格上：字号变大后同一段文字的布局宽度随之变大。
func TestResultFontSizeAffectsCells(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true,
		Columns: []string{"id", "name"},
		Rows:    [][]any{{int64(1), "alice"}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	for range 3 {
		tt.Frame()
	}
	small, ok := tt.Find("alice")
	if !ok {
		t.Fatalf("默认结果里找不到单元格: %q", tt.Texts())
	}
	a.fontPrefs.Result.Size = 24
	for range 3 {
		tt.Frame()
	}
	big, ok := tt.Find("alice")
	if !ok {
		t.Fatal("放大结果区字体后单元格消失")
	}
	if big.W <= small.W {
		t.Fatalf("结果区字号没生效: 默认宽 %v，放大后 %v", small.W, big.W)
	}
}
