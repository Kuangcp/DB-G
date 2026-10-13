package highlight

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// runeOf returns the rune offsets of the first occurrence of sub in s.
func runeOf(s, sub string) (int, int) {
	bi := strings.Index(s, sub)
	if bi < 0 {
		return -1, -1
	}
	return utf8.RuneCountInString(s[:bi]), utf8.RuneCountInString(s[:bi]) + utf8.RuneCountInString(sub)
}

func TestTokenizeLine(t *testing.T) {
	line := "SELECT id, '华东大区', 99.99 FROM t -- note"
	runs := TokenizeLine(line)
	selS, selE := runeOf(line, "SELECT")
	strS, strE := runeOf(line, "'华东大区'")
	numS, numE := runeOf(line, "99.99")
	fromS, fromE := runeOf(line, "FROM")
	comS, comE := runeOf(line, "-- note")
	want := []Run{
		{selS, selE, KKeyword},
		{strS, strE, KString},
		{numS, numE, KNumber},
		{fromS, fromE, KKeyword},
		{comS, comE, KComment},
	}
	if len(runs) != len(want) {
		t.Fatalf("got %d runs %+v, want %d", len(runs), runs, len(want))
	}
	for i, w := range want {
		if runs[i] != w {
			t.Errorf("run[%d]=%+v want %+v", i, runs[i], w)
		}
	}
}

func TestEscapedQuote(t *testing.T) {
	line := "SELECT 'it''s' AS a"
	runs := TokenizeLine(line)
	selS, selE := runeOf(line, "SELECT")
	sS, sE := runeOf(line, "'it''s'")
	asS, asE := runeOf(line, "AS")
	want := []Run{
		{selS, selE, KKeyword},
		{sS, sE, KString},
		{asS, asE, KKeyword},
	}
	if len(runs) != len(want) {
		t.Fatalf("got %+v want %+v", runs, want)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Errorf("run[%d]=%+v want %+v", i, runs[i], want[i])
		}
	}
}

func TestKeywordNotStringInside(t *testing.T) {
	// keyword list words inside a string must stay string, and a keyword
	// right after a number boundary must still be found.
	line := "WHERE 'SELECT' AND 42 FROM x"
	runs := TokenizeLine(line)
	if len(runs) != 5 {
		t.Fatalf("got %+v", runs)
	}
	if runs[1].Kind != KString {
		t.Errorf("runs[1]=%+v want string", runs[1])
	}
	if runs[2].Kind != KKeyword { // AND
		t.Errorf("runs[2]=%+v want keyword", runs[2])
	}
	if runs[3].Kind != KNumber {
		t.Errorf("runs[3]=%+v want number", runs[3])
	}
	if runs[4].Kind != KKeyword { // FROM
		t.Errorf("runs[4]=%+v want keyword", runs[4])
	}
}