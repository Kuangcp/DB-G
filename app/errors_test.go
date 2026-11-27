package app

import (
	"errors"
	"testing"

	"db-g/engine"
)

// 引擎/驱动的结构化错误按语言展示；数据库原始错误原样透出。
func TestErrorText(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{engine.NewUserError(engine.ErrNoSchemaSelected, ""), "未选择 schema"},
		{engine.NewUserError(engine.ErrDDLNotFound, "orders"), "未找到 orders 的 DDL"},
		{engine.NewUserError(engine.ErrAnalyzeUnsupported, ""), "该数据源不支持 EXPLAIN ANALYZE"},
		{engine.NewUserError(engine.ErrParamsFormat, "bogus"), "连接参数格式应为 key=value: bogus"},
	}
	for _, c := range cases {
		if got := ErrorText(c.err); got != c.want {
			t.Fatalf("ErrorText(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	raw := errors.New("ERROR: duplicate key value violates unique constraint")
	if got := ErrorText(raw); got != raw.Error() {
		t.Fatalf("raw database error must pass through, got %q", got)
	}
	if got := ErrorText(nil); got != "" {
		t.Fatalf("nil error = %q", got)
	}
}
