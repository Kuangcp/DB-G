package sql

import "unicode/utf8"

// LexKind 是词法单元类型。
type LexKind int

const (
	LexSpace LexKind = iota
	LexComment
	LexString
	LexWord
	LexNumber
	LexPunct
)

// LexToken 是带字节范围的词法单元；Text 对字符串/注释包含分隔符。
type LexToken struct {
	Kind       LexKind
	Text       string
	Start, End int
}

// Lex 把 SQL 切成空白/注释/字符串/标识符/数字/标点；字符串与注释整体保留。
func Lex(s string) []LexToken {
	var out []LexToken
	for i := 0; i < len(s); {
		start := i
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f':
			for i < len(s) && isLexSpace(s[i]) {
				i++
			}
			out = append(out, LexToken{Kind: LexSpace, Text: s[start:i], Start: start, End: i})
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			out = append(out, LexToken{Kind: LexComment, Text: s[start:i], Start: start, End: i})
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i < len(s) && !(s[i] == '*' && i+1 < len(s) && s[i+1] == '/') {
				i++
			}
			if i < len(s) {
				i += 2
			}
			out = append(out, LexToken{Kind: LexComment, Text: s[start:i], Start: start, End: i})
		case c == '\'' || c == '"' || c == '`':
			i = SkipQuoted(s, i, c)
			out = append(out, LexToken{Kind: LexString, Text: s[start:i], Start: start, End: i})
		case IsWordStart(c):
			for i < len(s) && IsWordPart(s[i]) {
				i++
			}
			out = append(out, LexToken{Kind: LexWord, Text: s[start:i], Start: start, End: i})
		case c >= '0' && c <= '9':
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
				i++
			}
			out = append(out, LexToken{Kind: LexNumber, Text: s[start:i], Start: start, End: i})
		default:
			i++
			out = append(out, LexToken{Kind: LexPunct, Text: s[start:i], Start: start, End: i})
		}
	}
	return out
}

// LexCode 返回用于语法判断的非空白/非注释/非字符串 token。
func LexCode(s string) []LexToken {
	all := Lex(s)
	out := make([]LexToken, 0, len(all))
	for _, tok := range all {
		switch tok.Kind {
		case LexWord, LexNumber, LexPunct:
			out = append(out, tok)
		}
	}
	return out
}

// HasCode 报告片段里是否有非注释、非空白的代码（字符串算代码）。
func HasCode(s string) bool {
	for _, tok := range Lex(s) {
		if tok.Kind != LexSpace && tok.Kind != LexComment {
			return true
		}
	}
	return false
}

func SkipQuoted(s string, i int, quote byte) int {
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

func isLexSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '\v' || b == '\f'
}

func IsWordStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= utf8.RuneSelf
}

func IsWordPart(b byte) bool {
	return IsWordStart(b) || (b >= '0' && b <= '9') || b == '$'
}
