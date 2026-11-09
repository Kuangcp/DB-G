package app

import (
	"strings"
)

// SQLRisk 是高危 SQL 检查结果；Level 2 表示默认就应重点提醒。
type SQLRisk struct {
	Level   int
	Rule    string
	Message string
}

// CheckSQLRisks 对一段 SQL 做确定性检查（不调用模型、不执行语句）。
func CheckSQLRisks(sql string) []SQLRisk {
	var out []SQLRisk
	for _, st := range SplitStatements(sql) {
		body := strings.TrimSpace(sql[st.Start:st.End])
		if body == "" {
			continue
		}
		out = append(out, checkStatementRisk(body)...)
	}
	return out
}

func checkStatementRisk(sql string) []SQLRisk {
	toks := lexSQL(sql)
	if len(toks) == 0 {
		return nil
	}
	head := strings.ToUpper(toks[0].text)
	switch head {
	case "UPDATE", "DELETE":
		where := topLevelToken(toks, "WHERE")
		if where < 0 {
			return []SQLRisk{{
				Level: 2, Rule: head + "-without-where",
				Message: head + " 没有 WHERE 条件，可能影响整表。",
			}}
		}
		rest := strings.TrimSpace(sql[toks[where].end:])
		if isTautology(rest) {
			return []SQLRisk{{
				Level: 2, Rule: head + "-tautology",
				Message: head + " 的 WHERE 条件是恒真条件，等同于没有过滤。",
			}}
		}
	case "DROP":
		return []SQLRisk{{Level: 2, Rule: "drop", Message: "DROP 会删除对象且通常不可恢复。"}}
	case "TRUNCATE":
		return []SQLRisk{{Level: 2, Rule: "truncate", Message: "TRUNCATE 会清空整张表。"}}
	}
	return nil
}

func isTautology(rest string) bool {
	s := strings.ToUpper(strings.TrimSpace(rest))
	s = strings.TrimSuffix(strings.TrimSpace(s), ";")
	s = strings.Join(strings.Fields(s), " ")
	switch s {
	case "1=1", "1 = 1", "TRUE", "'1'='1'", "'A'='A'":
		return true
	}
	return false
}

func topLevelToken(toks []sqlToken, want string) int {
	depth := 0
	for i, tok := range toks {
		if tok.kind == 'p' {
			switch tok.text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			}
			continue
		}
		if depth == 0 && strings.EqualFold(tok.text, want) {
			return i
		}
	}
	return -1
}

// sqlToken 是 app 内部的轻量词：标识符/数字 'i'、标点 'p'。
// 字符串与注释整体跳过，避免把 WHERE 1=1 里的内容误判。
type sqlToken struct {
	kind       byte
	text       string
	start, end int
}

func lexSQL(s string) []sqlToken {
	var out []sqlToken
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i < len(s) && !(s[i] == '*' && i+1 < len(s) && s[i+1] == '/') {
				i++
			}
			if i < len(s) {
				i += 2
			}
		case c == '\'' || c == '"' || c == '`':
			i = skipSQLQuoted(s, i, c)
		case isSQLWordStart(c):
			start := i
			for i < len(s) && isSQLWordPart(s[i]) {
				i++
			}
			out = append(out, sqlToken{kind: 'i', text: s[start:i], start: start, end: i})
		case c >= '0' && c <= '9':
			start := i
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
				i++
			}
			out = append(out, sqlToken{kind: 'i', text: s[start:i], start: start, end: i})
		default:
			out = append(out, sqlToken{kind: 'p', text: string(c)})
			i++
		}
	}
	return out
}
