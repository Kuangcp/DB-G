package ui

import "testing"

// 导出进度/确认状态收进 exportState，不再散落在 root 的 export* 字段上。
func TestExportStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.exportState.Format = "json"
	a.exportState.Busy = true
	a.exportState.Rows.Store(42)
	if a.exportState.Format != "json" || !a.exportState.Busy {
		t.Fatalf("format=%q busy=%v", a.exportState.Format, a.exportState.Busy)
	}
	if got := a.exportState.Rows.Load(); got != 42 {
		t.Fatalf("rows = %d", got)
	}
}
