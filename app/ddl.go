package app

import "strings"

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
func FirstSQLKeyword(sql string) string {
	i := 0
	for i < len(sql) {
		switch {
		case isSQLSpace(sql[i]):
			i++
		case sql[i] == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case sql[i] == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			for i < len(sql) && !(sql[i] == '*' && i+1 < len(sql) && sql[i+1] == '/') {
				i++
			}
			if i < len(sql) {
				i += 2
			}
		default:
			start := i
			for i < len(sql) && isSQLWordPart(sql[i]) {
				i++
			}
			return strings.ToUpper(sql[start:i])
		}
	}
	return ""
}
