package main

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

var findColor = ui.RGB(255, 160, 200)

// FindAll returns non-overlapping rune-offset ui.TextRange of every
// occurrence of q in s (case-sensitive). Empty query or no match: nil.
func FindAll(s, q string) []ui.TextRange {
	if q == "" {
		return nil
	}
	var out []ui.TextRange
	b, ri := 0, 0
	for {
		bi := strings.Index(s[b:], q)
		if bi < 0 {
			return out
		}
		absB := b + bi
		start := ri + utf8.RuneCountInString(s[b:absB])
		end := start + utf8.RuneCountInString(q)
		out = append(out, ui.TextRange{Start: start, End: end, Color: findColor})
		b = absB + len(q)
		ri = end
	}
}