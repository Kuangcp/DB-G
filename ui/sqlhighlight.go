package ui

import (
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/sql"
)

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
			if end > start && sql.IsExtraKeyword(line[start:end]) {
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
