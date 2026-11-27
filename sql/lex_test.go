package sql

import "testing"

func TestLexTokensAndSpans(t *testing.T) {
	text := "select ';' -- c\nfrom t"
	toks := Lex(text)
	if len(toks) == 0 {
		t.Fatal("no tokens")
	}
	var words, strings, comments int
	for _, tok := range toks {
		if tok.Start < 0 || tok.End > len(text) || tok.Start > tok.End {
			t.Fatalf("bad span %+v for %q", tok, text)
		}
		if tok.Text != text[tok.Start:tok.End] && tok.Kind != LexString && tok.Kind != LexComment {
			t.Fatalf("span text mismatch %+v", tok)
		}
		switch tok.Kind {
		case LexWord:
			words++
		case LexString:
			strings++
		case LexComment:
			comments++
		}
	}
	if words != 3 || strings != 1 || comments != 1 {
		t.Fatalf("words=%d strings=%d comments=%d", words, strings, comments)
	}
	if HasCode(" -- only comment\n  ") {
		t.Fatal("comment-only must be no code")
	}
	if !HasCode("select 1") {
		t.Fatal("select must have code")
	}
	if got := LexCode("select  1"); len(got) != 2 || got[0].Text != "select" || got[1].Text != "1" {
		t.Fatalf("code tokens = %+v", got)
	}
}
