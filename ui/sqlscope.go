package ui

import (
	"strings"
)

// relationRef 是语句里出现的一张表；Alias 非空表示 FROM/JOIN 给了别名。
// Table 为空、Alias 非空表示派生表（FROM (SELECT …) d）：不提供列，只挡住目录回退。
type relationRef struct {
	Table string
	Alias string
}

// sqlToken 是词法扫描的最小单位：标识符（去掉引号包裹）、标点、或需要忽略的字面量。
type sqlToken struct {
	kind byte // 'i' ident, 'p' punct
	text string
}

// tokenizeSQL 做补全所需的词法近似：标识符、标点；字符串与注释整体跳过。
// 带引号的标识符（"x" / `x` / [x]）按标识符返回并去掉包裹。
func tokenizeSQL(s string) []sqlToken {
	var out []sqlToken
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
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
		case c == '\'':
			i = skipQuoted(s, i, '\'')
		case c == '"' || c == '`':
			end := skipQuoted(s, i, c)
			inner := s[i+1 : end-1]
			if end <= i+1 {
				inner = ""
			}
			if inner != "" {
				out = append(out, sqlToken{kind: 'i', text: inner})
			}
			i = end
		case c == '[': // MSSQL 风格的引号标识符（顺手支持）
			end := strings.IndexByte(s[i:], ']')
			if end < 0 {
				i = len(s)
				continue
			}
			if end > 1 {
				out = append(out, sqlToken{kind: 'i', text: s[i+1 : i+end]})
			}
			i += end + 1
		case isIdentStart(c):
			start := i
			for i < len(s) && isIdentPart(s[i]) {
				i++
			}
			out = append(out, sqlToken{kind: 'i', text: s[start:i]})
		case c >= '0' && c <= '9':
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
				i++
			}
		default:
			out = append(out, sqlToken{kind: 'p', text: string(c)})
			i++
		}
	}
	return out
}

