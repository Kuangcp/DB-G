package sql

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Relation 是语句里出现的一张表；Alias 非空表示 FROM/JOIN 给了别名。
// Table 为空、Alias 非空表示派生表（FROM (SELECT …) d）：不提供列，只挡住目录回退。
type Relation struct {
	Table  string
	Schema string // 限定名里的 schema（"" = 未限定）
	Alias  string
}

// Token 是词法扫描的最小单位：标识符（去掉引号包裹）、标点、或需要忽略的字面量。
type Token struct {
	Kind byte // 'i' ident, 'p' punct
	Text string
}

// Tokenize 做补全所需的词法近似：标识符、标点；字符串与注释整体跳过。
// 带引号的标识符（"x" / `x`）按标识符返回并去掉包裹；数字不参与补全。
func Tokenize(s string) []Token {
	var out []Token
	for _, tok := range Lex(s) {
		switch tok.Kind {
		case LexWord:
			out = append(out, Token{Kind: 'i', Text: tok.Text})
		case LexString:
			if tok.Text == "" {
				continue
			}
			switch tok.Text[0] {
			case '"', '`':
				if len(tok.Text) < 2 {
					continue
				}
				if inner := tok.Text[1 : len(tok.Text)-1]; inner != "" {
					out = append(out, Token{Kind: 'i', Text: inner})
				}
			}
		case LexPunct:
			out = append(out, Token{Kind: 'p', Text: tok.Text})
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

// sqlScopeWindow 是作用域扫描的窗口半宽：补全在每次按键都会跑，不能全文扫描
// （11 MB 的 100k 行控制台全文扫一次约 8 ms）。超过窗口的单条语句会退化为
// 「只看窗口内的片段」，属于可接受的上限。
const sqlScopeWindow = 64 << 10

// StatementScopeOnly 返回光标所在的作用域文本：最内层包住光标的括号块（子查询），
// 没有括号时是光标所在的整条语句（按 ; 切，跳过字符串/注释）。offset 为字节偏移。
func StatementScopeOnly(text string, offset int) string {
	scope, _ := StatementScope(text, offset)
	return scope
}

// StatementScope 同 StatementScopeOnly，额外返回光标在返回作用域文本里的字节偏移。
func StatementScope(text string, offset int) (string, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	// 只取光标附近的一段：先按字节窗口，再对齐到 UTF-8 边界。
	if lo, hi := offset-sqlScopeWindow, offset+sqlScopeWindow; lo > 0 || hi < len(text) {
		if lo < 0 {
			lo = 0
		}
		if hi > len(text) {
			hi = len(text)
		}
		for lo > 0 && !utf8.RuneStart(text[lo]) {
			lo--
		}
		for hi < len(text) && !utf8.RuneStart(text[hi]) {
			hi++
		}
		return scopeInSegment(text[lo:hi], offset-lo)
	}
	return scopeInSegment(text, offset)
}

// trimScope 去掉作用域两端空白，并把光标偏移同步平移到裁剪后的文本。
func trimScope(scope string, offset int) (string, int) {
	left := len(scope) - len(strings.TrimLeft(scope, " \t\r\n"))
	right := len(scope) - len(strings.TrimRight(scope, " \t\r\n"))
	if left+right >= len(scope) {
		return "", 0
	}
	trimmed := scope[left : len(scope)-right]
	offset -= left
	if offset < 0 {
		offset = 0
	}
	if offset > len(trimmed) {
		offset = len(trimmed)
	}
	return trimmed, offset
}

// scopeInSegment 在给定片段内做语句/括号作用域分析（offset 相对片段起点），
// 返回作用域文本与光标在其中的偏移。
// 只有**子查询**括号（内容以 SELECT / WITH 开头）才切出独立作用域；函数调用、
// 表达式分组、IN (…) 列表等括号不改变可见的表，光标在 sum( 里仍按整条语句
// 解析 FROM/JOIN，从而能补出列名。
func scopeInSegment(text string, offset int) (string, int) {
	start, end := statementBounds(text, offset)
	end = softStatementEnd(text, start, offset, end)
	type span struct{ open, close int }
	var spans []span // 内层先闭合，先记录
	var stack []int
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
			stack = append(stack, i)
		case ')':
			if n := len(stack); n > 0 {
				open := stack[n-1]
				stack = stack[:n-1]
				if open < offset && offset <= i {
					spans = append(spans, span{open: open, close: i})
				}
			}
		}
		i++
	}
	// 未闭合的括号（边输入边补全时常见）：光标在里面的也算候选，闭合位置取语句尾。
	var blankAt []int
	for _, open := range stack {
		if open < offset {
			spans = append(spans, span{open: open, close: end})
		}
	}
	// 按左括号位置从大到小 = 从内到外。
	sort.Slice(spans, func(i, j int) bool { return spans[i].open > spans[j].open })
	for _, sp := range spans {
		body := text[sp.open+1 : sp.close]
		if isSubqueryBody(body) {
			return trimScope(body, offset-(sp.open+1))
		}
		if sp.close == end {
			blankAt = append(blankAt, sp.open)
		}
	}
	// 没有包住光标的子查询：返回整条语句，由 relationsIn 按括号深度只取本层
	// 关系；未闭合的函数/表达式括号抹掉，让 FROM 回到本层可见。
	scope := text[start:end]
	if len(blankAt) > 0 {
		b := []byte(scope)
		for _, at := range blankAt {
			b[at-start] = ' '
		}
		scope = string(b)
	}
	return trimScope(scope, offset-start)
}

// isSubqueryBody 报告括号内容是否是子查询（首个词是 SELECT / WITH）。
func isSubqueryBody(body string) bool {
	for _, tok := range Tokenize(body) {
		if tok.Kind != 'i' {
			continue
		}
		return strings.EqualFold(tok.Text, "select") || strings.EqualFold(tok.Text, "with")
	}
	return false
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

// softStatementOpeners 是可以在行首开启一条新语句的关键字。
var softStatementOpeners = map[string]bool{
	"SELECT": true, "WITH": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"CREATE": true, "ALTER": true, "DROP": true, "REPLACE": true, "TRUNCATE": true,
	"BEGIN": true, "COMMIT": true, "ROLLBACK": true, "PRAGMA": true, "EXPLAIN": true,
	"ANALYZE": true, "VACUUM": true, "DESCRIBE": true, "DESC": true, "CALL": true,
	"GRANT": true, "REVOKE": true, "SHOW": true, "SET": true, "USE": true,
}

// softPrevGuards 是紧挨着新语句关键字时不能切分的前一个 token：
// UNION SELECT、INSERT … SELECT（由 first guards 兜）、CREATE VIEW AS SELECT 等。
var softPrevGuards = map[string]bool{
	"UNION": true, "INTERSECT": true, "EXCEPT": true, "ALL": true, "AS": true,
	"ON": true, "USING": true, "SET": true, "VALUES": true, "RETURNING": true,
	"THEN": true, "ELSE": true, "WHEN": true, "(": true, ",": true,
}

// softFirstGuards 是整条语句以这些词开头时不做软切分：
// CTE 主查询、INSERT … SELECT、CREATE … AS SELECT、EXPLAIN SELECT。
var softFirstGuards = map[string]bool{
	"WITH": true, "INSERT": true, "REPLACE": true, "CREATE": true, "EXPLAIN": true,
	"ANALYZE": true, "DESCRIBE": true, "DESC": true,
}

// softStatementEnd 在硬分号边界 hardEnd 之前，找光标之后行首、顶层的下一条语句起点。
// 只服务补全作用域：执行切分仍然只认分号。
func softStatementEnd(text string, start, offset, hardEnd int) int {
	if offset < start {
		offset = start
	}
	if offset > hardEnd {
		offset = hardEnd
	}
	depth := 0
	lineStart := true
	seen := false
	first, prev := "", ""
	for i := start; i < hardEnd; {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if c == '\n' {
			lineStart = true
			i++
			continue
		}
		// 行注释
		if c == '-' && i+1 < hardEnd && text[i+1] == '-' {
			for i < hardEnd && text[i] != '\n' {
				i++
			}
			continue
		}
		// 块注释：含换行时下一 token 也算行首。
		if c == '/' && i+1 < hardEnd && text[i+1] == '*' {
			i += 2
			newline := false
			for i < hardEnd && !(text[i] == '*' && i+1 < hardEnd && text[i+1] == '/') {
				if text[i] == '\n' {
					newline = true
				}
				i++
			}
			if i < hardEnd {
				i += 2
			}
			if newline {
				lineStart = true
			}
			continue
		}
		// 字符串/引号标识符：整体跳过，不算语句开头。
		if c == '\'' || c == '"' || c == '`' {
			i = skipQuoted(text, i, c)
			seen = true
			lineStart = false
			prev = string(c)
			continue
		}
		if IsIdentStart(c) {
			tokStart := i
			for i < hardEnd && IsIdentPart(text[i]) {
				i++
			}
			up := strings.ToUpper(text[tokStart:i])
			if i >= offset && lineStart && depth == 0 && seen &&
				softStatementOpeners[up] && !softPrevGuards[prev] && !softFirstGuards[first] {
				return tokStart
			}
			if first == "" {
				first = up
			}
			prev = up
			seen = true
			lineStart = false
			continue
		}
		if c >= '0' && c <= '9' {
			for i < hardEnd && (text[i] >= '0' && text[i] <= '9' || text[i] == '.') {
				i++
			}
			prev = "#"
			seen = true
			lineStart = false
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		prev = string(c)
		seen = true
		lineStart = false
		i++
	}
	return hardEnd
}

// RelationPositionAt 报告 scope 的 offset 处是否正在等待表/视图名：
// FROM/JOIN 列表、UPDATE 目标、INSERT INTO / TABLE 目标。
// 输入没写分号时多条语句可能被并进同一作用域，这里只用到光标之前的子句状态。
func RelationPositionAt(scope string, offset int) bool {
	if offset < 0 {
		offset = 0
	}
	if offset > len(scope) {
		offset = len(scope)
	}
	// 正在输入的词还没完成：截到词首，把它当成不存在。
	word := offset
	for word > 0 && IsIdentPart(scope[word-1]) {
		word--
	}
	toks := Tokenize(scope[:word])
	const (
		clauseNone = iota
		clauseSelect
		clauseFrom
		clauseJoin
		clauseUpdate
		clauseInto
		clauseOther
	)
	clause := clauseNone
	expect, after, qualified, derived := false, false, false, false
	depth := 0
	for _, tok := range toks {
		if tok.Kind == 'p' {
			switch tok.Text {
			case "(":
				if depth == 0 && expect {
					expect, after, derived = false, false, true
				}
				depth++
			case ")":
				if depth > 0 {
					depth--
					if depth == 0 && derived {
						after, derived = true, false
					}
				}
			case ",":
				if depth == 0 && clause == clauseFrom && !expect {
					expect, after = true, false
				}
			case ".":
				if depth == 0 && after {
					qualified, after = true, false
				}
			}
			continue
		}
		if depth > 0 {
			continue
		}
		up := strings.ToUpper(tok.Text)
		if expect {
			if up == "ONLY" || up == "LATERAL" {
				continue
			}
			if clauseKeywords[up] {
				expect, after, clause = false, false, clauseOther
				continue
			}
			expect, after, qualified = false, true, false
			continue
		}
		if qualified {
			qualified, after = false, true
			continue
		}
		switch up {
		case "FROM":
			clause, expect, after = clauseFrom, true, false
		case "JOIN":
			clause, expect, after = clauseJoin, true, false
		case "UPDATE":
			clause, expect, after = clauseUpdate, true, false
		case "INTO":
			clause, expect, after = clauseInto, true, false
		case "TABLE":
			clause, expect, after = clauseOther, true, false
		case "SELECT":
			clause, expect, after = clauseSelect, false, false
		case "AS":
			after = false
		default:
			if clauseKeywords[up] {
				clause, expect, after = clauseOther, false, false
			} else if after {
				after = false
			}
		}
	}
	return expect || qualified
}

// relationsIn 从作用域文本里提取表：FROM 列表、JOIN、UPDATE、INTO；
// CTE（WITH 名 AS (…)) 展开为其内部 FROM 的表（递归，深度上限 8，环保护）；
// 派生表（FROM (SELECT …) s）登记为 Table 空、Alias 非空的占位。
func Relations(scope string) []Relation {
	return relationsInDepth(scope, nil, 0)
}

func relationsInDepth(scope string, inherited map[string][]Relation, depth int) []Relation {
	if depth > 8 {
		return nil
	}
	toks := Tokenize(scope)
	cte := map[string][]Relation{}
	for k, v := range inherited {
		cte[k] = v
	}

	i := 0
	// CTE 头：WITH [RECURSIVE] name AS ( … ) [, name AS ( … )]*
	if i < len(toks) && toks[i].Kind == 'i' && strings.EqualFold(toks[i].Text, "with") {
		i++
		if i < len(toks) && toks[i].Kind == 'i' && strings.EqualFold(toks[i].Text, "recursive") {
			i++
		}
		for i < len(toks) {
			name := toks[i].Text
			if i+1 >= len(toks) || !strings.EqualFold(toks[i+1].Text, "as") {
				break
			}
			open := i + 2
			if open >= len(toks) || toks[open].Text != "(" {
				break
			}
			body, next := parenBody(toks, open)
			cte[strings.ToLower(name)] = relationsInDepth(body, cte, depth+1)
			i = next
			if i < len(toks) && toks[i].Text == "," {
				i++
				continue
			}
			break
		}
	}

	var out []Relation
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
	nest := 0
	for ; i < len(toks); i++ {
		tok := toks[i]
		if tok.Kind == 'p' {
			switch tok.Text {
			case "(":
				nest++
			case ")":
				if nest > 0 {
					nest--
				}
			}
			continue
		}
		if nest != 0 {
			continue // 子查询内部不参与本层作用域
		}
		switch strings.ToUpper(tok.Text) {
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
				if next < len(toks) && toks[next].Text == "," {
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

func add2(out *[]Relation, seen map[string]bool, table, alias string) {
	if table != "" {
		key := strings.ToLower(table)
		if seen[key] {
			return
		}
		seen[key] = true
	}
	*out = append(*out, Relation{Table: table, Alias: alias})
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
func parseRelation(toks []Token, i int) ([]Relation, int, bool) {
	for i < len(toks) && toks[i].Kind == 'p' && toks[i].Text == "," {
		i++
	}
	if i >= len(toks) {
		return nil, i, false
	}
	if toks[i].Kind == 'p' && toks[i].Text == "(" {
		_, next := parenBody(toks, i)
		alias := ""
		if next < len(toks) && toks[next].Kind == 'i' && strings.EqualFold(toks[next].Text, "as") {
			next++
		}
		if next < len(toks) && toks[next].Kind == 'i' && !clauseKeywords[strings.ToUpper(toks[next].Text)] {
			alias = toks[next].Text
			next++
		}
		return []Relation{{Alias: alias}}, next, true
	}
	if toks[i].Kind != 'i' || clauseKeywords[strings.ToUpper(toks[i].Text)] {
		return nil, i, false
	}
	ref := Relation{Table: toks[i].Text}
	i++
	// schema.table
	for i+1 < len(toks) && toks[i].Kind == 'p' && toks[i].Text == "." && toks[i+1].Kind == 'i' {
		ref.Schema = ref.Table
		ref.Table = toks[i+1].Text
		i += 2
	}
	// [AS] alias
	if i < len(toks) && toks[i].Kind == 'i' && strings.EqualFold(toks[i].Text, "as") {
		i++
	}
	if i < len(toks) && toks[i].Kind == 'i' && !clauseKeywords[strings.ToUpper(toks[i].Text)] {
		ref.Alias = toks[i].Text
		i++
	}
	return []Relation{ref}, i, true
}

// parenBody 返回以 toks[i] == "(" 开头的括号体文本（再 token 化回字符串）与闭合后的下标。
// 为了让内层解析复用同一套逻辑，这里把 body 重新拼成 SQL 片段。
func parenBody(toks []Token, i int) (string, int) {
	depth := 0
	start := i + 1
	for ; i < len(toks); i++ {
		if toks[i].Kind != 'p' {
			continue
		}
		switch toks[i].Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				var b strings.Builder
				for _, t := range toks[start:i] {
					if t.Kind == 'i' && b.Len() > 0 {
						last := b.String()[b.Len()-1]
						if IsIdentPart(last) {
							b.WriteByte(' ')
						}
					}
					b.WriteString(t.Text)
					if t.Kind == 'i' {
						b.WriteByte(' ')
					}
				}
				return b.String(), i + 1
			}
		}
	}
	return "", len(toks)
}

// AliasMap 把别名映射到表名（点号分支用）；派生表别名映射到空串（明确不解析）。
func AliasMap(rels []Relation) map[string]string {
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

// CTENames 收集 WITH ... AS 的 CTE 名，避免把 CTE 引用当成缺失表。
func CTENames(toks []Token) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < len(toks); i++ {
		if toks[i].Kind == 'i' && strings.EqualFold(toks[i].Text, "with") {
			for i+1 < len(toks) {
				if toks[i+1].Kind != 'i' {
					break
				}
				out[strings.ToLower(toks[i+1].Text)] = true
				j := i + 2
				for j+1 < len(toks) && !(strings.EqualFold(toks[j].Text, "as") && toks[j+1].Text == "(") {
					j++
				}
				if j+1 >= len(toks) {
					break
				}
				_, next := parenBody(toks, j+1)
				if next >= len(toks) || toks[next].Text != "," {
					break
				}
				i = next - 1
			}
		}
	}
	return out
}
