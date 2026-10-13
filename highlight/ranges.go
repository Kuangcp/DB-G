package highlight

import (
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// LineStarts returns the byte and rune offset of each line's first
// character. A trailing '\n' does not add an empty final line.
func LineStarts(s string) (byteStarts, runeStarts []int) {
	byteStarts = []int{0}
	runeStarts = []int{0}
	b, ri := 0, 0
	for b < len(s) {
		r, sz := utf8.DecodeRuneInString(s[b:])
		if r == '\n' {
			byteStarts = append(byteStarts, b+sz)
			runeStarts = append(runeStarts, ri+1)
		}
		b += sz
		ri++
	}
	return byteStarts, runeStarts
}

// Tokenize lexes lines [lo,hi) and returns per-line runs indexed by
// absolute line number; lines outside the window are nil.
func Tokenize(s string, byteStarts []int, lo, hi int) [][]Run {
	n := len(byteStarts)
	if lo < 0 {
		lo = 0
	}
	if hi > n {
		hi = n
	}
	if lo >= hi {
		return nil
	}
	out := make([][]Run, n)
	for p := lo; p < hi; p++ {
		start := byteStarts[p]
		end := len(s)
		if p+1 < n {
			end = byteStarts[p+1]
		}
		if end > start && s[end-1] == '\n' {
			end--
		}
		out[p] = TokenizeLine(s[start:end])
	}
	return out
}

// Ranges merges per-line runs (indexed by absolute line number, window
// [lo,hi)) into absolute rune-offset ui.TextRange, merging adjacent runs
// of the same color within a line and dropping KDefault.
func Ranges(runsPerLine [][]Run, runeStarts []int, lo, hi int) []ui.TextRange {
	if lo < 0 {
		lo = 0
	}
	if hi > len(runeStarts) {
		hi = len(runeStarts)
	}
	var out []ui.TextRange
	for p := lo; p < hi; p++ {
		base := runeStarts[p]
		for _, r := range runsPerLine[p] {
			if r.Kind == KDefault {
				continue
			}
			c := r.Kind.Color()
			if k := len(out); k > 0 && out[k-1].End == base+r.Start && out[k-1].Color == c {
				out[k-1].End = base + r.End
				continue
			}
			out = append(out, ui.TextRange{Start: base + r.Start, End: base + r.End, Color: c})
		}
	}
	return out
}