package app

import (
	"strings"

	"db-g/sql"
)

// IsSchemaChangeSQL 报告语句是否可能改变表/列/索引等元数据。
// 只看首个关键字（跳过空白与注释），覆盖常见 DDL：
// ALTER、CREATE、DROP、RENAME、TRUNCATE。数据变更（INSERT/UPDATE/DELETE）
// 不触发元数据失效。
func IsSchemaChangeSQL(sql string) bool {
	switch FirstSQLKeyword(sql) {
	case "ALTER", "CREATE", "DROP", "RENAME", "TRUNCATE":
		return true
	}
	return false
}

// FirstSQLKeyword 返回去掉前导空白与注释后的第一个 SQL 关键字（大写）。
func FirstSQLKeyword(text string) string {
	toks := sql.LexCode(text)
	if len(toks) == 0 {
		return ""
	}
	return strings.ToUpper(toks[0].Text)
}
