package engine

// QueryResult 是一次语句执行的结果：查询结果集，或无结果集语句的受影响行数。
type QueryResult struct {
	Columns   []string
	Rows      [][]any
	Truncated bool
	IsQuery   bool
	Affected  int64
}
