package app

import "strings"

// formatKeywords 会被大写的关键字（只影响大小写，不改变语义）。
var formatKeywords = func() map[string]bool {
	words := `select from where group by order having limit offset insert into values update set
	delete join left right inner outer full cross on using union all as and or not null is in exists
	between like case when then else end create table view index drop alter add column primary key
	foreign references default unique check constraint begin commit rollback with recursive distinct
	asc desc returning if replace temporary temp database schema grant revoke cascade do nothing
	natural explain analyze pragma vacuum`
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[strings.ToUpper(w)] = true
	}
	return m
}()

// formatClauseStarts 在这些关键字前换行（仅顶层括号深度）。
var formatClauseStarts = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "GROUP": true, "ORDER": true, "HAVING": true,
	"LIMIT": true, "OFFSET": true, "UNION": true, "EXCEPT": true, "INTERSECT": true,
	"INSERT": true, "UPDATE": true, "DELETE": true, "VALUES": true, "SET": true,
	"JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true, "FULL": true,
	"CROSS": true, "NATURAL": true, "WITH": true, "CREATE": true, "DROP": true, "ALTER": true,
	"RETURNING": true, "ON": true,
}

// FormatSQL 做保守格式化：只改空白与关键字大小写，字符串/注释原样保留。
// 结构看不懂（引号/括号不平衡）时返回原文。
func FormatSQL(sql string) (string, error) {
	out, _, ok := formatOne(sql)
	if !ok {
		return sql, nil
	}
	return out, nil
}

// formatOne 格式化一段文本；ok=false 表示结构可疑、应保持原样。
func formatOne(text string) (string, int, bool) {
	var b strings.Builder
	depth := 0
	i := 0
	pendingSpace := false
	atLineStart := true

	writeSpace := func() {
		if atLineStart {
			return
		}
		b.WriteByte(' ')
		atLineStart = false
	}
	for i < len(text) {
		c := text[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			if pendingSpace {
				writeSpace()
				pendingSpace = false
			}
			end := skipSQLQuoted(text, i, c)
			if end == len(text) && text[end-1] != c {
				return "", 0, false // 引号未闭合
			}
			b.WriteString(text[i:end])
			atLineStart = false
			i = end
		case c == '-' && i+1 < len(text) && text[i+1] == '-':
			if pendingSpace {
				writeSpace()
				pendingSpace = false
			}
			end := i
			for end < len(text) && text[end] != '\n' {
				end++
			}
			b.WriteString(text[i:end])
			atLineStart = false
			i = end
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			if pendingSpace {
				writeSpace()
				pendingSpace = false
			}
			end := i + 2
			for end < len(text) && !(text[end] == '*' && end+1 < len(text) && text[end+1] == '/') {
				end++
			}
			if end < len(text) {
				end += 2
			}
			b.WriteString(text[i:end])
			atLineStart = false
			i = end
		case c == '\n':
			b.WriteByte('\n')
			atLineStart = true
			pendingSpace = false
			i++
		case c == ' ' || c == '\t' || c == '\r':
			pendingSpace = true
			i++
		case c == '(':
			depth++
			if pendingSpace {
				writeSpace()
				pendingSpace = false
			}
			b.WriteByte(c)
			atLineStart = false
			i++
		case c == ')':
			depth--
			if depth < 0 {
				return "", 0, false
			}
			if pendingSpace {
				writeSpace()
				pendingSpace = false
			}
			b.WriteByte(c)
			atLineStart = false
			i++
		case isSQLWordStart(c):
			end := i
			for end < len(text) && isSQLWordPart(text[end]) {
				end++
			}
			word := strings.ToUpper(text[i:end])
			if depth == 0 && formatClauseStarts[word] {
				if !atLineStart {
					trimTrailingSpace(&b)
					b.WriteByte('\n')
					atLineStart = true
				}
			} else if pendingSpace {
				writeSpace()
			}
			pendingSpace = false
			if formatKeywords[word] {
				b.WriteString(word)
			} else {
				b.WriteString(text[i:end])
			}
			atLineStart = false
			i = end
		default:
			if pendingSpace {
				writeSpace()
				pendingSpace = false
			}
			b.WriteByte(c)
			atLineStart = false
			i++
		}
	}
	if depth != 0 {
		return "", 0, false
	}
	return strings.TrimSpace(b.String()), len(text), true
}

// trimTrailingSpace 去掉 Builder 末尾的空格（避免换行前留空格）。
func trimTrailingSpace(b *strings.Builder) {
	s := b.String()
	trimmed := strings.TrimRight(s, " \t")
	if len(trimmed) == len(s) {
		return
	}
	b.Reset()
	b.WriteString(trimmed)
}
