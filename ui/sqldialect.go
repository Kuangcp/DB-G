package ui

import (
	"strings"
	"sync"

	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/sql"
)

// completionTokenKind 把补全词类型映射成高亮 token 类型。
func completionTokenKind(k sql.WordKind) myeditor.TokenKind {
	switch k {
	case sql.WordKeyword:
		return myeditor.TokenKeyword
	case sql.WordType:
		return myeditor.TokenType
	case sql.WordFunction:
		return myeditor.TokenFunction
	}
	return myeditor.TokenVariable
}

var (
	sqlLangMu    sync.Mutex
	sqlLangCache = map[string]*myeditor.Language{}
)

// sqlLanguageForKind 返回 SQL 语言：在 mygo 的通用词表之上把方言词重新标成
// 关键字/类型/函数，从而得到对应数据源的高亮。
func sqlLanguageForKind(kind string) *myeditor.Language {
	base := myeditor.LanguageNamed("SQL")
	terms := sql.DialectTermMap(kind)
	if base == nil || len(terms) == 0 {
		return base
	}
	sqlLangMu.Lock()
	defer sqlLangMu.Unlock()
	if cached, ok := sqlLangCache[kind]; ok {
		return cached
	}
	clone := *base
	inner := base.Tokenizer
	if inner == nil {
		sqlLangCache[kind] = &clone
		return &clone
	}
	clone.Tokenizer = myeditor.TokenizerFunc(func(line string, state uint64, tokens []myeditor.Token) ([]myeditor.Token, uint64) {
		start := len(tokens)
		out, st := inner.Tokenize(line, state, tokens)
		if start != 0 {
			return out, st
		}
		prev := 0
		for i := range out {
			word := line[prev:out[i].End]
			prev = out[i].End
			if out[i].Kind != myeditor.TokenVariable {
				continue
			}
			if k, ok := terms[strings.ToLower(word)]; ok {
				out[i].Kind = completionTokenKind(k)
			}
		}
		return out, st
	})
	sqlLangCache[kind] = &clone
	return &clone
}
