package engine

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
}
