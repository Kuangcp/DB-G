package sql

import "strings"

// IsIdentStart 报告字节能否作为 SQL 标识符首字符（不含引号包裹的标识符）。
func IsIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// IsIdentPart 报告字节能否出现在标识符中间。
func IsIdentPart(b byte) bool {
	return IsIdentStart(b) || (b >= '0' && b <= '9') || b == '$'
}

// FirstWord 返回字符串里的第一个标识符词（跳过标点/空白）。
func FirstWord(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < len(s); i++ {
		if !IsIdentPart(s[i]) {
			return s[:i]
		}
	}
	return s
}