// skipQuoted 返回闭合引号之后的位置（未闭合则到结尾）。
func skipQuoted(s string, i int, quote byte) int {
	i++
	for i < len(s) {
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote { // 双写转义
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(s)
}

// stmtScopeAt 返回光标所在的作用域文本：最内层包住光标的括号块（子查询），
// 没有括号时是光标所在的整条语句（按 ; 切，跳过字符串/注释）。offset 为字节偏移。
func stmtScopeAt(text string, offset int) string {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	start, end := statementBounds(text, offset)
	// 在语句内找最内层括号；同时记下不含光标的括号块（稍后抹掉，防止内层 FROM 泄漏到外层作用域）
	type span struct{ open, close int }
	var blanks []span
	depth := 0
	open := -1
	bestOpen, bestClose := -1, -1
	for i := start; i < end; {
		switch c := text[i]; c {
		case '\'', '"', '`':
			i = skipQuoted(text, i, c)
			continue
		case '-':
			if i+1 < end && text[i+1] == '-' {
				for i < end && text[i] != '\n' {
					i++
				}
				continue
			}
		case '/':
			if i+1 < end && text[i+1] == '*' {
				i += 2
				for i < end && !(text[i] == '*' && i+1 < end && text[i+1] == '/') {
					i++
				}
				if i < end {
					i += 2
				}
				continue
			}
		case '(':
			if open < 0 {
				open = i
			}
			depth++
		case ')':
			if depth > 0 {
				depth--
				if depth == 0 {
					if open < offset && offset <= i {
						bestOpen, bestClose = open, i
					} else {
						blanks = append(blanks, span{open: open, close: i})
					}
					open = -1
				}
			}
		}
		i++
	}
	if bestOpen >= 0 {
		return strings.TrimSpace(text[bestOpen+1 : bestClose])
	}
	// 外层作用域：把不含光标的括号块内容抹成空格，但保留括号本身（派生表解析仍能认出）。
	if len(blanks) == 0 {
		return strings.TrimSpace(text[start:end])
	}
	buf := []byte(text[start:end])
	for _, sp := range blanks {
		for k := sp.open + 1 - start; k < sp.close-start; k++ {
			buf[k] = ' '
		}
	}
	return strings.TrimSpace(string(buf))
}

// statementBounds 返回光标所在语句的 [start,end)（跳过字符串/注释，深度 0 的 ; 切分）。
func statementBounds(text string, offset int) (int, int) {
	start := 0
	depth := 0
	for i := 0; i < len(text); {
		switch c := text[i]; c {
		case '\'', '"', '`':
			i = skipQuoted(text, i, c)
			continue
		case '-':
			if i+1 < len(text) && text[i+1] == '-' {
				for i < len(text) && text[i] != '\n' {
					i++
				}
				continue
			}
		case '/':
			if i+1 < len(text) && text[i+1] == '*' {
				i += 2
				for i < len(text) && !(text[i] == '*' && i+1 < len(text) && text[i+1] == '/') {
					i++
				}
				if i < len(text) {
					i += 2
				}
				continue
			}
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				if i < offset {
					start = i + 1
				} else {
					return start, i
				}
			}
		}
		i++
	}
	return start, len(text)
}

// relationsIn 从作用域文本里提取表：FROM 列表、JOIN、UPDATE、INTO；
// CTE（WITH 名 AS (…)) 展开为其内部 FROM 的表（递归，深度上限 8，环保护）；
// 派生表（FROM (SELECT …) s）登记为 Table 空、Alias 非空的占位。
func relationsIn(scope string) []relationRef {
	return relationsInDepth(scope, nil, 0)
}

func relationsInDepth(scope string, inherited map[string][]relationRef, depth int) []relationRef {
	if depth > 8 {
		return nil
	}
	toks := tokenizeSQL(scope)
	cte := map[string][]relationRef{}
	for k, v := range inherited {
		cte[k] = v
	}

	i := 0
	// CTE 头：WITH [RECURSIVE] name AS ( … ) [, name AS ( … )]*
	if i < len(toks) && toks[i].kind == 'i' && strings.EqualFold(toks[i].text, "with") {
		i++
		if i < len(toks) && toks[i].kind == 'i' && strings.EqualFold(toks[i].text, "recursive") {
			i++
		}
		for i < len(toks) {
			name := toks[i].text
			if i+1 >= len(toks) || !strings.EqualFold(toks[i+1].text, "as") {
				break
			}
			open := i + 2
			if open >= len(toks) || toks[open].text != "(" {
				break
			}
			body, next := parenBody(toks, open)
			cte[strings.ToLower(name)] = relationsInDepth(body, cte, depth+1)
			i = next
			if i < len(toks) && toks[i].text == "," {
				i++
				continue
			}
			break
		}
	}

	var out []relationRef
	seen := map[string]bool{}
	add := func(table, alias string) {
		if table != "" {
			for _, r := range cte[strings.ToLower(table)] {
				add2(&out, seen, r.Table, r.Alias)
			}
		}
		if table != "" && cte[strings.ToLower(table)] != nil {
			return
		}
		add2(&out, seen, table, alias)
	}
	for ; i < len(toks); i++ {
		tok := toks[i]
		if tok.kind != 'i' {
			continue
		}
		switch strings.ToUpper(tok.text) {
		case "FROM":
			next := i + 1
			for {
				refs, after, ok := parseRelation(toks, next)
				if !ok {
					break
				}
				for _, r := range refs {
					add(r.Table, r.Alias)
				}
				next = after
				if next < len(toks) && toks[next].text == "," {
					next++
					continue
				}
				break
			}
			i = next - 1
		case "JOIN", "UPDATE", "INTO":
			refs, after, ok := parseRelation(toks, i+1)
			if ok {
				for _, r := range refs {
					add(r.Table, r.Alias)
				}
				i = after - 1
			}
		}
	}
	return out
}

func add2(out *[]relationRef, seen map[string]bool, table, alias string) {
	if table != "" {
		key := strings.ToLower(table)
		if seen[key] {
			return
		}
		seen[key] = true
	}
	*out = append(*out, relationRef{Table: table, Alias: alias})
}

// clauseKeywords 是 FROM/JOIN 列表的终止词：碰到它们说明关系列表结束。
var clauseKeywords = map[string]bool{
	"WHERE": true, "GROUP": true, "ORDER": true, "HAVING": true, "LIMIT": true,
	"OFFSET": true, "UNION": true, "EXCEPT": true, "INTERSECT": true, "ON": true,
	"USING": true, "SET": true, "VALUES": true, "SELECT": true, "RETURNING": true,
	"LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true, "FULL": true,
	"CROSS": true, "NATURAL": true, "JOIN": true, "WITH": true, "WINDOW": true,
	"FOR": true, "INTO": true, "UPDATE": true, "INSERT": true, "DELETE": true,
}

// parseRelation 解析 `[[schema.]table] [AS] [alias]` 或派生表 `( … ) [AS] [alias]`。
// 返回关系引用、下一个待读 token 下标、是否解析成功。
func parseRelation(toks []sqlToken, i int) ([]relationRef, int, bool) {
	for i < len(toks) && toks[i].kind == 'p' && toks[i].text == "," {
		i++
	}
	if i >= len(toks) {
		return nil, i, false
	}
	if toks[i].kind == 'p' && toks[i].text == "(" {
		_, next := parenBody(toks, i)
		alias := ""
		if next < len(toks) && toks[next].kind == 'i' && strings.EqualFold(toks[next].text, "as") {
			next++
		}
		if next < len(toks) && toks[next].kind == 'i' && !clauseKeywords[strings.ToUpper(toks[next].text)] {
			alias = toks[next].text
			next++
		}
		return []relationRef{{Alias: alias}}, next, true
	}
	if toks[i].kind != 'i' || clauseKeywords[strings.ToUpper(toks[i].text)] {
		return nil, i, false
	}
	ref := relationRef{Table: toks[i].text}
	i++
	// schema.table
	for i+1 < len(toks) && toks[i].kind == 'p' && toks[i].text == "." && toks[i+1].kind == 'i' {
		ref.Table = toks[i+1].text
		i += 2
	}
	// [AS] alias
	if i < len(toks) && toks[i].kind == 'i' && strings.EqualFold(toks[i].text, "as") {
		i++
	}
	if i < len(toks) && toks[i].kind == 'i' && !clauseKeywords[strings.ToUpper(toks[i].text)] {
		ref.Alias = toks[i].text
		i++
	}
	return []relationRef{ref}, i, true
}

// parenBody 返回以 toks[i] == "(" 开头的括号体文本（再 token 化回字符串）与闭合后的下标。
// 为了让内层解析复用同一套逻辑，这里把 body 重新拼成 SQL 片段。
func parenBody(toks []sqlToken, i int) (string, int) {
	depth := 0
	start := i + 1
	for ; i < len(toks); i++ {
		if toks[i].kind != 'p' {
			continue
		}
		switch toks[i].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				var b strings.Builder
				for _, t := range toks[start:i] {
					if t.kind == 'i' && b.Len() > 0 {
						last := b.String()[b.Len()-1]
						if isIdentPart(last) {
							b.WriteByte(' ')
						}
					}
					b.WriteString(t.text)
					if t.kind == 'i' {
						b.WriteByte(' ')
					}
				}
				return b.String(), i + 1
			}
		}
	}
	return "", len(toks)
}

// aliasMap 把别名映射到表名（点号分支用）；派生表别名映射到空串（明确不解析）。
func aliasMap(rels []relationRef) map[string]string {
	m := map[string]string{}
	for _, r := range rels {
		if r.Alias != "" {
			m[r.Alias] = r.Table
		}
		if r.Table != "" {
			m[r.Table] = r.Table
		}
	}
	return m
}
