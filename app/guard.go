package app

import (
	"strings"

	"db-g/locales"
	"db-g/sql"
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

func checkStatementRisk(stmt string) []SQLRisk {
	toks := sql.LexCode(stmt)
	if len(toks) == 0 {
		return nil
	}
	head := strings.ToUpper(toks[0].Text)
	switch head {
	case "UPDATE", "DELETE":
		where := topLevelToken(toks, "WHERE")
		if where < 0 {
			return []SQLRisk{{
				Level: 2, Rule: head + "-without-where",
				Message: locales.AppGuardNoWhere(head),
			}}
		}
		rest := strings.TrimSpace(stmt[toks[where].End:])
		if isTautology(rest) {
			return []SQLRisk{{
				Level: 2, Rule: head + "-tautology",
				Message: locales.AppGuardTruthyWhere(head),
			}}
		}
	case "DROP":
		return []SQLRisk{{Level: 2, Rule: "drop", Message: locales.AppGuardDrop()}}
	case "TRUNCATE":
		return []SQLRisk{{Level: 2, Rule: "truncate", Message: locales.AppGuardTruncate()}}
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

func topLevelToken(toks []sql.LexToken, want string) int {
	depth := 0
	for i, tok := range toks {
		if tok.Kind == sql.LexPunct {
			switch tok.Text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			}
			continue
		}
		if depth == 0 && strings.EqualFold(tok.Text, want) {
			return i
		}
	}
	return -1
}
