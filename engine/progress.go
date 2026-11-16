package engine

import "context"

// QueryProgress 是一条查询已经读到的数据量，供 UI 在执行中显示进度。
type QueryProgress struct {
	Rows  int
	Bytes int64
}

type progressKey struct{}

// WithProgress 给 ctx 挂一个进度回调；驱动在扫描行时通过 ReportProgress 上报。
// 不设置回调时 ReportProgress 是空操作，Session 接口不需要因此变化。
func WithProgress(ctx context.Context, fn func(QueryProgress)) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, fn)
}

// ReportProgress 上报当前已读取的行数/字节数；没有挂回调时忽略。
func ReportProgress(ctx context.Context, p QueryProgress) {
	if fn, ok := ctx.Value(progressKey{}).(func(QueryProgress)); ok && fn != nil {
		fn(p)
	}
}
