// Package highlight provides a line-scoped SQL tokenizer producing
// rune-offset runs, and range builders on top of it for mygo's TextRanges.
package highlight

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

type Kind uint8

const (
	KDefault Kind = iota
	KKeyword
	KString
	KComment
	KNumber
)

func (k Kind) Color() ui.Color {
	switch k {
	case KKeyword:
		return ui.RGB(132, 140, 255)
	case KString:
		return ui.RGB(178, 145, 90)
	case KComment:
		return ui.RGB(112, 135, 115)
	case KNumber:
		return ui.RGB(130, 178, 210)
	default:
		return ui.RGB(210, 210, 210)
	}
}

// Run is a token span within a single line, in rune offsets.
type Run struct {
	Start, End int
	Kind       Kind
}

func isIdentStart(r rune) bool { return unicode.IsLetter(r) || r == '_' }
func isIdentPart(r rune) bool  { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' }
func isNumPart(r rune) bool    { return (r >= '0' && r <= '9') || r == '.' }

func kwWord(ident string) bool {
	switch ident {
	case "SELECT", "FROM", "WHERE", "INSERT", "INTO", "VALUES", "ORDER",
		"BY", "UPDATE", "DELETE", "CREATE", "TABLE", "DROP", "ALTER",
		"ADD", "JOIN", "LEFT", "RIGHT", "FULL", "INNER", "OUTER", "ON",
		"AS", "AND", "OR", "NOT", "NULL", "IS", "GROUP", "HAVING",
		"LIMIT", "OFFSET", "SET", "IN", "DISTINCT", "COUNT", "SUM",
		"AVG", "MIN", "MAX", "CASE", "WHEN", "THEN", "ELSE", "END",
		"UNION", "ALL", "ASC", "DESC", "PRIMARY", "KEY", "FOREIGN",
		"REFERENCES", "DEFAULT", "INDEX", "EXISTS", "LIKE", "BETWEEN",
		"CAST", "COALESCE":
		return true
	}
	return false
}

// TokenizeLine scans one SQL line, returning runs of KKeyword / KString /
// KComment / KNumber in rune offsets. Plain identifiers and operators are
// not emitted. It handles '...' strings with '' escapes, and -- comments.
func TokenizeLine(s string) []Run {
	var runs []Run
	i, ri := 0, 0 // byte and rune cursors at the current rune
	peek := func() rune {
		if i >= len(s) {
			return 0
		}
		r, _ := utf8.DecodeRuneInString(s[i:])
		return r
	}
	advance := func() {
		_, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
		ri++
	}
	tok := -1 // start rune of the current token; -1 = none
	var tokK Kind
	flush := func() {
		if tok >= 0 {
			runs = append(runs, Run{Start: tok, End: ri, Kind: tokK})
			tok = -1
		}
	}
	begin := func(k Kind) {
		tok, tokK = ri, k
	}

	for i < len(s) {
		r := peek()
		switch {
		case r == '-' && i+1 < len(s) && s[i+1] == '-':
			flush()
			begin(KComment)
			for i < len(s) {
				advance()
			}
			flush()
		case r == '\'':
			flush()
			begin(KString)
			advance()
			for i < len(s) {
				r2 := peek()
				advance()
				if r2 == '\'' {
					if i < len(s) && peek() == '\'' {
						advance() // '' escape
						continue
					}
					break
				}
			}
			flush()
		case isIdentStart(r):
			flush()
			begin(KDefault)
			byteStart := i
			for i < len(s) && isIdentPart(peek()) {
				advance()
			}
			if kwWord(strings.ToUpper(s[byteStart:i])) {
				tokK = KKeyword
			} else {
				tok = -1 // plain identifier: not emitted
			}
			flush()
		case isNumPart(r):
			flush()
			begin(KNumber)
			for i < len(s) && isNumPart(peek()) {
				advance()
			}
			flush()
		default:
			flush()
			advance()
		}
	}
	return runs
}