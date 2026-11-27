package engine

import "strings"

// QuoteIdent 按数据源方言引用标识符：MySQL/ClickHouse 反引号，其余双引号。
func QuoteIdent(kind, name string) string {
	if kind == "mysql" || kind == "clickhouse" {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// QuoteIdentFunc 返回绑定某方言的标识符引用函数，供 DriverMeta 注册。
func QuoteIdentFunc(kind string) func(string) string {
	return func(name string) string { return QuoteIdent(kind, name) }
}

var defaultQuoteEscaper = strings.NewReplacer(
	`\`, `\\`,
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
	"\x00", `\0`,
)

// QuoteString 生成 SQL 字符串字面量。MySQL/ClickHouse 支持反斜杠转义，先把反斜杠与
// 控制字符转义；PostgreSQL/SQLite 保留反斜杠，只双写单引号。
func QuoteString(kind, s string) string {
	if kind == "mysql" || kind == "clickhouse" {
		s = defaultQuoteEscaper.Replace(s)
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
