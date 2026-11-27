package app

import (
	"fmt"
	"strings"

	"db-g/engine"
	"db-g/sql"
)

// PageBody 返回可被派生表包裹的原始 SELECT/WITH 正文；其他语句不可分页。
func PageBody(query string) (string, bool) {
	stmts := SplitStatements(query)
	if len(stmts) != 1 {
		return "", false
	}
	body := strings.TrimSpace(query[stmts[0].Start:stmts[0].End])
	head := strings.ToUpper(sql.FirstWord(body))
	if head != "SELECT" && head != "WITH" {
		return "", false
	}
	return body, true
}

// PageSQL 把一条 SELECT/WITH 语句包成派生表再取某一页。
func PageSQL(query string, offset, limit int) (string, bool) {
	body, ok := PageBody(query)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("SELECT * FROM (%s) AS dbg LIMIT %d OFFSET %d", body, limit, offset), true
}

// PageCountSQL 生成统计原始查询总行数的语句；方言计数表达式来自驱动元数据。
func PageCountSQL(kind, query string) (string, bool) {
	body, ok := PageBody(query)
	if !ok {
		return "", false
	}
	fn := "COUNT(*)"
	if m, ok := engine.Meta(kind); ok && m.CountExpr != "" {
		fn = m.CountExpr
	}
	return fmt.Sprintf("SELECT %s FROM (%s) AS dbg_count", fn, body), true
}
