package engine

import "context"

// 查询结果的大小预算：驱动每读完一行把文本/字节单元格累加，超过后停止读取
// 并置 QueryResult.Truncated。默认 8 MiB，可在设置里调整。
// 预算挂在每次执行的 context 上，不是进程级全局状态。
const (
	DefaultResultBudgetMB = 8
	MinResultBudgetMB     = 1
	MaxResultBudgetMB     = 256
)

type resultBudgetKey struct{}

// WithResultBudget 给 ctx 挂本次执行的结果预算（MB）；越界值夹到允许范围。
func WithResultBudget(ctx context.Context, mb int) context.Context {
	if mb < MinResultBudgetMB {
		mb = MinResultBudgetMB
	}
	if mb > MaxResultBudgetMB {
		mb = MaxResultBudgetMB
	}
	return context.WithValue(ctx, resultBudgetKey{}, mb<<20)
}

// ResultBudget 返回本次执行的结果预算（字节）；未指定时用默认值。
// 驱动在读取结果时调用。
func ResultBudget(ctx context.Context) int {
	if b, ok := ctx.Value(resultBudgetKey{}).(int); ok && b > 0 {
		return b
	}
	return DefaultResultBudgetMB << 20
}
