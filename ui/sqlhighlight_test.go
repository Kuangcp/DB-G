package ui

import (
	"strings"
	"testing"

	myeditor "github.com/egoist/mygo/ui/editor"
)

func TestSQLHighlightRecognizesExtraKeywords(t *testing.T) {
	lang := myeditor.LanguageNamed("SQL")
	if lang == nil || lang.Tokenizer == nil {
		t.Fatal("SQL language missing")
	}
	line := "TRUNCATE TABLE t"
	toks, _ := lang.Tokenizer.Tokenize(line, 0, nil)
	found := false
	start := 0
	for _, tok := range toks {
		if tok.End <= len(line) && strings.EqualFold(strings.TrimSpace(line[start:tok.End]), "TRUNCATE") {
			found = tok.Kind == myeditor.TokenKeyword
			break
		}
		start = tok.End
	}
	if !found {
		t.Fatalf("TRUNCATE must be highlighted as a keyword: %+v", toks)
	}
}

func TestSQLHighlightIgnoresKeywordsInStrings(t *testing.T) {
	lang := myeditor.LanguageNamed("SQL")
	line := "select 'truncate'"
	toks, _ := lang.Tokenizer.Tokenize(line, 0, nil)
	start := 0
	for _, tok := range toks {
		if tok.End <= len(line) && strings.Contains(strings.ToLower(line[start:tok.End]), "truncate") {
			if tok.Kind == myeditor.TokenKeyword {
				t.Fatalf("keyword inside string must stay string: %+v", toks)
			}
		}
		start = tok.End
	}
}
