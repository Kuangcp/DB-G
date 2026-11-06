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

// colListIndent 是 CREATE TABLE 列定义里的缩进（两个空格）。
const colListIndent = 2

// formatOne 格式化一段文本；ok=false 表示结构可疑、应保持原样。
func formatOne(text string) (string, int, bool) {
	var b strings.Builder
	depth := 0
	i := 0
	pendingSpace := false
	atLineStart := true
	colDepth := 0          // >0 表示正在 CREATE TABLE 的列定义括号里（值为该括号的深度）
	lineIndent := 0        // 行首缩进空格数
	var stmtWords []string // 当前语句已出现的词（大写），用来认出 CREATE TABLE
	ourBreak := false      // 上一件事就是本函数自己断的行（用来吸掉输入里的重复换行）

	// writeIndent 在行首补缩进并把行标记为已开始。
	writeIndent := func() {
		if atLineStart && lineIndent > 0 {
			b.WriteString(strings.Repeat(" ", lineIndent))
		}
		atLineStart = false
	}
	// emit 写一段原样内容（行首会补缩进）。
	emit := func(s string) {
		writeIndent()
		b.WriteString(s)
		ourBreak = false
	}
	writeSpace := func() {
		if atLineStart {
			return
		}
		b.WriteByte(' ')
		atLineStart = false
	}
	// newline 断行并清掉行尾空格；缩进由 writeIndent 在下一行补。
	// 已经在行首时不再多断一次（保证反复格式化结果稳定）。
	newline := func() {
		trimTrailingSpace(&b)
		if !atLineStart {
			b.WriteByte('\n')
		}
		atLineStart = true
		pendingSpace = false
		ourBreak = true
	}
	// isColumnList 报告当前这个 "(“ 是否打开了 CREATE TABLE 的列定义。
	isColumnList := func() bool {
		if depth != 1 || len(stmtWords) == 0 || stmtWords[0] != "CREATE" {
			return false
		}
		table := false
		for _, w := range stmtWords {
			if w == "AS" {
				return false // CTAS：括号是查询里的，不是列定义
			}
			if w == "TABLE" {
				table = true
			}
		}
		return table
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
			emit(text[i:end])
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
			emit(text[i:end])
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
			emit(text[i:end])
			i = end
		case c == '\n':
			if ourBreak {
				// 本函数刚断过行，输入里的那个换行是重复的。
				ourBreak = false
				i++
				continue
			}
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
			emit("(")
			if isColumnList() {
				// 列定义各占一行，相对 CREATE 缩进两格。
				colDepth = depth
				lineIndent = colListIndent
				newline()
			}
			i++
		case c == ')':
			depth--
			if depth < 0 {
				return "", 0, false
			}
			if colDepth > 0 && depth < colDepth {
				// 列定义结束：右括号另起一行、顶格。
				colDepth = 0
				lineIndent = 0
				newline()
				emit(")")
			} else {
				if pendingSpace {
					writeSpace()
					pendingSpace = false
				}
				emit(")")
			}
			stmtWords = nil
			i++
		case c == ',':
			if pendingSpace {
				writeSpace()
				pendingSpace = false
			}
			emit(",")
			if colDepth > 0 && depth == colDepth {
				newline()
			}
			i++
		case isSQLWordStart(c):
			end := i
			for end < len(text) && isSQLWordPart(text[end]) {
				end++
			}
			word := strings.ToUpper(text[i:end])
			if depth == 0 && formatClauseStarts[word] {
				if !atLineStart {
					newline()
				}
			} else if pendingSpace {
				writeSpace()
			}
			pendingSpace = false
			if formatKeywords[word] {
				emit(word)
			} else {
				emit(text[i:end])
			}
			if depth == 0 {
				stmtWords = append(stmtWords, word)
			}
			i = end
		default:
			if pendingSpace {
				writeSpace()
				pendingSpace = false
			}
			if c == ';' {
				stmtWords = nil // 语句结束，后面的 CREATE 重新认
			}
			emit(string(c))
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
