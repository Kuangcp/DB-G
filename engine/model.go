package engine

import "time"

// QueryResult 是一次语句执行的结果：查询结果集，或无结果集语句的受影响行数。
type QueryResult struct {
	Columns []string
	// ColumnTypes 与 Columns 对齐，是驱动给出的数据库类型名（DatabaseTypeName），
	// 驱动不提供时为空。UI 用它区分 MySQL 的 DATE / DATETIME / TIMESTAMP 等。
	ColumnTypes []string
	Rows        [][]any
	Truncated   bool
	IsQuery     bool
	Affected    int64
	// ExecDuration 是驱动把语句交给数据库到拿到首批结果/执行完成的时间；
	// FetchDuration 是随后读取结果行的时间（不含执行排队）。
	ExecDuration  time.Duration
	FetchDuration time.Duration
}
