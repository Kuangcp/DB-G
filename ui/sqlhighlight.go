package ui

import (
	"strings"

	myeditor "github.com/egoist/mygo/ui/editor"
)

// sqlExtraKeyword 报告 mygo 内置 SQL 词表里缺少、但编辑器高亮应该识别为关键字的整词。
// 用长度 + EqualFold 避免每 token 分配小写字符串。
func sqlExtraKeyword(s string) bool {
	switch len(s) {
	case 3:
		return strings.EqualFold(s, "use")
	case 4:
		return strings.EqualFold(s, "call") || strings.EqualFold(s, "show") ||
			strings.EqualFold(s, "desc")
	case 5:
		return strings.EqualFold(s, "merge")
	case 8:
		return strings.EqualFold(s, "truncate") || strings.EqualFold(s, "describe")
	}
	return false
}

// sqlKeywordTokenizer 在 mygo SQL 词法结果上补标扩展关键字。
type sqlKeywordTokenizer struct {
	base myeditor.Tokenizer
}

// Tokenize 保留原 tokenizer 的切分与状态，只把缺失的整词改成 TokenKeyword。
func (t sqlKeywordTokenizer) Tokenize(line string, state uint64, tokens []myeditor.Token) ([]myeditor.Token, uint64) {
	out, next := t.base.Tokenize(line, state, tokens)
	start := 0
	for i := range out {
		end := out[i].End
		if end < start || end > len(line) {
			break
		}
		switch out[i].Kind {
		case myeditor.TokenString, myeditor.TokenComment, myeditor.TokenEscape:
			// 字符串/注释里的词不改。
		default:
			if end > start && sqlExtraKeyword(line[start:end]) {
				out[i].Kind = myeditor.TokenKeyword
			}
		}
		start = end
	}
	return out, next
}

func init() {
	lang := myeditor.LanguageNamed("SQL")
	if lang == nil || lang.Tokenizer == nil {
		return
	}
	lang.Tokenizer = sqlKeywordTokenizer{base: lang.Tokenizer}
}
