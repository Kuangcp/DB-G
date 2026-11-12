package engine

import "sync/atomic"

// 查询结果的大小预算：驱动每读完一行把文本/字节单元格累加，超过后停止读取
// 并置 QueryResult.Truncated。默认 8 MiB，可在设置里调整。
const (
	DefaultResultBudgetMB = 8
	MinResultBudgetMB     = 1
	MaxResultBudgetMB     = 256
)

var resultBudgetBytes atomic.Int64

func init() {
	resultBudgetBytes.Store(DefaultResultBudgetMB << 20)
}

// SetResultBudgetMB 设置结果大小预算（MB）；越界值夹到允许范围。
func SetResultBudgetMB(mb int) {
	if mb < MinResultBudgetMB {
		mb = MinResultBudgetMB
	}
	if mb > MaxResultBudgetMB {
		mb = MaxResultBudgetMB
	}
	resultBudgetBytes.Store(int64(mb) << 20)
}

// ResultBudgetBytes 返回当前结果大小预算（字节）。驱动在读取结果时调用。
func ResultBudgetBytes() int { return int(resultBudgetBytes.Load()) }
