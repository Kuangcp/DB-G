package ui

import "sync/atomic"

// exportState 是结果导出的确认、进度与最近一次成功路径。
type exportState struct {
	Confirm      bool
	Format       string
	Status       string
	Path         string // 最近一次成功导出的文件，双击状态可打开所在目录
	Busy         bool
	Full         bool
	Rows         atomic.Int64
	CloseConfirm bool
}
