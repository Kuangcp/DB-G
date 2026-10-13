package highlight

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestLineStarts(t *testing.T) {
	s := "ab\nc\ndef\n"
	bs, rs := LineStarts(s)
	want := []int{0, 3, 5, 9}
	for i := range want {
		if bs[i] != want[i] || rs[i] != want[i] {
			t.Fatalf("i=%d bs=%v rs=%v want %v", i, bs, rs, want)
		}
	}
	if len(bs) != len(want) {
		t.Fatalf("line count %d want %d", len(bs), len(want))
	}

	// non-ASCII: 选(3B/1R) 择(3B/1R) \n(1B) → next line at byte 7, rune 3
	s2 := "选择\nx"
	bs2, rs2 := LineStarts(s2)
	if bs2[1] != 7 || rs2[1] != 3 {
		t.Fatalf("line1 byte=%d rune=%d want 7/3", bs2[1], rs2[1])
	}
}

func TestTokenizeRange(t *testing.T) {
	s := "SELECT a FROM t\nWHERE x = 42\n-- c"
	bs, _ := LineStarts(s)
	// lex only lines 1..2 (0-based)
	runs := Tokenize(s, bs, 1, 3)
	if len(runs) != 3 {
		t.Fatalf("runs len=%d want 3", len(runs))
	}
	if runs[0] != nil {
		t.Fatalf("line0 should be nil, got %+v", runs[0])
	}
	if len(runs[1]) != 2 || runs[1][0].Kind != KKeyword || runs[1][1].Kind != KNumber {
		t.Fatalf("line1=%+v", runs[1])
	}
	if len(runs[2]) != 1 || runs[2][0].Kind != KComment {
		t.Fatalf("line2=%+v", runs[2])
	}
}

func TestRangesOffsets(t *testing.T) {
	s := "SELECT FROM\nx='它'\n"
	bs, rs := LineStarts(s)
	runs := Tokenize(s, bs, 0, 2)
	toks := Ranges(runs, rs, 0, 2)
	// line0: SELECT(0..6) FROM(7..11); line1 base rune 12: string '它' 14..17
	want := []ui.TextRange{
		{Start: 0, End: 6, Color: KKeyword.Color()},
		{Start: 7, End: 11, Color: KKeyword.Color()},
		{Start: 14, End: 17, Color: KString.Color()},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %+v want %+v", toks, want)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Errorf("toks[%d]=%+v want %+v", i, toks[i], want[i])
		}
	}
}

func TestRangesMergeSynthetic(t *testing.T) {
	// adjacent same-color runs within a line must merge.
	runs := [][]Run{
		{{Start: 0, End: 3, Kind: KKeyword}, {Start: 3, End: 6, Kind: KKeyword}},
		{{Start: 10, End: 12, Kind: KString}},
	}
	runeStarts := []int{0, 20}
	toks := Ranges(runs, runeStarts, 0, 2)
	want := []ui.TextRange{
		{Start: 0, End: 6, Color: KKeyword.Color()},
		{Start: 30, End: 32, Color: KString.Color()},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %+v want %+v", toks, want)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Errorf("toks[%d]=%+v want %+v", i, toks[i], want[i])
		}
	}
}