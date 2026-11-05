package app

import (
	"strings"
	"unicode/utf8"
)

// SQLStatement 是一条语句在原文中的字节范围（不含分隔的 ';' 与首尾空白）。
type SQLStatement struct {
	Start, End int
}

// SplitStatements 按顶层 ';' 切语句：跳过字符串（'…'、"…"、`…`）、行注释（--）、
// 块注释（/*…*/）与触发器/存储过程的 BEGIN…END 体（仅当语句里出现 TRIGGER 时）。
// 空语句与只有注释的片段被丢弃。
func SplitStatements(text string) []SQLStatement {
	var out []SQLStatement
	start := 0
	depth := 0 // 括号深度
	block := 0 // BEGIN…END 深度（仅 TRIGGER 语句）
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			i = skipSQLQuoted(text, i, c)
			continue
		case c == '-' && i+1 < len(text) && text[i+1] == '-':
			for i < len(text) && text[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			i += 2
			for i < len(text) && !(text[i] == '*' && i+1 < len(text) && text[i+1] == '/') {
				i++
			}
			if i < len(text) {
				i += 2
			}
			continue
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case isSQLWordStart(c):
			end := i
			for end < len(text) && isSQLWordPart(text[end]) {
				end++
			}
			word := strings.ToUpper(text[i:end])
			switch word {
			case "BEGIN":
				if isTriggerPrefix(text[start:i]) {
					block++
				}
			case "END":
				if block > 0 {
					block--
				}
			}
			i = end
			continue
		case c == ';' && depth == 0 && block == 0:
			appendStatement(&out, text, start, i)
			start = i + 1
		}
		i++
	}
	appendStatement(&out, text, start, len(text))
	return out
}

// StatementAt 返回光标所在语句；光标落在空白/分隔符上时取前一条（没有则后一条）。
func StatementAt(text string, offset int) (SQLStatement, bool) {
	stmts := SplitStatements(text)
	if len(stmts) == 0 {
		return SQLStatement{}, false
	}
	if offset < 0 {
		offset = 0
	}
	for _, s := range stmts {
		if offset >= s.Start && offset <= s.End {
			return s, true
		}
	}
	var prev SQLStatement
	found := false
	for _, s := range stmts {
		if s.Start > offset {
			if found {
				return prev, true
			}
			return s, true
		}
		prev, found = s, true
	}
	return prev, found
}

// skipSQLQuoted 返回闭合引号之后的位置（未闭合则到结尾）；双写引号是转义。
func skipSQLQuoted(s string, i int, quote byte) int {
	i++
	for i < len(s) {
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(s)
}

// appendStatement 去掉首尾空白与「只有注释」的片段后记录。
func appendStatement(out *[]SQLStatement, text string, start, end int) {
	for start < end && isSQLSpace(text[start]) {
		start++
	}
	for end > start && isSQLSpace(text[end-1]) {
		end--
	}
	if start >= end || !hasSQLCode(text[start:end]) {
		return
	}
	*out = append(*out, SQLStatement{Start: start, End: end})
}

// hasSQLCode 判断片段里是否有非注释、非空白的代码。
func hasSQLCode(s string) bool {
	for i := 0; i < len(s); {
		switch {
		case isSQLSpace(s[i]):
			i++
		case s[i] == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i < len(s) && !(s[i] == '*' && i+1 < len(s) && s[i+1] == '/') {
				i++
			}
			if i < len(s) {
				i += 2
			}
		default:
			return true
		}
	}
	return false
}

// isTriggerPrefix 判断当前语句开头（到 BEGIN 之前）是否声明了触发器/存储过程，
// 这类语句的 BEGIN…END 体里的 ';' 不能当语句分隔。
func isTriggerPrefix(s string) bool {
	upper := strings.ToUpper(s)
	return strings.Contains(upper, "TRIGGER") || strings.Contains(upper, "PROCEDURE") ||
		strings.Contains(upper, "FUNCTION")
}

func isSQLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

func isSQLWordStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= utf8.RuneSelf
}

func isSQLWordPart(b byte) bool {
	return isSQLWordStart(b) || (b >= '0' && b <= '9') || b == '$'
}
